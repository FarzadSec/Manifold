package service

import (
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func inboundEnabled(t *testing.T, id int) bool {
	t.Helper()
	ib, err := (&InboundService{}).GetInbound(id)
	if err != nil {
		t.Fatal(err)
	}
	return ib.Enable
}

// A node over its period's limit stops serving, and serves again when the period
// turns; an inbound the admin had switched off stays off through both.
func TestNodeUsageLimitSwitchesTheNodeOffForThePeriod(t *testing.T) {
	db := initTrafficTestDB(t)
	tehran, _ := time.LoadLocation("Asia/Tehran")
	usageZone.Lock()
	usageZone.loc, usageZone.read = tehran, time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	usageZone.Unlock()
	t.Cleanup(func() { usageZone.Lock(); usageZone.loc = nil; usageZone.Unlock() })

	node := model.Node{
		Name: "capped", Address: "203.0.113.20", Port: 2053, ApiToken: "x",
		UsageLimit: 1000, UsagePeriod: "monthly", UsageResetDay: 5,
	}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	nid := node.Id
	serving := &model.Inbound{UserId: 1, Tag: "n-serving", Enable: true, Port: 41201, Protocol: model.VLESS, NodeID: &nid}
	off := &model.Inbound{UserId: 1, Tag: "n-off", Enable: true, Port: 41202, Protocol: model.VLESS, NodeID: &nid}
	for _, ib := range []*model.Inbound{serving, off} {
		if err := db.Create(ib).Error; err != nil {
			t.Fatal(err)
		}
	}
	db.Model(off).Update("enable", false)

	now := time.Date(2026, 11, 20, 12, 0, 0, 0, tehran)
	periodDay := time.Date(2026, 11, 5, 0, 0, 0, 0, tehran)
	before := time.Date(2026, 11, 4, 0, 0, 0, 0, tehran)
	for _, row := range []model.ClientUsage{
		{Email: "a", NodeId: nid, Daily: true, Start: before.Unix(), Up: 5000},
		{Email: "a", NodeId: nid, Daily: true, Start: periodDay.Unix(), Up: 600},
		{Email: "b", NodeId: nid, Daily: true, Start: now.Add(-time.Hour).Unix(), Down: 300},
		{Email: "c", NodeId: 0, Daily: true, Start: periodDay.Unix(), Down: 9000},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := &NodeService{}
	if err := svc.EnforceUsageLimits(now); err != nil {
		t.Fatal(err)
	}
	if !inboundEnabled(t, serving.Id) {
		t.Fatal("900 bytes this period is under the 1000 limit, yet the node stopped")
	}

	db.Create(&model.ClientUsage{Email: "b", NodeId: nid, Daily: true, Start: periodDay.Add(24 * time.Hour).Unix(), Down: 100})
	if err := svc.EnforceUsageLimits(now); err != nil {
		t.Fatal(err)
	}
	var stored model.Node
	db.First(&stored, nid)
	if inboundEnabled(t, serving.Id) || !stored.UsageDisabled {
		t.Fatalf("at the limit the node still serves (inbound on=%v, flagged=%v)", inboundEnabled(t, serving.Id), stored.UsageDisabled)
	}

	next := time.Date(2026, 12, 5, 0, 30, 0, 0, tehran)
	if err := svc.EnforceUsageLimits(next); err != nil {
		t.Fatal(err)
	}
	db.First(&stored, nid)
	if !inboundEnabled(t, serving.Id) || stored.UsageDisabled {
		t.Fatal("a new period began and the node still does not serve")
	}
	if inboundEnabled(t, off.Id) {
		t.Fatal("the inbound the admin had switched off was switched on")
	}
}
