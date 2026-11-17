package service

import (
	"slices"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

const dayMs = int64(24 * time.Hour / time.Millisecond)

// PlanService keeps the catalogue new users are created from.
type PlanService struct{}

// List returns the plans in their display order.
func (s *PlanService) List() ([]model.Plan, error) {
	var plans []model.Plan
	err := database.GetDB().Order("sort_order").Order("id").Find(&plans).Error
	return plans, err
}

// Get returns one plan.
func (s *PlanService) Get(id int) (*model.Plan, error) {
	var p model.Plan
	if err := database.GetDB().First(&p, id).Error; err != nil {
		return nil, common.NewError("plan not found")
	}
	return &p, nil
}

func (s *PlanService) validate(p *model.Plan) error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return common.NewError("a plan needs a name")
	}
	if err := validateClientSpeedLimit(p.SpeedLimit); err != nil {
		return err
	}
	if p.TotalBytes < 0 || p.Days < 0 || p.LimitIp < 0 || p.UserLimit < 0 || p.LimitHwid < 0 || p.Reset < 0 {
		return common.NewError("plan limits cannot be negative")
	}
	if p.TrafficReset == "" {
		p.TrafficReset = "never"
	}
	if p.TrafficResetDay < 1 || p.TrafficResetDay > 31 {
		p.TrafficResetDay = 1
	}
	if err := validateClientTrafficCycle(&model.Client{
		TrafficReset: p.TrafficReset, TrafficResetDay: p.TrafficResetDay, TrafficResetMonth: p.TrafficResetMonth,
		TrafficResetEvery: p.TrafficResetEvery, TrafficResetCalendar: p.TrafficResetCalendar,
	}); err != nil {
		return err
	}
	if !p.AllInbounds {
		if len(p.InboundIds) == 0 {
			return common.NewError("pick the plan's inbounds, or all inbounds")
		}
		var n int64
		if err := database.GetDB().Model(&model.Inbound{}).Where("id IN ?", p.InboundIds).Count(&n).Error; err != nil {
			return err
		}
		if int(n) != len(uniqueInts(p.InboundIds)) {
			return common.NewError("one of the plan's inbounds does not exist")
		}
	}
	return nil
}

// Save creates the plan, or replaces the one with its id.
func (s *PlanService) Save(p *model.Plan) error {
	if err := s.validate(p); err != nil {
		return err
	}
	db := database.GetDB()
	var clash int64
	if err := db.Model(&model.Plan{}).Where("name = ? AND id <> ?", p.Name, p.Id).Count(&clash).Error; err != nil {
		return err
	}
	if clash > 0 {
		return common.NewError("a plan with that name already exists")
	}
	if p.Id == 0 {
		return db.Create(p).Error
	}
	return db.Save(p).Error
}

// Delete removes a plan; users created from it keep every limit they were given.
func (s *PlanService) Delete(id int) error {
	return database.GetDB().Delete(&model.Plan{}, id).Error
}

// ApplyPlan copies the plan's limits onto a new user; nothing links the user to it afterwards.
func ApplyPlan(p *model.Plan, payload *ClientCreatePayload, allInboundIDs []int, now time.Time) {
	c := &payload.Client
	c.TotalGB = p.TotalBytes
	switch {
	case p.Days == 0:
		c.ExpiryTime = 0
	case p.StartOnFirstUse:
		c.ExpiryTime = -int64(p.Days) * dayMs
	default:
		c.ExpiryTime = now.UnixMilli() + int64(p.Days)*dayMs
	}
	c.LimitIP = p.LimitIp
	c.UserLimit = p.UserLimit
	c.SpeedLimit = p.SpeedLimit
	payload.LimitHwid = p.LimitHwid
	c.Reset = p.Reset
	c.TrafficReset = p.TrafficReset
	c.TrafficResetDay = p.TrafficResetDay
	c.TrafficResetMonth = p.TrafficResetMonth
	c.TrafficResetEvery = p.TrafficResetEvery
	c.TrafficResetCalendar = p.TrafficResetCalendar
	if p.Group != "" {
		c.Group = p.Group
	}
	if p.NamePrefix != "" && !strings.HasPrefix(c.Email, p.NamePrefix) {
		c.Email = p.NamePrefix + c.Email
	}
	if p.NameSuffix != "" && !strings.HasSuffix(c.Email, p.NameSuffix) {
		c.Email += p.NameSuffix
	}
	if p.AllInbounds {
		payload.InboundIds = slices.Clone(allInboundIDs)
	} else {
		payload.InboundIds = slices.Clone(p.InboundIds)
	}
}
