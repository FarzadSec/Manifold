// Package ingress shares one port among many services: it reads the name a connection
// asks for (TLS SNI, or the HTTP Host) and hands the untouched stream to that service.
package ingress

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	helloTimeout = 10 * time.Second
	dialTimeout  = 5 * time.Second
)

// Route sends the names in Hosts to Target. A host "*.example.com" matches one label under it.
type Route struct {
	Hosts []string `json:"hosts"`
	// ALPN, when set, narrows the route to TLS clients offering one of these protocols.
	ALPN          []string `json:"alpn,omitempty"`
	Target        string   `json:"target"`
	ProxyProtocol int      `json:"proxyProtocol,omitempty"`
}

// Listener is one port and the routes tried on it, in order.
type Listener struct {
	Id      int
	Listen  string
	Port    int
	Routes  []Route
	Default Route
}

func (l Listener) addr() string {
	return net.JoinHostPort(l.Listen, strconv.Itoa(l.Port))
}

// match picks the route for a peeked connection: the first whose host and ALPN fit, else Default.
func (l Listener) match(p peek) (Route, bool) {
	for _, r := range l.Routes {
		if hostMatches(r.Hosts, p.host) && alpnMatches(r.ALPN, p) {
			return r, true
		}
	}
	return l.Default, l.Default.Target != ""
}

func hostMatches(patterns []string, host string) bool {
	if host == "" {
		return false
	}
	for _, pattern := range patterns {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if suffix, ok := strings.CutPrefix(pattern, "*."); ok {
			label, rest, found := strings.Cut(host, ".")
			if found && label != "" && rest == suffix {
				return true
			}
			continue
		}
		if pattern == host {
			return true
		}
	}
	return false
}

func alpnMatches(want []string, p peek) bool {
	if len(want) == 0 {
		return true
	}
	if !p.tls {
		return false
	}
	for _, w := range want {
		for _, offered := range p.alpn {
			if strings.EqualFold(w, offered) {
				return true
			}
		}
	}
	return false
}

// ValidPattern accepts a host name, an address, or "*." and a name.
func ValidPattern(pattern string) bool {
	name := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(pattern)), "*.")
	if _, err := netip.ParseAddr(name); err == nil && !strings.HasPrefix(pattern, "*.") {
		return true
	}
	if name == "" || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

// running is one bound port and the routes it uses now, swapped without dropping connections.
type running struct {
	ln     net.Listener
	routes atomic.Pointer[Listener]
}

// Manager keeps the configured listeners bound and routing.
type Manager struct {
	mu      sync.Mutex
	running map[int]*running
	errors  map[int]string
}

func NewManager() *Manager {
	return &Manager{running: map[int]*running{}, errors: map[int]string{}}
}

// Apply binds what is new, rebinds what moved, swaps routes in place and closes the rest.
func (m *Manager) Apply(listeners []Listener) {
	m.mu.Lock()
	defer m.mu.Unlock()
	want := map[int]Listener{}
	for _, l := range listeners {
		want[l.Id] = l
	}
	for id, r := range m.running {
		l, keep := want[id]
		if keep && r.routes.Load().addr() == l.addr() {
			continue
		}
		_ = r.ln.Close()
		delete(m.running, id)
	}
	m.errors = map[int]string{}
	for id, l := range want {
		if r, ok := m.running[id]; ok {
			r.routes.Store(&l)
			continue
		}
		var lc net.ListenConfig
		ln, err := lc.Listen(context.Background(), "tcp", l.addr())
		if err != nil {
			m.errors[id] = err.Error()
			continue
		}
		r := &running{ln: ln}
		r.routes.Store(&l)
		m.running[id] = r
		go r.serve()
	}
}

// Errors names the listeners that could not bind, with the reason.
func (m *Manager) Errors() map[int]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[int]string, len(m.errors))
	for k, v := range m.errors {
		out[k] = v
	}
	return out
}

// Addr is where a running listener is bound, for tests and status.
func (m *Manager) Addr(id int) net.Addr {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.running[id]; ok {
		return r.ln.Addr()
	}
	return nil
}

// Close stops every listener; connections already handed over finish on their own.
func (m *Manager) Close() {
	m.Apply(nil)
}

func (r *running) serve() {
	for {
		conn, err := r.ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}
		go handle(conn, r.routes.Load())
	}
}

func handle(client net.Conn, l *Listener) {
	defer client.Close()
	_ = client.SetReadDeadline(time.Now().Add(helloTimeout))
	reader := bufio.NewReaderSize(client, maxHelloBytes+recordHeader)
	p, err := readFirst(reader)
	if err != nil {
		return
	}
	_ = client.SetReadDeadline(time.Time{})
	route, ok := l.match(p)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	var d net.Dialer
	target, err := d.DialContext(ctx, "tcp", route.Target)
	if err != nil {
		return
	}
	defer target.Close()
	head := p.bytes
	if route.ProxyProtocol > 0 {
		head = append(proxyHeader(route.ProxyProtocol, client.RemoteAddr(), client.LocalAddr()), p.bytes...)
	}
	if _, err := target.Write(head); err != nil {
		return
	}
	pipe(client, reader, target)
}

// pipe copies both ways until each side is done, closing the write half it finished.
func pipe(client net.Conn, clientReader io.Reader, target net.Conn) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(target, clientReader)
		closeWrite(target)
	}()
	_, _ = io.Copy(client, target)
	closeWrite(client)
	wg.Wait()
}

func closeWrite(c net.Conn) {
	if tcp, ok := c.(*net.TCPConn); ok {
		_ = tcp.CloseWrite()
		return
	}
	_ = c.Close()
}

var proxyV2Signature = []byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A}

// proxyHeader tells the target who the client really is, in PROXY protocol v1 or v2.
func proxyHeader(version int, src, dst net.Addr) []byte {
	s, _ := netip.ParseAddrPort(src.String())
	d, _ := netip.ParseAddrPort(dst.String())
	sa, da := s.Addr().Unmap(), d.Addr().Unmap()
	if sa.Is4() != da.Is4() {
		sa, da = netip.AddrFrom16(sa.As16()), netip.AddrFrom16(da.As16())
	}
	if version == 1 {
		family := "TCP4"
		if !sa.Is4() {
			family = "TCP6"
		}
		return []byte(fmt.Sprintf("PROXY %s %s %s %d %d\r\n", family, sa, da, s.Port(), d.Port()))
	}
	var addrs []byte
	family := byte(0x11)
	if sa.Is4() {
		a, b := sa.As4(), da.As4()
		addrs = append(append(addrs, a[:]...), b[:]...)
	} else {
		family = 0x21
		a, b := sa.As16(), da.As16()
		addrs = append(append(addrs, a[:]...), b[:]...)
	}
	addrs = binary.BigEndian.AppendUint16(addrs, s.Port())
	addrs = binary.BigEndian.AppendUint16(addrs, d.Port())
	out := append([]byte{}, proxyV2Signature...)
	out = append(out, 0x21, family)
	out = binary.BigEndian.AppendUint16(out, uint16(len(addrs)))
	return append(out, addrs...)
}
