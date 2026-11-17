package service

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/importer"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func marzbanFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db.sqlite3")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, s := range []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT, status TEXT, used_traffic INTEGER, data_limit INTEGER, expire INTEGER, note TEXT, data_limit_reset_strategy TEXT, sub_revoked_at DATETIME)`,
		`CREATE TABLE proxies (id INTEGER PRIMARY KEY, user_id INTEGER, type TEXT, settings TEXT)`,
		`CREATE TABLE jwt (id INTEGER PRIMARY KEY, secret_key TEXT)`,
		`INSERT INTO jwt VALUES (1, 'sekret')`,
		`INSERT INTO users VALUES (1, 'alice', 'active', 5000, 10000, 1800000000, 'vip', 'month', NULL)`,
		`INSERT INTO users VALUES (2, 'taken', 'active', 0, 0, NULL, NULL, 'no_reset', NULL)`,
		`INSERT INTO users VALUES (3, 'tro', 'disabled', 0, 0, NULL, NULL, 'no_reset', NULL)`,
		`INSERT INTO proxies VALUES (1, 1, 'VLESS', '{"id":"11111111-1111-4111-8111-111111111111","flow":"xtls-rprx-vision"}')`,
		`INSERT INTO proxies VALUES (2, 1, 'Trojan', '{"password":"alicepass"}')`,
		`INSERT INTO proxies VALUES (3, 2, 'VLESS', '{"id":"22222222-2222-4222-8222-222222222222"}')`,
		`INSERT INTO proxies VALUES (4, 3, 'Trojan', '{"password":"tropass"}')`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	return path
}

func TestImportFromMarzbanCreatesUsersOnThePickedInbounds(t *testing.T) {
	setupBulkDB(t)
	db := database.GetDB()
	vless := &model.Inbound{
		UserId: 1, Enable: true, Port: 44001, Tag: "in-vless", Protocol: model.VLESS,
		Settings: `{"clients":[],"decryption":"none"}`, StreamSettings: `{"network":"tcp","security":"none"}`,
	}
	trojan := &model.Inbound{
		UserId: 1, Enable: true, Port: 44002, Tag: "in-trojan", Protocol: model.Trojan,
		Settings: `{"clients":[]}`, StreamSettings: `{"network":"tcp","security":"none"}`,
	}
	for _, ib := range []*model.Inbound{vless, trojan} {
		if err := db.Create(ib).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := (&ClientService{}).Create(&InboundService{}, &ClientCreatePayload{
		Client: model.Client{Email: "taken", ID: "33333333-3333-4333-8333-333333333333", Enable: true, SubID: "keep"}, InboundIds: []int{vless.Id},
	}); err != nil {
		t.Fatal(err)
	}

	res, err := ReadPanelFile(context.Background(), marzbanFixture(t), []byte("SQLite format 3\x00"))
	if err != nil {
		t.Fatal(err)
	}
	s := &PanelImportService{}
	preview, err := s.Preview(res)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Users != 3 || preview.Existing != 1 || preview.Protocols["vless"] != 2 || preview.Protocols["trojan"] != 2 || !preview.HasSecret {
		t.Fatalf("preview = %+v", preview)
	}
	out, err := s.Apply(ImportApplyRequest{Id: preview.Id, Inbounds: map[string][]int{"vless": {vless.Id}, "trojan": {trojan.Id}}, KeepLinks: true})
	if err != nil {
		t.Fatal(err)
	}
	if out.Created != 2 || len(out.Skipped) != 1 || out.Skipped[0].Email != "taken" || out.Aliases != 2 {
		t.Fatalf("apply = %+v", out)
	}
	var alice model.ClientRecord
	db.Where("email = ?", "alice").First(&alice)
	if alice.UUID != "11111111-1111-4111-8111-111111111111" || alice.Password != "alicepass" ||
		alice.TotalGB != 10000 || alice.ExpiryTime != 1800000000000 || alice.TrafficReset != "monthly" || alice.Comment != "vip" {
		t.Fatalf("alice = %+v", alice)
	}
	var links []int
	db.Model(&model.ClientInbound{}).Where("client_id = ?", alice.Id).Order("inbound_id").Pluck("inbound_id", &links)
	if !slices.Equal(links, []int{vless.Id, trojan.Id}) {
		t.Fatalf("alice is on inbounds %v", links)
	}
	var tro model.ClientRecord
	db.Where("email = ?", "tro").First(&tro)
	db.Model(&model.ClientInbound{}).Where("client_id = ?", tro.Id).Pluck("inbound_id", &links)
	if tro.Enable || !slices.Equal(links, []int{trojan.Id}) {
		t.Fatalf("tro = %+v on %v; a trojan-only user belongs on the trojan inbound alone", tro, links)
	}
	var ct xray.ClientTraffic
	db.Where("email = ?", "alice").First(&ct)
	if ct.Down != 5000 {
		t.Fatalf("alice's used traffic = %d", ct.Down)
	}
	var kept model.ClientRecord
	db.Where("email = ?", "taken").First(&kept)
	if kept.SubID != "keep" {
		t.Fatal("the user already on the panel was changed")
	}
	if sub, ok := ResolveSubAlias(marzbanTokenFor(t, "alice")); !ok || sub != alice.SubID {
		t.Fatalf("an old alice link resolved to %q %v, want %q", sub, ok, alice.SubID)
	}
	if _, err := s.Apply(ImportApplyRequest{Id: preview.Id, Inbounds: map[string][]int{"trojan": {vless.Id}}}); err == nil {
		t.Fatal("a VLESS inbound was taken for trojan users")
	}
}

func marzbanTokenFor(t *testing.T, name string) string {
	t.Helper()
	return importer.MarzbanToken(name, "sekret", time.Now())
}
