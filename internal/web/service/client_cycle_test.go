package service

import (
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestCycleDueOnTheDayEachShapeNames(t *testing.T) {
	tehran, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		t.Fatal(err)
	}
	at := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 5, 0, tehran) }
	tenDaysAgo := at(2026, 11, 7).UnixMilli()
	cases := []struct {
		name  string
		cycle ClientResetCycle
		now   time.Time
		want  bool
	}{
		// 2026-11-18 is a Wednesday.
		{"weekly on its weekday", ClientResetCycle{TrafficReset: "weekly", TrafficResetDay: 3}, at(2026, 11, 18), true},
		{"weekly on another weekday", ClientResetCycle{TrafficReset: "weekly", TrafficResetDay: 4}, at(2026, 11, 18), false},
		{"weekly saved before weekdays runs on Sunday", ClientResetCycle{TrafficReset: "weekly"}, at(2026, 11, 22), true},
		// Mehr 1405 has 30 days: a cycle on the 31st falls on 30 Mehr, 22 October 2026.
		{"Persian monthly past the month's end", ClientResetCycle{TrafficReset: "monthly", TrafficResetDay: 31, TrafficResetCalendar: "persian"}, at(2026, 10, 22), true},
		{"Persian monthly the day before", ClientResetCycle{TrafficReset: "monthly", TrafficResetDay: 31, TrafficResetCalendar: "persian"}, at(2026, 10, 21), false},
		{"Gregorian monthly", ClientResetCycle{TrafficReset: "monthly", TrafficResetDay: 17}, at(2026, 11, 17), true},
		{"yearly on 29 February of a common year", ClientResetCycle{TrafficReset: "yearly", TrafficResetMonth: 2, TrafficResetDay: 29}, at(2027, 2, 28), true},
		{"yearly in another month", ClientResetCycle{TrafficReset: "yearly", TrafficResetMonth: 3, TrafficResetDay: 28}, at(2027, 2, 28), false},
		// 1405 is not leap: Esfand ends on the 29th, 20 March 2027.
		{"Persian yearly on 30 Esfand of a common year", ClientResetCycle{TrafficReset: "yearly", TrafficResetMonth: 12, TrafficResetDay: 30, TrafficResetCalendar: "persian"}, at(2027, 3, 20), true},
		{"every 10 days, tenth day", ClientResetCycle{TrafficReset: "days", TrafficResetEvery: 10, TrafficResetAt: tenDaysAgo}, at(2026, 11, 17), true},
		{"every 10 days, ninth day", ClientResetCycle{TrafficReset: "days", TrafficResetEvery: 10, TrafficResetAt: tenDaysAgo}, at(2026, 11, 16), false},
		{"every 10 days counts from creation before a first reset", ClientResetCycle{TrafficReset: "days", TrafficResetEvery: 10, CreatedAt: tenDaysAgo}, at(2026, 11, 17), true},
	}
	for _, tc := range cases {
		if got := tc.cycle.CycleDue(tc.now); got != tc.want {
			t.Errorf("%s: CycleDue(%s) = %v, want %v", tc.name, tc.now.Format("2006-01-02"), got, tc.want)
		}
	}
}

func TestCycleShapesThatNoDateCouldHoldAreRefused(t *testing.T) {
	for _, c := range []model.Client{
		{TrafficReset: "yearly", TrafficResetDay: 1},
		{TrafficReset: "days"},
		{TrafficReset: "weekly", TrafficResetDay: 9},
		{TrafficReset: "monthly", TrafficResetDay: 1, TrafficResetCalendar: "hijri"},
	} {
		if err := validateClientTrafficCycle(&c); err == nil {
			t.Errorf("%+v was accepted", c)
		}
	}
	ok := model.Client{TrafficReset: "yearly", TrafficResetMonth: 12, TrafficResetDay: 30, TrafficResetCalendar: "persian"}
	if err := validateClientTrafficCycle(&ok); err != nil {
		t.Fatalf("a Persian yearly cycle was refused: %v", err)
	}
}
