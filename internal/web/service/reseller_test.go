package service

import (
	"errors"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/access"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestCycleStartAt(t *testing.T) {
	loc := time.UTC
	at := func(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, loc) }
	cases := []struct {
		name       string
		now        time.Time
		cycle      string
		day, month int
		want       time.Time
	}{
		{"weekly on a later weekday", at(2026, 11, 19, 15), CycleWeekly, 1, 0, at(2026, 11, 16, 0)},
		{"weekly on the reset day itself", at(2026, 11, 16, 0), CycleWeekly, 1, 0, at(2026, 11, 16, 0)},
		{"weekly on Sunday as day 7", at(2026, 11, 21, 9), CycleWeekly, 7, 0, at(2026, 11, 15, 0)},
		{"monthly after the day", at(2026, 11, 20, 8), CycleMonthly, 5, 0, at(2026, 11, 5, 0)},
		{"monthly before the day goes back a month", at(2026, 11, 3, 8), CycleMonthly, 5, 0, at(2026, 10, 5, 0)},
		{"monthly day 31 in a short month", at(2026, 3, 1, 8), CycleMonthly, 31, 0, at(2026, 2, 28, 0)},
		{"yearly before the date goes back a year", at(2026, 2, 1, 8), CycleYearly, 21, 3, at(2025, 3, 21, 0)},
		{"yearly after the date", at(2026, 4, 1, 8), CycleYearly, 21, 3, at(2026, 3, 21, 0)},
		{"no cycle", at(2026, 4, 1, 8), CycleNone, 1, 1, time.Time{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CycleStartAt(tc.now, tc.cycle, tc.day, tc.month); !got.Equal(tc.want) {
				t.Fatalf("CycleStartAt = %v, want %v", got, tc.want)
			}
		})
	}
}

func seedReseller(t *testing.T, volume, used int64) model.User {
	t.Helper()
	r := model.User{Username: "seller", Tier: access.TierReseller, MayEditUsers: true, VolumeBytes: volume, CycleUsed: used, ResetCycle: CycleNone}
	if err := database.GetDB().Create(&r).Error; err != nil {
		t.Fatal(err)
	}
	return r
}

func seedOwnedClient(t *testing.T, svc *ClientService, port int, email string, ownerID int, enable bool) *model.Inbound {
	t.Helper()
	c := model.Client{Email: email, ID: "11111111-1111-1111-1111-11111111111" + string(rune('0'+port%10)), Enable: enable}
	ib := mkInbound(t, port, model.VLESS, clientsSettings(t, []model.Client{c}))
	if err := svc.SyncInbound(nil, ib.Id, []model.Client{c}); err != nil {
		t.Fatalf("seed linkage: %v", err)
	}
	mkTraffic(t, ib.Id, email, 0, 0, 0, 0, enable)
	if err := database.GetDB().Model(&model.ClientRecord{}).Where("email = ?", email).
		Updates(map[string]any{"owner_id": ownerID, "enable": enable}).Error; err != nil {
		t.Fatal(err)
	}
	return ib
}

func usedOf(t *testing.T, id int) model.User {
	t.Helper()
	var u model.User
	if err := database.GetDB().First(&u, id).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

// A reseller pays for its users' traffic as it is added, and the panel's own users charge nobody.
func TestTrafficIsChargedToTheOwningReseller(t *testing.T) {
	setupBulkDB(t)
	svc, inboundSvc := &ClientService{}, &InboundService{}
	r := seedReseller(t, 0, 0)
	seedOwnedClient(t, svc, 53001, "bought@x", r.Id, true)
	seedOwnedClient(t, svc, 53002, "house@x", 0, true)

	_, _, err := inboundSvc.AddTraffic(nil, []*xray.ClientTraffic{
		{Email: "bought@x", Up: 300, Down: 700},
		{Email: "house@x", Up: 5000, Down: 5000},
	})
	if err != nil {
		t.Fatalf("AddTraffic: %v", err)
	}
	got := usedOf(t, r.Id)
	if got.CycleUsed != 1000 || got.TotalUsed != 1000 {
		t.Fatalf("reseller charged cycle=%d total=%d, want 1000 and 1000", got.CycleUsed, got.TotalUsed)
	}
}

// Running out switches off only the reseller's enabled users; renewal brings back exactly
// those, while a user switched off by hand stays off.
func TestEnforceSuspendsAndRestoresOnlyTheResellersUsers(t *testing.T) {
	setupBulkDB(t)
	svc, inboundSvc := &ClientService{}, &InboundService{}
	r := seedReseller(t, 1000, 1500)
	seedOwnedClient(t, svc, 53011, "active@x", r.Id, true)
	seedOwnedClient(t, svc, 53012, "byhand@x", r.Id, false)
	seedOwnedClient(t, svc, 53013, "panel@x", 0, true)
	resellers := &ResellerService{}

	if err := resellers.Enforce(inboundSvc, time.Now()); err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if got := usedOf(t, r.Id).Suspended; got != access.SuspendedVolume {
		t.Fatalf("suspended = %q, want %q", got, access.SuspendedVolume)
	}
	if recordEnableOf(t, svc, "active@x") || !recordEnableOf(t, svc, "panel@x") {
		t.Fatal("suspension must switch off the reseller's users and only those")
	}

	if err := database.GetDB().Model(&model.User{}).Where("id = ?", r.Id).Update("volume_bytes", 5000).Error; err != nil {
		t.Fatal(err)
	}
	if err := resellers.Enforce(inboundSvc, time.Now()); err != nil {
		t.Fatalf("Enforce after renewal: %v", err)
	}
	if got := usedOf(t, r.Id).Suspended; got != "" {
		t.Fatalf("suspended = %q after renewal, want none", got)
	}
	if !recordEnableOf(t, svc, "active@x") {
		t.Fatal("renewal did not bring back the user the suspension switched off")
	}
	if recordEnableOf(t, svc, "byhand@x") {
		t.Fatal("renewal switched on a user that was off by hand")
	}
}

// An expired reseller is cut off at once: its sessions end with the epoch bump.
func TestEnforceEndsAnExpiredResellersSessions(t *testing.T) {
	setupBulkDB(t)
	r := seedReseller(t, 0, 0)
	if err := database.GetDB().Model(&model.User{}).Where("id = ?", r.Id).
		Update("expires_at", time.Now().Add(-time.Minute).UnixMilli()).Error; err != nil {
		t.Fatal(err)
	}
	if err := (&ResellerService{}).Enforce(&InboundService{}, time.Now()); err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	got := usedOf(t, r.Id)
	if got.Suspended != access.SuspendedExpired || got.LoginEpoch != r.LoginEpoch+1 {
		t.Fatalf("suspended=%q epoch=%d, want expired and epoch %d", got.Suspended, got.LoginEpoch, r.LoginEpoch+1)
	}
	if _, err := access.Resolve(&got); !errors.Is(err, access.ErrAccountExpired) {
		t.Fatalf("Resolve of an expired reseller = %v, want ErrAccountExpired", err)
	}
}

// A new cycle starts the count again and lifts a volume suspension.
func TestEnforceStartsANewCycle(t *testing.T) {
	setupBulkDB(t)
	r := seedReseller(t, 1000, 1500)
	if err := database.GetDB().Model(&model.User{}).Where("id = ?", r.Id).Updates(map[string]any{
		"reset_cycle": CycleMonthly, "reset_day": 1, "cycle_start": time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local).UnixMilli(),
		"suspended": access.SuspendedVolume,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := (&ResellerService{}).Enforce(&InboundService{}, time.Date(2026, 11, 2, 10, 0, 0, 0, time.Local)); err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	got := usedOf(t, r.Id)
	if got.CycleUsed != 0 || got.Suspended != "" {
		t.Fatalf("after the new cycle used=%d suspended=%q, want 0 and none", got.CycleUsed, got.Suspended)
	}
}

// A role decides what a used-up allowance and a switch-off do to an account's users; lifting
// the cause brings back the users the panel switched off.
func TestRolePoliciesDecideWhenAnAccountsUsersGoOff(t *testing.T) {
	cases := []struct {
		name     string
		tier     string
		role     *model.Role
		limited  bool
		disabled bool
		wantOff  bool
	}{
		{"used up without a role", access.TierReseller, nil, true, false, true},
		{"used up, the role keeps users on", access.TierReseller, &model.Role{KeepUsersWhenLimited: true}, true, false, false},
		{"switched off without a role", access.TierReseller, nil, false, true, false},
		{"switched off, the role turns users off", access.TierReseller, &model.Role{UsersOffWhenDisabled: true}, false, true, true},
		{"an operator switched off, the role turns users off", access.TierOperator, &model.Role{UsersOffWhenDisabled: true}, false, true, true},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupBulkDB(t)
			db := database.GetDB()
			acc := model.User{Username: "acc", Tier: tc.tier, Disabled: tc.disabled, ResetCycle: CycleNone}
			if tc.limited {
				acc.VolumeBytes, acc.CycleUsed = 1000, 1500
			}
			if tc.role != nil {
				tc.role.Name, tc.role.Tier = "policy", tc.tier
				if err := db.Create(tc.role).Error; err != nil {
					t.Fatal(err)
				}
				acc.RoleId = tc.role.Id
			}
			if err := db.Create(&acc).Error; err != nil {
				t.Fatal(err)
			}
			svc, inboundSvc, resellers := &ClientService{}, &InboundService{}, &ResellerService{}
			seedOwnedClient(t, svc, 53100+i, "u@x", acc.Id, true)

			if err := resellers.Enforce(inboundSvc, time.Now()); err != nil {
				t.Fatalf("Enforce: %v", err)
			}
			if off := !recordEnableOf(t, svc, "u@x"); off != tc.wantOff {
				t.Fatalf("user off = %v, want %v", off, tc.wantOff)
			}
			if got := usedOf(t, acc.Id).UsersOff; got != tc.wantOff {
				t.Fatalf("usersOff = %v, want %v", got, tc.wantOff)
			}
			if err := db.Model(&model.User{}).Where("id = ?", acc.Id).
				Updates(map[string]any{"volume_bytes": 0, "disabled": false}).Error; err != nil {
				t.Fatal(err)
			}
			if err := resellers.Enforce(inboundSvc, time.Now()); err != nil {
				t.Fatalf("Enforce after lifting: %v", err)
			}
			if !recordEnableOf(t, svc, "u@x") || usedOf(t, acc.Id).UsersOff {
				t.Fatal("lifting the cause did not bring the user back on")
			}
		})
	}
}
