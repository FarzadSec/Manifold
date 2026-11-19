package tunnel

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// The UDP half of the tunnel test measures what WireGuard, L2TP and OpenVPN over UDP
// get from a tunnel. Datagrams start with a header; requests carry a MAC after it.
const (
	udpMagic    = "MFU1"
	udpHead     = 21
	udpMACLen   = 8
	udpDatagram = 1200
	udpMaxMbps  = 4000
	udpMaxDown  = 30 * time.Second
	udpDowns    = 4
	udpMinStep  = 250 * time.Millisecond
)

const (
	udpOpEcho     byte = 'e'
	udpOpBegin    byte = 'b'
	udpOpData     byte = 'd'
	udpOpReport   byte = 'r'
	udpOpDown     byte = 'D'
	udpOpDownData byte = 'x'
)

func udpMAC(key, head []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(head)
	return m.Sum(nil)[:udpMACLen]
}

func udpPacket(op byte, flow, seq uint32, size int) []byte {
	b := make([]byte, max(size, udpHead))
	copy(b, udpMagic)
	b[4] = op
	binary.BigEndian.PutUint32(b[5:], flow)
	binary.BigEndian.PutUint32(b[9:], seq)
	binary.BigEndian.PutUint64(b[13:], uint64(time.Now().UnixNano()))
	return b
}

// udpRequest is a header with its MAC and an optional body.
func udpRequest(key []byte, op byte, flow, seq uint32, body []byte) []byte {
	b := udpPacket(op, flow, seq, udpHead)
	b = append(b, udpMAC(key, b[:udpHead])...)
	return append(b, body...)
}

func udpFields(b []byte) (op byte, flow, seq uint32, ok bool) {
	if len(b) < udpHead || string(b[:4]) != udpMagic {
		return 0, 0, 0, false
	}
	return b[4], binary.BigEndian.Uint32(b[5:]), binary.BigEndian.Uint32(b[9:]), true
}

func randomFlow() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return binary.BigEndian.Uint32(b[:]) | 1
}

// udpEndpoint is one wildcard socket that learns which address each datagram reached
// and answers from it, so a reply leaves through the tunnel the request came in by.
type udpEndpoint interface {
	read(buf []byte) (int, netip.Addr, net.Addr, error)
	write(b []byte, src netip.Addr, to net.Addr) error
	close() error
}

type udp4Endpoint struct{ c *ipv4.PacketConn }

func (u udp4Endpoint) read(buf []byte) (int, netip.Addr, net.Addr, error) {
	n, cm, from, err := u.c.ReadFrom(buf)
	var dst netip.Addr
	if cm != nil {
		dst, _ = netip.AddrFromSlice(cm.Dst)
	}
	return n, dst.Unmap(), from, err
}

func (u udp4Endpoint) write(b []byte, src netip.Addr, to net.Addr) error {
	var cm *ipv4.ControlMessage
	if src.Is4() {
		cm = &ipv4.ControlMessage{Src: src.AsSlice()}
	}
	_, err := u.c.WriteTo(b, cm, to)
	return err
}

func (u udp4Endpoint) close() error { return u.c.Close() }

type udp6Endpoint struct{ c *ipv6.PacketConn }

func (u udp6Endpoint) read(buf []byte) (int, netip.Addr, net.Addr, error) {
	n, cm, from, err := u.c.ReadFrom(buf)
	var dst netip.Addr
	if cm != nil {
		dst, _ = netip.AddrFromSlice(cm.Dst)
	}
	return n, dst.Unmap(), from, err
}

func (u udp6Endpoint) write(b []byte, src netip.Addr, to net.Addr) error {
	var cm *ipv6.ControlMessage
	if src.Is6() {
		cm = &ipv6.ControlMessage{Src: src.AsSlice()}
	}
	_, err := u.c.WriteTo(b, cm, to)
	return err
}

func (u udp6Endpoint) close() error { return u.c.Close() }

// listenUDPBench opens the port for IPv4 and, when the host has it, IPv6.
func listenUDPBench(port int) ([]udpEndpoint, error) {
	lc := net.ListenConfig{}
	c4, err := lc.ListenPacket(context.Background(), "udp4", "0.0.0.0:"+strconv.Itoa(port))
	if err != nil {
		return nil, fmt.Errorf("tunnel: the test port %d/udp cannot be opened: %w", port, err)
	}
	p4 := ipv4.NewPacketConn(c4)
	if err := p4.SetControlMessage(ipv4.FlagDst, true); err != nil {
		_ = c4.Close()
		return nil, err
	}
	out := []udpEndpoint{udp4Endpoint{p4}}
	if c6, err := lc.ListenPacket(context.Background(), "udp6", "[::]:"+strconv.Itoa(port)); err == nil {
		p6 := ipv6.NewPacketConn(c6)
		if p6.SetControlMessage(ipv6.FlagDst, true) == nil {
			out = append(out, udp6Endpoint{p6})
		} else {
			_ = c6.Close()
		}
	}
	return out, nil
}

type udpFlow struct {
	bytes, packets, sent int64
	first, last          time.Time
	started              time.Time
}

// udpServer counts uploads, sends downloads and echoes for the relay's measurements.
type udpServer struct {
	key       []byte
	allow     func(netip.Addr) bool
	endpoints []udpEndpoint
	mu        sync.Mutex
	flows     map[uint32]*udpFlow
	downs     int
	closed    chan struct{}
	wg        sync.WaitGroup
}

func startUDPServer(port int, key []byte, allow func(netip.Addr) bool) (*udpServer, error) {
	eps, err := listenUDPBench(port)
	if err != nil {
		return nil, err
	}
	s := &udpServer{key: key, allow: allow, endpoints: eps, flows: map[uint32]*udpFlow{}, closed: make(chan struct{})}
	for _, ep := range eps {
		s.wg.Go(func() { s.serve(ep) })
	}
	return s, nil
}

func (s *udpServer) close() {
	close(s.closed)
	for _, ep := range s.endpoints {
		_ = ep.close()
	}
	s.wg.Wait()
}

func (s *udpServer) serve(ep udpEndpoint) {
	buf := make([]byte, 65535)
	for {
		n, dst, from, err := ep.read(buf)
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		op, flow, _, ok := udpFields(buf[:n])
		if !ok || (s.allow != nil && !s.allow(dst)) {
			continue
		}
		if op == udpOpData {
			s.count(flow, n)
			continue
		}
		if n < udpHead+udpMACLen || !hmac.Equal(buf[udpHead:udpHead+udpMACLen], udpMAC(s.key, buf[:udpHead])) {
			continue
		}
		switch op {
		case udpOpEcho:
			_ = ep.write(buf[:n], dst, from)
		case udpOpBegin:
			s.begin(flow)
			_ = ep.write(buf[:n], dst, from)
		case udpOpReport:
			_ = ep.write(s.report(flow, buf[:udpHead]), dst, from)
		case udpOpDown:
			body := buf[udpHead+udpMACLen : n]
			if len(body) < 14 {
				continue
			}
			bps := binary.BigEndian.Uint64(body)
			dur := time.Duration(binary.BigEndian.Uint32(body[8:])) * time.Millisecond
			size := int(binary.BigEndian.Uint16(body[12:]))
			if s.startDown(flow) {
				s.wg.Go(func() { s.sendDown(ep, dst, from, flow, bps, dur, size) })
			}
		}
	}
}

func (s *udpServer) begin(flow uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, f := range s.flows {
		if time.Since(f.started) > 2*time.Minute {
			delete(s.flows, id)
		}
	}
	if s.flows[flow] == nil {
		s.flows[flow] = &udpFlow{started: time.Now()}
	}
}

func (s *udpServer) count(flow uint32, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.flows[flow]
	if f == nil || f.sent > 0 {
		return
	}
	now := time.Now()
	if f.packets == 0 {
		f.first = now
	}
	f.bytes += int64(n)
	f.packets++
	f.last = now
}

// startDown takes a flow id once only, so a replayed request starts nothing.
func (s *udpServer) startDown(flow uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.flows[flow] != nil || s.downs >= udpDowns {
		return false
	}
	s.flows[flow] = &udpFlow{started: time.Now()}
	s.downs++
	return true
}

func (s *udpServer) report(flow uint32, head []byte) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]byte(nil), head...)
	out[4] = udpOpReport
	var body [32]byte
	if f := s.flows[flow]; f != nil {
		binary.BigEndian.PutUint64(body[0:], uint64(f.bytes))
		binary.BigEndian.PutUint64(body[8:], uint64(f.packets))
		binary.BigEndian.PutUint64(body[16:], uint64(f.last.Sub(f.first)))
		binary.BigEndian.PutUint64(body[24:], uint64(f.sent))
	}
	return append(out, body[:]...)
}

// sendDown paces datagrams at the asked rate by the bytes the elapsed time allows.
func (s *udpServer) sendDown(ep udpEndpoint, src netip.Addr, to net.Addr, flow uint32, bps uint64, dur time.Duration, size int) {
	defer func() {
		s.mu.Lock()
		s.downs--
		s.mu.Unlock()
	}()
	bps = min(bps, udpMaxMbps*1_000_000)
	dur = min(dur, udpMaxDown)
	size = min(max(size, udpHead), 1400)
	start := time.Now()
	var seq uint32
	var sentBytes float64
	for time.Since(start) < dur {
		select {
		case <-s.closed:
			return
		default:
		}
		allowed := float64(bps) / 8 * time.Since(start).Seconds()
		for sentBytes+float64(size) <= allowed {
			if ep.write(udpPacket(udpOpDownData, flow, seq, size), src, to) == nil {
				seq++
			}
			sentBytes += float64(size)
		}
		time.Sleep(time.Millisecond)
	}
	s.mu.Lock()
	if f := s.flows[flow]; f != nil {
		f.sent = max(int64(seq), 1)
	}
	s.mu.Unlock()
}

func dialUDP(ctx context.Context, addr string) (*net.UDPConn, error) {
	c, err := (&net.Dialer{}).DialContext(ctx, "udp", addr)
	if err != nil {
		return nil, err
	}
	return c.(*net.UDPConn), nil
}

// udpAsk sends a request up to three times and returns the first reply of its flow and op.
func udpAsk(c *net.UDPConn, req []byte, want byte, timeout time.Duration) ([]byte, time.Duration, error) {
	_, flow, seq, _ := udpFields(req)
	buf := make([]byte, 2048)
	for range 3 {
		sent := time.Now()
		if _, err := c.Write(req); err != nil {
			return nil, 0, err
		}
		_ = c.SetReadDeadline(sent.Add(timeout))
		for {
			n, err := c.Read(buf)
			if err != nil {
				break
			}
			op, f, s, ok := udpFields(buf[:n])
			if ok && op == want && f == flow && s == seq {
				return append([]byte(nil), buf[:n]...), time.Since(sent), nil
			}
		}
	}
	return nil, 0, errors.New("no UDP answer through the tunnel")
}

func udpWaitUp(ctx context.Context, p BenchPlan) (time.Duration, error) {
	start := time.Now()
	for {
		if c, err := dialUDP(ctx, p.Dial); err == nil {
			_, _, err = udpAsk(c, udpRequest(p.Key, udpOpEcho, randomFlow(), 0, nil), udpOpEcho, 500*time.Millisecond)
			_ = c.Close()
			if err == nil {
				return time.Since(start), nil
			}
		}
		if time.Since(start) >= p.Setup {
			return 0, fmt.Errorf("no UDP answer within %s", p.Setup.Round(time.Second))
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// udpPings sends count echoes at a steady gap and collects the replies beside them, so a
// lost datagram costs nothing but its own count.
func udpPings(ctx context.Context, p BenchPlan, count int, gap time.Duration) ([]time.Duration, int) {
	c, err := dialUDP(ctx, p.Dial)
	if err != nil {
		return nil, count
	}
	defer c.Close()
	flow := randomFlow()
	sent := make([]time.Time, count)
	rtts := make([]time.Duration, count)
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 2048)
		_ = c.SetReadDeadline(time.Now().Add(time.Duration(count)*gap + time.Second))
		for {
			n, err := c.Read(buf)
			if err != nil {
				return
			}
			op, f, seq, ok := udpFields(buf[:n])
			if !ok || op != udpOpEcho || f != flow || int(seq) >= count {
				continue
			}
			mu.Lock()
			if rtts[seq] == 0 && !sent[seq].IsZero() {
				rtts[seq] = max(time.Since(sent[seq]), time.Nanosecond)
			}
			mu.Unlock()
		}
	}()
	for i := range count {
		if ctx.Err() != nil {
			break
		}
		mu.Lock()
		sent[i] = time.Now()
		mu.Unlock()
		_, _ = c.Write(udpRequest(p.Key, udpOpEcho, flow, uint32(i), nil))
		time.Sleep(gap)
	}
	<-done
	var got []time.Duration
	for _, r := range rtts {
		if r > 0 {
			got = append(got, r)
		}
	}
	return got, count - len(got)
}

type udpStep struct {
	mbps, loss float64
	bytes      int64
}

func lossOf(received, sent int64) float64 {
	if sent <= 0 {
		return 100
	}
	return math.Round(max(0, 1-float64(received)/float64(sent))*1000) / 10
}

func udpUpload(ctx context.Context, p BenchPlan, mbps float64, dur time.Duration) (udpStep, error) {
	c, err := dialUDP(ctx, p.Dial)
	if err != nil {
		return udpStep{}, err
	}
	defer c.Close()
	flow := randomFlow()
	if _, _, err := udpAsk(c, udpRequest(p.Key, udpOpBegin, flow, 0, nil), udpOpBegin, time.Second); err != nil {
		return udpStep{}, err
	}
	bps := mbps * 1e6
	start := time.Now()
	var seq uint32
	var sentBytes float64
	for time.Since(start) < dur && ctx.Err() == nil {
		allowed := bps / 8 * time.Since(start).Seconds()
		for sentBytes+udpDatagram <= allowed {
			if _, err := c.Write(udpPacket(udpOpData, flow, seq, udpDatagram)); err == nil {
				seq++
			}
			sentBytes += udpDatagram
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	reply, _, err := udpAsk(c, udpRequest(p.Key, udpOpReport, flow, 1, nil), udpOpReport, time.Second)
	if err != nil || len(reply) < udpHead+32 {
		return udpStep{}, errors.New("the origin did not report what it received over UDP")
	}
	body := reply[udpHead:]
	got := int64(binary.BigEndian.Uint64(body))
	packets := int64(binary.BigEndian.Uint64(body[8:]))
	took := time.Duration(binary.BigEndian.Uint64(body[16:]))
	return udpStep{mbps: mbps2(got, took, dur), loss: lossOf(packets, int64(seq)), bytes: got}, nil
}

func udpDownload(ctx context.Context, p BenchPlan, mbps float64, dur time.Duration) (udpStep, error) {
	c, err := dialUDP(ctx, p.Dial)
	if err != nil {
		return udpStep{}, err
	}
	defer c.Close()
	body := make([]byte, 14)
	binary.BigEndian.PutUint64(body, uint64(mbps*1e6))
	binary.BigEndian.PutUint32(body[8:], uint32(dur/time.Millisecond))
	binary.BigEndian.PutUint16(body[12:], udpDatagram)
	buf := make([]byte, 2048)
	var flow uint32
	var bytes, packets int64
	var first, last, asked time.Time
	// A request lost on the way starts nothing, so a second one goes out under a new flow.
	for attempt := 0; attempt < 2 && packets == 0 && ctx.Err() == nil; attempt++ {
		flow, asked = randomFlow(), time.Now()
		if _, err := c.Write(udpRequest(p.Key, udpOpDown, flow, 0, body)); err != nil {
			return udpStep{}, err
		}
		end := asked.Add(dur + 2*time.Second)
		for ctx.Err() == nil {
			wait := time.Now().Add(time.Second)
			if packets == 0 {
				wait = time.Now().Add(2 * time.Second)
			}
			if end.Before(wait) {
				wait = end
			}
			_ = c.SetReadDeadline(wait)
			n, err := c.Read(buf)
			if err != nil {
				break
			}
			op, f, _, ok := udpFields(buf[:n])
			if !ok || op != udpOpDownData || f != flow {
				continue
			}
			if packets == 0 {
				first = time.Now()
			}
			bytes += int64(n)
			packets++
			last = time.Now()
		}
	}
	if packets == 0 {
		return udpStep{loss: 100}, errors.New("no UDP data came back through the tunnel")
	}
	time.Sleep(max(time.Until(asked.Add(dur+300*time.Millisecond)), 200*time.Millisecond))
	reply, _, err := udpAsk(c, udpRequest(p.Key, udpOpReport, flow, 1, nil), udpOpReport, time.Second)
	if err != nil || len(reply) < udpHead+32 {
		return udpStep{}, errors.New("the origin did not report what it sent over UDP")
	}
	sent := int64(binary.BigEndian.Uint64(reply[udpHead+24:]))
	return udpStep{mbps: mbps2(bytes, last.Sub(first), dur), loss: lossOf(packets, sent), bytes: bytes}, nil
}

// mbps2 is the received rate, over the receiver's own span unless that span is too short
// to mean anything, as with one datagram.
func mbps2(n int64, span, dur time.Duration) float64 {
	if span < dur/4 {
		span = dur
	}
	return mbps(n, span)
}

// udpLadder sends at start and quadruples while little is lost; once much is, what got
// through is the path's rate, tried again just below it, with one more step if needed.
func udpLadder(ctx context.Context, start float64, budget int64, maxSteps int, stepDur time.Duration, run func(float64, time.Duration) (udpStep, error)) (udpStep, int64, error) {
	rate := min(max(start, 5), udpMaxMbps)
	var steps []udpStep
	var used int64
	var lastErr error
	for i := 0; i < maxSteps+1 && ctx.Err() == nil; i++ {
		if i == maxSteps && steps[len(steps)-1].loss <= 2 {
			break
		}
		dur := stepDur
		// Each step gets its share of the budget, spent in a shorter step, not at a lower rate.
		if budget > 0 {
			share := float64(budget/int64(maxSteps+1)) * 8 / 1e6
			if rate*dur.Seconds() > share {
				dur = time.Duration(share / rate * float64(time.Second))
			}
			if dur < udpMinStep {
				dur, rate = udpMinStep, share/udpMinStep.Seconds()
			}
			if rate < 1 {
				break
			}
		}
		st, err := run(rate, dur)
		used += st.bytes
		if err != nil {
			lastErr = err
			break
		}
		steps = append(steps, st)
		if st.loss > 2 {
			rate = max(st.mbps*0.95, 1)
		} else if rate = min(rate*4, udpMaxMbps); st.mbps >= udpMaxMbps {
			break
		}
	}
	if len(steps) == 0 {
		return udpStep{}, used, lastErr
	}
	return pickStep(steps), used, nil
}

func pickStep(steps []udpStep) udpStep {
	best := -1
	for i, s := range steps {
		if s.loss <= 5 && (best < 0 || s.mbps > steps[best].mbps) {
			best = i
		}
	}
	if best >= 0 {
		return steps[best]
	}
	best = 0
	for i, s := range steps {
		if s.loss < steps[best].loss {
			best = i
		}
	}
	return steps[best]
}
