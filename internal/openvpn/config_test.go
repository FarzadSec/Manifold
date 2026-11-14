package openvpn

import (
	"net/netip"
	"strings"
	"testing"
)

func configLines(inst Instance, f files) map[string]bool {
	lines := map[string]bool{}
	for _, line := range strings.Split(renderServerConfig(inst, f), "\n") {
		lines[line] = true
	}
	return lines
}

func TestServerConfigFollowsTheInbound(t *testing.T) {
	base := Instance{
		Id:     7,
		Port:   1194,
		Proto:  ProtoUDP,
		Subnet: netip.MustParsePrefix("10.20.0.0/20"),
		DNS:    []string{"1.1.1.1", "9.9.9.9"},
	}
	tests := []struct {
		name    string
		mutate  func(*Instance)
		want    []string
		wantNot []string
	}{
		{
			name: "UDP on every address",
			want: []string{
				"port 1194", "proto udp", "dev mfovpn7", "server 10.20.0.0 255.255.240.0",
				"explicit-exit-notify 1", `push "dhcp-option DNS 1.1.1.1"`, `push "dhcp-option DNS 9.9.9.9"`,
				"management-client-auth", "management-hold", "verify-client-cert none",
			},
			wantNot: []string{"local 0.0.0.0"},
		},
		{
			// OpenVPN refuses to start when explicit-exit-notify meets TCP.
			name:    "TCP",
			mutate:  func(i *Instance) { i.Proto = ProtoTCP },
			want:    []string{"proto tcp-server"},
			wantNot: []string{"explicit-exit-notify 1", "proto udp"},
		},
		{
			name:    "wildcard IPv4 listen is not a bind address",
			mutate:  func(i *Instance) { i.Listen = "0.0.0.0" },
			want:    []string{"proto udp"},
			wantNot: []string{"local 0.0.0.0"},
		},
		{
			name:   "IPv4 listen address",
			mutate: func(i *Instance) { i.Listen = "192.0.2.10" },
			want:   []string{"local 192.0.2.10", "proto udp"},
		},
		{
			name:   "IPv6 listen address needs the v6 transport",
			mutate: func(i *Instance) { i.Listen, i.Proto = "2001:db8::1", ProtoTCP },
			want:   []string{"local 2001:db8::1", "proto tcp6-server"},
		},
		{
			name:    "IPv6 wildcard listens dual-stack",
			mutate:  func(i *Instance) { i.Listen = "::" },
			want:    []string{"proto udp6"},
			wantNot: []string{"local ::"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := base
			if tt.mutate != nil {
				tt.mutate(&inst)
			}
			lines := configLines(inst, filesIn("/opt/manifold/openvpn", inst.Id))
			for _, want := range tt.want {
				if !lines[want] {
					t.Errorf("config lacks %q", want)
				}
			}
			for _, wantNot := range tt.wantNot {
				if lines[wantNot] {
					t.Errorf("config has %q", wantNot)
				}
			}
		})
	}
}

func TestServerConfigQuotesPaths(t *testing.T) {
	inst := Instance{Id: 7, Port: 1194, Proto: ProtoUDP, Subnet: netip.MustParsePrefix("10.8.0.0/24")}
	lines := configLines(inst, filesIn(`/opt/my "panel"/openvpn`, inst.Id))
	want := `ca "/opt/my \"panel\"/openvpn/7/ca.crt"`
	if !lines[want] {
		t.Fatalf("config lacks %q", want)
	}
}

func TestWriteRefusesASocketPathTheKernelWouldTruncate(t *testing.T) {
	f := filesIn("/tmp/"+strings.Repeat("d", 100), 7)
	err := f.write(Instance{Id: 7})
	if err == nil || !strings.Contains(err.Error(), "too long for a unix socket") {
		t.Fatalf("write error = %v, want the socket path refusal", err)
	}
}

func TestProfileCarriesTheLoginOnlyWhenAsked(t *testing.T) {
	profile := Profile{
		Remark:   "Germany\nremote evil.example 1",
		Host:     "vpn.example.com",
		Port:     443,
		Server:   ServerSettings{Proto: ProtoTCP, CACert: "CA-PEM\n", TLSCryptKey: "\nSTATIC-KEY\n"},
		Username: "alice",
		Password: "p@ss word",
	}

	embedded := profile.Render()
	for _, want := range []string{
		"# Germany remote evil.example 1\n",
		"\nremote vpn.example.com 443\n",
		"\nproto tcp-client\n",
		"\nverify-x509-name " + serverCommonName + " name\n",
		"<auth-user-pass>\nalice\np@ss word\n</auth-user-pass>\n",
		"<ca>\nCA-PEM\n</ca>\n",
		"<tls-crypt>\nSTATIC-KEY\n</tls-crypt>\n",
	} {
		if !strings.Contains(embedded, want) {
			t.Errorf("embedded profile lacks %q:\n%s", want, embedded)
		}
	}
	if strings.Contains(embedded, "\nauth-user-pass\n") {
		t.Error("embedded profile also asks for the login")
	}
	if strings.Count(embedded, "\nremote ") != 1 {
		t.Errorf("the remark added a remote line:\n%s", embedded)
	}

	profile.Server.PromptCredentials = true
	profile.Server.Proto = ProtoUDP
	prompting := profile.Render()
	if !strings.Contains(prompting, "\nauth-user-pass\n") || !strings.Contains(prompting, "\nproto udp\n") {
		t.Errorf("prompting profile:\n%s", prompting)
	}
	if strings.Contains(prompting, "p@ss word") || strings.Contains(prompting, "<auth-user-pass>") {
		t.Errorf("prompting profile leaks the login:\n%s", prompting)
	}
}

func TestProfileFileNameIsSafeToDownload(t *testing.T) {
	if got, want := ProfileFileName(`a/b c"@x.io`, 3), "a_b_c__x.io-3.ovpn"; got != want {
		t.Fatalf("ProfileFileName = %q, want %q", got, want)
	}
}
