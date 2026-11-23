//go:build linux

package tunnel

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

const relayNS = "mfturelay"

// TestRelayHelper runs the relay's end inside its namespace for the kernel link
// test, which starts this binary again under ip netns exec.
func TestRelayHelper(t *testing.T) {
	spec := os.Getenv("XUI_TUNNEL_HELPER")
	if spec == "" {
		t.Skip("started by TestRealKernelLinkForwardsAPort")
	}
	var relay Tunnel
	if err := json.Unmarshal([]byte(spec), &relay); err != nil {
		t.Fatal(err)
	}
	for _, f := range relay.Settings.ReverseForwards {
		port, _ := strconv.Atoi(f.Target)
		sourceEcho(t, port)
	}
	m := newManager(t.TempDir(), func() string { return "" })
	m.Reconcile([]Tunnel{relay})
	_, _ = io.Copy(io.Discard, os.Stdin)
	m.RemoveAll()
}

func run(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.CommandContext(context.Background(), args[0], args[1:]...).CombinedOutput(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
}

// kernelNetwork joins this namespace (the panel's server) to a relay namespace,
// and the relay to a user namespace that has no route to the panel at all.
func kernelNetwork(t *testing.T) {
	t.Helper()
	cleanup := func() {
		for _, ns := range []string{relayNS, "mftuuser"} {
			_ = exec.CommandContext(context.Background(), "ip", "netns", "del", ns).Run()
		}
		_ = exec.CommandContext(context.Background(), "ip", "link", "del", "mftuv0").Run()
	}
	cleanup()
	t.Cleanup(cleanup)
	run(t, "ip", "netns", "add", relayNS)
	run(t, "ip", "netns", "add", "mftuuser")
	run(t, "ip", "link", "add", "mftuv0", "type", "veth", "peer", "name", "mftuv1")
	run(t, "ip", "link", "set", "mftuv1", "netns", relayNS)
	run(t, "ip", "addr", "add", "192.168.252.1/24", "dev", "mftuv0")
	run(t, "ip", "link", "set", "mftuv0", "up")
	run(t, "ip", "-n", relayNS, "addr", "add", "192.168.252.2/24", "dev", "mftuv1")
	run(t, "ip", "-n", relayNS, "link", "set", "mftuv1", "up")
	run(t, "ip", "-n", relayNS, "link", "set", "lo", "up")
	run(t, "ip", "-n", relayNS, "link", "add", "mftuv2", "type", "veth", "peer", "name", "mftuv3")
	run(t, "ip", "-n", relayNS, "link", "set", "mftuv3", "netns", "mftuuser")
	run(t, "ip", "-n", relayNS, "addr", "add", "198.18.5.1/24", "dev", "mftuv2")
	run(t, "ip", "-n", relayNS, "link", "set", "mftuv2", "up")
	run(t, "ip", "-n", "mftuuser", "addr", "add", "198.18.5.2/24", "dev", "mftuv3")
	run(t, "ip", "-n", "mftuuser", "link", "set", "mftuv3", "up")
	run(t, "ip", "-n", "mftuuser", "link", "set", "lo", "up")
}

// sourceEcho answers each connection with the address it came from.
func sourceEcho(t *testing.T, port int) {
	t.Helper()
	l, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", strconv.Itoa(port)))
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
			host, _, _ := net.SplitHostPort(c.RemoteAddr().String())
			_, _ = c.Write([]byte(host + "\n"))
			_ = c.Close()
		}
	}()
}

// TestRealKernelLinkForwardsAPort builds a real GRE link between this namespace
// and a relay namespace, and connects through the relay's forwarded port from a
// third namespace, once masqueraded and once keeping the user's address.
func TestRealKernelLinkForwardsAPort(t *testing.T) {
	if os.Getenv("XUI_TUNNEL_E2E") == "" {
		t.Skip("set XUI_TUNNEL_E2E=1 and run as root to build real kernel links")
	}
	if os.Geteuid() != 0 {
		t.Fatal("XUI_TUNNEL_E2E is set but the test is not running as root")
	}
	kernelNetwork(t)
	sourceEcho(t, 18080)

	for _, tc := range []struct {
		nat        string
		wantSource string
	}{
		{NATMasquerade, "10.81.0.1"},
		{NATPreserve, "198.18.5.2"},
	} {
		t.Run(tc.nat, func(t *testing.T) {
			panel := normalized(t, Tunnel{ID: 41, Name: "e2e", Kind: KindGRE, Role: RoleClient, Enabled: true, Settings: TunnelSettings{
				LocalAddr: "192.168.252.1", RemoteAddr: "192.168.252.2", LocalIP: "10.81.0.2/30", PeerIP: "10.81.0.1",
				NAT: tc.nat, Forwards: []PortForward{{Listen: "8080", Target: "18080"}},
				ReverseForwards: []PortForward{{Listen: "9090", Target: "19090"}},
			}})
			relay := Mirror(panel, "192.168.252.1")
			relay.ID = 42
			relay.Settings.LocalAddr = "192.168.252.2"

			spec, _ := json.Marshal(relay)
			self, _ := os.Executable()
			helper := exec.CommandContext(context.Background(), "ip", "netns", "exec", relayNS, self, "-test.run", "^TestRelayHelper$")
			helper.Env = append(os.Environ(), "XUI_TUNNEL_HELPER="+string(spec))
			stdin, _ := helper.StdinPipe()
			out := &strings.Builder{}
			helper.Stdout, helper.Stderr = out, out
			if err := helper.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = stdin.Close()
				_ = helper.Wait()
			})

			m := newManager(t.TempDir(), func() string { return "" })
			t.Cleanup(m.RemoveAll)
			m.Reconcile([]Tunnel{panel})

			var got string
			deadline := time.Now().Add(20 * time.Second)
			for time.Now().Before(deadline) && got != tc.wantSource {
				time.Sleep(500 * time.Millisecond)
				reply, err := exec.CommandContext(context.Background(), "ip", "netns", "exec", "mftuuser",
					"bash", "-c", "exec 3<>/dev/tcp/198.18.5.1/8080 && timeout 2 head -n1 <&3").Output()
				if err == nil {
					got = strings.TrimSpace(string(reply))
				}
			}
			if got != tc.wantSource {
				t.Fatalf("the panel saw the user as %q through the relay, want %q\nrelay:\n%s", got, tc.wantSource, out.String())
			}

			deadline = time.Now().Add(20 * time.Second)
			state := ""
			for time.Now().Before(deadline) && state != StateUp {
				time.Sleep(time.Second)
				if st := m.Statuses(); len(st) == 1 {
					state = st[0].State
				}
			}
			if state != StateUp {
				t.Fatalf("the link reports %q, want %q", state, StateUp)
			}

			// The panel's own forward: the relay reaches a service of its own through the panel's port.
			back := ""
			deadline = time.Now().Add(20 * time.Second)
			for time.Now().Before(deadline) && back != "10.81.0.2" {
				reply, err := exec.CommandContext(context.Background(), "ip", "netns", "exec", relayNS,
					"bash", "-c", "exec 3<>/dev/tcp/192.168.252.1/9090 && timeout 2 head -n1 <&3").Output()
				if err == nil {
					back = strings.TrimSpace(string(reply))
				}
				time.Sleep(500 * time.Millisecond)
			}
			if back != "10.81.0.2" {
				t.Fatalf("through the panel's port 9090 the relay's service saw %q, want the panel's tunnel address", back)
			}
		})
	}
}
