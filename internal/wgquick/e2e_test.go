//go:build linux

package wgquick

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	e2eInboundID  = 991
	e2eServerAddr = "192.168.252.1"
	// Reachable only through the server, with no route back to the tunnel
	// subnet: an answer from it proves both the tunnel and the NAT.
	e2eInternetAddr = "198.19.0.2"
)

func sh(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.CommandContext(context.Background(), name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func quiet(name string, args ...string) error {
	return exec.CommandContext(context.Background(), name, args...).Run()
}

func keypair(t *testing.T) (private, public string) {
	t.Helper()
	private = sh(t, "wg", "genkey")
	cmd := exec.CommandContext(context.Background(), "wg", "pubkey")
	cmd.Stdin = strings.NewReader(private + "\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("wg pubkey: %v", err)
	}
	return private, strings.TrimSpace(string(out))
}

func e2eNetwork(t *testing.T) (clientNS string) {
	t.Helper()
	clientNS, internetNS := "mfwge2ec", "mfwge2ei"
	cleanup := func() {
		_ = quiet("ip", "netns", "del", clientNS)
		_ = quiet("ip", "netns", "del", internetNS)
		_ = quiet("ip", "link", "del", "mfwgc0")
		_ = quiet("ip", "link", "del", "mfwgi0")
	}
	cleanup()
	t.Cleanup(cleanup)

	sh(t, "ip", "netns", "add", clientNS)
	sh(t, "ip", "link", "add", "mfwgc0", "type", "veth", "peer", "name", "mfwgc1")
	sh(t, "ip", "link", "set", "mfwgc1", "netns", clientNS)
	sh(t, "ip", "addr", "add", e2eServerAddr+"/24", "dev", "mfwgc0")
	sh(t, "ip", "link", "set", "mfwgc0", "up")
	sh(t, "ip", "netns", "exec", clientNS, "ip", "addr", "add", "192.168.252.2/24", "dev", "mfwgc1")
	sh(t, "ip", "netns", "exec", clientNS, "ip", "link", "set", "mfwgc1", "up")
	sh(t, "ip", "netns", "exec", clientNS, "ip", "link", "set", "lo", "up")

	sh(t, "ip", "netns", "add", internetNS)
	sh(t, "ip", "link", "add", "mfwgi0", "type", "veth", "peer", "name", "mfwgi1")
	sh(t, "ip", "link", "set", "mfwgi1", "netns", internetNS)
	sh(t, "ip", "addr", "add", "198.19.0.1/24", "dev", "mfwgi0")
	sh(t, "ip", "link", "set", "mfwgi0", "up")
	sh(t, "ip", "netns", "exec", internetNS, "ip", "addr", "add", e2eInternetAddr+"/24", "dev", "mfwgi1")
	sh(t, "ip", "netns", "exec", internetNS, "ip", "link", "set", "mfwgi1", "up")
	return clientNS
}

func reaches(clientNS string) bool {
	return quiet("ip", "netns", "exec", clientNS, "ping", "-c", "2", "-i", "0.2", "-W", "2", e2eInternetAddr) == nil
}

func eventually(t *testing.T, what string, check func() bool) {
	t.Helper()
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(500 * time.Millisecond) {
		if check() {
			return
		}
	}
	t.Fatalf("timed out waiting for: %s", what)
}

// TestRealInterfaceAndPeer drives a real WireGuard interface through the
// manager and a real peer through its tunnel. It needs root, wg-quick, nft and iproute2.
func TestRealInterfaceAndPeer(t *testing.T) {
	if os.Getenv("XUI_WIREGUARD_E2E") == "" {
		t.Skip("set XUI_WIREGUARD_E2E=1 and run as root to drive a real WireGuard interface")
	}
	if os.Geteuid() != 0 {
		t.Fatal("XUI_WIREGUARD_E2E is set but the test is not running as root")
	}
	clientNS := e2eNetwork(t)
	// The real directory: where AppArmor confines wg-quick, it reads configs nowhere else.
	dir := configDir
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	serverPrivate, serverPublic := keypair(t)
	clientPrivate, clientPublic := keypair(t)
	inst := Instance{
		Id:         e2eInboundID,
		Tag:        "inbound-e2e",
		Interface:  "mfwge2e",
		ListenPort: 51898,
		PrivateKey: serverPrivate,
		Addresses:  []netip.Prefix{netip.MustParsePrefix("10.198.0.1/24")},
		Peers: []Peer{{
			Name:       "alice",
			PublicKey:  clientPublic,
			AllowedIPs: []netip.Prefix{netip.MustParsePrefix("10.198.0.2/32")},
		}},
	}
	mgr := newManager(dir, osSystem{})
	t.Cleanup(mgr.StopAll)
	if err := mgr.Ensure(inst); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	clientConf := filepath.Join(dir, "mfwge2ecl.conf")
	conf := fmt.Sprintf("[Interface]\nPrivateKey = %s\nAddress = 10.198.0.2/32\n\n[Peer]\nPublicKey = %s\nEndpoint = %s:%d\nAllowedIPs = 10.198.0.0/24, 198.19.0.0/24\nPersistentKeepalive = 1\n",
		clientPrivate, serverPublic, e2eServerAddr, inst.ListenPort)
	if err := os.WriteFile(clientConf, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = quiet("ip", "netns", "exec", clientNS, "wg-quick", "down", clientConf)
		_ = os.Remove(clientConf)
	})
	sh(t, "ip", "netns", "exec", clientNS, "wg-quick", "up", clientConf)

	eventually(t, "the peer to reach the internet through the tunnel and the NAT", func() bool { return reaches(clientNS) })
	sh(t, "ip", "netns", "exec", clientNS, "ping", "-c", "20", "-i", "0.05", "-s", "1000", "-W", "3", e2eInternetAddr)

	traffic, online := mgr.CollectTraffic()
	if len(traffic) != 1 || traffic[0].Email != "alice" || traffic[0].Tag != inst.Tag {
		t.Fatalf("traffic = %+v, want one row for alice on %s", traffic, inst.Tag)
	}
	// 20 echo requests and replies of 1000 bytes each crossed the tunnel.
	if up, down := traffic[0].Up, traffic[0].Down; up < 20_000 || down < 20_000 || up > 200_000 || down > 200_000 {
		t.Fatalf("alice was billed up=%d down=%d, want a little over 20000 each", up, down)
	}
	if !slices.Equal(online, []string{"alice"}) {
		t.Fatalf("online = %v, want [alice]", online)
	}

	t.Run("a removed peer is cut off and a restored one works again without a restart", func(t *testing.T) {
		withoutAlice := inst
		withoutAlice.Peers = nil
		if err := mgr.Ensure(withoutAlice); err != nil {
			t.Fatalf("Ensure: %v", err)
		}
		if reaches(clientNS) {
			t.Fatal("the removed peer still reaches the internet")
		}
		if err := mgr.Ensure(inst); err != nil {
			t.Fatalf("Ensure: %v", err)
		}
		eventually(t, "the restored peer to reach the internet again", func() bool { return reaches(clientNS) })
	})

	mgr.StopAll()
	if quiet("ip", "link", "show", inst.Interface) == nil {
		t.Fatal("the interface outlived the manager")
	}
	if quiet("nft", "list", "table", "ip", "manifold_"+egressOwner(inst.Id)) == nil {
		t.Fatal("the NAT table outlived the interface")
	}
	if _, err := os.Stat(configPath(dir, inst.Interface)); err == nil {
		t.Fatal("the config outlived the interface")
	}
}
