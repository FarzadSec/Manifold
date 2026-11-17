package service

import (
	"math"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// A node that answered is up, one whose heartbeat went stale is down, and a slot is recorded once.
func TestNodeHealthRecordsWhetherTheNodeAnswered(t *testing.T) {
	db := initTrafficTestDB(t)
	now := time.Date(2026, 11, 17, 10, 7, 0, 0, time.UTC)
	nodes := []model.Node{
		{Name: "fresh", Address: "203.0.113.30", Port: 2053, ApiToken: "x", Enable: true, Status: "online", LastHeartbeat: now.Add(-30 * time.Second).Unix(), CpuPct: 12, MemPct: 40},
		{Name: "stale", Address: "203.0.113.31", Port: 2053, ApiToken: "x", Enable: true, Status: "online", LastHeartbeat: now.Add(-10 * time.Minute).Unix()},
		{Name: "off", Address: "203.0.113.32", Port: 2053, ApiToken: "x", Status: "online", LastHeartbeat: now.Unix()},
	}
	for i := range nodes {
		if err := db.Create(&nodes[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	db.Model(&nodes[2]).Update("enable", false)
	svc := &NodeService{}
	for range 2 {
		if err := svc.RecordHealth(now); err != nil {
			t.Fatal(err)
		}
	}
	var rows []model.NodeHealth
	db.Order("node_id").Find(&rows)
	if len(rows) != 2 {
		t.Fatalf("recorded %d samples, want one per enabled node per slot: %+v", len(rows), rows)
	}
	if !rows[0].Up || rows[0].Cpu != 12 || rows[0].At != now.Truncate(5*time.Minute).Unix() {
		t.Fatalf("fresh node sample = %+v", rows[0])
	}
	if rows[1].Up {
		t.Fatal("a node silent for ten minutes was recorded as up")
	}
}

// Uptime counts only the time the panel watched: a gap with no samples is neither up nor down.
func TestNodeAvailabilityCountsOnlyWatchedTime(t *testing.T) {
	db := initTrafficTestDB(t)
	node := model.Node{Name: "n", Address: "203.0.113.33", Port: 2053, ApiToken: "x"}
	db.Create(&node)
	to := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	from := to.Add(-2 * time.Hour)
	for i := range 24 {
		at := from.Add(time.Duration(i) * 5 * time.Minute)
		switch {
		case i < 10:
			db.Create(&model.NodeHealth{NodeId: node.Id, At: at.Unix(), Up: true, Cpu: 10})
		case i < 12:
			db.Create(&model.NodeHealth{NodeId: node.Id, At: at.Unix(), Up: false})
		}
	}
	stats, err := (&NodeService{}).Stats(node.Id, UsageWindow{From: from, To: to}, to)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(stats.UptimePct-100*10.0/12) > 0.01 {
		t.Fatalf("uptime = %.2f%%, want 83.33%%: ten up of twelve watched slots", stats.UptimePct)
	}
	states := map[string]int{}
	for _, bar := range stats.Availability {
		states[bar.State]++
	}
	if states["up"] == 0 || states["down"] == 0 || states["unwatched"] == 0 {
		t.Fatalf("availability bar = %v, want up, down and unwatched stretches", states)
	}
	if last := stats.Availability[len(stats.Availability)-1]; last.State != "unwatched" {
		t.Fatalf("the unrecorded last hour reads %q, want unwatched", last.State)
	}
}
