package tunnel

import (
	"reflect"
	"strings"
	"testing"
)

func greClient() Tunnel {
	return Tunnel{
		Name: "uk relay", Kind: KindGRE, Role: RoleClient, Enabled: true,
		Settings: TunnelSettings{
			RemoteAddr: "198.51.100.7", LocalIP: "10.80.0.2/30", PeerIP: "10.80.0.1",
			Forwards: []PortForward{{Listen: "443"}, {Listen: "2201", Target: "22", Proto: ProtoBoth}},
		},
	}
}

func TestNormalizeFillsWhatEachKindNeeds(t *testing.T) {
	gre, err := Normalize(greClient())
	if err != nil {
		t.Fatal(err)
	}
	if gre.Settings.TTL != 255 || gre.Settings.NAT != NATMasquerade || len(gre.PairID) != 24 {
		t.Fatalf("GRE defaults: ttl %d, nat %q, pair %q", gre.Settings.TTL, gre.Settings.NAT, gre.PairID)
	}
	if gre.Settings.Forwards[0].Proto != ProtoTCP || gre.Settings.Forwards[1].Target != "22" {
		t.Fatalf("forwards = %+v", gre.Settings.Forwards)
	}

	reverse, err := Normalize(Tunnel{
		Name: "iran", Kind: KindReverse, Role: RoleClient,
		Settings: TunnelSettings{Transport: "tcpmux", Port: 8443, RemoteAddr: "relay.example.com", LocalIP: "10.0.0.1/30", MTU: 1400},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reverse.Settings.Token) != 48 {
		t.Fatalf("token %q was not generated", reverse.Settings.Token)
	}
	if reverse.Settings.LocalIP != "" || reverse.Settings.MTU != 0 {
		t.Fatalf("a reverse tunnel kept fields of a kernel link: %+v", reverse.Settings)
	}
}

func TestNormalizeRefusesWhatCouldNotRun(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Tunnel)
		wantErr string
	}{
		{"IPv6 peer for an IPv4 GRE link", func(t *Tunnel) { t.Settings.RemoteAddr = "2001:db8::7" }, "a gre link needs an IPv4 address"},
		{"host name for a kernel link", func(t *Tunnel) { t.Settings.RemoteAddr = "relay.example.com" }, "must be an IP address"},
		{"peer outside the prefix", func(t *Tunnel) { t.Settings.PeerIP = "10.80.0.9" }, "must be another address inside 10.80.0.0/30"},
		{"peer is this end", func(t *Tunnel) { t.Settings.PeerIP = "10.80.0.2" }, "must be another address inside"},
		{"a /31 leaves no peer", func(t *Tunnel) { t.Settings.LocalIP = "10.80.0.2/31" }, "leaves no room"},
		{"SIT with IPv4 inside", func(t *Tunnel) { t.Kind = KindSIT }, "must be IPv6"},
		{"key on IPIP", func(t *Tunnel) { t.Kind, t.Settings.Key = KindIPIP, 7 }, "only GRE links carry a key"},
		{"range remapped on a kernel link", func(t *Tunnel) {
			t.Settings.Forwards = []PortForward{{Listen: "1000-1009", Target: "2000-2009"}}
		}, "to the same ports only"},
		{"port forwarded twice", func(t *Tunnel) {
			t.Settings.Forwards = []PortForward{{Listen: "440-450"}, {Listen: "443", Proto: ProtoBoth}}
		}, "port 443 is forwarded twice"},
		{"unknown kind", func(t *Tunnel) { t.Kind = "vxlan" }, `kind "vxlan" is not one of`},
		{"reverse client without its relay", func(t *Tunnel) {
			t.Kind, t.Settings = KindReverse, TunnelSettings{Transport: "tcp", Port: 443}
		}, "the relay's address"},
		{"UDP alone over BackPack", func(t *Tunnel) {
			t.Kind, t.Settings = KindReverse, TunnelSettings{Transport: "tcp", Port: 443, RemoteAddr: "r.example", Forwards: []PortForward{{Listen: "53", Proto: ProtoUDP}}}
		}, "not UDP alone"},
		{"token with spaces", func(t *Tunnel) {
			t.Kind, t.Settings = KindReverse, TunnelSettings{Transport: "tcp", Port: 443, RemoteAddr: "r.example", Token: "a token with spaces"}
		}, "a token is 16 to 128"},
		{"layer-3 dialer without the other address", func(t *Tunnel) {
			t.Kind, t.Settings = KindL3, TunnelSettings{Carrier: "udp", Port: 9000, Dialer: RoleClient, LocalIP: "10.9.0.2/30", PeerIP: "10.9.0.1"}
		}, "the side that dials needs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tun := greClient()
			tt.mutate(&tun)
			_, err := Normalize(tun)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// The code one panel shows must make the other panel the matching end: same
// secret and ports, addresses swapped, and this panel's address as its remote.
func TestPairingCodeMakesTheOtherEnd(t *testing.T) {
	tests := []struct {
		name       string
		end        Tunnel
		wantRemote string
	}{
		{"GRE from the client", greClient(), "203.0.113.10"},
		{"reverse from the client leaves the relay's remote empty", Tunnel{Name: "r", Kind: KindReverse, Role: RoleClient, Settings: TunnelSettings{
			Transport: "wssmux", Port: 443, RemoteAddr: "relay.example.com", Forwards: []PortForward{{Listen: "8080", Target: "80"}},
		}}, ""},
		{"reverse from the relay tells the client where to dial", Tunnel{Name: "r", Kind: KindReverse, Role: RoleServer, Settings: TunnelSettings{
			Transport: "kcp", Port: 4000,
		}}, "203.0.113.10"},
		{"layer-3 where the relay dials", Tunnel{Name: "l", Kind: KindL3, Role: RoleClient, Settings: TunnelSettings{
			Carrier: "udp", Port: 9000, LocalIP: "10.9.0.2/30", PeerIP: "10.9.0.1",
		}}, "203.0.113.10"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			end, err := Normalize(tt.end)
			if err != nil {
				t.Fatal(err)
			}
			code, err := PairingCode(end, "203.0.113.10")
			if err != nil {
				t.Fatal(err)
			}
			other, err := ParsePairingCode(code)
			if err != nil {
				t.Fatal(err)
			}
			if other.Role != Opposite(end.Role) || other.PairID != end.PairID || other.Kind != end.Kind {
				t.Fatalf("other end %+v does not pair with %+v", other, end)
			}
			if other.Settings.RemoteAddr != tt.wantRemote {
				t.Fatalf("other end's remote = %q, want %q", other.Settings.RemoteAddr, tt.wantRemote)
			}
			if other.Settings.Token != end.Settings.Token || !reflect.DeepEqual(other.Settings.Forwards, end.Settings.Forwards) {
				t.Fatalf("secret or forwards differ: %+v vs %+v", other.Settings, end.Settings)
			}
			if end.HasInterface() && (other.Settings.PeerIP != "10.80.0.2" && other.Settings.PeerIP != "10.9.0.2") {
				t.Fatalf("other end's peer = %q, want this end's own tunnel address", other.Settings.PeerIP)
			}
		})
	}
}

func TestParsePairingCodeRefusesForeignText(t *testing.T) {
	for _, code := range []string{"backpack://abc", PairingScheme + "!!!", PairingScheme + base64URL([]byte(`{"v":2}`))} {
		if _, err := ParsePairingCode(code); err == nil {
			t.Errorf("code %q was accepted", code)
		}
	}
}

func TestListenPortsCoverTheRelaysPorts(t *testing.T) {
	end, err := Normalize(Tunnel{Name: "r", Kind: KindReverse, Role: RoleServer, Settings: TunnelSettings{
		Transport: "quic", Port: 4000, Forwards: []PortForward{{Listen: "8000-8002", Proto: ProtoBoth}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]string{4000: ProtoUDP, 8000: ProtoBoth, 8001: ProtoBoth, 8002: ProtoBoth}
	if got := end.ListenPorts(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ports = %v, want %v", got, want)
	}
	end.Role = RoleClient
	if got := end.ListenPorts(); len(got) != 0 {
		t.Fatalf("a reverse client listens on %v", got)
	}
}
