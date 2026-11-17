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

// Reach is the plans and inbounds an account may hand its users; a list counts only when limited.
type Reach struct {
	PlansOnly       bool
	Plans           []int
	InboundsLimited bool
	Inbounds        []int
}

// ReachOf is the stricter of a reseller account's lists and its role's; either may be absent.
func ReachOf(acc *model.User, role *model.Role) Reach {
	var r Reach
	if acc != nil && acc.Tier == access.TierReseller {
		r = Reach{PlansOnly: acc.PlansOnly, Plans: acc.AllowedPlanIds, InboundsLimited: acc.InboundsLimited, Inbounds: acc.AllowedInboundIds}
	}
	if role != nil {
		r.PlansOnly, r.Plans = narrowIDs(r.PlansOnly, r.Plans, role.PlansOnly, role.AllowedPlanIds)
		r.InboundsLimited, r.Inbounds = narrowIDs(r.InboundsLimited, r.Inbounds, role.InboundsLimited, role.AllowedInboundIds)
	}
	return r
}

func narrowIDs(limited bool, ids []int, alsoLimited bool, also []int) (bool, []int) {
	switch {
	case limited && alsoLimited:
		both := []int{}
		for _, id := range ids {
			if slices.Contains(also, id) {
				both = append(both, id)
			}
		}
		return true, both
	case alsoLimited:
		return true, also
	}
	return limited, ids
}

// Reach is what the actor may hand its own users, from its account as stored now and its role.
func (ca ClientAccess) Reach() (Reach, error) {
	if !ca.reseller() {
		return ReachOf(nil, ca.Actor.Bounds()), nil
	}
	acc, err := ca.account()
	if err != nil {
		return Reach{}, err
	}
	return ReachOf(&acc, ca.Actor.Bounds()), nil
}

// CheckMayChangeLimits refuses limit changes for an account that sells from plans only.
func (ca ClientAccess) CheckMayChangeLimits() error {
	r, err := ca.Reach()
	if err != nil {
		return err
	}
	if r.PlansOnly {
		return common.NewError("this account sells from plans only: it may change names and credentials, not limits")
	}
	return nil
}

// NarrowInbounds is the inbounds a user put on ids lands on: their linked copies too, kept to
// those the actor may put its users on.
func (ca ClientAccess) NarrowInbounds(ids []int) ([]int, error) {
	r, err := ca.Reach()
	if err != nil {
		return nil, err
	}
	linked, err := LinkedIDs(ids)
	if err != nil {
		return nil, err
	}
	return r.narrowInbounds(linked)
}

func (r Reach) narrowInbounds(ids []int) ([]int, error) {
	if !r.InboundsLimited {
		return ids, nil
	}
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if slices.Contains(r.Inbounds, id) {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil, common.NewError("none of the chosen inbounds is open to this account")
	}
	return out, nil
}

// AllowedInboundIDs is the inbounds the actor may use, or nil when it may use all of them.
func (ca ClientAccess) AllowedInboundIDs() ([]int, error) {
	r, err := ca.Reach()
	if err != nil || !r.InboundsLimited {
		return nil, err
	}
	if r.Inbounds == nil {
		return []int{}, nil
	}
	return r.Inbounds, nil
}

// UserChange is the limits an operation sets on a user; nil leaves one as it is, unchecked.
type UserChange struct {
	Total   *int64
	Expiry  *int64
	Devices *int
}

// CheckChange holds what an operation sets inside the bounds of the actor's role: a ceiling
// refuses an unlimited value, a floor a limited value below it.
func (ca ClientAccess) CheckChange(ch UserChange, now time.Time) error {
	role := ca.Actor.Bounds()
	if role == nil {
		return nil
	}
	if ch.Total != nil {
		total := *ch.Total
		if role.MaxUserBytes > 0 && (total <= 0 || total > role.MaxUserBytes) {
			return common.NewErrorf("this account's role gives each user at most %s", common.FormatTraffic(role.MaxUserBytes))
		}
		if role.MinUserBytes > 0 && total > 0 && total < role.MinUserBytes {
			return common.NewErrorf("this account's role gives each user at least %s", common.FormatTraffic(role.MinUserBytes))
		}
	}
	if ch.Expiry != nil {
		days := planDays(*ch.Expiry, now)
		if role.MaxPlanDays > 0 && (days < 0 || days > int64(role.MaxPlanDays)) {
			return common.NewErrorf("this account's role sells plans of at most %d days", role.MaxPlanDays)
		}
		if role.MinPlanDays > 0 && days >= 0 && days < int64(role.MinPlanDays) {
			return common.NewErrorf("this account's role sells plans of at least %d days", role.MinPlanDays)
		}
	}
	if ch.Devices != nil {
		devices := *ch.Devices
		if role.MaxDevicesPerUser > 0 && (devices <= 0 || devices > role.MaxDevicesPerUser) {
			return common.NewErrorf("this account's role allows at most %d devices per user", role.MaxDevicesPerUser)
		}
		if role.MinDevicesPerUser > 0 && devices > 0 && devices < role.MinDevicesPerUser {
			return common.NewErrorf("this account's role allows at least %d devices per user", role.MinDevicesPerUser)
		}
	}
	return nil
}

// planDays is a plan's length counted from first use or from now; -1 when it never ends.
func planDays(expiry int64, now time.Time) int64 {
	switch {
	case expiry == 0:
		return -1
	case expiry < 0:
		return -expiry / dayMs
	default:
		return (expiry - now.UnixMilli() + dayMs - 1) / dayMs
	}
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
	ownerRole := roleOf(ownerAcc)
	reach := ReachOf(&ownerAcc, ownerRole)
	if ownerAcc.Tier == access.TierReseller && ownerAcc.Suspended != "" {
		return 0, common.NewError("this account's allowance is used up until its next cycle or renewal")
	}
	if reach.PlansOnly && (plan == nil || !slices.Contains(reach.Plans, plan.Id)) {
		return 0, common.NewError("this account sells from its plans only")
	}
	if ownerAcc.Tier == access.TierReseller {
		if err := checkOwnerRoom(ownerAcc, ownerRole, len(payloads)); err != nil {
			return 0, err
		}
	}
	for _, p := range payloads {
		if plan != nil {
			ApplyPlan(plan, p, allInboundIDs, now)
		}
		linked, err := LinkedIDs(p.InboundIds)
		if err != nil {
			return 0, err
		}
		narrowed, err := reach.narrowInbounds(linked)
		if err != nil {
			return 0, err
		}
		p.InboundIds = narrowed
		if err := ca.CheckChange(UserChange{Total: &p.Client.TotalGB, Expiry: &p.Client.ExpiryTime, Devices: &p.LimitHwid}, now); err != nil {
			return 0, err
		}
		if ca.reseller() {
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
// roleOf is the account's role, nil when it has none or the role is gone.
func roleOf(acc model.User) *model.Role {
	if acc.RoleId == 0 {
		return nil
	}
	var role model.Role
	if database.GetDB().First(&role, acc.RoleId).Error != nil {
		return nil
	}
	return &role
}

func checkOwnerRoom(acc model.User, role *model.Role, adding int) error {
	limit := acc.MaxUsers
	if role != nil && role.MaxUsersPerAccount > 0 && (limit == 0 || role.MaxUsersPerAccount < limit) {
		limit = role.MaxUsersPerAccount
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
