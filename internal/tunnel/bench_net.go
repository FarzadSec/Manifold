package tunnel

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// The tunnel test measures a candidate with a protocol of its own: the origin answers on
// one port, and the relay dials it through the tunnel, the way users' traffic travels.
const (
	benchMagic = "MFTB1"
	benchNonce = 16
	benchMAC   = 16
	benchFrame = 16
	benchChunk = 64 << 10
)

const (
	benchOpEcho     byte = 'E'
	benchOpUpload   byte = 'U'
	benchOpDownload byte = 'D'
)

// Phases of one measurement, as the test's progress names them.
const (
	BenchPhaseSetup    = "setup"
	BenchPhaseLatency  = "latency"
	BenchPhaseDownload = "download"
	BenchPhaseUpload   = "upload"
	BenchPhaseLoad     = "load"
	BenchPhaseUDP      = "udp"
)

func benchMACOf(key, nonce []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(benchMagic))
	m.Write(nonce)
	return m.Sum(nil)[:benchMAC]
}

// BenchServer answers the relay's measurements on the origin while a test runs, over
// TCP and over UDP on the same port.
type BenchServer struct {
	ln      net.Listener
	udp     *udpServer
	key     []byte
	allow   func(local netip.Addr) bool
	payload []byte
	wg      sync.WaitGroup
	mu      sync.Mutex
	conns   map[net.Conn]struct{}
	closed  bool
}

// ListenBench opens the origin's answering port. allow decides by the address a
// connection reached, so the port serves only what came through a test tunnel.
func ListenBench(port int, key []byte, allow func(local netip.Addr) bool) (*BenchServer, error) {
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return nil, fmt.Errorf("tunnel: the test port %d cannot be opened: %w", port, err)
	}
	udp, err := startUDPServer(port, key, allow)
	if err != nil {
		_ = ln.Close()
		return nil, err
	}
	payload := make([]byte, benchChunk)
	_, _ = rand.Read(payload)
	s := &BenchServer{ln: ln, udp: udp, key: key, allow: allow, payload: payload, conns: map[net.Conn]struct{}{}}
	s.wg.Go(s.serve)
	return s, nil
}

// Close stops answering and cuts the measurements under way.
func (s *BenchServer) Close() error {
	s.mu.Lock()
	s.closed = true
	err := s.ln.Close()
	for c := range s.conns {
		_ = c.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
	s.udp.close()
	return err
}

func (s *BenchServer) serve() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = c.Close()
			return
		}
		s.conns[c] = struct{}{}
		s.mu.Unlock()
		s.wg.Go(func() {
			s.handle(c)
			s.mu.Lock()
			delete(s.conns, c)
			s.mu.Unlock()
		})
	}
}

func addrOf(a net.Addr) netip.Addr {
	if tcp, ok := a.(*net.TCPAddr); ok {
		if ip, ok := netip.AddrFromSlice(tcp.IP); ok {
			return ip.Unmap()
		}
	}
	return netip.Addr{}
}

func (s *BenchServer) handle(c net.Conn) {
	defer c.Close()
	if s.allow != nil && !s.allow(addrOf(c.LocalAddr())) {
		return
	}
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	nonce := make([]byte, benchNonce)
	_, _ = rand.Read(nonce)
	if _, err := c.Write(append([]byte(benchMagic), nonce...)); err != nil {
		return
	}
	var req [benchMAC + 5]byte
	if _, err := io.ReadFull(c, req[:]); err != nil || !hmac.Equal(req[:benchMAC], benchMACOf(s.key, nonce)) {
		return
	}
	arg := time.Duration(binary.BigEndian.Uint32(req[benchMAC+1:])) * time.Millisecond
	switch req[benchMAC] {
	case benchOpEcho:
		frame := make([]byte, benchFrame)
		for {
			_ = c.SetDeadline(time.Now().Add(30 * time.Second))
			if _, err := io.ReadFull(c, frame); err != nil {
				return
			}
			if _, err := c.Write(frame); err != nil {
				return
			}
		}
	case benchOpUpload:
		_ = c.SetDeadline(time.Now().Add(arg + 30*time.Second))
		total, took, err := readUploadFrames(c)
		if err != nil {
			return
		}
		var report [16]byte
		binary.BigEndian.PutUint64(report[:8], uint64(total))
		binary.BigEndian.PutUint64(report[8:], uint64(took))
		_, _ = c.Write(report[:])
	case benchOpDownload:
		end := time.Now().Add(arg)
		_ = c.SetDeadline(end.Add(10 * time.Second))
		for time.Now().Before(end) {
			if _, err := c.Write(s.payload); err != nil {
				return
			}
		}
	}
}

// An upload is framed, a length before each chunk and a zero length at its end: a
// half-close would end it more simply, but muxed tunnels close both ways on it.
func readUploadFrames(c net.Conn) (int64, time.Duration, error) {
	buf := make([]byte, benchChunk)
	var header [4]byte
	var total int64
	var first time.Time
	for {
		if _, err := io.ReadFull(c, header[:]); err != nil {
			return 0, 0, err
		}
		n := binary.BigEndian.Uint32(header[:])
		if n == 0 {
			if total == 0 {
				return 0, 0, nil
			}
			return total, time.Since(first), nil
		}
		if n > benchChunk {
			return 0, 0, errors.New("an upload frame is larger than the test sends")
		}
		if total == 0 {
			first = time.Now()
		}
		if _, err := io.ReadFull(c, buf[:n]); err != nil {
			return 0, 0, err
		}
		total += int64(n)
	}
}

func dialBench(ctx context.Context, addr string, key []byte, op byte, arg time.Duration) (net.Conn, error) {
	dctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	c, err := (&net.Dialer{}).DialContext(dctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	_ = c.SetDeadline(time.Now().Add(6 * time.Second))
	hello := make([]byte, len(benchMagic)+benchNonce)
	if _, err := io.ReadFull(c, hello); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("no answer through the tunnel: %w", err)
	}
	if string(hello[:len(benchMagic)]) != benchMagic {
		_ = c.Close()
		return nil, errors.New("something other than the test answered through the tunnel")
	}
	req := append(benchMACOf(key, hello[len(benchMagic):]), op, 0, 0, 0, 0)
	binary.BigEndian.PutUint32(req[benchMAC+1:], uint32(arg/time.Millisecond))
	if _, err := c.Write(req); err != nil {
		_ = c.Close()
		return nil, err
	}
	_ = c.SetDeadline(time.Time{})
	return c, nil
}

// BenchPlan is how one candidate is measured from the relay.
type BenchPlan struct {
	// Dial reaches the origin's test port through the tunnel; Echo, when set, is the
	// origin's tunnel address, pinged for the loss a stream protocol would hide.
	Dial    string
	Echo    string
	Key     []byte
	Setup   time.Duration
	Phase   time.Duration
	Cap     int64
	Streams int
	// Pings, UDPPings and UDPSteps default to 20, 50 and 3; UDPCap, each way, to Cap.
	Pings    int
	UDPPings int
	UDPSteps int
	UDPCap   int64
}

func (p BenchPlan) withDefaults() BenchPlan {
	if p.Pings <= 0 {
		p.Pings = benchPings
	}
	if p.UDPPings <= 0 {
		p.UDPPings = benchUDPPings
	}
	if p.UDPSteps <= 0 {
		p.UDPSteps = 3
	}
	if p.UDPCap <= 0 {
		p.UDPCap = p.Cap
	}
	return p
}

// BenchMetrics is what one measurement found; Error is set when the tunnel carried neither
// TCP nor UDP, Warning when one of them or a later phase broke off.
type BenchMetrics struct {
	TCP           bool    `json:"tcp" example:"true"`
	UDP           bool    `json:"udp" example:"true"`
	SetupMs       float64 `json:"setupMs" example:"850"`
	LatencyMs     float64 `json:"latencyMs" example:"42.1"`
	JitterMs      float64 `json:"jitterMs" example:"1.3"`
	Loss          float64 `json:"loss" example:"0"`
	DownMbps      float64 `json:"downMbps" example:"412.5"`
	UpMbps        float64 `json:"upMbps" example:"398.2"`
	LoadDownMbps  float64 `json:"loadDownMbps" example:"640.8"`
	LoadUpMbps    float64 `json:"loadUpMbps" example:"602.4"`
	LoadLatencyMs float64 `json:"loadLatencyMs" example:"61.7"`
	Streams       int     `json:"streams" example:"8"`
	StreamErrors  int     `json:"streamErrors" example:"0"`
	UDPLatencyMs  float64 `json:"udpLatencyMs" example:"41.8"`
	UDPJitterMs   float64 `json:"udpJitterMs" example:"0.9"`
	UDPLoss       float64 `json:"udpLoss" example:"0"`
	UDPDownMbps   float64 `json:"udpDownMbps" example:"380.2"`
	UDPDownLoss   float64 `json:"udpDownLoss" example:"0.4"`
	UDPUpMbps     float64 `json:"udpUpMbps" example:"371.5"`
	UDPUpLoss     float64 `json:"udpUpLoss" example:"0.2"`
	Bytes         int64   `json:"bytes" example:"268435456"`
	Error         string  `json:"error,omitempty" example:""`
	Warning       string  `json:"warning,omitempty" example:""`
}

const (
	benchPings    = 20
	benchUDPPings = 50
	// benchGrace is how long the other protocol may still come up once one has.
	benchGrace = 3 * time.Second
)

// waitEither waits for TCP and UDP to cross the tunnel at once; once one has, the other
// gets benchGrace, since a tunnel that carries only one should not cost the whole setup.
func waitEither(ctx context.Context, p BenchPlan) (setup time.Duration, tcpErr, udpErr error) {
	tctx, tcancel := context.WithCancel(ctx)
	defer tcancel()
	uctx, ucancel := context.WithCancel(ctx)
	defer ucancel()
	type up struct {
		took time.Duration
		err  error
		udp  bool
	}
	ch := make(chan up, 2)
	go func() { d, err := benchWaitUp(tctx, p); ch <- up{d, err, false} }()
	go func() { d, err := udpWaitUp(uctx, p); ch <- up{d, err, true} }()
	for range 2 {
		r := <-ch
		if r.udp {
			udpErr = r.err
		} else {
			tcpErr = r.err
		}
		if r.err == nil && setup == 0 {
			setup = r.took
			other := tcancel
			if !r.udp {
				other = ucancel
			}
			stop := time.AfterFunc(benchGrace, other)
			defer stop.Stop()
		}
	}
	if errors.Is(tcpErr, context.Canceled) && ctx.Err() == nil {
		tcpErr = errors.New("no TCP connection crossed the tunnel")
	}
	if errors.Is(udpErr, context.Canceled) && ctx.Err() == nil {
		udpErr = errors.New("no UDP datagram crossed the tunnel")
	}
	return setup, tcpErr, udpErr
}

// Measure waits for the tunnel to carry TCP or UDP, then measures what it carries: round
// trips, streams alone and under load with pings beside them, and UDP's loss and rate.
func Measure(ctx context.Context, p BenchPlan, phase func(string)) BenchMetrics {
	if phase == nil {
		phase = func(string) {}
	}
	p = p.withDefaults()
	var m BenchMetrics
	phase(BenchPhaseSetup)
	setup, tcpErr, udpErr := waitEither(ctx, p)
	m.TCP, m.UDP = tcpErr == nil, udpErr == nil
	if !m.TCP && !m.UDP {
		m.Error = tcpErr.Error()
		return m
	}
	m.SetupMs = millis(setup)
	if m.TCP {
		measureTCP(ctx, p, phase, &m)
	} else {
		m.Warning = "TCP: " + tcpErr.Error()
	}
	if m.UDP {
		measureUDP(ctx, p, phase, &m)
	} else if m.Warning == "" {
		m.Warning = "UDP: " + udpErr.Error()
	}
	return m
}

func measureTCP(ctx context.Context, p BenchPlan, phase func(string), m *BenchMetrics) {
	phase(BenchPhaseLatency)
	icmpLoss := -1.0
	var pinger sync.WaitGroup
	if target, err := netip.ParseAddr(p.Echo); err == nil {
		pinger.Go(func() { icmpLoss = icmpBurstLoss(target, p.Pings, 100*time.Millisecond, 1500*time.Millisecond) })
	}
	rtts, lost := benchPingSeries(ctx, p, p.Pings, 100*time.Millisecond)
	pinger.Wait()
	if len(rtts) == 0 {
		m.TCP = false
		m.Warning = "TCP: the tunnel connected but stopped answering"
		return
	}
	m.LatencyMs, m.JitterMs = rttStats(rtts)
	m.Loss = math.Round(float64(lost)*1000/float64(p.Pings)) / 10
	if icmpLoss >= 0 {
		m.Loss = icmpLoss
	}

	warn := func(what string, err error) {
		m.StreamErrors++
		if m.Warning == "" {
			m.Warning = what + ": " + err.Error()
		}
	}
	phase(BenchPhaseDownload)
	n, d, err := benchDownload(ctx, p.Dial, p.Key, p.Phase, p.Cap)
	m.DownMbps, m.Bytes = mbps(n, d), m.Bytes+n
	if err != nil {
		warn("download", err)
	}
	phase(BenchPhaseUpload)
	n, d, err = benchUpload(ctx, p.Dial, p.Key, p.Phase, p.Cap)
	m.UpMbps, m.Bytes = mbps(n, d), m.Bytes+n
	if err != nil {
		warn("upload", err)
	}
	if p.Streams > 0 {
		phase(BenchPhaseLoad)
		load := benchLoad(ctx, p)
		m.LoadDownMbps, m.LoadUpMbps, m.LoadLatencyMs = load.down, load.up, load.latency
		m.Streams, m.Bytes = p.Streams, m.Bytes+load.bytes
		m.StreamErrors += load.failed
		if load.err != nil {
			warn("load", load.err)
		}
	}
}

func measureUDP(ctx context.Context, p BenchPlan, phase func(string), m *BenchMetrics) {
	phase(BenchPhaseUDP)
	rtts, lost := udpPings(ctx, p, p.UDPPings, 20*time.Millisecond)
	m.UDPLoss = math.Round(float64(lost)*1000/float64(p.UDPPings)) / 10
	if len(rtts) > 0 {
		m.UDPLatencyMs, m.UDPJitterMs = rttStats(rtts)
	}
	stepDur := max(p.Phase/time.Duration(p.UDPSteps), 500*time.Millisecond)
	budget := p.UDPCap
	start := func(tcp float64) float64 {
		if tcp > 0 {
			return tcp
		}
		return 50
	}
	warn := func(what string, err error) {
		if m.Warning == "" {
			m.Warning = what + ": " + err.Error()
		}
	}
	up, used, err := udpLadder(ctx, start(m.UpMbps), budget, p.UDPSteps, stepDur, func(r float64, d time.Duration) (udpStep, error) {
		return udpUpload(ctx, p, r, d)
	})
	m.UDPUpMbps, m.UDPUpLoss, m.Bytes = up.mbps, up.loss, m.Bytes+used
	if err != nil {
		warn("UDP upload", err)
	}
	down, used, err := udpLadder(ctx, start(m.DownMbps), budget, p.UDPSteps, stepDur, func(r float64, d time.Duration) (udpStep, error) {
		return udpDownload(ctx, p, r, d)
	})
	m.UDPDownMbps, m.UDPDownLoss, m.Bytes = down.mbps, down.loss, m.Bytes+used
	if err != nil {
		warn("UDP download", err)
	}
}

func millis(d time.Duration) float64 { return math.Round(float64(d.Microseconds())/10) / 100 }

func mbps(n int64, d time.Duration) float64 {
	if n <= 0 || d < time.Microsecond {
		return 0
	}
	return math.Round(float64(n)*8/d.Seconds()/1e4) / 100
}

func rttStats(rtts []time.Duration) (mean, jitter float64) {
	var sum, diff time.Duration
	for i, r := range rtts {
		sum += r
		if i > 0 {
			diff += (r - rtts[i-1]).Abs()
		}
	}
	mean = millis(sum / time.Duration(len(rtts)))
	if len(rtts) > 1 {
		jitter = millis(diff / time.Duration(len(rtts)-1))
	}
	return mean, jitter
}

func benchWaitUp(ctx context.Context, p BenchPlan) (time.Duration, error) {
	start := time.Now()
	var last error
	for {
		if c, err := dialBench(ctx, p.Dial, p.Key, benchOpEcho, 0); err == nil {
			_, err = benchEcho(c, 0, 5*time.Second)
			_ = c.Close()
			if err == nil {
				return time.Since(start), nil
			}
			last = err
		} else {
			last = err
		}
		if time.Since(start) >= p.Setup {
			return 0, fmt.Errorf("the tunnel carried no connection within %s: %w", p.Setup.Round(time.Second), last)
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func benchEcho(c net.Conn, seq uint64, timeout time.Duration) (time.Duration, error) {
	var frame [benchFrame]byte
	binary.BigEndian.PutUint64(frame[:8], seq)
	sent := time.Now()
	binary.BigEndian.PutUint64(frame[8:], uint64(sent.UnixNano()))
	_ = c.SetDeadline(sent.Add(timeout))
	if _, err := c.Write(frame[:]); err != nil {
		return 0, err
	}
	var reply [benchFrame]byte
	if _, err := io.ReadFull(c, reply[:]); err != nil {
		return 0, err
	}
	if reply != frame {
		return 0, errors.New("the tunnel changed an echo")
	}
	return time.Since(sent), nil
}

// benchPingSeries sends count echoes; one that times out counts as lost and the
// connection is opened again, since a stalled stream holds every echo behind it.
func benchPingSeries(ctx context.Context, p BenchPlan, count int, gap time.Duration) ([]time.Duration, int) {
	var rtts []time.Duration
	lost := 0
	var c net.Conn
	defer func() {
		if c != nil {
			_ = c.Close()
		}
	}()
	for i := range count {
		if ctx.Err() != nil {
			return rtts, lost + count - i
		}
		if c == nil {
			var err error
			if c, err = dialBench(ctx, p.Dial, p.Key, benchOpEcho, 0); err != nil {
				lost++
				continue
			}
		}
		rtt, err := benchEcho(c, uint64(i), 2*time.Second)
		if err != nil {
			lost++
			_ = c.Close()
			c = nil
			continue
		}
		rtts = append(rtts, rtt)
		time.Sleep(gap)
	}
	return rtts, lost
}

func benchDownload(ctx context.Context, addr string, key []byte, phase time.Duration, capBytes int64) (int64, time.Duration, error) {
	c, err := dialBench(ctx, addr, key, benchOpDownload, phase)
	if err != nil {
		return 0, 0, err
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	_ = c.SetDeadline(time.Now().Add(phase + 15*time.Second))
	buf := make([]byte, benchChunk)
	var total int64
	var first time.Time
	for capBytes <= 0 || total < capBytes {
		n, err := c.Read(buf)
		if n > 0 {
			if total == 0 {
				first = time.Now()
			}
			total += int64(n)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return total, time.Since(first), err
		}
	}
	return total, time.Since(first), nil
}

func benchUpload(ctx context.Context, addr string, key []byte, phase time.Duration, capBytes int64) (int64, time.Duration, error) {
	c, err := dialBench(ctx, addr, key, benchOpUpload, phase)
	if err != nil {
		return 0, 0, err
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	frame := make([]byte, 4+benchChunk)
	binary.BigEndian.PutUint32(frame, benchChunk)
	_, _ = rand.Read(frame[4:])
	end := time.Now().Add(phase)
	_ = c.SetDeadline(end.Add(15 * time.Second))
	var sent int64
	for time.Now().Before(end) && (capBytes <= 0 || sent < capBytes) {
		if _, err := c.Write(frame); err != nil {
			return 0, 0, err
		}
		sent += benchChunk
	}
	if _, err := c.Write(make([]byte, 4)); err != nil {
		return 0, 0, err
	}
	var report [16]byte
	if _, err := io.ReadFull(c, report[:]); err != nil {
		return 0, 0, fmt.Errorf("the origin did not report what it received: %w", err)
	}
	return int64(binary.BigEndian.Uint64(report[:8])), time.Duration(binary.BigEndian.Uint64(report[8:])), nil
}

type loadResult struct {
	down, up, latency float64
	bytes             int64
	failed            int
	err               error
}

// benchLoad runs half the streams each way at once and pings beside them; the streams
// share the phase's cap, so a test of many candidates moves a bounded amount of data.
func benchLoad(ctx context.Context, p BenchPlan) loadResult {
	downs := max(p.Streams/2, 1)
	ups := max(p.Streams-downs, 1)
	perStream := int64(0)
	if p.Cap > 0 {
		perStream = max(p.Cap/int64(downs), benchChunk)
	}
	lctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	var res loadResult
	var downBytes, upBytes int64
	var downTime, upTime time.Duration
	var wg sync.WaitGroup
	record := func(n int64, d time.Duration, err error, isDown bool) {
		mu.Lock()
		defer mu.Unlock()
		if isDown {
			downBytes += n
			downTime = max(downTime, d)
		} else {
			upBytes += n
			upTime = max(upTime, d)
		}
		if err != nil {
			res.failed++
			if res.err == nil {
				res.err = err
			}
		}
	}
	for range downs {
		wg.Go(func() {
			n, d, err := benchDownload(lctx, p.Dial, p.Key, p.Phase, perStream)
			record(n, d, err, true)
		})
	}
	upCap := int64(0)
	if p.Cap > 0 {
		upCap = max(p.Cap/int64(ups), benchChunk)
	}
	for range ups {
		wg.Go(func() {
			n, d, err := benchUpload(lctx, p.Dial, p.Key, p.Phase, upCap)
			record(n, d, err, false)
		})
	}
	done := make(chan struct{})
	var rtts []time.Duration
	go func() {
		defer close(done)
		time.Sleep(300 * time.Millisecond)
		c, err := dialBench(lctx, p.Dial, p.Key, benchOpEcho, 0)
		if err != nil {
			return
		}
		defer c.Close()
		for seq := uint64(0); lctx.Err() == nil; seq++ {
			rtt, err := benchEcho(c, seq, 3*time.Second)
			if err != nil {
				return
			}
			rtts = append(rtts, rtt)
			time.Sleep(200 * time.Millisecond)
		}
	}()
	wg.Wait()
	cancel()
	<-done
	res.down, res.up = mbps(downBytes, downTime), mbps(upBytes, upTime)
	res.bytes = downBytes + upBytes
	if len(rtts) > 0 {
		res.latency, _ = rttStats(rtts)
	}
	return res
}

// icmpBurstLoss pings the far tunnel address count times and returns the share lost, or
// -1 when this host cannot send ICMP at all.
func icmpBurstLoss(target netip.Addr, count int, gap, wait time.Duration) float64 {
	conn, datagram, err := listen(target)
	if err != nil {
		return -1
	}
	defer conn.Close()
	var idRaw [2]byte
	_, _ = rand.Read(idRaw[:])
	id := int(binary.BigEndian.Uint16(idRaw[:]))
	var kind icmp.Type = ipv4.ICMPTypeEcho
	reply, proto := icmp.Type(ipv4.ICMPTypeEchoReply), 1
	if target.Is6() {
		kind, reply, proto = ipv6.ICMPTypeEchoRequest, ipv6.ICMPTypeEchoReply, 58
	}
	var dst net.Addr = &net.IPAddr{IP: target.AsSlice()}
	if datagram {
		dst = &net.UDPAddr{IP: target.AsSlice()}
	}
	seen := map[int]bool{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 1500)
		_ = conn.SetReadDeadline(time.Now().Add(time.Duration(count)*gap + wait))
		for len(seen) < count {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			parsed, err := icmp.ParseMessage(proto, buf[:n])
			if err != nil || parsed.Type != reply || !sameHost(from, target) {
				continue
			}
			if echo, ok := parsed.Body.(*icmp.Echo); ok && (datagram || echo.ID == id) && echo.Seq >= 1 && echo.Seq <= count {
				seen[echo.Seq] = true
			}
		}
	}()
	sent := 0
	for seq := 1; seq <= count; seq++ {
		msg := icmp.Message{Type: kind, Body: &icmp.Echo{ID: id, Seq: seq, Data: []byte("manifold-tunnel-test")}}
		if wire, err := msg.Marshal(nil); err == nil {
			if _, err := conn.WriteTo(wire, dst); err == nil {
				sent++
			}
		}
		time.Sleep(gap)
	}
	<-done
	if sent == 0 {
		return -1
	}
	return math.Round(float64(count-len(seen))*1000/float64(count)) / 10
}
