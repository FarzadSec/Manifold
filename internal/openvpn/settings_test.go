package openvpn

import (
	"encoding/json"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func mustNormalize(t *testing.T, settings, previous string) InboundSettings {
	t.Helper()
	out, err := NormalizeSettings(settings, previous)
	if err != nil {
		t.Fatalf("NormalizeSettings: %v", err)
	}
	var parsed InboundSettings
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("normalized settings are not JSON: %v", err)
	}
	return parsed
}

func TestNormalizeSettingsGivesANewInboundWorkingKeys(t *testing.T) {
	parsed := mustNormalize(t, `{"clients":[{"email":"alice","password":"pw","enable":true}]}`, "")

	if parsed.Server.Proto != ProtoUDP || parsed.Server.Subnet != "10.8.0.0/24" {
		t.Fatalf("defaults = %s %s, want udp 10.8.0.0/24", parsed.Server.Proto, parsed.Server.Subnet)
	}
	if !slices.Equal(parsed.Server.DNS, []string{"1.1.1.1", "8.8.8.8"}) {
		t.Fatalf("default DNS = %v", parsed.Server.DNS)
	}
	if name, err := checkPKI(*parsed.Server); err != nil || name != serverCommonName {
		t.Fatalf("generated keys: name %q, err %v; want a valid chain named %q", name, err, serverCommonName)
	}
	if parsed.Server.ServerName != serverCommonName || parsed.Server.TLSMode != TLSModeCrypt {
		t.Fatalf("server name %q, tls mode %q", parsed.Server.ServerName, parsed.Server.TLSMode)
	}
	if err := checkStaticKey(parsed.Server.TLSCryptKey); err != nil {
		t.Fatalf("generated tls-crypt key: %v", err)
	}
	if len(parsed.Clients) != 1 || parsed.Clients[0].Email != "alice" || parsed.Clients[0].Password != "pw" {
		t.Fatalf("clients were not kept: %+v", parsed.Clients)
	}
}

// A form that never saw the private keys saves without them; issuing new ones
// would silently break every profile already handed out.
func TestNormalizeSettingsKeepsStoredKeysWhenASaveOmitsThem(t *testing.T) {
	stored, err := NormalizeSettings(`{}`, "")
	if err != nil {
		t.Fatal(err)
	}
	before := mustNormalize(t, stored, "")

	redacted := before.Server.ForProfiles()
	redacted.TLSCryptKey = ""
	redacted.Subnet = "10.20.0.7/22"
	edit, err := json.Marshal(InboundSettings{Server: &redacted, Clients: []model.Client{}})
	if err != nil {
		t.Fatal(err)
	}
	after := mustNormalize(t, string(edit), stored)

	if after.Server.CAKey != before.Server.CAKey || after.Server.ServerKey != before.Server.ServerKey ||
		after.Server.ServerCert != before.Server.ServerCert || after.Server.TLSCryptKey != before.Server.TLSCryptKey {
		t.Fatal("a save without keys replaced the stored keys")
	}
	if after.Server.Subnet != "10.20.0.0/22" {
		t.Fatalf("subnet = %s, want the network address 10.20.0.0/22", after.Server.Subnet)
	}
}

func TestNormalizeSettingsRejectsWhatOpenVPNWouldRefuse(t *testing.T) {
	other, err := generatePKI()
	if err != nil {
		t.Fatal(err)
	}
	own, err := generatePKI()
	if err != nil {
		t.Fatal(err)
	}
	foreignServer, err := json.Marshal(ServerSettings{
		CACert: own.caCert, CAKey: own.caKey, ServerCert: other.serverCert, ServerKey: other.serverKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	swappedKey, err := json.Marshal(ServerSettings{
		CACert: own.caCert, CAKey: own.caKey, ServerCert: own.serverCert, ServerKey: other.serverKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		settings string
		wantErr  string
	}{
		{name: "transport", settings: `{"server":{"proto":"sctp"}}`, wantErr: `proto "sctp" must be udp or tcp`},
		{name: "IPv6 subnet", settings: `{"server":{"subnet":"fd00::/64"}}`, wantErr: "is not an IPv4 network"},
		{name: "subnet too small for a pool", settings: `{"server":{"subnet":"10.8.0.0/30"}}`, wantErr: "must be between /16 and /29"},
		{name: "subnet larger than the pool", settings: `{"server":{"subnet":"10.0.0.0/8"}}`, wantErr: "must be between /16 and /29"},
		{name: "DNS name instead of address", settings: `{"server":{"dns":["dns.google"]}}`, wantErr: `DNS server "dns.google" is not an IPv4 address`},
		{name: "server certificate of another CA", settings: `{"server":` + string(foreignServer) + `}`, wantErr: "serverCert is not signed by caCert"},
		{name: "key of another certificate", settings: `{"server":` + string(swappedKey) + `}`, wantErr: "serverKey does not belong to serverCert"},
		{name: "MTU below what IPv4 needs", settings: `{"server":{"mtu":500}}`, wantErr: "mtu 500 must be 0 for the default or between 576 and 9000"},
		{name: "mssfix above any MTU", settings: `{"server":{"mssfix":10000}}`, wantErr: "mssfix 10000 must be"},
		{name: "cipher outside the allowed set", settings: `{"server":{"dataCiphers":["BF-CBC"]}}`, wantErr: `data cipher "BF-CBC" must be one of`},
		{name: "unknown control channel mode", settings: `{"server":{"tlsMode":"tls-crypt-v2"}}`, wantErr: `tlsMode "tls-crypt-v2" must be`},
		{name: "extra line with an inline block", settings: `{"server":{"clientDirectives":"<ca>\nx\n</ca>"}}`, wantErr: "is not one OpenVPN directive"},
		{name: "extra line the panel writes itself", settings: `{"server":{"clientDirectives":"remote evil.example 443"}}`, wantErr: `"remote" is written by the panel`},
		{name: "TLS floor OpenVPN lacks", settings: `{"server":{"tlsVersionMin":"1.1"}}`, wantErr: `tlsVersionMin "1.1" must be 1.2 or 1.3`},
		{name: "renegotiation below a minute", settings: `{"server":{"renegSec":30}}`, wantErr: "renegSec 30 must be -1 for never, 0 for the default or between 60 and 604800"},
		{name: "negative client cap", settings: `{"server":{"maxClients":-2}}`, wantErr: "maxClients cannot be negative"},
		{name: "search domain with a space", settings: `{"server":{"searchDomains":["corp example"]}}`, wantErr: `search domain "corp example" is not a domain name`},
		{name: "truncated tls-crypt key", settings: `{"server":{"tlsCryptKey":"-----BEGIN OpenVPN Static key V1-----\nabcd\n-----END OpenVPN Static key V1-----"}}`, wantErr: "tlsCryptKey must hold 256 bytes of hex"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeSettings(tt.settings, "")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestCredentialRules(t *testing.T) {
	names := map[string]bool{
		"alice":                  true,
		"a.b-c_d@x:y/z=1":        true,
		"":                       false,
		"has space":              false,
		"علی":                    false,
		"semi;colon":             false,
		strings.Repeat("a", 129): false,
	}
	for name, want := range names {
		if got := ValidateUsername(name) == nil; got != want {
			t.Errorf("ValidateUsername(%q) valid = %v, want %v", name, got, want)
		}
	}
	passwords := map[string]bool{
		"p@ss word=,\"'":         true,
		"":                       false,
		"line\nbreak":            false,
		strings.Repeat("p", 129): false,
	}
	for password, want := range passwords {
		if got := ValidatePassword(password) == nil; got != want {
			t.Errorf("ValidatePassword(%q) valid = %v, want %v", password, got, want)
		}
	}
}

func inboundWith(t *testing.T, mutate func(*InboundSettings), clients ...model.Client) *model.Inbound {
	t.Helper()
	base, err := NormalizeSettings(`{}`, "")
	if err != nil {
		t.Fatal(err)
	}
	var parsed InboundSettings
	if err := json.Unmarshal([]byte(base), &parsed); err != nil {
		t.Fatal(err)
	}
	parsed.Clients = clients
	if mutate != nil {
		mutate(&parsed)
	}
	raw, err := json.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	return &model.Inbound{Id: 4, Tag: "inbound-1194", Port: 1194, Protocol: model.OpenVPN, Settings: string(raw)}
}

func TestInstanceHoldsOnlyAccountsThatCanLogIn(t *testing.T) {
	ib := inboundWith(t, nil,
		model.Client{Email: "alice", Password: "pw", Enable: true, UserLimit: 2, LimitIP: 5},
		model.Client{Email: "disabled", Password: "pw", Enable: false},
		model.Client{Email: "no password", Password: "pw", Enable: true},
		model.Client{Email: "nopass", Enable: true},
		model.Client{Email: "bob", Password: "pw2", Enable: true, UserLimit: -3, LimitIP: 4},
	)
	inst, ok := InstanceFromInbound(ib)
	if !ok {
		t.Fatal("InstanceFromInbound refused a usable inbound")
	}
	want := []User{{Name: "alice", Password: "pw", MaxSessions: 2}, {Name: "bob", Password: "pw2"}}
	if !slices.Equal(inst.Users, want) {
		t.Fatalf("users = %+v, want %+v", inst.Users, want)
	}
	if inst.Subnet != netip.MustParsePrefix("10.8.0.0/24") || inst.Port != 1194 || inst.Proto != ProtoUDP {
		t.Fatalf("instance = %+v", inst)
	}
}

// A switched-off account must be refused by a live server, not time out against a closed port.
func TestInstanceWithoutAnEnabledClientStillRuns(t *testing.T) {
	inst, ok := InstanceFromInbound(inboundWith(t, nil, model.Client{Email: "alice", Password: "pw"}))
	if !ok || len(inst.Users) != 0 {
		t.Fatalf("instance = %+v ok=%v, want a server that accepts nobody", inst.Users, ok)
	}
}

func TestInstanceIsRefusedWhenNothingCouldRun(t *testing.T) {
	alice := model.Client{Email: "alice", Password: "pw", Enable: true}
	tests := []struct {
		name string
		ib   *model.Inbound
	}{
		{name: "keys missing", ib: inboundWith(t, func(s *InboundSettings) { s.Server.ServerKey = "" }, alice)},
		{name: "unix socket as listen address", ib: func() *model.Inbound {
			ib := inboundWith(t, nil, alice)
			ib.Listen = "/run/x.sock"
			return ib
		}()},
		{name: "another protocol", ib: func() *model.Inbound {
			ib := inboundWith(t, nil, alice)
			ib.Protocol = model.VLESS
			return ib
		}()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := InstanceFromInbound(tt.ib); ok {
				t.Fatal("InstanceFromInbound accepted it")
			}
		})
	}
}

// The fingerprints decide between applying a change live and restarting the
// server, which disconnects everyone.
func TestFingerprintsSeparateAccountChangesFromRestarts(t *testing.T) {
	alice := model.Client{Email: "alice", Password: "pw", Enable: true}
	bob := model.Client{Email: "bob", Password: "pw", Enable: true}
	base, _ := InstanceFromInbound(inboundWith(t, nil, alice, bob))

	reordered := base
	reordered.Users = []User{base.Users[1], base.Users[0]}
	if reordered.usersFingerprint() != base.usersFingerprint() {
		t.Fatal("client order changed the accounts fingerprint")
	}

	rekeyed := base
	rekeyed.Users = []User{{Name: "alice", Password: "other"}, base.Users[1]}
	if rekeyed.usersFingerprint() == base.usersFingerprint() {
		t.Fatal("a new password did not change the accounts fingerprint")
	}
	if rekeyed.structuralFingerprint() != base.structuralFingerprint() {
		t.Fatal("a new password would restart the server")
	}

	for name, mutate := range map[string]func(*Instance){
		"port":   func(i *Instance) { i.Port++ },
		"proto":  func(i *Instance) { i.Proto = ProtoTCP },
		"subnet": func(i *Instance) { i.Subnet = netip.MustParsePrefix("10.9.0.0/24") },
		"dns":    func(i *Instance) { i.DNS = []string{"9.9.9.9"} },
		"listen": func(i *Instance) { i.Listen = "192.0.2.1" },
	} {
		changed := base
		mutate(&changed)
		if changed.structuralFingerprint() == base.structuralFingerprint() {
			t.Errorf("a changed %s would not restart the server", name)
		}
	}
}

// A key set made for routers without ECDSA, with a name of the admin's choosing,
// must be accepted as it is, and profiles must then verify that name.
func TestNormalizeSettingsKeepsAnIssuedRSAKeySetAndItsName(t *testing.T) {
	keys, err := GenerateKeys(PKIOptions{KeyType: KeyRSA2048, Days: 30, CAName: "Acme VPN CA", ServerName: "vpn.acme.example"})
	if err != nil {
		t.Fatal(err)
	}
	server, err := json.Marshal(ServerSettings{
		CACert: keys.CACert, CAKey: keys.CAKey, ServerCert: keys.ServerCert, ServerKey: keys.ServerKey,
		TLSCryptKey: keys.TLSCryptKey, TLSMode: TLSModeAuth, MTU: 1400, DataCiphers: []string{"aes-128-gcm", "AES-128-GCM"},
		ClientDirectives: "  sndbuf 0\r\n# comment\n\nrcvbuf 0  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed := mustNormalize(t, `{"server":`+string(server)+`}`, "")
	if parsed.Server.CACert != keys.CACert || parsed.Server.ServerKey != keys.ServerKey || parsed.Server.TLSCryptKey != keys.TLSCryptKey {
		t.Fatal("the issued key set was replaced")
	}
	if parsed.Server.ServerName != "vpn.acme.example" {
		t.Fatalf("server name = %q, want the certificate's", parsed.Server.ServerName)
	}
	if !slices.Equal(parsed.Server.DataCiphers, []string{"AES-128-GCM"}) || parsed.Server.ClientDirectives != "sndbuf 0\nrcvbuf 0" {
		t.Fatalf("ciphers %v, directives %q", parsed.Server.DataCiphers, parsed.Server.ClientDirectives)
	}
	profile := Profile{Host: "h", Port: 1, Server: parsed.Server.ForProfiles()}.Render()
	if !strings.Contains(profile, "\nverify-x509-name vpn.acme.example name\n") {
		t.Fatalf("profile does not verify the certificate's name:\n%s", profile)
	}
}

func TestKeySetOptionsAreChecked(t *testing.T) {
	tests := []struct {
		name    string
		opts    PKIOptions
		wantErr string
	}{
		{name: "unknown key type", opts: PKIOptions{KeyType: "dsa"}, wantErr: `key type "dsa" must be`},
		{name: "a name verify-x509-name cannot carry", opts: PKIOptions{ServerName: "my server"}, wantErr: `server name "my server" must be`},
		{name: "a lifetime past a century", opts: PKIOptions{Days: 40000}, wantErr: "not 40000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := GenerateKeys(tt.opts)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestDescribeCertificateReportsWhatTheAdminChose(t *testing.T) {
	keys, err := GenerateKeys(PKIOptions{KeyType: KeyECDSAP384, Days: 10, CAName: "Acme VPN CA", ServerName: "edge-1"})
	if err != nil {
		t.Fatal(err)
	}
	info, err := DescribeCertificate(keys.ServerCert)
	if err != nil {
		t.Fatal(err)
	}
	if info.Subject != "edge-1" || info.Issuer != "Acme VPN CA" || info.KeyType != "ecdsa-p384" {
		t.Fatalf("info = %+v", info)
	}
	if days := info.NotAfter.Sub(info.NotBefore).Hours() / 24; days < 10 || days > 10.1 {
		t.Fatalf("certificate lasts %.2f days, want 10", days)
	}
	if len(info.SHA256) != 95 || strings.Count(info.SHA256, ":") != 31 {
		t.Fatalf("fingerprint %q is not 32 colon-separated bytes", info.SHA256)
	}
}
