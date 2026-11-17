package service

import (
	"bytes"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/metrics"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func seedSummary(t *testing.T, now time.Time) {
	t.Helper()
	db := database.GetDB()
	day := 24 * time.Hour
	users := []struct {
		email          string
		enable         bool
		up, total      int64
		expiry         int64
		trafficEnabled bool
	}{
		{"active", true, 10, 100, now.Add(10 * day).UnixMilli(), true},
		{"off", false, 0, 0, 0, true},
		{"expired", true, 0, 0, now.Add(-day).UnixMilli(), true},
		{"limited", true, 100, 100, 0, true},
		{"soon", true, 0, 0, now.Add(day / 2).UnixMilli(), true},
		{"depleted", true, 0, 0, 0, false},
	}
	for _, u := range users {
		if err := db.Create(&model.ClientRecord{Email: u.email, SubID: u.email, Enable: u.enable}).Error; err != nil {
			t.Fatal(err)
		}
		if !u.enable {
			db.Model(&model.ClientRecord{}).Where("email = ?", u.email).Update("enable", false)
		}
		ct := xray.ClientTraffic{Email: u.email, Enable: u.trafficEnabled, Up: u.up, Total: u.total, ExpiryTime: u.expiry}
		if err := db.Create(&ct).Error; err != nil {
			t.Fatal(err)
		}
	}
	today := dayStart(now, usageLocation(db, now)).Unix()
	db.Create(&[]model.ClientUsage{
		{Email: "active", Daily: true, Start: today, Up: 300, Down: 700},
		{Email: "limited", NodeId: 2, Daily: true, Start: today, Up: 24},
		{Email: "active", Daily: true, Start: today - 86400, Up: 5000},
		{Email: "active", Start: today, Up: 9999},
	})
	db.Create(&[]model.Node{
		{Name: "fra", Enable: true, Status: "online", LatencyMs: 12},
		{Name: "ams", Enable: true, Status: "offline"},
		{Name: "old", Enable: true, Status: "online"},
	})
	db.Model(&model.Node{}).Where("name = ?", "old").Update("enable", false)
	db.Create(&[]model.WebhookDelivery{
		{WebhookId: 1, Status: "pending", CreatedAt: now.UnixMilli()},
		{WebhookId: 1, Status: "failed", CreatedAt: now.Add(-time.Hour).UnixMilli()},
		{WebhookId: 1, Status: "failed", CreatedAt: now.Add(-48 * time.Hour).UnixMilli()},
	})
	db.Create(&[]model.BackupTarget{
		{Name: "local", Enable: true, Kind: "local", LastRun: 1000, LastSize: 2048},
		{Name: "s3", Enable: true, Kind: "s3", LastRun: 3000, LastError: "denied"},
	})
	db.Create(&model.Inbound{UserId: 1, Enable: true, Port: 4433, Tag: "in-4433", Protocol: model.VLESS,
		Up: 11, Down: 22, Settings: `{"clients":[]}`})
}

func TestSummaryCountsEachState(t *testing.T) {
	setupBulkDB(t)
	now := time.Now()
	seedSummary(t, now)
	got, err := Summary(now)
	if err != nil {
		t.Fatal(err)
	}
	want := PanelSummary{
		Users:        UserCounts{Total: 6, Active: 2, Disabled: 2, Expired: 1, Expiring: 1, Limited: 1},
		TrafficToday: 1024, NodesTotal: 2, NodesOnline: 1,
		WebhooksPending: 1, WebhooksFailed: 1, BackupsFailing: 1, LastBackup: 1000,
	}
	if got != want {
		t.Fatalf("Summary =\n%+v\nwant\n%+v", got, want)
	}
}

func TestMetricsExposeStatesInboundsAndOptInUsers(t *testing.T) {
	setupBulkDB(t)
	now := time.Now()
	seedSummary(t, now)
	scrape := func(perUser bool) string {
		fams, err := Metrics(&ServerService{}, MetricsOptions{PerUser: perUser}, now)
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		if err := metrics.Write(&b, fams...); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	out := scrape(false)
	for _, line := range []string{
		`manifold_users{state="limited"} 1`,
		`manifold_users{state="active"} 2`,
		`manifold_traffic_today_bytes 1024`,
		`manifold_inbound_traffic_bytes_total{tag="in-4433",protocol="vless",node="",direction="down"} 22`,
		`manifold_node_up{node="fra"} 1`,
		`manifold_node_up{node="ams"} 0`,
		`manifold_backup_last_ok{target="s3"} 0`,
		`manifold_backup_last_size_bytes{target="local"} 2048`,
	} {
		if !strings.Contains(out, line+"\n") {
			t.Errorf("scrape lacks %s", line)
		}
	}
	if strings.Contains(out, `node="old"`) || strings.Contains(out, "manifold_user_traffic_bytes_total") {
		t.Fatalf("a disabled node or per-user series leaked into the default scrape:\n%s", out)
	}
	if !strings.Contains(scrape(true), `manifold_user_traffic_bytes_total{user="limited",direction="up"} 100`+"\n") {
		t.Fatal("users=1 does not list each user's traffic")
	}
}

// The Grafana dashboard shipped in manifold/grafana reads these names; renaming one breaks it.
func TestGrafanaDashboardReadsMetricsTheScrapeHas(t *testing.T) {
	setupBulkDB(t)
	now := time.Now()
	seedSummary(t, now)
	fams, err := Metrics(&ServerService{}, MetricsOptions{PerUser: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, f := range fams {
		have[f.Name] = true
	}
	raw, err := os.ReadFile("../../../manifold/grafana/manifold.json")
	if err != nil {
		t.Fatal(err)
	}
	var dash struct {
		Panels []struct {
			Title   string
			Targets []struct{ Expr string }
		}
	}
	if err := json.Unmarshal(raw, &dash); err != nil {
		t.Fatal(err)
	}
	names := regexp.MustCompile(`manifold_[a-z_]+`)
	for _, p := range dash.Panels {
		for _, target := range p.Targets {
			for _, name := range names.FindAllString(target.Expr, -1) {
				if !have[name] {
					t.Errorf("panel %q reads %s, which the scrape does not have", p.Title, name)
				}
			}
		}
	}
}
