package service

import (
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func pinUsageZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	usageZone.Lock()
	usageZone.loc, usageZone.read = loc, time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	usageZone.Unlock()
	t.Cleanup(func() {
		usageZone.Lock()
		usageZone.loc = nil
		usageZone.Unlock()
	})
	return loc
}

func TestTrafficIsKeptPerHourDayAndServer(t *testing.T) {
	setupConflictDB(t)
	pinUsageZone(t, "Asia/Tehran")
	seedClientTraffic(t, 1, "alice", true)
	for range 2 {
		if _, _, err := (&InboundService{}).AddTraffic(nil, []*xray.ClientTraffic{{Email: "alice", Up: 100, Down: 300}}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	if err := recordClientUsage(database.GetDB(), 4, map[string]usageDelta{"alice": {up: 5, down: 7}}, now); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Create(&model.Node{Name: "fra", Address: "fra.example", Port: 2053}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Model(&model.Node{}).Where("name = ?", "fra").Update("id", 4).Error; err != nil {
		t.Fatal(err)
	}

	view, err := (&UsageService{}).ForClient("alice", UsageWindow{From: now.Add(-time.Hour), To: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Points) != 1 || view.Points[0].Up != 205 || view.Points[0].Down != 607 {
		t.Fatalf("points = %+v, want one hour holding both servers' traffic", view.Points)
	}
	if len(view.Nodes) != 2 || view.Nodes[0] != (NodeUsage{NodeId: 0, Up: 200, Down: 600}) ||
		view.Nodes[1] != (NodeUsage{NodeId: 4, Name: "fra", Up: 5, Down: 7}) {
		t.Fatalf("nodes = %+v, want this panel and fra apart", view.Nodes)
	}
	var day model.ClientUsage
	if err := database.GetDB().Where("email = ? AND daily = ? AND node_id = 0", "alice", true).First(&day).Error; err != nil {
		t.Fatal(err)
	}
	tehran := now.In(usageZone.loc)
	midnight := time.Date(tehran.Year(), tehran.Month(), tehran.Day(), 0, 0, 0, 0, usageZone.loc)
	if day.Start != midnight.Unix() || day.Up != 200 {
		t.Fatalf("day row = %+v, want it to start at Tehran's midnight %d", day, midnight.Unix())
	}
}

func TestAPersianMonthWindowRunsFromItsFirstToItsLastDay(t *testing.T) {
	loc := pinUsageZone(t, "Asia/Tehran")
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, loc)
	w, err := ParseUsageWindow("month", "1405-08", "persian", now)
	if err != nil {
		t.Fatal(err)
	}
	if !w.From.Equal(time.Date(2026, 10, 23, 0, 0, 0, 0, loc)) || !w.To.Equal(time.Date(2026, 11, 22, 0, 0, 0, 0, loc)) || !w.Daily {
		t.Fatalf("Aban 1405 = %s to %s daily=%v, want 23 Oct to 22 Nov 2026 by days", w.From, w.To, w.Daily)
	}
	w, err = ParseUsageWindow("month", "2026-12", "", now)
	if err != nil || !w.From.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, loc)) || w.Daily {
		t.Fatalf("December 2026 = %+v, %v; want the hours of a month still kept", w, err)
	}
	if _, err := ParseUsageWindow("month", "1405-13", "persian", now); err == nil {
		t.Fatal("a thirteenth month was accepted")
	}
}

func TestPruneKeepsDaysLongerThanHoursAndDropsDeletedUsers(t *testing.T) {
	setupConflictDB(t)
	db := database.GetDB()
	if err := db.Create(&model.ClientRecord{Email: "kept", Enable: true}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	old := now.Add(-40 * 24 * time.Hour).Unix()
	rows := []model.ClientUsage{
		{Email: "kept", Start: old, Up: 1},
		{Email: "kept", Daily: true, Start: old, Up: 1},
		{Email: "kept", Daily: true, Start: now.Add(-500 * 24 * time.Hour).Unix(), Up: 1},
		{Email: "gone", Daily: true, Start: now.Unix(), Up: 1},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := (&UsageService{}).Prune(now); err != nil {
		t.Fatal(err)
	}
	var left []model.ClientUsage
	db.Find(&left)
	if len(left) != 1 || left[0].Email != "kept" || !left[0].Daily || left[0].Start != old {
		t.Fatalf("left = %+v, want only kept's 40-day-old day", left)
	}
}
