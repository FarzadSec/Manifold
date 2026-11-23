//go:build linux

package egress

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// nsSystem runs this package's commands inside a network namespace.
type nsSystem struct{ ns string }

func (s nsSystem) run(stdin, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "ip", append([]string{"netns", "exec", s.ns, name}, args...)...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

func (nsSystem) has(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func (s nsSystem) enableIPv4Forwarding() error {
	_, err := s.run("", "sysctl", "-qw", "net.ipv4.ip_forward=1")
	return err
}

func sh(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.CommandContext(context.Background(), args[0], args[1:]...).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// inNamespace runs fn on a thread that has joined the named network namespace;
// sockets fn opens stay in that namespace.
func inNamespace(t *testing.T, ns string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		// The thread is left locked and so ends with the goroutine, never back in the pool.
		f, err := os.Open("/var/run/netns/" + ns)
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		if err := unix.Setns(int(f.Fd()), unix.CLONE_NEWNET); err != nil {
			t.Error(err)
			return
		}
		fn()
	}()
	<-done
}

func maxSegment(t *testing.T, c net.Conn) int {
	t.Helper()
	raw, err := c.(*net.TCPConn).SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var mss int
	var serr error
	if err := raw.Control(func(fd uintptr) { mss, serr = unix.GetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_MAXSEG) }); err != nil || serr != nil {
		t.Fatalf("TCP_MAXSEG: %v %v", err, serr)
	}
	return mss
}

// narrowPath links user, relay, far and server namespaces in a line, with a
// 1400-byte link between relay and far, as a tunnel narrows a relay's path.
func narrowPath(t *testing.T) {
	t.Helper()
	names := []string{"mfeuser", "mferelay", "mfefar", "mfesrv"}
	for _, ns := range names {
		_ = exec.CommandContext(context.Background(), "ip", "netns", "del", ns).Run()
		sh(t, "ip", "netns", "add", ns)
		sh(t, "ip", "-n", ns, "link", "set", "lo", "up")
	}
	t.Cleanup(func() {
		for _, ns := range names {
			_ = exec.CommandContext(context.Background(), "ip", "netns", "del", ns).Run()
		}
	})
	links := []struct {
		a, b, devA, devB, addrA, addrB string
		mtu                            int
	}{
		{"mfeuser", "mferelay", "eth0", "mfe-user", "10.91.1.2/24", "10.91.1.1/24", 1500},
		{"mferelay", "mfefar", "mfe-narrow", "mfe-relay", "10.91.2.1/24", "10.91.2.2/24", 1400},
		{"mfefar", "mfesrv", "mfe-srv", "eth0", "10.91.3.1/24", "10.91.3.2/24", 1500},
	}
	for _, l := range links {
		sh(t, "ip", "link", "add", "mfetmp-a", "netns", l.a, "type", "veth", "peer", "name", "mfetmp-b", "netns", l.b)
		sh(t, "ip", "-n", l.a, "link", "set", "mfetmp-a", "name", l.devA, "mtu", fmt.Sprint(l.mtu), "up")
		sh(t, "ip", "-n", l.b, "link", "set", "mfetmp-b", "name", l.devB, "mtu", fmt.Sprint(l.mtu), "up")
		sh(t, "ip", "-n", l.a, "addr", "add", l.addrA, "dev", l.devA)
		sh(t, "ip", "-n", l.b, "addr", "add", l.addrB, "dev", l.devB)
	}
	sh(t, "ip", "-n", "mfeuser", "route", "add", "default", "via", "10.91.1.1")
	sh(t, "ip", "-n", "mferelay", "route", "add", "10.91.3.0/24", "via", "10.91.2.2")
	sh(t, "ip", "-n", "mfefar", "route", "add", "10.91.1.0/24", "via", "10.91.2.1")
	sh(t, "ip", "-n", "mfesrv", "route", "add", "default", "via", "10.91.3.1")
	sh(t, "ip", "netns", "exec", "mfefar", "sysctl", "-qw", "net.ipv4.ip_forward=1")
}

// TestRealRelayFitsTCPToTheNarrowLink forwards a relay port over a 1400-byte link
// with the user's address kept, and checks both ends were told to send what fits it.
func TestRealRelayFitsTCPToTheNarrowLink(t *testing.T) {
	if os.Getenv("XUI_EGRESS_E2E") == "" {
		t.Skip("set XUI_EGRESS_E2E=1 and run as root to forward through real namespaces")
	}
	if os.Geteuid() != 0 {
		t.Fatal("XUI_EGRESS_E2E is set but the test is not running as root")
	}
	narrowPath(t)
	prev := host
	host = nsSystem{ns: "mferelay"}
	t.Cleanup(func() { host = prev })
	rules := []PortForward{{Proto: "tcp", Low: 8080, High: 8080, TargetLow: 18080}}
	if err := ApplyForwards("e2e", "mfe-narrow", netip.MustParseAddr("10.91.3.2"), rules, false); err != nil {
		t.Fatal(err)
	}

	var ln net.Listener
	inNamespace(t, "mfesrv", func() {
		var err error
		if ln, err = net.Listen("tcp4", "10.91.3.2:18080"); err != nil {
			t.Error(err)
		}
	})
	if ln == nil {
		t.FailNow()
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- c
		}
	}()

	var user net.Conn
	inNamespace(t, "mfeuser", func() {
		d := net.Dialer{Timeout: 5 * time.Second}
		var err error
		if user, err = d.DialContext(context.Background(), "tcp4", "10.91.1.1:8080"); err != nil {
			t.Error(err)
		}
	})
	if user == nil {
		t.FailNow()
	}
	defer user.Close()
	var server net.Conn
	select {
	case server = <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("the forwarded connection never reached the server")
	}
	defer server.Close()

	// Both ends sit on 1500-byte links and advertise 1460; only the relay knows the path is 1400,
	// which carries 1360 bytes of TCP payload (less any TCP options).
	if got := maxSegment(t, user); got > 1360 {
		t.Errorf("the user sends segments of %d bytes, more than the 1400-byte link carries", got)
	}
	if got := maxSegment(t, server); got > 1360 {
		t.Errorf("the server sends segments of %d bytes, more than the 1400-byte link carries", got)
	}
	RemoveTables("e2e")
}
