package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

// A node's multiplier scales what its traffic costs a user's quota, while the usage
// history keeps the bytes that crossed the wire.
func TestNodeMultiplierScalesQuotaNotHistory(t *testing.T) {
	db := initTrafficTestDB(t)
	node := model.Node{Name: "double", Address: "203.0.113.9", Port: 2053, ApiToken: "x", TrafficMultiplier: 2}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	createNodeInboundWithClient(t, db, node.Id, "n-in", 41101, "alice")
	svc := &InboundService{}
	syncNode(t, svc, node.Id, "n-in", xray.ClientTraffic{Email: "alice", Up: 100, Down: 1000, Enable: true})
	before := readTraffic(t, db, "alice")
	syncNode(t, svc, node.Id, "n-in", xray.ClientTraffic{Email: "alice", Up: 150, Down: 1400, Enable: true})
	after := readTraffic(t, db, "alice")
	if up, down := after.Up-before.Up, after.Down-before.Down; up != 100 || down != 800 {
		t.Fatalf("quota grew by %d/%d, want 100/800: twice the 50/400 that crossed the node", up, down)
	}
	var rows []model.ClientUsage
	db.Where("email = ? AND node_id = ? AND daily = ?", "alice", node.Id, false).Find(&rows)
	var histUp, histDown int64
	for _, r := range rows {
		histUp += r.Up
		histDown += r.Down
	}
	if histUp != 50 || histDown != 400 {
		t.Fatalf("history recorded %d/%d, want the raw 50/400", histUp, histDown)
	}
}
