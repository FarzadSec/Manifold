package service

import (
	"slices"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/web/access"
)

// ClientAccess applies one actor's ownership, allowance and role limits to user operations.
type ClientAccess struct {
	Actor *access.Actor
}

func (ca ClientAccess) reseller() bool { return ca.Actor.IsReseller() }

// account is the reseller's row as stored now, not as it was when the request was authenticated.
func (ca ClientAccess) account() (model.User, error) {
	var u model.User
	err := database.GetDB().First(&u, ca.Actor.UserId).Error
	return u, err
}

// CheckOwned refuses when any of the emails belongs to someone else.
func (ca ClientAccess) CheckOwned(emails []string) error {
	missing, err := NotOwned(ca.Actor.UserId, emails)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return common.NewError("user not found: " + strings.Join(missing, ", "))
	}
	return nil
}

// CheckMayEdit refuses a change to existing users for a reseller that may only create them.
func (ca ClientAccess) CheckMayEdit() error {
	if !ca.reseller() {
		return nil
	}
	acc, err := ca.account()
	if err != nil {
		return err
	}
	if !acc.MayEditUsers {
		return common.NewError("this account may create users but not change or delete them")
	}
	return nil
}

// CheckMayEnable refuses switching users on while the reseller's allowance is used up.
func (ca ClientAccess) CheckMayEnable() error {
	if !ca.reseller() {
		return nil
	}
	acc, err := ca.account()
	if err != nil {
		return err
	}
	if acc.Suspended != "" {
		return common.NewError("this account's allowance is used up until its next cycle or renewal")
	}
	return nil
}

// CheckMayChangeLimits refuses limit changes for a reseller that sells from plans only.
func (ca ClientAccess) CheckMayChangeLimits() error {
	if !ca.reseller() {
		return nil
	}
	acc, err := ca.account()
	if err != nil {
		return err
	}
	if acc.PlansOnly {
		return common.NewError("this account sells from plans only: it may change names and credentials, not limits")
	}
	return nil
}

// NarrowInbounds keeps only the inbounds a reseller may put its users on.
func (ca ClientAccess) NarrowInbounds(ids []int) ([]int, error) {
	if !ca.reseller() {
		return ids, nil
	}
	acc, err := ca.account()
	if err != nil {
		return nil, err
	}
	if !acc.InboundsLimited {
		return ids, nil
	}
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if slices.Contains(acc.AllowedInboundIds, id) {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil, common.NewError("none of the chosen inbounds is open to this account")
	}
	return out, nil
}

// AllowedInboundIDs is the inbounds a reseller may use, or nil when it may use all of them.
func (ca ClientAccess) AllowedInboundIDs() ([]int, error) {
	if !ca.reseller() {
		return nil, nil
	}
	acc, err := ca.account()
	if err != nil || !acc.InboundsLimited {
		return nil, err
	}
	if acc.AllowedInboundIds == nil {
		return []int{}, nil
	}
	return acc.AllowedInboundIds, nil
}

// checkDevices enforces a role's device cap: a user with no device limit is refused, not capped.
func (ca ClientAccess) checkDevices(limitHwid int) error {
	if ca.Actor.Role == nil || ca.Actor.Role.MaxDevicesPerUser == 0 {
		return nil
	}
	if limitHwid == 0 || limitHwid > ca.Actor.Role.MaxDevicesPerUser {
		return common.NewErrorf("this account's role allows at most %d devices per user", ca.Actor.Role.MaxDevicesPerUser)
	}
	return nil
}

// checkLength enforces a role's longest plan, counted from first use or from now.
func (ca ClientAccess) checkLength(expiry int64, now time.Time) error {
	if ca.Actor.Role == nil || ca.Actor.Role.MaxPlanDays == 0 {
		return nil
	}
	var days int64
	switch {
	case expiry == 0:
		days = -1
	case expiry < 0:
		days = -expiry / dayMs
	default:
		days = (expiry - now.UnixMilli() + dayMs - 1) / dayMs
	}
	if days < 0 || days > int64(ca.Actor.Role.MaxPlanDays) {
		return common.NewErrorf("this account's role sells plans of at most %d days", ca.Actor.Role.MaxPlanDays)
	}
	return nil
}

// CheckUpdate applies the device and length caps to an edit of an existing user.
func (ca ClientAccess) CheckUpdate(expiry int64, limitHwid int, now time.Time) error {
	if err := ca.checkDevices(limitHwid); err != nil {
		return err
	}
	return ca.checkLength(expiry, now)
}

// PrepareCreate applies a plan, picks the owner and checks every limit before users are
// created; it returns the owner to give the created users.
func (ca ClientAccess) PrepareCreate(payloads []*ClientCreatePayload, planID, requestedOwner int, allInboundIDs []int, now time.Time) (int, error) {
	owner := 0
	switch {
	case ca.Actor.IsMainAdmin():
		owner = requestedOwner
	default:
		owner = ca.Actor.UserId
	}
	var ownerAcc model.User
	if owner > 0 {
		if err := database.GetDB().First(&ownerAcc, owner).Error; err != nil {
			return 0, common.NewError("the chosen owner does not exist")
		}
	}
	var plan *model.Plan
	if planID > 0 {
		p, err := (&PlanService{}).Get(planID)
		if err != nil {
			return 0, err
		}
		plan = p
	}
	if ownerAcc.Tier == access.TierReseller {
		if ownerAcc.Suspended != "" {
			return 0, common.NewError("this account's allowance is used up until its next cycle or renewal")
		}
		if ownerAcc.PlansOnly && (plan == nil || !slices.Contains(ownerAcc.AllowedPlanIds, plan.Id)) {
			return 0, common.NewError("this account sells from its plans only")
		}
		if err := checkOwnerRoom(ownerAcc, len(payloads)); err != nil {
			return 0, err
		}
	}
	for _, p := range payloads {
		if plan != nil {
			ApplyPlan(plan, p, allInboundIDs, now)
		}
		if ownerAcc.Tier == access.TierReseller && ownerAcc.InboundsLimited {
			narrowed := make([]int, 0, len(p.InboundIds))
			for _, id := range p.InboundIds {
				if slices.Contains(ownerAcc.AllowedInboundIds, id) {
					narrowed = append(narrowed, id)
				}
			}
			if len(narrowed) == 0 {
				return 0, common.NewError("none of the chosen inbounds is open to this account")
			}
			p.InboundIds = narrowed
		}
		if ca.reseller() {
			if err := ca.checkDevices(p.LimitHwid); err != nil {
				return 0, err
			}
			if err := ca.checkLength(p.Client.ExpiryTime, now); err != nil {
				return 0, err
			}
			var taken int64
			if err := database.GetDB().Model(&model.ClientRecord{}).
				Where("LOWER(email) = ? AND owner_id <> ?", strings.ToLower(strings.TrimSpace(p.Client.Email)), ca.Actor.UserId).
				Count(&taken).Error; err != nil {
				return 0, err
			}
			if taken > 0 {
				return 0, common.NewError("that name is already in use: " + p.Client.Email)
			}
		}
	}
	return owner, nil
}

// checkOwnerRoom refuses when the reseller would own more users than its account or role allow.
func checkOwnerRoom(acc model.User, adding int) error {
	limit := acc.MaxUsers
	if acc.RoleId > 0 {
		var role model.Role
		if err := database.GetDB().First(&role, acc.RoleId).Error; err == nil && role.MaxUsersPerAccount > 0 {
			if limit == 0 || role.MaxUsersPerAccount < limit {
				limit = role.MaxUsersPerAccount
			}
		}
	}
	if limit == 0 {
		return nil
	}
	owned, err := CountOwned(acc.Id)
	if err != nil {
		return err
	}
	if int(owned)+adding > limit {
		return common.NewErrorf("this account may own at most %d users (it owns %d)", limit, owned)
	}
	return nil
}
