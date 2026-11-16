package speedlimit

import (
	"io"
	"net"
	"net/netip"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The real-kernel tests replace the host's manifold_speed table, so they run only when asked.
func requireKernel(t *testing.T) *Limiter {
	t.Helper()
	if os.Getenv("XUI_SPEED_E2E") != "1" {
		t.Skip("set XUI_SPEED_E2E=1 (as root, with nft) to police real sockets")
	}
	l := New()
	t.Cleanup(func() { _ = l.Apply(nil) })
	return l
}

func TestKernelAcceptsTheTableAndEveryDelta(t *testing.T) {
	l := requireKernel(t)
	steps := [][]Account{
		{{ID: 12, Mbps: 20, Addrs: []netip.Prefix{pfx("10.8.0.5/32"), pfx("fd00::5/128")}}, {ID: 7, Mbps: 1, Ifaces: []string{"ppp3"}}},
		{{ID: 12, Mbps: 40, Addrs: []netip.Prefix{pfx("10.8.0.6/32")}}, {ID: 7, Mbps: 1, Ifaces: []string{"ppp4"}}, {ID: 8, Mbps: 3}},
		{{ID: 8, Mbps: 3, Addrs: []netip.Prefix{pfx("10.8.0.6/32")}}},
	}
	for i, accounts := range steps {
		if err := l.Apply(accounts); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	out, err := osSystem{}.run("", "nft", "list", "table", "inet", table)
	if err != nil {
		t.Fatalf("listing: %v %s", err, out)
	}
	for _, want := range []string{"limit u8_up", "10.8.0.6 : \"u8_up\"", "0x4e000008 : \"u8_down\""} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
	for _, gone := range []string{"u12_", "u7_", "ppp"} {
		if strings.Contains(out, gone) {
			t.Errorf("table still holds %q:\n%s", gone, out)
		}
	}
}

func TestKernelHoldsAMarkedSocketToItsRate(t *testing.T) {
	l := requireKernel(t)
	if err := l.Apply([]Account{{ID: 12, Mbps: 8}}); err != nil {
		t.Fatal(err)
	}
	for _, upload := range []bool{true, false} {
		mbps := measure(t, MarkBase|12, upload)
		// The first second also spends the bucket's initial second of credit.
		if mbps < 5 || mbps > 12 {
			t.Errorf("upload=%v ran at %.1f Mbps, want about 8", upload, mbps)
		}
	}
	if err := l.Apply(nil); err != nil {
		t.Fatal(err)
	}
	if mbps := measure(t, MarkBase|12, false); mbps < 100 {
		t.Errorf("after the limit was lifted the socket ran at %.1f Mbps", mbps)
	}
}

// measure moves data for four seconds over loopback through a socket carrying mark
// and returns the rate of the last three, past the bucket's initial credit.
func measure(t *testing.T, mark uint32, upload bool) float64 {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	buf := make([]byte, 64*1024)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		if upload {
			_, _ = io.Copy(io.Discard, c)
			return
		}
		for {
			if _, err := c.Write(buf); err != nil {
				return
			}
		}
	}()
	d := net.Dialer{Control: func(_, _ string, rc syscall.RawConn) error {
		var serr error
		_ = rc.Control(func(fd uintptr) {
			serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, int(mark))
		})
		return serr
	}}
	c, err := d.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	start := time.Now()
	var counted int64
	for time.Since(start) < 4*time.Second {
		_ = c.SetDeadline(time.Now().Add(300 * time.Millisecond))
		var n int
		if upload {
			n, _ = c.Write(buf)
		} else {
			n, _ = c.Read(buf)
		}
		if time.Since(start) > time.Second {
			counted += int64(n)
		}
	}
	return float64(counted) * 8 / (time.Since(start) - time.Second).Seconds() / 1e6
}
