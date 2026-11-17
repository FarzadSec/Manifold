package service

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/web/access"
)

// Reseller reset cycles.
const (
	CycleNone    = "none"
	CycleWeekly  = "weekly"
	CycleMonthly = "monthly"
	CycleYearly  = "yearly"
)

// chargeOwnerUsage adds traffic to each owning account in the transaction that adds it to the
// users themselves, so a reseller pays exactly what was used while it owned the user.
func chargeOwnerUsage(tx *gorm.DB, usage map[string]int64) error {
	emails := make([]string, 0, len(usage))
	for email, bytes := range usage {
		if bytes > 0 {
			emails = append(emails, email)
		}
	}
	if len(emails) == 0 {
		return nil
	}
	var rows []struct {
		Email   string
		OwnerId int
	}
	if err := tx.Model(&model.ClientRecord{}).Select("email", "owner_id").
		Where("owner_id > 0 AND email IN ?", emails).Scan(&rows).Error; err != nil {
		return err
	}
	byOwner := map[int]int64{}
	for _, r := range rows {
		byOwner[r.OwnerId] += usage[r.Email]
	}
	for _, owner := range slices.Sorted(maps.Keys(byOwner)) {
		bytes := byOwner[owner]
		if err := tx.Exec(`UPDATE users SET cycle_used = cycle_used + ?, total_used = total_used + ? WHERE id = ?`,
			bytes, bytes, owner).Error; err != nil {
			return err
		}
	}
	return nil
}

// CycleStartAt is when the reseller's current cycle began: the last reset day at midnight.
func CycleStartAt(now time.Time, cycle string, day, month int) time.Time {
	midnight := func(t time.Time) time.Time {
		y, m, d := t.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
	}
	clampDay := func(y int, m time.Month, d int) int {
		last := time.Date(y, m+1, 0, 0, 0, 0, 0, now.Location()).Day()
		return min(max(d, 1), last)
	}
	switch cycle {
	case CycleWeekly:
		want := time.Weekday(day % 7)
		if day < 1 || day > 7 {
			want = time.Monday
		}
		back := (int(now.Weekday()) - int(want) + 7) % 7
		return midnight(now.AddDate(0, 0, -back))
	case CycleMonthly:
		y, m, _ := now.Date()
		start := time.Date(y, m, clampDay(y, m, day), 0, 0, 0, 0, now.Location())
		if start.After(now) {
			py, pm, _ := start.AddDate(0, 0, -start.Day()).Date()
			start = time.Date(py, pm, clampDay(py, pm, day), 0, 0, 0, 0, now.Location())
		}
		return start
	case CycleYearly:
		mon := time.Month(min(max(month, 1), 12))
		y := now.Year()
		start := time.Date(y, mon, clampDay(y, mon, day), 0, 0, 0, 0, now.Location())
		if start.After(now) {
			start = time.Date(y-1, mon, clampDay(y-1, mon, day), 0, 0, 0, 0, now.Location())
		}
		return start
	default:
		return time.Time{}
	}
}

// resellerSuspension is the reason a reseller's users must be off now, or "".
func resellerSuspension(r *model.User, nowMs int64) string {
	if r.ExpiresAt > 0 && nowMs >= r.ExpiresAt {
		return access.SuspendedExpired
	}
	if r.VolumeBytes > 0 && r.CycleUsed >= r.VolumeBytes {
		return access.SuspendedVolume
	}
	return ""
}

// ResellerService keeps every reseller inside its allowance.
type ResellerService struct{}

// Enforce starts new cycles, then switches a reseller's users off when it runs out and back on
// when it is renewed; users switched off by hand are never touched.
func (s *ResellerService) Enforce(inboundSvc *InboundService, now time.Time) error {
	db := database.GetDB()
	var resellers []model.User
	if err := db.Where("tier = ?", access.TierReseller).Find(&resellers).Error; err != nil {
		return err
	}
	var errs []error
	for i := range resellers {
		if err := s.enforceOne(db, inboundSvc, &resellers[i], now); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *ResellerService) enforceOne(db *gorm.DB, inboundSvc *InboundService, r *model.User, now time.Time) error {
	if r.ResetCycle != "" && r.ResetCycle != CycleNone {
		start := CycleStartAt(now, r.ResetCycle, r.ResetDay, r.ResetMonth).UnixMilli()
		if start > r.CycleStart {
			if err := db.Model(&model.User{}).Where("id = ?", r.Id).
				Updates(map[string]any{"cycle_start": start, "cycle_used": 0}).Error; err != nil {
				return err
			}
			r.CycleStart, r.CycleUsed = start, 0
		}
	}
	want := resellerSuspension(r, now.UnixMilli())
	if want == r.Suspended {
		return nil
	}
	clientSvc := ClientService{}
	if want != "" {
		var emails []string
		if err := db.Model(&model.ClientRecord{}).Where("owner_id = ? AND enable = ?", r.Id, true).
			Pluck("email", &emails).Error; err != nil {
			return err
		}
		if len(emails) > 0 {
			if _, _, err := clientSvc.BulkSetEnable(inboundSvc, emails, false); err != nil {
				return err
			}
			if err := db.Model(&model.ClientRecord{}).Where("owner_id = ? AND email IN ?", r.Id, emails).
				Update("owner_suspended", true).Error; err != nil {
				return err
			}
		}
		if want == access.SuspendedExpired {
			if err := db.Exec(`UPDATE users SET login_epoch = login_epoch + 1 WHERE id = ?`, r.Id).Error; err != nil {
				return err
			}
		}
		logger.Infof("reseller %q is out of its allowance (%s): %d users switched off", r.Username, want, len(emails))
	} else if err := s.restoreUsers(db, inboundSvc, r.Id); err != nil {
		return err
	}
	return db.Model(&model.User{}).Where("id = ?", r.Id).Update("suspended", want).Error
}

// RestoreUsers switches back on the users a reseller's suspension switched off, when the
// account stops being a reseller.
func (s *ResellerService) RestoreUsers(inboundSvc *InboundService, ownerID int) error {
	return s.restoreUsers(database.GetDB(), inboundSvc, ownerID)
}

// restoreUsers switches back on the users a reseller's suspension switched off.
func (s *ResellerService) restoreUsers(db *gorm.DB, inboundSvc *InboundService, ownerID int) error {
	var emails []string
	if err := db.Model(&model.ClientRecord{}).Where("owner_id = ? AND owner_suspended = ?", ownerID, true).
		Pluck("email", &emails).Error; err != nil {
		return err
	}
	if len(emails) == 0 {
		return nil
	}
	clientSvc := ClientService{}
	if _, _, err := clientSvc.BulkSetEnable(inboundSvc, emails, true); err != nil {
		return err
	}
	return db.Model(&model.ClientRecord{}).Where("email IN ?", emails).Update("owner_suspended", false).Error
}

// ReleaseUsers hands every user of an account to the panel, switching back on those its
// suspension switched off, when the account is deleted.
func (s *ResellerService) ReleaseUsers(inboundSvc *InboundService, ownerID int) error {
	db := database.GetDB()
	if err := s.restoreUsers(db, inboundSvc, ownerID); err != nil {
		return err
	}
	return db.Model(&model.ClientRecord{}).Where("owner_id = ?", ownerID).Update("owner_id", 0).Error
}

// NotOwned returns the emails of the list the account does not own, matched case-insensitively.
func NotOwned(ownerID int, emails []string) ([]string, error) {
	wanted := map[string]string{}
	for _, e := range emails {
		if e = strings.TrimSpace(e); e != "" {
			wanted[strings.ToLower(e)] = e
		}
	}
	if len(wanted) == 0 {
		return nil, nil
	}
	var owned []string
	if err := database.GetDB().Model(&model.ClientRecord{}).
		Where("owner_id = ? AND LOWER(email) IN ?", ownerID, slices.Collect(maps.Keys(wanted))).
		Pluck("LOWER(email)", &owned).Error; err != nil {
		return nil, err
	}
	for _, e := range owned {
		delete(wanted, e)
	}
	return slices.Sorted(maps.Values(wanted)), nil
}

// OwnedEmailSet is every email the account owns, lower-cased, for filtering lists.
func OwnedEmailSet(ownerID int) (map[string]bool, error) {
	return ownedEmailsOf(ownerID)
}

func ownedEmailsOf(ownerID int) (map[string]bool, error) {
	var emails []string
	if err := database.GetDB().Model(&model.ClientRecord{}).Where("owner_id = ?", ownerID).
		Pluck("LOWER(email)", &emails).Error; err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(emails))
	for _, e := range emails {
		set[e] = true
	}
	return set, nil
}

// CountOwned is how many users the account owns.
func CountOwned(ownerID int) (int64, error) {
	var n int64
	err := database.GetDB().Model(&model.ClientRecord{}).Where("owner_id = ?", ownerID).Count(&n).Error
	return n, err
}

// SetOwner hands users to an account (0 is the panel).
func SetOwner(emails []string, ownerID int) error {
	if len(emails) == 0 {
		return nil
	}
	lower := make([]string, 0, len(emails))
	for _, e := range emails {
		lower = append(lower, strings.ToLower(strings.TrimSpace(e)))
	}
	return database.GetDB().Model(&model.ClientRecord{}).Where("LOWER(email) IN ?", lower).
		Updates(map[string]any{"owner_id": ownerID, "owner_suspended": false}).Error
}

// ChangeOwner hands users to another account (0 is the panel); a reseller's user limit is
// checked once for the whole selection, and a suspended reseller takes no users.
func (s *ClientService) ChangeOwner(inboundSvc *InboundService, emails []string, ownerID int) error {
	clean := trimmedUniqueEmails(emails)
	if len(clean) == 0 {
		return common.NewError("no users selected")
	}
	if ownerID > 0 {
		var acc model.User
		if err := database.GetDB().First(&acc, ownerID).Error; err != nil {
			return common.NewError("the chosen owner does not exist")
		}
		if acc.Tier == access.TierReseller {
			already, err := NotOwned(ownerID, clean)
			if err != nil {
				return err
			}
			if err := checkOwnerRoom(acc, roleOf(acc), len(already)); err != nil {
				return err
			}
			if acc.Suspended != "" {
				return common.NewError("that reseller's allowance is used up")
			}
		}
	}
	var suspended []string
	if err := database.GetDB().Model(&model.ClientRecord{}).Where("email IN ? AND owner_suspended = ?", clean, true).
		Pluck("email", &suspended).Error; err != nil {
		return err
	}
	if len(suspended) > 0 {
		if _, _, err := s.BulkSetEnable(inboundSvc, suspended, true); err != nil {
			return err
		}
	}
	return SetOwner(clean, ownerID)
}
