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
		Id:      7,
		Port:    1194,
		Proto:   ProtoUDP,
		TLSMode: TLSModeCrypt,
		Subnet:  netip.MustParsePrefix("10.20.0.0/20"),
		DNS:     []string{"1.1.1.1", "9.9.9.9"},
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
				`tls-crypt "/opt/manifold/openvpn/7/tls-crypt.key"`,
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
			name:    "the default keeps tls-crypt and routes everything",
			want:    []string{`push "redirect-gateway def1 bypass-dhcp"`},
			wantNot: []string{"auth SHA256", "tun-mtu 0", "mssfix 0", "data-ciphers "},
		},
		{
			name: "tunnel options reach the server",
			mutate: func(i *Instance) {
				i.MTU, i.MSSFix, i.DataCiphers, i.SplitTunnel = 1400, 1360, []string{"AES-128-GCM", "CHACHA20-POLY1305"}, true
			},
			want:    []string{"tun-mtu 1400", "mssfix 1360", "data-ciphers AES-128-GCM:CHACHA20-POLY1305"},
			wantNot: []string{`push "redirect-gateway def1 bypass-dhcp"`},
		},
		{
			name:    "tls-auth for apps without tls-crypt",
			mutate:  func(i *Instance) { i.TLSMode = TLSModeAuth },
			want:    []string{`tls-auth "/opt/manifold/openvpn/7/tls-crypt.key" 0`, "auth SHA256"},
			wantNot: []string{`tls-crypt "/opt/manifold/openvpn/7/tls-crypt.key"`},
		},
		{
			name:    "no control channel key for routers that know none",
			mutate:  func(i *Instance) { i.TLSMode = TLSModeNone },
			wantNot: []string{`tls-crypt "/opt/manifold/openvpn/7/tls-crypt.key"`, `tls-auth "/opt/manifold/openvpn/7/tls-crypt.key" 0`},
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

func TestProfileFollowsTheInboundsProfileOptions(t *testing.T) {
	profile := Profile{
		Host: "vpn.example.com", Port: 1194, Username: "alice", Password: "pw",
		Server: ServerSettings{
			Proto: ProtoUDP, CACert: "CA-PEM", TLSCryptKey: "STATIC-KEY", TLSMode: TLSModeAuth,
			MTU: 1400, MSSFix: 1360, DataCiphers: []string{"AES-128-GCM"}, ServerName: "edge-1",
			ClientDirectives: "sndbuf 0\nrcvbuf 0",
		},
	}
	rendered := profile.Render()
	for _, want := range []string{
		"\nverify-x509-name edge-1 name\n", "\ntun-mtu 1400\n", "\nmssfix 1360\n", "\ndata-ciphers AES-128-GCM\n",
		"\nsndbuf 0\nrcvbuf 0\n", "\nauth SHA256\nkey-direction 1\n<tls-auth>\nSTATIC-KEY\n</tls-auth>\n",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("profile lacks %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "<tls-crypt>") {
		t.Errorf("a tls-auth profile also carries tls-crypt:\n%s", rendered)
	}

	profile.Server.TLSMode = TLSModeNone
	if bare := profile.Render(); strings.Contains(bare, "<tls-auth>") || strings.Contains(bare, "<tls-crypt>") || strings.Contains(bare, "key-direction") {
		t.Errorf("a profile without a control channel key still carries one:\n%s", bare)
	}
}
