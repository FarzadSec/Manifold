package service

import (
	"encoding/json"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// The speed limit travels to nodes inside the inbound's settings, so a bulk change
// has to reach that copy as well as the record the panel reads.
func TestBulkSetLimitsReachesTheRecordAndTheInboundCopy(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	clients := []model.Client{
		{Email: "s1@x", ID: "11111111-1111-1111-1111-111111111111", SubID: "s1", Enable: true, LimitIP: 3},
		{Email: "s2@x", ID: "22222222-2222-2222-2222-222222222222", SubID: "s2", Enable: true},
	}
	ib := mkInboundStream(t, 30011, model.VLESS, clientsSettings(t, clients), wsStream)
	if err := svc.SyncInbound(nil, ib.Id, clients); err != nil {
		t.Fatal(err)
	}
	speed := 25
	res, _, err := svc.BulkAdjustWithLimits(&InboundService{}, []string{"s1@x"}, 0, 0, "", nil, "", BulkLimits{SpeedLimit: &speed})
	if err != nil || res.Adjusted != 1 {
		t.Fatalf("BulkAdjustWithLimits = %+v, %v; want one client adjusted", res, err)
	}
	rec, err := svc.GetRecordByEmail(nil, "s1@x")
	if err != nil || rec.SpeedLimit != 25 || rec.LimitIP != 3 {
		t.Fatalf("record = %+v, %v; want 25 Mbps and its address limit kept", rec, err)
	}
	var stored model.Inbound
	database.GetDB().First(&stored, ib.Id)
	var settings struct {
		Clients []model.Client `json:"clients"`
	}
	if err := json.Unmarshal([]byte(stored.Settings), &settings); err != nil {
		t.Fatal(err)
	}
	for _, c := range settings.Clients {
		want := 0
		if c.Email == "s1@x" {
			want = 25
		}
		if c.SpeedLimit != want {
			t.Fatalf("inbound copy of %s has speedLimit %d, want %d", c.Email, c.SpeedLimit, want)
		}
	}
	tooFast := 200000
	if _, _, err := svc.BulkAdjustWithLimits(&InboundService{}, []string{"s1@x"}, 0, 0, "", nil, "", BulkLimits{SpeedLimit: &tooFast}); err == nil {
		t.Fatal("a speed past the table's range was accepted")
	}
}

// OpenVPN reads its session cap from the inbound's copy, so a bulk user limit has to
// land there and in the record, without touching the IP limit.
func TestBulkSetUserLimitReachesTheTunnelCopy(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	ib := mkInbound(t, 47031, model.OpenVPN, openvpnSettings(t, `{"clients":[]}`))
	for _, email := range []string{"u1", "u2"} {
		if _, err := svc.CreateOne(&InboundService{}, ib.Id, model.Client{Email: email, Password: "pw-" + email, Enable: true, LimitIP: 4}); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.GetDB().Create(&model.ClientRecord{Email: "detached", Enable: true}).Error; err != nil {
		t.Fatal(err)
	}
	two := 2
	if res, _, err := svc.BulkAdjustWithLimits(&InboundService{}, []string{"u1", "detached"}, 0, 0, "", nil, "", BulkLimits{UserLimit: &two}); err != nil || res.Adjusted != 2 {
		t.Fatalf("BulkAdjustWithLimits = %+v, %v; want two clients adjusted", res, err)
	}
	if rec := lookupClientRecord(t, "u1"); rec.UserLimit != 2 || rec.LimitIP != 4 {
		t.Fatalf("record = %+v; want user limit 2 and the IP limit kept", rec)
	}
	if rec := lookupClientRecord(t, "detached"); rec.UserLimit != 2 {
		t.Fatalf("a client on no inbound kept user limit %d, want 2", rec.UserLimit)
	}
	instances, err := (&InboundService{}).DesiredOpenVPNInstances()
	if err != nil || len(instances) != 1 {
		t.Fatalf("instances = %+v, %v", instances, err)
	}
	for _, u := range instances[0].Users {
		if want := map[string]int{"u1": 2, "u2": 0}[u.Name]; u.MaxSessions != want {
			t.Fatalf("%s may hold %d sessions, want %d", u.Name, u.MaxSessions, want)
		}
	}
	negative := -1
	if _, _, err := svc.BulkAdjustWithLimits(&InboundService{}, []string{"u1"}, 0, 0, "", nil, "", BulkLimits{UserLimit: &negative}); err == nil {
		t.Fatal("a negative user limit was accepted")
	}
}
