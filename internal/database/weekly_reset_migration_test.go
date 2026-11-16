package database

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// The day of a weekly cycle now names its weekday; cycles saved before ran on
// Sundays and must keep doing so, in the record and in the inbound's copy.
func TestWeeklyCyclesSavedBeforeWeekdaysStayOnSunday(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = CloseDB() })
	settings := `{"clients":[{"email":"weekly","trafficReset":"weekly","trafficResetDay":1},{"email":"monthly","trafficReset":"monthly","trafficResetDay":1}]}`
	if err := db.Create(&model.Inbound{UserId: 1, Port: 40999, Protocol: model.VLESS, Tag: "w", Settings: settings}).Error; err != nil {
		t.Fatal(err)
	}
	for _, rec := range []model.ClientRecord{
		{Email: "weekly", TrafficReset: "weekly", TrafficResetDay: 1},
		{Email: "monthly", TrafficReset: "monthly", TrafficResetDay: 1},
	} {
		if err := db.Create(&rec).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := pinWeeklyResetsToSunday(); err != nil {
		t.Fatal(err)
	}
	days := map[string]int{}
	var recs []model.ClientRecord
	db.Find(&recs)
	for _, r := range recs {
		days[r.Email] = r.TrafficResetDay
	}
	if days["weekly"] != 7 || days["monthly"] != 1 {
		t.Fatalf("record days = %v, want weekly on 7 and monthly untouched", days)
	}
	var ib model.Inbound
	db.Where("tag = ?", "w").First(&ib)
	var parsed struct {
		Clients []model.Client `json:"clients"`
	}
	if err := json.Unmarshal([]byte(ib.Settings), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Clients[0].TrafficResetDay != 7 || parsed.Clients[1].TrafficResetDay != 1 {
		t.Fatalf("inbound copy = %+v, want the weekly client on 7", parsed.Clients)
	}
}
