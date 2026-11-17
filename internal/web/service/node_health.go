package service

import (
	"time"

	"gorm.io/gorm/clause"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

const (
	nodeHealthSlot = 5 * time.Minute
	nodeHealthKeep = 90 * 24 * time.Hour
	// A heartbeat older than this means the node stopped answering, whatever its last status said.
	nodeHealthStale = 2 * time.Minute
	nodeStatsBars   = 120
	nodeStatsPoints = 240
)

// NodeHealthPoint is the node's load over one stretch of the chart.
type NodeHealthPoint struct {
	At  int64   `json:"at" example:"1700000000"`
	Cpu float64 `json:"cpu" example:"12.5"`
	Mem float64 `json:"mem" example:"40.1"`
}

// AvailabilityBar is one stretch of the availability bar: up, down (any slot unanswered) or
// unwatched (the panel recorded nothing, being off or the node not yet added).
type AvailabilityBar struct {
	From  int64  `json:"from" example:"1700000000"`
	State string `json:"state" example:"up"`
}

// NodeStatsView is a node's health and traffic over a window.
type NodeStatsView struct {
	From         int64             `json:"from" example:"1700000000"`
	To           int64             `json:"to" example:"1700086400"`
	UptimePct    float64           `json:"uptimePct" example:"99.5"`
	WatchedPct   float64           `json:"watchedPct" example:"100"`
	Availability []AvailabilityBar `json:"availability"`
	Points       []NodeHealthPoint `json:"points"`
	Users        []UserUsage       `json:"users"`
}

// RecordHealth takes the five-minute look at every enabled node, once per slot.
func (s *NodeService) RecordHealth(now time.Time) error {
	var nodes []model.Node
	if err := database.GetDB().Select("id", "status", "last_heartbeat", "cpu_pct", "mem_pct", "latency_ms").
		Where("enable = ?", true).Find(&nodes).Error; err != nil {
		return err
	}
	if len(nodes) == 0 {
		return nil
	}
	slot := now.Truncate(nodeHealthSlot).Unix()
	rows := make([]model.NodeHealth, 0, len(nodes))
	for _, n := range nodes {
		up := n.Status == "online" && now.Unix()-n.LastHeartbeat <= int64(nodeHealthStale/time.Second)
		row := model.NodeHealth{NodeId: n.Id, At: slot, Up: up}
		if up {
			row.Cpu, row.Mem, row.LatencyMs = n.CpuPct, n.MemPct, n.LatencyMs
		}
		rows = append(rows, row)
	}
	return database.GetDB().Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

// PruneHealth drops samples past what is kept.
func (s *NodeService) PruneHealth(now time.Time) error {
	return database.GetDB().Where("at < ?", now.Add(-nodeHealthKeep).Unix()).Delete(&model.NodeHealth{}).Error
}

// Stats is the node's health over w and who used it how much.
func (s *NodeService) Stats(id int, w UsageWindow, now time.Time) (NodeStatsView, error) {
	from, to := w.From.Truncate(nodeHealthSlot), w.To
	view := NodeStatsView{From: from.Unix(), To: to.Unix(), Availability: []AvailabilityBar{}, Points: []NodeHealthPoint{}}
	var samples []model.NodeHealth
	if err := database.GetDB().Where("node_id = ? AND at >= ? AND at < ?", id, from.Unix(), to.Unix()).
		Order("at").Find(&samples).Error; err != nil {
		return view, err
	}
	slots := max(int(to.Sub(from)/nodeHealthSlot), 1)
	up, down := 0, 0
	for _, sample := range samples {
		if sample.Up {
			up++
		} else {
			down++
		}
	}
	if up+down > 0 {
		view.UptimePct = 100 * float64(up) / float64(up+down)
	}
	view.WatchedPct = min(100, 100*float64(up+down)/float64(slots))
	view.Availability = availabilityBars(samples, from, to, min(nodeStatsBars, slots))
	view.Points = healthPoints(samples, from, to, min(nodeStatsPoints, slots))
	users, err := (&UsageService{}).ForNode(id, w, nil)
	view.Users = users
	return view, err
}

// stretchOf is the index of the stretch at falls in, when [from, to) is cut into n.
func stretchOf(at int64, from, to time.Time, n int) int {
	span := to.Unix() - from.Unix()
	if span <= 0 {
		return 0
	}
	return min(n-1, max(0, int((at-from.Unix())*int64(n)/span)))
}

func stretchStart(i int, from, to time.Time, n int) int64 {
	return from.Unix() + (to.Unix()-from.Unix())*int64(i)/int64(n)
}

func availabilityBars(samples []model.NodeHealth, from, to time.Time, n int) []AvailabilityBar {
	bars := make([]AvailabilityBar, n)
	for i := range bars {
		bars[i] = AvailabilityBar{From: stretchStart(i, from, to, n), State: "unwatched"}
	}
	for _, sample := range samples {
		bar := &bars[stretchOf(sample.At, from, to, n)]
		switch {
		case !sample.Up:
			bar.State = "down"
		case bar.State == "unwatched":
			bar.State = "up"
		}
	}
	return bars
}

func healthPoints(samples []model.NodeHealth, from, to time.Time, n int) []NodeHealthPoint {
	type sum struct {
		cpu, mem float64
		count    int
	}
	sums := make([]sum, n)
	for _, sample := range samples {
		if !sample.Up {
			continue
		}
		s := &sums[stretchOf(sample.At, from, to, n)]
		s.cpu += sample.Cpu
		s.mem += sample.Mem
		s.count++
	}
	points := make([]NodeHealthPoint, 0, n)
	for i, s := range sums {
		if s.count == 0 {
			continue
		}
		points = append(points, NodeHealthPoint{
			At:  stretchStart(i, from, to, n),
			Cpu: s.cpu / float64(s.count),
			Mem: s.mem / float64(s.count),
		})
	}
	return points
}
