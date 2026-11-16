//go:build linux

package openvpn

import (
	"bytes"
	"context"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	e2eInboundID  = 990
	e2eServerAddr = "192.168.251.1"
	// Reachable only through the server, and with no route back to the tunnel
	// subnet, so an answer from it proves both the tunnel and the NAT.
	e2eInternetAddr = "198.18.0.2"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type e2eClient struct {
	cmd  *exec.Cmd
	out  *lockedBuffer
	done chan struct{}
}

func sh(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.CommandContext(context.Background(), name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

func quiet(name string, args ...string) error {
	return exec.CommandContext(context.Background(), name, args...).Run()
}

// e2eNetwork builds a client namespace and an "internet" namespace, each
// joined to the host by its own veth pair.
func e2eNetwork(t *testing.T) (clientNS string) {
	t.Helper()
	clientNS, internetNS := "mfe2eclient", "mfe2einet"
	cleanup := func() {
		_ = quiet("ip", "netns", "del", clientNS)
		_ = quiet("ip", "netns", "del", internetNS)
		_ = quiet("ip", "link", "del", "mfe2ec0")
		_ = quiet("ip", "link", "del", "mfe2ei0")
	}
	cleanup()
	t.Cleanup(cleanup)

	sh(t, "ip", "netns", "add", clientNS)
	sh(t, "ip", "link", "add", "mfe2ec0", "type", "veth", "peer", "name", "mfe2ec1")
	sh(t, "ip", "link", "set", "mfe2ec1", "netns", clientNS)
	sh(t, "ip", "addr", "add", e2eServerAddr+"/24", "dev", "mfe2ec0")
	sh(t, "ip", "link", "set", "mfe2ec0", "up")
	sh(t, "ip", "netns", "exec", clientNS, "ip", "addr", "add", "192.168.251.2/24", "dev", "mfe2ec1")
	sh(t, "ip", "netns", "exec", clientNS, "ip", "link", "set", "mfe2ec1", "up")
	sh(t, "ip", "netns", "exec", clientNS, "ip", "link", "set", "lo", "up")
	sh(t, "ip", "netns", "exec", clientNS, "ip", "route", "add", "default", "via", e2eServerAddr)

	sh(t, "ip", "netns", "add", internetNS)
	sh(t, "ip", "link", "add", "mfe2ei0", "type", "veth", "peer", "name", "mfe2ei1")
	sh(t, "ip", "link", "set", "mfe2ei1", "netns", internetNS)
	sh(t, "ip", "addr", "add", "198.18.0.1/24", "dev", "mfe2ei0")
	sh(t, "ip", "link", "set", "mfe2ei0", "up")
	sh(t, "ip", "netns", "exec", internetNS, "ip", "addr", "add", e2eInternetAddr+"/24", "dev", "mfe2ei1")
	sh(t, "ip", "netns", "exec", internetNS, "ip", "link", "set", "mfe2ei1", "up")
	return clientNS
}

func startE2EClient(t *testing.T, ns, dir, name string, profile Profile) *e2eClient {
	t.Helper()
	path := filepath.Join(dir, name+".ovpn")
	if err := os.WriteFile(path, []byte(profile.Render()), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &e2eClient{out: &lockedBuffer{}, done: make(chan struct{})}
	c.cmd = exec.CommandContext(context.Background(), "ip", "netns", "exec", ns, "openvpn", "--config", path)
	c.cmd.Stdout = c.out
	c.cmd.Stderr = c.out
	if err := c.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = c.cmd.Wait()
		close(c.done)
	}()
	t.Cleanup(func() {
		_ = c.cmd.Process.Kill()
		<-c.done
	})
	return c
}

func (c *e2eClient) waitFor(t *testing.T, text string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(c.out.String(), text) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("client never logged %q:\n%s", text, c.out.String())
}

func (c *e2eClient) waitExit(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-c.done:
	case <-time.After(timeout):
		t.Fatalf("client is still running:\n%s", c.out.String())
	}
}

// TestRealServerAndClient drives a real openvpn server through the manager and
// real clients through its tunnel. It needs root, openvpn, nft and iproute2.
func TestRealServerAndClient(t *testing.T) {
	if os.Getenv("XUI_OPENVPN_E2E") == "" {
		t.Skip("set XUI_OPENVPN_E2E=1 and run as root to drive a real openvpn")
	}
	if os.Geteuid() != 0 {
		t.Fatal("XUI_OPENVPN_E2E is set but the test is not running as root")
	}
	clientNS := e2eNetwork(t)
	baseDir, err := os.MkdirTemp("/tmp", "mfovpn")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(baseDir) })

	pki, err := generatePKI()
	if err != nil {
		t.Fatal(err)
	}
	tlsCrypt, err := generateStaticKey()
	if err != nil {
		t.Fatal(err)
	}
	inst := Instance{
		Id:          e2eInboundID,
		Tag:         "inbound-e2e",
		Port:        21194,
		Proto:       ProtoUDP,
		Subnet:      netip.MustParsePrefix("10.199.0.0/24"),
		DNS:         []string{"1.1.1.1"},
		CACert:      pki.caCert,
		ServerCert:  pki.serverCert,
		ServerKey:   pki.serverKey,
		TLSCryptKey: tlsCrypt,
		Users: []User{
			{Name: "alice", Password: "alice-pass", MaxSessions: 1},
			{Name: "bob", Password: "bob-pass"},
		},
	}
	mgr := newManager(baseDir)
	t.Cleanup(mgr.StopAll)
	if err := mgr.Ensure(inst); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	profile := func(username, password string) Profile {
		return Profile{
			Host:     e2eServerAddr,
			Port:     inst.Port,
			Server:   ServerSettings{Proto: inst.Proto, CACert: inst.CACert, TLSCryptKey: inst.TLSCryptKey},
			Username: username,
			Password: password,
		}
	}

	alice := startE2EClient(t, clientNS, baseDir, "alice", profile("alice", "alice-pass"))
	alice.waitFor(t, "Initialization Sequence Completed", 40*time.Second)

	sh(t, "ip", "netns", "exec", clientNS, "ping", "-c", "20", "-i", "0.05", "-s", "1000", "-W", "3", e2eInternetAddr)

	var up, down int64
	var online []string
	deadline := time.Now().Add(4 * byteCountInterval * time.Second)
	for time.Now().Before(deadline) && (up < 20_000 || down < 20_000) {
		time.Sleep(time.Second)
		traffic, connected := mgr.CollectTraffic()
		for _, row := range traffic {
			if row.Email != "alice" || row.Tag != inst.Tag {
				t.Fatalf("traffic billed to %q on %q, want alice on %q", row.Email, row.Tag, inst.Tag)
			}
			up += row.Up
			down += row.Down
		}
		online = connected
	}
	// 20 echo requests and replies of 1000 bytes each crossed the tunnel.
	if up < 20_000 || down < 20_000 || up > 200_000 || down > 200_000 {
		t.Fatalf("alice was billed up=%d down=%d, want a little over 20000 each", up, down)
	}
	if !slices.Equal(online, []string{"alice"}) {
		t.Fatalf("online = %v, want [alice]", online)
	}

	t.Run("a second device over the cap is refused", func(t *testing.T) {
		second := startE2EClient(t, clientNS, baseDir, "alice2", profile("alice", "alice-pass"))
		second.waitExit(t, 40*time.Second)
		if out := second.out.String(); !strings.Contains(out, "AUTH_FAILED") || !strings.Contains(out, "maximum number of devices") {
			t.Fatalf("second device was not refused for the device cap:\n%s", out)
		}
	})

	t.Run("a wrong password is refused", func(t *testing.T) {
		intruder := startE2EClient(t, clientNS, baseDir, "intruder", profile("bob", "not-bobs-pass"))
		intruder.waitExit(t, 40*time.Second)
		if out := intruder.out.String(); !strings.Contains(out, "AUTH_FAILED") {
			t.Fatalf("wrong password was not refused:\n%s", out)
		}
	})

	t.Run("a removed account is cut off without a restart", func(t *testing.T) {
		before := mgr.servers[inst.Id].proc
		withoutAlice := inst
		withoutAlice.Users = []User{{Name: "bob", Password: "bob-pass"}}
		if err := mgr.Ensure(withoutAlice); err != nil {
			t.Fatalf("Ensure: %v", err)
		}
		if mgr.servers[inst.Id].proc != before {
			t.Fatal("the server was restarted for an account change")
		}
		alice.waitExit(t, 40*time.Second)
		if out := alice.out.String(); !strings.Contains(out, "AUTH_FAILED") {
			t.Fatalf("the removed account was not refused on reconnect:\n%s", out)
		}
	})

	mgr.StopAll()
	if quiet("nft", "list", "table", "ip", "manifold_"+egressOwner(inst.Id)) == nil {
		t.Fatal("the NAT table outlived the server")
	}
	if quiet("ip", "link", "show", deviceName(inst.Id)) == nil {
		t.Fatal("the tunnel device outlived the server")
	}
}

// TestRealServerWithProfileOptions proves a real openvpn starts on, and a real
// client accepts, the options an admin can now set: RSA keys, tls-auth, MTU.
func TestRealServerWithProfileOptions(t *testing.T) {
	if os.Getenv("XUI_OPENVPN_E2E") == "" {
		t.Skip("set XUI_OPENVPN_E2E=1 and run as root to drive a real openvpn")
	}
	if os.Geteuid() != 0 {
		t.Fatal("XUI_OPENVPN_E2E is set but the test is not running as root")
	}
	clientNS := e2eNetwork(t)
	baseDir, err := os.MkdirTemp("/tmp", "mfovpn")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(baseDir) })

	keys, err := GenerateKeys(PKIOptions{KeyType: KeyRSA2048, Days: 30, CAName: "Acme VPN CA", ServerName: "edge-1"})
	if err != nil {
		t.Fatal(err)
	}
	server := ServerSettings{
		Proto: ProtoUDP, TLSMode: TLSModeAuth, MTU: 1400, MSSFix: 1360, DataCiphers: []string{"AES-128-GCM"},
		ServerName: "edge-1", CACert: keys.CACert, TLSCryptKey: keys.TLSCryptKey, ClientDirectives: "connect-retry 1",
	}
	inst := Instance{
		Id: e2eInboundID + 1, Tag: "inbound-e2e-options", Port: 21197, Proto: ProtoUDP,
		Subnet: netip.MustParsePrefix("10.198.0.0/24"), DNS: []string{"1.1.1.1"},
		MTU: server.MTU, MSSFix: server.MSSFix, DataCiphers: server.DataCiphers, TLSMode: server.TLSMode,
		CACert: keys.CACert, ServerCert: keys.ServerCert, ServerKey: keys.ServerKey, TLSCryptKey: keys.TLSCryptKey,
		Users: []User{{Name: "carol", Password: "carol-pass"}},
	}
	mgr := newManager(baseDir)
	t.Cleanup(mgr.StopAll)
	if err := mgr.Ensure(inst); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	client := startE2EClient(t, clientNS, baseDir, "carol", Profile{
		Host: e2eServerAddr, Port: inst.Port, Server: server, Username: "carol", Password: "carol-pass",
	})
	client.waitFor(t, "Initialization Sequence Completed", 40*time.Second)
	if out := client.out.String(); !strings.Contains(out, "AES-128-GCM") {
		t.Fatalf("the client did not settle on the only allowed cipher:\n%s", out)
	}
	sh(t, "ip", "netns", "exec", clientNS, "ping", "-c", "5", "-i", "0.1", "-W", "3", e2eInternetAddr)
}
