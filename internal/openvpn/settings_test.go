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
	if err := checkPKI(*parsed.Server); err != nil {
		t.Fatalf("generated keys do not form a valid chain: %v", err)
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
		model.Client{Email: "alice", Password: "pw", Enable: true, LimitIP: 2},
		model.Client{Email: "disabled", Password: "pw", Enable: false},
		model.Client{Email: "no password", Password: "pw", Enable: true},
		model.Client{Email: "nopass", Enable: true},
		model.Client{Email: "bob", Password: "pw2", Enable: true, LimitIP: -3},
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

func TestInstanceIsRefusedWhenNothingCouldRun(t *testing.T) {
	alice := model.Client{Email: "alice", Password: "pw", Enable: true}
	tests := []struct {
		name string
		ib   *model.Inbound
	}{
		{name: "no enabled client", ib: inboundWith(t, nil, model.Client{Email: "alice", Password: "pw"})},
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
