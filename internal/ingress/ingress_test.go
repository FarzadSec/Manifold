package ingress

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// backend accepts connections and reports the first bytes each one received.
func backend(t *testing.T) (string, chan []byte) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got := make(chan []byte, 8)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
				buf := make([]byte, 4096)
				n, _ := io.ReadAtLeast(c, buf, 1)
				got <- buf[:n]
				_, _ = c.Write([]byte("hello from backend"))
			}()
		}
	}()
	return ln.Addr().String(), got
}

func startRouter(t *testing.T, l Listener) (*Manager, string) {
	t.Helper()
	m := NewManager()
	t.Cleanup(m.Close)
	l.Id, l.Listen, l.Port = 1, "127.0.0.1", 0
	m.Apply([]Listener{l})
	if errs := m.Errors(); len(errs) > 0 {
		t.Fatal(errs)
	}
	return m, m.Addr(1).String()
}

// tlsHello sends a ClientHello for name and returns what came back, without finishing the handshake.
func tlsHello(t *testing.T, addr, name string, alpn ...string) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	c := tls.Client(conn, &tls.Config{ServerName: name, NextProtos: alpn, InsecureSkipVerify: true}) // lgtm[go/disabled-certificate-check]
	_ = c.Handshake()
}

func receive(t *testing.T, ch chan []byte) []byte {
	t.Helper()
	select {
	case b := <-ch:
		return b
	case <-time.After(3 * time.Second):
		t.Fatal("the backend received nothing")
		return nil
	}
}

func nothing(t *testing.T, ch chan []byte) {
	t.Helper()
	select {
	case b := <-ch:
		t.Fatalf("a backend that should get nothing received %q", b)
	case <-time.After(300 * time.Millisecond):
	}
}

// The ClientHello reaches the backend its name chose, byte for byte, so the backend
// (REALITY included) sees the handshake the client sent.
func TestTLSConnectionsGoWhereTheirNameSays(t *testing.T) {
	panelAddr, panel := backend(t)
	realityAddr, reality := backend(t)
	wildAddr, wild := backend(t)
	defAddr, def := backend(t)
	_, addr := startRouter(t, Listener{
		Routes: []Route{
			{Hosts: []string{"panel.example.com"}, Target: panelAddr},
			{Hosts: []string{"www.speedtest.net"}, Target: realityAddr},
			{Hosts: []string{"*.cdn.example.com"}, Target: wildAddr},
		},
		Default: Route{Target: defAddr},
	})

	tlsHello(t, addr, "panel.example.com")
	if got := receive(t, panel); got[0] != handshakeType || !bytes.Contains(got, []byte("panel.example.com")) {
		t.Fatalf("panel backend got %q, want the ClientHello", got)
	}
	tlsHello(t, addr, "www.speedtest.net")
	receive(t, reality)
	tlsHello(t, addr, "u1.cdn.example.com")
	receive(t, wild)
	tlsHello(t, addr, "a.b.cdn.example.com")
	receive(t, def)
	nothing(t, wild)
	nothing(t, panel)
}

// A plain HTTP request is routed by its Host header, port and all.
func TestHTTPRequestsGoWhereTheirHostSays(t *testing.T) {
	siteAddr, site := backend(t)
	_, addr := startRouter(t, Listener{Routes: []Route{{Hosts: []string{"site.example.com"}, Target: siteAddr}}})
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	req := "GET /x HTTP/1.1\r\nHost: site.example.com:8080\r\nUser-Agent: t\r\n\r\n"
	_, _ = conn.Write([]byte(req))
	if got := receive(t, site); string(got) != req {
		t.Fatalf("backend got %q, want the request untouched", got)
	}
	reply, _ := bufio.NewReader(conn).ReadString('d')
	if !strings.HasPrefix(reply, "hello from backend") {
		t.Fatalf("client got %q back", reply)
	}
}

// With no route and no default the connection is closed, never sent anywhere.
func TestUnknownNamesAreClosedWithoutADefault(t *testing.T) {
	aAddr, a := backend(t)
	_, addr := startRouter(t, Listener{Routes: []Route{{Hosts: []string{"a.example.com"}, Target: aAddr}}})
	tlsHello(t, addr, "b.example.com")
	nothing(t, a)
}

// ALPN narrows a route: h2 clients of one name can go elsewhere than the rest.
func TestALPNNarrowsARoute(t *testing.T) {
	h2Addr, h2 := backend(t)
	restAddr, rest := backend(t)
	_, addr := startRouter(t, Listener{Routes: []Route{
		{Hosts: []string{"x.example.com"}, ALPN: []string{"h2"}, Target: h2Addr},
		{Hosts: []string{"x.example.com"}, Target: restAddr},
	}})
	tlsHello(t, addr, "x.example.com", "h2", "http/1.1")
	receive(t, h2)
	tlsHello(t, addr, "x.example.com", "http/1.1")
	receive(t, rest)
}

// The PROXY header names the real client, so a target's per-user address limit still works.
func TestProxyProtocolNamesTheClient(t *testing.T) {
	for _, version := range []int{1, 2} {
		targetAddr, target := backend(t)
		_, addr := startRouter(t, Listener{Default: Route{Target: targetAddr, ProxyProtocol: version}})
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = conn.Write([]byte("GET / HTTP/1.1\r\nHost: a\r\n\r\n"))
		got := receive(t, target)
		conn.Close()
		local := conn.LocalAddr().(*net.TCPAddr)
		switch version {
		case 1:
			want := "PROXY TCP4 127.0.0.1 127.0.0.1 " + strconv.Itoa(local.Port) + " "
			if !strings.HasPrefix(string(got), want) || !strings.Contains(string(got), "\r\nGET / HTTP/1.1") {
				t.Fatalf("v1 header = %q, want it to start %q", got, want)
			}
		case 2:
			if !bytes.HasPrefix(got, proxyV2Signature) || got[13] != 0x11 {
				t.Fatalf("v2 header = % x", got[:16])
			}
			if port := int(got[24])<<8 | int(got[25]); port != local.Port {
				t.Fatalf("v2 source port = %d, want %d", port, local.Port)
			}
		}
	}
}

// A ClientHello split over two records, as some clients send, still yields its name.
func TestAClientHelloSplitAcrossRecordsIsRead(t *testing.T) {
	var buf bytes.Buffer
	srv, cli := net.Pipe()
	go func() {
		c := tls.Client(cli, &tls.Config{ServerName: "split.example.com", InsecureSkipVerify: true}) // lgtm[go/disabled-certificate-check]
		_ = c.Handshake()
	}()
	head := make([]byte, 5)
	_, _ = io.ReadFull(srv, head)
	n := int(head[3])<<8 | int(head[4])
	body := make([]byte, n)
	_, _ = io.ReadFull(srv, body)
	cut := n / 2
	for _, part := range [][]byte{body[:cut], body[cut:]} {
		buf.Write([]byte{0x16, head[1], head[2], byte(len(part) >> 8), byte(len(part))})
		buf.Write(part)
	}
	p, err := readFirst(bufio.NewReader(&buf))
	if err != nil || p.host != "split.example.com" || !p.tls {
		t.Fatalf("peek = %+v, %v", p.host, err)
	}
}

// Changing routes takes effect on the next connection without rebinding the port.
func TestRoutesChangeWithoutRebinding(t *testing.T) {
	oldAddr, old := backend(t)
	newAddr, fresh := backend(t)
	m, addr := startRouter(t, Listener{Default: Route{Target: oldAddr}})
	port := m.Addr(1).(*net.TCPAddr).Port
	m.Apply([]Listener{{Id: 1, Listen: "127.0.0.1", Port: 0, Default: Route{Target: newAddr}}})
	if a := m.Addr(1); a == nil || a.(*net.TCPAddr).Port != port {
		t.Fatalf("the listener was rebound: %v", a)
	}
	tlsHello(t, addr, "any.example.com")
	receive(t, fresh)
	nothing(t, old)
}
