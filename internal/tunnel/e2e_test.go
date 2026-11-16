//go:build linux

package tunnel

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func echoServer(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				line, err := bufio.NewReader(c).ReadString('\n')
				if err == nil {
					_, _ = c.Write([]byte("echo " + line))
				}
			}()
		}
	}()
	return l.Addr().(*net.TCPAddr).Port
}

// TestRealReverseTunnelCarriesAForwardedPort runs the unmodified BackPack
// binary at both ends, as the panel does, and sends a line through the relay.
func TestRealReverseTunnelCarriesAForwardedPort(t *testing.T) {
	binary := os.Getenv("XUI_BACKPACK_BIN")
	if binary == "" {
		t.Skip("set XUI_BACKPACK_BIN to a BackPack release binary to run both ends of a reverse tunnel")
	}
	target, listen, tunnelPort := echoServer(t), freePort(t), freePort(t)
	relay := normalized(t, Tunnel{ID: 1, Name: "e2e", Kind: KindReverse, Role: RoleServer, Enabled: true, Settings: TunnelSettings{
		Transport: "tcpmux", Port: tunnelPort, Forwards: []PortForward{{Listen: strconv.Itoa(listen), Target: strconv.Itoa(target)}},
	}})
	panel := Mirror(relay, "127.0.0.1")
	panel.ID = 2

	dir := t.TempDir()
	bin := func() string { return binary }
	server, client := newManager(filepath.Join(dir, "relay"), bin), newManager(filepath.Join(dir, "panel"), bin)
	t.Cleanup(server.StopAll)
	t.Cleanup(client.StopAll)
	server.Reconcile([]Tunnel{relay})
	client.Reconcile([]Tunnel{panel})

	deadline := time.Now().Add(30 * time.Second)
	var reply string
	for time.Now().Before(deadline) && reply != "echo hello\n" {
		time.Sleep(500 * time.Millisecond)
		conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(listen)))
		if err != nil {
			continue
		}
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = conn.Write([]byte("hello\n"))
		reply, _ = bufio.NewReader(conn).ReadString('\n')
		_ = conn.Close()
	}
	if reply != "echo hello\n" {
		t.Fatalf("no answer through the relay's port %d, last reply %q", listen, reply)
	}

	// BackPack writes its snapshot every thirty seconds, starting at once.
	deadline = time.Now().Add(45 * time.Second)
	state := ""
	for time.Now().Before(deadline) && state != StateUp {
		client.Reconcile([]Tunnel{panel})
		if st := client.Statuses(); len(st) == 1 {
			state = st[0].State
		}
		time.Sleep(time.Second)
	}
	if state != StateUp {
		t.Fatalf("the panel's end reports %q, want %q", state, StateUp)
	}

	server.Reconcile(nil)
	if _, err := os.Stat(filepath.Join(dir, "relay", "t1.toml")); !os.IsNotExist(err) {
		t.Fatalf("a removed tunnel kept its configuration: %v", err)
	}
}
