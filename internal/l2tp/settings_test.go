package l2tp

import (
	"encoding/json"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func serverIn(t *testing.T, settings string) ServerSettings {
	t.Helper()
	var parsed InboundSettings
	if err := json.Unmarshal([]byte(settings), &parsed); err != nil || parsed.Server == nil {
		t.Fatalf("settings have no server block: %v\n%s", err, settings)
	}
	return *parsed.Server
}

func TestNormalizeGivesANewInboundWorkingDefaults(t *testing.T) {
	out, err := NormalizeSettings(`{"clients":[{"email":"alice","enable":true}]}`, "")
	if err != nil {
		t.Fatalf("NormalizeSettings: %v", err)
	}
	server := serverIn(t, out)
	if !server.RequiresIPsec() || server.Subnet != "10.9.0.0/24" || server.MTU != 1280 || !slices.Equal(server.DNS, []string{"1.1.1.1", "8.8.8.8"}) {
		t.Fatalf("defaults = %+v", server)
	}
	if ValidatePSK(server.PSK) != nil || len(server.PSK) != 24 {
		t.Fatalf("generated key %q is not one the panel would accept", server.PSK)
	}
	var parsed InboundSettings
	_ = json.Unmarshal([]byte(out), &parsed)
	if len(parsed.Clients) != 1 || len(parsed.Clients[0].Password) != 16 {
		t.Fatalf("a client saved without a password got %q", parsed.Clients[0].Password)
	}
}

// Every configured device carries the key, so an edit must never rotate it by accident.
func TestNormalizeKeepsTheStoredKeyWhenASaveLeavesItOut(t *testing.T) {
	stored := `{"server":{"psk":"the-stored-key-1","subnet":"10.9.0.0/24"}}`
	out, err := NormalizeSettings(`{"server":{"subnet":"10.20.0.0/22","ipsec":false}}`, stored)
	if err != nil {
		t.Fatalf("NormalizeSettings: %v", err)
	}
	server := serverIn(t, out)
	if server.PSK != "the-stored-key-1" || server.Subnet != "10.20.0.0/22" || server.RequiresIPsec() {
		t.Fatalf("server = %+v", server)
	}
	replaced, err := NormalizeSettings(`{"server":{"psk":"a-new-key-of-mine"}}`, stored)
	if err != nil || serverIn(t, replaced).PSK != "a-new-key-of-mine" {
		t.Fatalf("an explicit new key was not taken: %v %s", err, replaced)
	}
}

func TestNormalizeRefusesWhatTheDaemonsCouldNotRun(t *testing.T) {
	tests := []struct {
		name, settings, want string
	}{
		{"subnet that is no network", `{"server":{"subnet":"ten"}}`, `l2tp: subnet "ten" is not an IPv4 network such as 10.9.0.0/24`},
		{"subnet too small for a pool", `{"server":{"subnet":"10.9.0.0/30"}}`, `l2tp: subnet "10.9.0.0/30" must be between /16 and /29`},
		{"IPv6 DNS", `{"server":{"dns":["2606:4700::1111"]}}`, `l2tp: DNS server "2606:4700::1111" is not an IPv4 address`},
		{"more DNS servers than PPP carries", `{"server":{"dns":["1.1.1.1","8.8.8.8","9.9.9.9"]}}`, "l2tp: PPP hands a client at most two DNS servers"},
		{"key with a quote", `{"server":{"psk":"abc\"defgh"}}`, "l2tp: the IPsec key must be 8 to 64 letters, digits or ! @ # $ % ^ & * ( ) _ + = . , : ; ~ -"},
		{"short key", `{"server":{"psk":"short"}}`, "l2tp: the IPsec key must be 8 to 64 letters, digits or ! @ # $ % ^ & * ( ) _ + = . , : ; ~ -"},
		{"MTU below what IPv4 needs", `{"server":{"mtu":500}}`, "l2tp: the MTU must be between 576 and 1460"},
		{"login name with a space", `{"clients":[{"email":"al ice","password":"secret-pass"}]}`, `l2tp: "al ice" cannot be a login name: use 1 to 128 letters, digits or _ . @ : / = -`},
		{"password with a line break", `{"clients":[{"email":"alice","password":"a\nb"}]}`, "l2tp: a password cannot hold control characters"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeSettings(tt.settings, "")
			if got := errText(err); got != tt.want {
				t.Fatalf("error = %q, want %q", got, tt.want)
			}
		})
	}
}

func l2tpInbound(t *testing.T, settings string) *model.Inbound {
	t.Helper()
	normalized, err := NormalizeSettings(settings, "")
	if err != nil {
		t.Fatal(err)
	}
	return &model.Inbound{Id: 4, Tag: "inbound-1701", Port: Port, Protocol: model.L2TP, Settings: normalized}
}

func TestInstanceServesEnabledAccountsFromThePoolBehindTheServer(t *testing.T) {
	inst, ok := InstanceFromInbound(l2tpInbound(t, `{"server":{"psk":"shared-key-123","subnet":"10.9.4.0/22","dns":["9.9.9.9"],"mtu":1400},"clients":[
		{"email":"alice","password":"alice-pass","enable":true,"userLimit":2,"limitIp":5},
		{"email":"bob","password":"bob-pass","enable":false},
		{"email":"carol","password":"carol-pass","enable":true,"limitIp":3}]}`))
	if !ok {
		t.Fatal("InstanceFromInbound refused a valid inbound")
	}
	if !inst.IPsec || inst.PSK != "shared-key-123" || inst.MTU != 1400 || !slices.Equal(inst.DNS, []string{"9.9.9.9"}) {
		t.Fatalf("instance = %+v", inst)
	}
	first, last := inst.pool()
	if inst.serverAddress() != netip.MustParseAddr("10.9.4.1") || first != netip.MustParseAddr("10.9.4.2") || last != netip.MustParseAddr("10.9.7.254") {
		t.Fatalf("server %s, pool %s-%s", inst.serverAddress(), first, last)
	}
	want := []User{{Name: "alice", Password: "alice-pass", MaxSessions: 2}, {Name: "carol", Password: "carol-pass"}}
	if !slices.Equal(inst.Users, want) {
		t.Fatalf("users = %+v, want %+v", inst.Users, want)
	}
}

// With every account switched off the server still answers, so a customer is
// told the login was refused instead of waiting on a dead port.
func TestInstanceWithoutAccountsStillRuns(t *testing.T) {
	inst, ok := InstanceFromInbound(l2tpInbound(t, `{"clients":[{"email":"alice","password":"alice-pass","enable":false}]}`))
	if !ok || len(inst.Users) != 0 {
		t.Fatalf("instance = %+v (ok=%v), want a server without users", inst, ok)
	}
}

func TestInstanceIsRefusedWhenItCannotBeAServer(t *testing.T) {
	valid := l2tpInbound(t, `{}`)
	otherProtocol := *valid
	otherProtocol.Protocol = model.VLESS
	otherPort := *valid
	otherPort.Port = 1702
	unnormalized := *valid
	unnormalized.Settings = `{"server":{"subnet":"10.9.0.0/24"}}`
	for name, ib := range map[string]*model.Inbound{
		"another protocol": &otherProtocol,
		// Every L2TP client dials 1701; no app lets its user choose.
		"another port":         &otherPort,
		"settings never saved": &unnormalized,
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := InstanceFromInbound(ib); ok {
				t.Fatal("InstanceFromInbound accepted it")
			}
		})
	}
}

func TestOnlyWhatNeedsARestartMovesTheStructuralFingerprint(t *testing.T) {
	base, _ := InstanceFromInbound(l2tpInbound(t, `{"server":{"psk":"shared-key-123"},"clients":[{"email":"alice","password":"alice-pass","enable":true}]}`))
	moreUsers, _ := InstanceFromInbound(l2tpInbound(t, `{"server":{"psk":"shared-key-123"},"clients":[{"email":"alice","password":"alice-pass","enable":true},{"email":"bob","password":"bob-pass","enable":true}]}`))
	otherKey, _ := InstanceFromInbound(l2tpInbound(t, `{"server":{"psk":"another-key-123"},"clients":[{"email":"alice","password":"alice-pass","enable":true}]}`))
	if base.structuralFingerprint() != moreUsers.structuralFingerprint() {
		t.Fatal("a new account would restart the server and drop every session")
	}
	if base.usersFingerprint() == moreUsers.usersFingerprint() {
		t.Fatal("a new account does not reach the running server")
	}
	if base.structuralFingerprint() == otherKey.structuralFingerprint() {
		t.Fatal("a new IPsec key is not applied")
	}
	if !strings.Contains(errText(ValidateUsername(`corp\alice`)), "cannot be a login name") {
		t.Fatal("a backslash would be read as a Windows domain and never match the account")
	}
}
