package tunnel

import (
	"strings"
	"testing"
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
