package jalali

import (
	"testing"
	"time"
)

// Dates checked against the official Iranian calendar.
var pairs = []struct{ gy, gm, gd, jy, jm, jd int }{
	{2024, 1, 1, 1402, 10, 11},
	{2025, 3, 20, 1403, 12, 30},
	{2025, 3, 21, 1404, 1, 1},
	{2026, 3, 20, 1404, 12, 29},
	{2026, 3, 21, 1405, 1, 1},
	{2026, 11, 17, 1405, 8, 26},
	{2017, 3, 21, 1396, 1, 1},
	{1979, 2, 11, 1357, 11, 22},
}

func TestConversionBothWays(t *testing.T) {
	for _, p := range pairs {
		if jy, jm, jd := ToJalali(p.gy, p.gm, p.gd); jy != p.jy || jm != p.jm || jd != p.jd {
			t.Errorf("ToJalali(%d-%d-%d) = %d-%d-%d, want %d-%d-%d", p.gy, p.gm, p.gd, jy, jm, jd, p.jy, p.jm, p.jd)
		}
		if gy, gm, gd := ToGregorian(p.jy, p.jm, p.jd); gy != p.gy || gm != p.gm || gd != p.gd {
			t.Errorf("ToGregorian(%d-%d-%d) = %d-%d-%d, want %d-%d-%d", p.jy, p.jm, p.jd, gy, gm, gd, p.gy, p.gm, p.gd)
		}
	}
}

// 1403 is leap (Esfand has 30 days), 1404 is not.
func TestEsfandFollowsTheLeapYear(t *testing.T) {
	if MonthLength(1403, 12) != 30 || MonthLength(1404, 12) != 29 || MonthLength(1404, 7) != 30 || MonthLength(1404, 6) != 31 {
		t.Fatalf("month lengths: 1403/12=%d 1404/12=%d 1404/7=%d 1404/6=%d",
			MonthLength(1403, 12), MonthLength(1404, 12), MonthLength(1404, 7), MonthLength(1404, 6))
	}
}

func TestEveryDayOfAYearRoundTrips(t *testing.T) {
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 800; i++ {
		jy, jm, jd := Of(day)
		if got := Date(jy, jm, jd, time.UTC); !got.Equal(day) {
			t.Fatalf("%s -> %d-%d-%d -> %s", day.Format("2006-01-02"), jy, jm, jd, got.Format("2006-01-02"))
		}
		day = day.AddDate(0, 0, 1)
	}
}
