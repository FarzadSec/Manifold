package tunnel

import (
	"context"
	"net"
	"net/netip"
	"os"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	probeInterval = 2 * time.Second
	probeTimeout  = 2 * time.Second
	probeWindow   = 30
)

// result is one echo: its round trip, or lost.
type result struct {
	rtt  time.Duration
	lost bool
}

// window holds the last echoes of one tunnel; replies past the timeout count as lost.
type window struct {
	mu      sync.Mutex
	results []result
}

func (w *window) add(r result) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.results = append(w.results, r)
	if len(w.results) > probeWindow {
		w.results = w.results[len(w.results)-probeWindow:]
	}
}

// summary returns the loss in percent and the mean round trip of the answered echoes,
// and how many of the most recent echoes in a row were lost.
func (w *window) summary() (loss float64, rtt time.Duration, recentLost, total int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var lost int
	var sum time.Duration
	for _, r := range w.results {
		if r.lost {
			lost++
			continue
		}
		sum += r.rtt
	}
	for i := len(w.results) - 1; i >= 0 && w.results[i].lost; i-- {
		recentLost++
	}
	total = len(w.results)
	if total == 0 {
		return 0, 0, 0, 0
	}
	if answered := total - lost; answered > 0 {
		rtt = sum / time.Duration(answered)
	}
	return float64(lost) * 100 / float64(total), rtt, recentLost, total
}

// prober echoes the far end of one tunnel until stopped.
type prober struct {
	target netip.Addr
	window window
	cancel context.CancelFunc
	done   chan struct{}
}

func startProber(target netip.Addr) *prober {
	ctx, cancel := context.WithCancel(context.Background())
	p := &prober{target: target, cancel: cancel, done: make(chan struct{})}
	go p.run(ctx)
	return p
}

func (p *prober) stop() {
	p.cancel()
	<-p.done
}

// listen prefers the unprivileged ICMP socket and falls back to a raw one.
func listen(target netip.Addr) (*icmp.PacketConn, bool, error) {
	network, raw, wildcard := "udp4", "ip4:icmp", "0.0.0.0"
	if target.Is6() {
		network, raw, wildcard = "udp6", "ip6:ipv6-icmp", "::"
	}
	if c, err := icmp.ListenPacket(network, wildcard); err == nil {
		return c, true, nil
	}
	c, err := icmp.ListenPacket(raw, wildcard)
	return c, false, err
}

func (p *prober) run(ctx context.Context) {
	defer close(p.done)
	ticker := time.NewTicker(probeInterval)
	defer ticker.Stop()
	var seq int
	for {
		seq++
		p.window.add(p.echo(seq))
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (p *prober) echo(seq int) result {
	conn, datagram, err := listen(p.target)
	if err != nil {
		return result{lost: true}
	}
	defer conn.Close()
	id := os.Getpid() & 0xffff
	var kind icmp.Type = ipv4.ICMPTypeEcho
	reply, proto := icmp.Type(ipv4.ICMPTypeEchoReply), 1
	if p.target.Is6() {
		kind, reply, proto = ipv6.ICMPTypeEchoRequest, ipv6.ICMPTypeEchoReply, 58
	}
	msg := icmp.Message{Type: kind, Body: &icmp.Echo{ID: id, Seq: seq & 0xffff, Data: []byte("manifold-tunnel")}}
	wire, err := msg.Marshal(nil)
	if err != nil {
		return result{lost: true}
	}
	var dst net.Addr = &net.IPAddr{IP: p.target.AsSlice()}
	if datagram {
		dst = &net.UDPAddr{IP: p.target.AsSlice()}
	}
	sent := time.Now()
	if _, err := conn.WriteTo(wire, dst); err != nil {
		return result{lost: true}
	}
	buf := make([]byte, 1500)
	_ = conn.SetReadDeadline(sent.Add(probeTimeout))
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			return result{lost: true}
		}
		parsed, err := icmp.ParseMessage(proto, buf[:n])
		if err != nil || parsed.Type != reply {
			continue
		}
		echo, ok := parsed.Body.(*icmp.Echo)
		if !ok || echo.Seq != seq&0xffff || (!datagram && echo.ID != id) || !sameHost(from, p.target) {
			continue
		}
		return result{rtt: time.Since(sent)}
	}
}

func sameHost(from net.Addr, target netip.Addr) bool {
	var ip net.IP
	switch a := from.(type) {
	case *net.UDPAddr:
		ip = a.IP
	case *net.IPAddr:
		ip = a.IP
	}
	got, ok := netip.AddrFromSlice(ip)
	return ok && got.Unmap() == target.Unmap()
}
