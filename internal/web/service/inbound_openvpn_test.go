package service

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/openvpn"
)

func openvpnSettings(t *testing.T, settings string) string {
	t.Helper()
	normalized, err := openvpn.NormalizeSettings(settings, "")
	if err != nil {
		t.Fatalf("NormalizeSettings: %v", err)
	}
	return normalized
}

func openvpnInstalled(t *testing.T, installed bool) {
	t.Helper()
	prev := openvpn.FindBinary
	openvpn.FindBinary = func() (string, error) {
		if installed {
			return "/usr/sbin/openvpn", nil
		}
		return "", openvpn.ErrNotInstalled
	}
	t.Cleanup(func() { openvpn.FindBinary = prev })
}

func userNames(users []openvpn.User) []string {
	names := make([]string, 0, len(users))
	for _, u := range users {
		names = append(names, u.Name)
	}
	return names
}

func TestDesiredOpenVPNInstancesServeOnlyAccountsWithinTheirLimits(t *testing.T) {
	setupConflictDB(t)
	svc := &InboundService{}

	seedInboundConflict(t, "ovpn-served", "", 47001, model.OpenVPN, "", openvpnSettings(t, `{"clients":[`+
		`{"email":"alice","password":"pw-a","enable":true,"limitIp":2},`+
		`{"email":"bob","password":"pw-b","enable":true},`+
		`{"email":"carol","password":"pw-c","enable":false}]}`))
	served := loadInboundByTag(t, "ovpn-served")
	seedClientTraffic(t, served.Id, "alice", true)
	seedClientTraffic(t, served.Id, "bob", false)
	seedClientTraffic(t, served.Id, "carol", true)

	seedInboundConflict(t, "ovpn-all-depleted", "", 47002, model.OpenVPN, "",
		openvpnSettings(t, `{"clients":[{"email":"dave","password":"pw-d","enable":true}]}`))
	seedClientTraffic(t, loadInboundByTag(t, "ovpn-all-depleted").Id, "dave", false)

	nodeID := 5
	seedInboundConflictNode(t, "ovpn-node-owned", "", 47003, model.OpenVPN, "",
		openvpnSettings(t, `{"clients":[{"email":"erin","password":"pw-e","enable":true}]}`), &nodeID)

	instances, err := svc.DesiredOpenVPNInstances()
	if err != nil {
		t.Fatalf("DesiredOpenVPNInstances: %v", err)
	}
	// The inbound whose only account is used up stays up, to refuse that account in words.
	if len(instances) != 2 || instances[0].Id != served.Id || instances[1].Tag != "ovpn-all-depleted" {
		t.Fatalf("instances = %+v, want this panel's two inbounds", instances)
	}
	want := []openvpn.User{{Name: "alice", Password: "pw-a", MaxSessions: 2}}
	if !slices.Equal(instances[0].Users, want) {
		t.Fatalf("users = %+v, want %+v", instances[0].Users, want)
	}
	if len(instances[1].Users) != 0 {
		t.Fatalf("the used-up account is still served: %+v", instances[1].Users)
	}

	// The save path and the job must agree, or the two would keep cutting each other's sessions.
	built, err := svc.buildInboundForLocalRuntime(database.GetDB(), served)
	if err != nil {
		t.Fatalf("buildInboundForLocalRuntime: %v", err)
	}
	pushed, ok := openvpn.InstanceFromInbound(built)
	if !ok || !slices.Equal(pushed.Users, instances[0].Users) {
		t.Fatalf("save path serves %v, job serves %v", userNames(pushed.Users), userNames(instances[0].Users))
	}
}

func TestAddInboundCompletesAnOpenVPNInbound(t *testing.T) {
	setupConflictDB(t)
	openvpnInstalled(t, true)
	svc := &InboundService{}

	added, _, err := svc.AddInbound(&model.Inbound{
		UserId:   1,
		Remark:   "ovpn",
		Enable:   true,
		Port:     47010,
		Protocol: model.OpenVPN,
		Tag:      "inbound-47010",
		Settings: `{"server":{"proto":"tcp"},"clients":[{"email":"alice","enable":true}]}`,
	})
	if err != nil {
		t.Fatalf("AddInbound: %v", err)
	}
	stored := loadInboundByTag(t, added.Tag)
	inst, ok := openvpn.InstanceFromInbound(stored)
	if !ok {
		t.Fatalf("the stored inbound cannot be served: %s", stored.Settings)
	}
	if inst.Proto != openvpn.ProtoTCP || len(inst.Users) != 1 || inst.Users[0].Name != "alice" || len(inst.Users[0].Password) != 16 {
		t.Fatalf("instance = proto %s users %+v, want tcp and alice with a generated 16-character password", inst.Proto, inst.Users)
	}

	// The edit form never receives the private keys; saving it must not issue new ones.
	edit := *stored
	var settings openvpn.InboundSettings
	if err := json.Unmarshal([]byte(stored.Settings), &settings); err != nil {
		t.Fatal(err)
	}
	redacted := settings.Server.ForProfiles()
	redacted.DNS = []string{"9.9.9.9"}
	settings.Server = &redacted
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	edit.Settings = string(raw)
	if _, _, err := svc.UpdateInbound(&edit); err != nil {
		t.Fatalf("UpdateInbound: %v", err)
	}
	after, ok := openvpn.InstanceFromInbound(loadInboundByTag(t, added.Tag))
	if !ok {
		t.Fatal("the edited inbound cannot be served")
	}
	if after.ServerKey != inst.ServerKey || after.CACert != inst.CACert || after.TLSCryptKey != inst.TLSCryptKey {
		t.Fatal("an edit without keys replaced the inbound's keys")
	}
	if !slices.Equal(after.DNS, []string{"9.9.9.9"}) {
		t.Fatalf("DNS = %v, want the edited value", after.DNS)
	}
}

func TestAddInboundRefusesOpenVPNItCouldNotRun(t *testing.T) {
	tests := []struct {
		name      string
		installed bool
		settings  string
		nodeID    *int
		wantErr   string
	}{
		{name: "openvpn missing on this server", settings: `{"clients":[{"email":"alice","enable":true}]}`, wantErr: "OpenVPN is not installed on this server"},
		{name: "login name OpenVPN would rewrite", installed: true, settings: `{"clients":[{"email":"ali ce","enable":true}]}`, wantErr: "cannot be a login name"},
		{name: "subnet without room for clients", installed: true, settings: `{"server":{"subnet":"10.8.0.0/30"}}`, wantErr: "must be between /16 and /29"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupConflictDB(t)
			openvpnInstalled(t, tt.installed)
			_, _, err := (&InboundService{}).AddInbound(&model.Inbound{
				UserId: 1, Enable: true, Port: 47020, Protocol: model.OpenVPN, Tag: "inbound-47020", Settings: tt.settings, NodeID: tt.nodeID,
			})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("AddInbound error = %v, want it to contain %q", err, tt.wantErr)
			}
			var count int64
			if err := database.GetDB().Model(&model.Inbound{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("a refused inbound was stored (count=%d, err=%v)", count, err)
			}
		})
	}
}

func TestOpenVPNListensOnItsOwnTransportOnly(t *testing.T) {
	udp := openvpnSettings(t, `{}`)
	tcp := openvpnSettings(t, `{"server":{"proto":"tcp"}}`)
	if got := inboundTransports(model.OpenVPN, "", udp); got != transportUDP {
		t.Fatalf("UDP inbound transports = %v, want UDP only", got)
	}
	if got := inboundTransports(model.OpenVPN, "", tcp); got != transportTCP {
		t.Fatalf("TCP inbound transports = %v, want TCP only", got)
	}
}
