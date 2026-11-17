package service

import (
	"slices"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/l2tp"
)

func l2tpSettings(t *testing.T, settings string) string {
	t.Helper()
	normalized, err := l2tp.NormalizeSettings(settings, "")
	if err != nil {
		t.Fatalf("NormalizeSettings: %v", err)
	}
	return normalized
}

func l2tpPrograms(t *testing.T, missing ...string) {
	t.Helper()
	prev := l2tp.MissingPrograms
	l2tp.MissingPrograms = func(bool) []string { return missing }
	t.Cleanup(func() { l2tp.MissingPrograms = prev })
}

func TestDesiredL2TPInstancesServeOnlyAccountsWithinTheirLimits(t *testing.T) {
	setupConflictDB(t)
	svc := &InboundService{}

	seedInboundConflict(t, "l2tp-served", "", l2tp.Port, model.L2TP, "", l2tpSettings(t, `{"clients":[`+
		`{"email":"alice","password":"pw-alice","enable":true,"userLimit":2,"limitIp":5},`+
		`{"email":"bob","password":"pw-bob","enable":true},`+
		`{"email":"carol","password":"pw-carol","enable":false}]}`))
	served := loadInboundByTag(t, "l2tp-served")
	seedClientTraffic(t, served.Id, "alice", true)
	seedClientTraffic(t, served.Id, "bob", false)
	seedClientTraffic(t, served.Id, "carol", true)

	nodeID := 5
	seedInboundConflictNode(t, "l2tp-node-owned", "", l2tp.Port, model.L2TP, "",
		l2tpSettings(t, `{"clients":[{"email":"erin","password":"pw-erin","enable":true}]}`), &nodeID)

	instances, err := svc.DesiredL2TPInstances()
	if err != nil {
		t.Fatalf("DesiredL2TPInstances: %v", err)
	}
	if len(instances) != 1 || instances[0].Id != served.Id {
		t.Fatalf("instances = %+v, want only this panel's inbound", instances)
	}
	want := []l2tp.User{{Name: "alice", Password: "pw-alice", MaxSessions: 2}}
	if !slices.Equal(instances[0].Users, want) {
		t.Fatalf("users = %+v, want %+v: bob is out of quota and carol is disabled", instances[0].Users, want)
	}

	// The save path and the job must agree, or the two would keep cutting each other's sessions.
	built, err := svc.buildInboundForLocalRuntime(database.GetDB(), served)
	if err != nil {
		t.Fatalf("buildInboundForLocalRuntime: %v", err)
	}
	pushed, ok := l2tp.InstanceFromInbound(built)
	if !ok || !slices.Equal(pushed.Users, instances[0].Users) {
		t.Fatalf("save path serves %+v, job serves %+v", pushed.Users, instances[0].Users)
	}
}

func TestAddInboundCompletesAnL2TPInboundAndKeepsItsKeyOnEdit(t *testing.T) {
	setupConflictDB(t)
	l2tpPrograms(t)
	svc := &InboundService{}

	added, _, err := svc.AddInbound(&model.Inbound{
		UserId: 1, Remark: "l2tp", Enable: true, Port: l2tp.Port, Protocol: model.L2TP, Tag: "inbound-1701",
		Settings: `{"clients":[{"email":"alice","enable":true}]}`,
	})
	if err != nil {
		t.Fatalf("AddInbound: %v", err)
	}
	stored := loadInboundByTag(t, added.Tag)
	inst, ok := l2tp.InstanceFromInbound(stored)
	if !ok {
		t.Fatalf("the stored inbound cannot be served: %s", stored.Settings)
	}
	if !inst.IPsec || len(inst.PSK) != 24 || len(inst.Users) != 1 || inst.Users[0].Name != "alice" || len(inst.Users[0].Password) != 16 {
		t.Fatalf("instance = %+v, want IPsec with a generated key and alice with a generated password", inst)
	}

	edit := *stored
	edit.Settings = `{"server":{"subnet":"10.30.0.0/24"},"clients":[{"email":"alice","password":"` + inst.Users[0].Password + `","enable":true}]}`
	if _, _, err := svc.UpdateInbound(&edit); err != nil {
		t.Fatalf("UpdateInbound: %v", err)
	}
	after, ok := l2tp.InstanceFromInbound(loadInboundByTag(t, added.Tag))
	if !ok || after.PSK != inst.PSK || after.Subnet.String() != "10.30.0.0/24" {
		t.Fatalf("after the edit: key kept=%v subnet=%s", after.PSK == inst.PSK, after.Subnet)
	}
}

func TestAddInboundRefusesL2TPItCouldNotRun(t *testing.T) {
	tests := []struct {
		name     string
		missing  []string
		port     int
		settings string
		wantErr  string
	}{
		{
			name: "programs missing on this server", missing: []string{"xl2tpd", "pppd"}, port: l2tp.Port, settings: `{}`,
			wantErr: "L2TP cannot run on this server: xl2tpd, pppd are not installed",
		},
		{name: "a port no client can dial", port: 1702, settings: `{}`, wantErr: "L2TP clients always dial UDP port 1701"},
		{name: "login name with a space", port: l2tp.Port, settings: `{"clients":[{"email":"ali ce","enable":true}]}`, wantErr: "cannot be a login name"},
		{name: "key too short to type safely", port: l2tp.Port, settings: `{"server":{"psk":"abc"}}`, wantErr: "the IPsec key must be 8 to 64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupConflictDB(t)
			l2tpPrograms(t, tt.missing...)
			_, _, err := (&InboundService{}).AddInbound(&model.Inbound{
				UserId: 1, Enable: true, Port: tt.port, Protocol: model.L2TP, Tag: "inbound-l2tp", Settings: tt.settings,
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

// Every L2TP client dials port 1701, so a second inbound on one server could never be reached.
func TestOneServerHoldsOneL2TPInbound(t *testing.T) {
	setupConflictDB(t)
	l2tpPrograms(t)
	svc := &InboundService{}
	seedInboundConflict(t, "l2tp-first", "", l2tp.Port, model.L2TP, "", l2tpSettings(t, `{}`))

	_, _, err := svc.AddInbound(&model.Inbound{
		UserId: 1, Enable: true, Port: l2tp.Port, Protocol: model.L2TP, Tag: "l2tp-second", Listen: "192.0.2.7", Settings: `{}`,
	})
	if err == nil || !strings.Contains(err.Error(), "already serves L2TP on this server") {
		t.Fatalf("error = %v, want the refusal of a second L2TP inbound", err)
	}

	// The same port on a node is another server.
	nodeID := 5
	seedInboundConflictNode(t, "l2tp-on-node", "", l2tp.Port, model.L2TP, "", l2tpSettings(t, `{}`), &nodeID)
	first := loadInboundByTag(t, "l2tp-first")
	edit := *first
	edit.Remark = "renamed"
	if _, _, err := svc.UpdateInbound(&edit); err != nil {
		t.Fatalf("editing the one inbound of this server was refused: %v", err)
	}
}
