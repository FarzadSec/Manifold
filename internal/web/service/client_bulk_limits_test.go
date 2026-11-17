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
