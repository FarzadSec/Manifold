package service

import (
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/util/jalali"
)

// The cycles the daily job decides on a date; hourly and daily ones run on their own clock.
var calendarCycles = []string{"weekly", "monthly", "yearly", "days"}

const maxCycleDays = 3650

// validateClientTrafficCycle refuses a cycle no job would ever select or could not place on a date.
func validateClientTrafficCycle(c *model.Client) error {
	if err := validateClientTrafficReset(c.TrafficReset, c.TrafficResetDay); err != nil {
		return err
	}
	switch c.TrafficResetCalendar {
	case "", "gregorian", "persian":
	default:
		return common.NewError("client trafficResetCalendar must be gregorian or persian, got:", c.TrafficResetCalendar)
	}
	switch c.TrafficReset {
	case "weekly":
		if c.TrafficResetDay > 7 {
			return common.NewError("a weekly cycle's trafficResetDay is a weekday from 1 (Monday) to 7 (Sunday), got:", c.TrafficResetDay)
		}
	case "yearly":
		if c.TrafficResetMonth < 1 || c.TrafficResetMonth > 12 {
			return common.NewError("a yearly cycle needs trafficResetMonth from 1 to 12, got:", c.TrafficResetMonth)
		}
	case "days":
		if c.TrafficResetEvery < 1 || c.TrafficResetEvery > maxCycleDays {
			return common.NewError("an every-N-days cycle needs trafficResetEvery from 1 to", maxCycleDays, "got:", c.TrafficResetEvery)
		}
	}
	return nil
}

// GetClientsWithCalendarCycles returns the clients on a weekly, monthly, yearly or every-N-days cycle.
func (s *ClientService) GetClientsWithCalendarCycles() ([]ClientResetCycle, error) {
	var cycles []ClientResetCycle
	err := clientResetCycles(database.GetDB()).
		Where("c.traffic_reset IN ?", calendarCycles).
		Scan(&cycles).Error
	return cycles, err
}

// MarkCycleReset records when a cycle reset the client, which an every-N-days cycle counts from.
func (s *ClientService) MarkCycleReset(email string, at time.Time) error {
	return database.GetDB().Model(&model.ClientRecord{}).Where("email = ?", email).
		Update("traffic_reset_at", at.UnixMilli()).Error
}

// CycleDue reports whether the client's cycle resets on the day now falls on, in now's location.
// A day the month lacks falls on its last day.
func (c ClientResetCycle) CycleDue(now time.Time) bool {
	day := max(c.TrafficResetDay, 1)
	switch c.TrafficReset {
	case "weekly":
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		// A weekly cycle saved before the weekday could be chosen ran on Sundays.
		want := c.TrafficResetDay
		if want < 1 || want > 7 {
			want = 7
		}
		return weekday == want
	case "monthly":
		if c.TrafficResetCalendar == "persian" {
			jy, jm, jd := jalali.Of(now)
			return jd == min(day, jalali.MonthLength(jy, jm))
		}
		return monthlyResetDay(now, day)
	case "yearly":
		if c.TrafficResetCalendar == "persian" {
			jy, jm, jd := jalali.Of(now)
			return jm == c.TrafficResetMonth && jd == min(day, jalali.MonthLength(jy, jm))
		}
		return int(now.Month()) == c.TrafficResetMonth && monthlyResetDay(now, day)
	case "days":
		start := c.TrafficResetAt
		if start <= 0 {
			start = c.CreatedAt
		}
		if start <= 0 || c.TrafficResetEvery < 1 {
			return false
		}
		from := dayStart(time.UnixMilli(start), now.Location())
		days := int(dayStart(now, now.Location()).Sub(from).Hours()/24 + 0.5)
		return days >= c.TrafficResetEvery
	}
	return false
}

func monthlyResetDay(now time.Time, day int) bool {
	lastDay := time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, now.Location()).Day()
	return now.Day() == min(day, lastDay)
}
