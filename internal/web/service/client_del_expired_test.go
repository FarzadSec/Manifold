package service

import (
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestDelExpiredTakesOnlyFixedDatesPastTheGrace(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	now := time.Now()
	day := int64(24 * time.Hour / time.Millisecond)
	clients := []model.Client{
		{Email: "long-gone@x", ID: "11111111-1111-1111-1111-111111111111", SubID: "a", Enable: false, ExpiryTime: now.UnixMilli() - 40*day},
		{Email: "recent@x", ID: "22222222-2222-2222-2222-222222222222", SubID: "b", Enable: true, ExpiryTime: now.UnixMilli() - 10*day},
		{Email: "not-started@x", ID: "33333333-3333-3333-3333-333333333333", SubID: "c", Enable: true, ExpiryTime: -30 * day},
		{Email: "renews@x", ID: "44444444-4444-4444-4444-444444444444", SubID: "d", Enable: true, ExpiryTime: now.UnixMilli() - 40*day, Reset: 30},
	}
	ib := mkInboundStream(t, 30021, model.VLESS, clientsSettings(t, clients), wsStream)
	if err := svc.SyncInbound(nil, ib.Id, clients); err != nil {
		t.Fatal(err)
	}
	for _, c := range clients {
		if err := database.GetDB().Create(&xray.ClientTraffic{InboundId: ib.Id, Email: c.Email, Enable: c.Enable, ExpiryTime: c.ExpiryTime, Reset: c.Reset}).Error; err != nil {
			t.Fatal(err)
		}
	}
	count, _, err := svc.DelExpired(&InboundService{}, 30, true, now)
	if err != nil || count != 1 {
		t.Fatalf("dry run = %d, %v; want only long-gone counted", count, err)
	}
	if _, err := svc.GetRecordByEmail(nil, "long-gone@x"); err != nil {
		t.Fatalf("a dry run deleted the user: %v", err)
	}
	if count, _, err = svc.DelExpired(&InboundService{}, 30, false, now); err != nil || count != 1 {
		t.Fatalf("DelExpired = %d, %v; want one deleted", count, err)
	}
	if _, err := svc.GetRecordByEmail(nil, "long-gone@x"); err == nil {
		t.Fatal("long-gone@x is still there")
	}
	for _, kept := range []string{"recent@x", "not-started@x", "renews@x"} {
		if _, err := svc.GetRecordByEmail(nil, kept); err != nil {
			t.Fatalf("%s was deleted: %v", kept, err)
		}
	}
}
