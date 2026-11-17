package service

import (
	"encoding/json"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// validateNodeUsageLimit refuses a period no day could start.
func validateNodeUsageLimit(n *model.Node) error {
	if n.UsageLimit < 0 {
		return common.NewError("node usage limit cannot be negative")
	}
	switch n.UsageResetCalendar {
	case "", "gregorian", "persian":
	default:
		return common.NewError("node usage calendar must be gregorian or persian")
	}
	switch n.UsagePeriod {
	case "":
		n.UsageLimit = 0
	case "weekly":
		if n.UsageResetDay < 1 || n.UsageResetDay > 7 {
			return common.NewError("a weekly node limit starts on a weekday from 1 (Monday) to 7 (Sunday)")
		}
	case "monthly", "yearly":
		if n.UsageResetDay < 1 || n.UsageResetDay > 31 {
			return common.NewError("a node limit period starts on a day from 1 to 31")
		}
		if n.UsagePeriod == "yearly" && (n.UsageResetMonth < 1 || n.UsageResetMonth > 12) {
			return common.NewError("a yearly node limit needs a month from 1 to 12")
		}
	default:
		return common.NewError("node usage period must be weekly, monthly or yearly")
	}
	return nil
}

// nodeUsagePeriodStart is the first day of the period now falls in, in now's location.
func nodeUsagePeriodStart(n *model.Node, now time.Time) time.Time {
	cycle := ClientResetCycle{
		TrafficReset:         n.UsagePeriod,
		TrafficResetDay:      n.UsageResetDay,
		TrafficResetMonth:    n.UsageResetMonth,
		TrafficResetCalendar: n.UsageResetCalendar,
	}
	day := dayStart(now, now.Location())
	for range 367 {
		if cycle.CycleDue(day) {
			return day
		}
		// Noon of the previous day, so a daylight-saving shift never skips or repeats a day.
		day = dayStart(day.Add(-12*time.Hour), now.Location())
	}
	return day
}

// nodePeriodUsage is the node's raw traffic since its period began, from the usage history.
func nodePeriodUsage(n *model.Node, now time.Time) (int64, error) {
	var used int64
	err := database.GetDB().Model(&model.ClientUsage{}).
		Where("node_id = ? AND daily = ? AND start >= ?", n.Id, true, nodePeriodStartUnix(n, now)).
		Select("COALESCE(SUM(up + down), 0)").Scan(&used).Error
	return used, err
}

func nodePeriodStartUnix(n *model.Node, now time.Time) int64 {
	return nodeUsagePeriodStart(n, now.In(usageLocation(database.GetDB(), now))).Unix()
}

// EnforceUsageLimits stops each node over its period's limit and starts again the ones
// whose period turned, or whose limit was raised or removed.
func (s *NodeService) EnforceUsageLimits(now time.Time) error {
	var nodes []model.Node
	if err := database.GetDB().
		Where("(usage_period <> '' AND usage_limit > 0) OR usage_disabled = ?", true).
		Find(&nodes).Error; err != nil {
		return err
	}
	for i := range nodes {
		n := &nodes[i]
		over := false
		if n.UsagePeriod != "" && n.UsageLimit > 0 {
			used, err := nodePeriodUsage(n, now)
			if err != nil {
				return err
			}
			over = used >= n.UsageLimit
		}
		switch {
		case over && !n.UsageDisabled:
			if err := s.stopForUsage(n); err != nil {
				return err
			}
		case !over && n.UsageDisabled:
			if err := s.resumeAfterUsage(n); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *NodeService) stopForUsage(n *model.Node) error {
	var ids []int
	if err := database.GetDB().Model(&model.Inbound{}).
		Where("node_id = ? AND enable = ?", n.Id, true).Pluck("id", &ids).Error; err != nil {
		return err
	}
	inbounds := &InboundService{}
	stopped := make([]int, 0, len(ids))
	for _, id := range ids {
		if _, err := inbounds.SetInboundEnable(id, false); err != nil {
			logger.Warning("node usage limit: stopping inbound", id, "of node", n.Name, "failed:", err)
			continue
		}
		stopped = append(stopped, id)
	}
	logger.Info("node", n.Name, "reached its usage limit; stopped", len(stopped), "inbounds until the period turns")
	return database.GetDB().Model(&model.Node{}).Where("id = ?", n.Id).Updates(map[string]any{
		"usage_disabled":         true,
		"usage_stopped_inbounds": intListJSON(stopped),
	}).Error
}

func (s *NodeService) resumeAfterUsage(n *model.Node) error {
	inbounds := &InboundService{}
	for _, id := range n.UsageStoppedInbounds {
		if _, err := inbounds.SetInboundEnable(id, true); err != nil && !database.IsNotFound(err) {
			logger.Warning("node usage limit: starting inbound", id, "of node", n.Name, "failed:", err)
		}
	}
	logger.Info("node", n.Name, "is under its usage limit again; started", len(n.UsageStoppedInbounds), "inbounds")
	return database.GetDB().Model(&model.Node{}).Where("id = ?", n.Id).Updates(map[string]any{
		"usage_disabled":         false,
		"usage_stopped_inbounds": intListJSON(nil),
	}).Error
}

// intListJSON is a serializer:json column's stored form, for an Updates map that bypasses the serializer.
func intListJSON(ids []int) string {
	if ids == nil {
		ids = []int{}
	}
	raw, _ := json.Marshal(ids)
	return string(raw)
}
