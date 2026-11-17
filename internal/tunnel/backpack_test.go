package tunnel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackPackConfigFollowsTheEnd(t *testing.T) {
	server := normalized(t, Tunnel{ID: 5, Name: "r", Kind: KindReverse, Role: RoleServer, Settings: TunnelSettings{
		Transport: "tcpmux", Port: 8443, Token: "0123456789abcdef0123", ProxyProtocol: true,
		Forwards: []PortForward{{Listen: "443"}, {Listen: "8080", Target: "80", Proto: ProtoBoth}, {Listen: "9000-9009", Target: "19000-19009"}},
	}})
	client := Mirror(server, "relay.example.com")
	client.ID = 9
	l3 := normalized(t, Tunnel{ID: 6, Name: "l", Kind: KindL3, Role: RoleServer, Settings: TunnelSettings{
		Carrier: "udp", Port: 9000, RemoteAddr: "2001:db8::2", Token: "0123456789abcdef0123", LocalIP: "10.9.0.1/30", PeerIP: "10.9.0.2",
		MTU: 1380, Forwards: []PortForward{{Listen: "443", Target: "2096"}},
	}})
	tests := []struct {
		name    string
		end     Tunnel
		want    []string
		wantNot []string
	}{
		{"reverse relay", server, []string{
			"[server]\n", `bind_addr = "0.0.0.0:8443"`, `transport = "tcpmux"`, `token = "0123456789abcdef0123"`,
			`ports = ["443=127.0.0.1:443", "8080=127.0.0.1:80", "9000-9009=19000-19009"]`,
			"accept_udp = true", "proxy_protocol = true", "skip_optz = true", "web_port = 0",
		}, []string{"[client]"}},
		{"reverse client", client, []string{
			"[client]\n", `remote_addr = "relay.example.com:8443"`, `transport = "tcpmux"`,
		}, []string{"ports =", "bind_addr"}},
		{"layer-3 relay dials IPv6", l3, []string{
			"[l3]\n", `mode = "dial"`, `addr = "[2001:db8::2]:9000"`, `iface = "mfbp6"`, `local_ip = "10.9.0.1/30"`,
			`peer_ip = "10.9.0.2"`, "mtu = 1380", `ports = ["443=2096"]`, "accept_udp = false",
		}, []string{"skip_optz"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text := renderBackPack(tt.end)
			for _, want := range tt.want {
				if !strings.Contains(text, want) {
					t.Errorf("config lacks %q:\n%s", want, text)
				}
			}
			for _, wantNot := range tt.wantNot {
				if strings.Contains(text, wantNot) {
					t.Errorf("config has %q:\n%s", wantNot, text)
				}
			}
		})
	}
}

func TestProbeWindowDecidesTheState(t *testing.T) {
	var w window
	for range 10 {
		w.add(result{rtt: 40e6})
	}
	if loss, rtt, recent, total := w.summary(); loss != 0 || rtt != 40e6 || recent != 0 || total != 10 {
		t.Fatalf("all answered: loss %v rtt %v recent %d total %d", loss, rtt, recent, total)
	}
	for range 3 {
		w.add(result{lost: true})
	}
	if loss, _, recent, _ := w.summary(); recent != 3 || loss < 23 || loss > 23.1 {
		t.Fatalf("three lost in a row: loss %v recent %d", loss, recent)
	}
	for range probeWindow {
		w.add(result{rtt: 1e6})
	}
	if loss, _, recent, total := w.summary(); loss != 0 || recent != 0 || total != probeWindow {
		t.Fatalf("old losses outlived the window: loss %v recent %d total %d", loss, recent, total)
	}
}

// The installed panel keeps its bin folder relative to its working directory, and BackPack
// runs in its config's folder; both paths it is given must still lead to their files.
func TestBackPackStartsFromTheRelativeBinFolderTheServiceUses(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.MkdirAll("bin/tunnels", 0o700); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncat \"$2\" > \"$2.seen\"\nsleep 30\n"
	if err := os.WriteFile("bin/backpack-test", []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("bin/tunnels/t1.toml", []byte("mode = \"test\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := startChild("bin/backpack-test", "bin/tunnels/t1.toml", "t1")
	if err != nil {
		t.Fatalf("BackPack did not start from the service's relative paths: %v", err)
	}
	t.Cleanup(c.stop)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if got, err := os.ReadFile(filepath.Join(root, "bin/tunnels/t1.toml.seen")); err == nil && string(got) == "mode = \"test\"\n" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("BackPack could not read its config through the path it was given")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !childRunningFor("bin/tunnels/t1.toml") {
		t.Fatal("the command line does not see the BackPack that runs this config")
	}
	t.Chdir(t.TempDir())
	if childRunningFor("bin/tunnels/t1.toml") {
		t.Fatal("another install's BackPack was taken for this one's")
	}
}
