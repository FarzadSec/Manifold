package panel

import (
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/util/crypto"
	"github.com/mhsanaei/3x-ui/v3/internal/web/access"
	"github.com/mhsanaei/3x-ui/v3/internal/web/entity"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// MinAdminPasswordLength is the shortest password an admin account may be given.
const MinAdminPasswordLength = 8

// AdminView is an admin account as the Admins page shows it, without its secrets.
type AdminView struct {
	model.User
	Password   string `json:"password,omitempty"`
	RoleName   string `json:"roleName" example:"Reseller"`
	OwnedUsers int64  `json:"ownedUsers" example:"12"`
}

// AdminInput is what the Admins form sends; an empty password keeps the current one.
type AdminInput struct {
	Username          string `json:"username" example:"seller1"`
	Password          string `json:"password" example:"long-password"`
	Tier              string `json:"tier" example:"reseller"`
	RoleId            int    `json:"roleId" example:"0"`
	Remark            string `json:"remark"`
	Disabled          bool   `json:"disabled" example:"false"`
	VolumeBytes       int64  `json:"volumeBytes" example:"1099511627776"`
	ResetCycle        string `json:"resetCycle" example:"monthly"`
	ResetDay          int    `json:"resetDay" example:"1"`
	ResetMonth        int    `json:"resetMonth" example:"0"`
	ExpiresAt         int64  `json:"expiresAt" example:"0"`
	MaxUsers          int    `json:"maxUsers" example:"100"`
	MayEditUsers      bool   `json:"mayEditUsers" example:"true"`
	PlansOnly         bool   `json:"plansOnly" example:"false"`
	AllowedPlanIds    []int  `json:"allowedPlanIds" example:"[1,2]"`
	InboundsLimited   bool   `json:"inboundsLimited" example:"false"`
	AllowedInboundIds []int  `json:"allowedInboundIds" example:"[1]"`
	// Permissions added to or taken from the role for this account alone.
	GrantedPermissions []string `json:"grantedPermissions" example:"[\"plans:write\"]"`
	DeniedPermissions  []string `json:"deniedPermissions" example:"[\"users:write\"]"`
	SubDomain          string   `json:"subDomain" example:"sub.example.com"`
	BrandTitle         string   `json:"brandTitle"`
	BrandSupportUrl    string   `json:"brandSupportUrl"`
	BrandAnnounce      string   `json:"brandAnnounce"`
}

// AdminService manages the panel's admin accounts.
type AdminService struct {
	inboundService  service.InboundService
	resellerService service.ResellerService
	tokenService    ApiTokenService
}

func toAdminView(u model.User, roles map[int]string, owned map[int]int64) AdminView {
	u.Password, u.TwoFactorSecret = "", ""
	name := roles[u.RoleId]
	if u.RoleId == 0 {
		name = builtinRoleName(u.Tier)
	}
	return AdminView{User: u, RoleName: name, OwnedUsers: owned[u.Id]}
}

func builtinRoleName(tier string) string {
	switch tier {
	case access.TierOperator:
		return "Operator"
	case access.TierReseller:
		return "Reseller"
	default:
		return "Main admin"
	}
}

// List returns every admin account, owner first.
func (s *AdminService) List() ([]AdminView, error) {
	db := database.GetDB()
	var users []model.User
	if err := db.Order("is_owner DESC").Order("id").Find(&users).Error; err != nil {
		return nil, err
	}
	var roles []model.Role
	if err := db.Find(&roles).Error; err != nil {
		return nil, err
	}
	names := map[int]string{}
	for _, r := range roles {
		names[r.Id] = r.Name
	}
	var counts []struct {
		OwnerId int
		N       int64
	}
	if err := db.Model(&model.ClientRecord{}).Select("owner_id, COUNT(*) AS n").Where("owner_id > 0").
		Group("owner_id").Scan(&counts).Error; err != nil {
		return nil, err
	}
	owned := map[int]int64{}
	for _, c := range counts {
		owned[c.OwnerId] = c.N
	}
	out := make([]AdminView, 0, len(users))
	for _, u := range users {
		out = append(out, toAdminView(u, names, owned))
	}
	return out, nil
}

func (s *AdminService) validate(in *AdminInput, id int) error {
	in.Username = strings.TrimSpace(in.Username)
	if in.Username == "" {
		return common.NewError("an admin needs a username")
	}
	if len(in.Username) > MaxUsernameLength {
		return common.NewErrorf("username can not be longer than %d bytes", MaxUsernameLength)
	}
	if !access.ValidTier(in.Tier) {
		return common.NewError("role tier must be admin, operator or reseller")
	}
	db := database.GetDB()
	var clash int64
	if err := db.Model(&model.User{}).Where("username = ? AND id <> ?", in.Username, id).Count(&clash).Error; err != nil {
		return err
	}
	if clash > 0 {
		return common.NewError("that username is taken")
	}
	if in.RoleId > 0 {
		var role model.Role
		if err := db.First(&role, in.RoleId).Error; err != nil {
			return common.NewError("that role does not exist")
		}
		if role.Tier != in.Tier {
			return common.NewError("the role belongs to another tier")
		}
	}
	ceiling := access.Ceiling(in.Tier)
	for _, p := range slices.Concat(in.GrantedPermissions, in.DeniedPermissions) {
		if !slices.Contains(ceiling, p) {
			return common.NewError("the tier does not include permission " + p)
		}
	}
	for _, p := range in.GrantedPermissions {
		if slices.Contains(in.DeniedPermissions, p) {
			return common.NewError("a permission cannot be both added and taken away: " + p)
		}
	}
	switch in.ResetCycle {
	case "":
		in.ResetCycle = service.CycleNone
	case service.CycleNone, service.CycleWeekly, service.CycleMonthly, service.CycleYearly:
	default:
		return common.NewError("reset cycle must be none, weekly, monthly or yearly")
	}
	if in.VolumeBytes < 0 || in.MaxUsers < 0 || in.ExpiresAt < 0 {
		return common.NewError("allowance values cannot be negative")
	}
	in.SubDomain = strings.ToLower(strings.TrimSpace(in.SubDomain))
	if in.SubDomain != "" && !entity.ValidSubDomain(in.SubDomain) {
		return common.NewError("a subscription domain is a host name, or *. and one, with no port or path")
	}
	if in.Tier != access.TierReseller {
		in.VolumeBytes, in.ResetCycle, in.ExpiresAt, in.MaxUsers = 0, service.CycleNone, 0, 0
		in.MayEditUsers, in.PlansOnly, in.InboundsLimited = true, false, false
		in.AllowedPlanIds, in.AllowedInboundIds, in.SubDomain = nil, nil, ""
	}
	return nil
}

func (in *AdminInput) apply(u *model.User) {
	u.Username, u.Tier, u.RoleId, u.Remark = in.Username, in.Tier, in.RoleId, in.Remark
	u.GrantedPermissions, u.DeniedPermissions, u.Disabled = in.GrantedPermissions, in.DeniedPermissions, in.Disabled
	u.VolumeBytes, u.ResetCycle, u.ResetDay, u.ResetMonth = in.VolumeBytes, in.ResetCycle, in.ResetDay, in.ResetMonth
	u.ExpiresAt, u.MaxUsers, u.MayEditUsers, u.PlansOnly = in.ExpiresAt, in.MaxUsers, in.MayEditUsers, in.PlansOnly
	u.AllowedPlanIds, u.InboundsLimited, u.AllowedInboundIds = in.AllowedPlanIds, in.InboundsLimited, in.AllowedInboundIds
	u.SubDomain = strings.TrimSpace(in.SubDomain)
	u.BrandTitle, u.BrandSupportUrl, u.BrandAnnounce = in.BrandTitle, in.BrandSupportUrl, in.BrandAnnounce
}

// Add creates an admin account.
func (s *AdminService) Add(in AdminInput) (*AdminView, error) {
	if err := s.validate(&in, 0); err != nil {
		return nil, err
	}
	if len(in.Password) < MinAdminPasswordLength {
		return nil, common.NewErrorf("the password needs at least %d characters", MinAdminPasswordLength)
	}
	hash, err := crypto.HashPasswordAsBcrypt(in.Password)
	if err != nil {
		return nil, err
	}
	u := model.User{Password: hash}
	in.apply(&u)
	if err := database.GetDB().Create(&u).Error; err != nil {
		return nil, err
	}
	v := toAdminView(u, nil, nil)
	return &v, nil
}

// Update edits an account; a new tier, role or password ends its sessions.
func (s *AdminService) Update(id int, in AdminInput) error {
	db := database.GetDB()
	var u model.User
	if err := db.First(&u, id).Error; err != nil {
		return common.NewError("admin not found")
	}
	if err := s.validate(&in, id); err != nil {
		return err
	}
	if u.IsOwner && in.Tier != access.TierAdmin {
		return common.NewError("the panel owner stays a main admin")
	}
	if u.IsOwner && in.Disabled {
		return common.NewError("the panel owner cannot be switched off")
	}
	relogin := u.Tier != in.Tier || u.RoleId != in.RoleId || in.Disabled && !u.Disabled
	if in.Password != "" {
		if len(in.Password) < MinAdminPasswordLength {
			return common.NewErrorf("the password needs at least %d characters", MinAdminPasswordLength)
		}
		hash, err := crypto.HashPasswordAsBcrypt(in.Password)
		if err != nil {
			return err
		}
		u.Password, relogin = hash, true
	}
	in.apply(&u)
	if relogin {
		u.LoginEpoch++
	}
	if err := db.Save(&u).Error; err != nil {
		return err
	}
	return s.resellerService.EnforceAccount(&s.inboundService, u.Id, time.Now())
}

// Delete removes an account; its users pass to the panel and its tokens are deleted.
func (s *AdminService) Delete(actorID, id int) error {
	db := database.GetDB()
	var u model.User
	if err := db.First(&u, id).Error; err != nil {
		return common.NewError("admin not found")
	}
	if u.IsOwner {
		return common.NewError("the panel owner cannot be deleted")
	}
	if u.Id == actorID {
		return common.NewError("an account cannot delete itself")
	}
	if err := s.resellerService.ReleaseUsers(&s.inboundService, u.Id); err != nil {
		return err
	}
	if err := s.tokenService.DeleteForUser(u.Id); err != nil {
		return err
	}
	return db.Delete(&model.User{}, u.Id).Error
}

// EndSessions signs one account out everywhere.
func (s *AdminService) EndSessions(id int) error {
	return database.GetDB().Model(&model.User{}).Where("id = ?", id).
		Update("login_epoch", gorm.Expr("login_epoch + 1")).Error
}

// Handover makes another main admin the owner; only the current owner may do it.
func (s *AdminService) Handover(actorID, id int) error {
	db := database.GetDB()
	var from, to model.User
	if err := db.First(&from, actorID).Error; err != nil || !from.IsOwner {
		return common.NewError("only the panel owner can hand the panel over")
	}
	if err := db.First(&to, id).Error; err != nil {
		return common.NewError("admin not found")
	}
	if to.Tier != access.TierAdmin {
		return common.NewError("the new owner must already be a main admin")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.User{}).Where("id = ?", from.Id).Update("is_owner", false).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.User{}).Where("id = ?", to.Id).Update("is_owner", true).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ApiToken{}).Where("user_id = 0").Update("user_id", from.Id).Error; err != nil {
			return err
		}
		return database.RekeyInboundsToOwner(tx, to.Id)
	})
}

// RoleService manages custom roles.
type RoleService struct{}

// List returns the custom roles; the three built-in roles are not stored.
func (s *RoleService) List() ([]model.Role, error) {
	var roles []model.Role
	err := database.GetDB().Order("id").Find(&roles).Error
	return roles, err
}

// Save creates or replaces a custom role; editing one ends the sessions of its accounts.
func (s *RoleService) Save(r *model.Role) error {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		return common.NewError("a role needs a name")
	}
	for _, reserved := range []string{"Main admin", "Operator", "Reseller"} {
		if strings.EqualFold(r.Name, reserved) {
			return common.NewError("that name belongs to a built-in role")
		}
	}
	if !access.ValidTier(r.Tier) {
		return common.NewError("a role is based on admin, operator or reseller")
	}
	ceiling := access.Ceiling(r.Tier)
	for _, p := range r.Permissions {
		if !slices.Contains(ceiling, p) {
			return common.NewError("the tier does not include permission " + p)
		}
	}
	if r.MaxUsersPerAccount < 0 || r.MaxDevicesPerUser < 0 || r.MaxPlanDays < 0 ||
		r.MinUserBytes < 0 || r.MaxUserBytes < 0 || r.MinPlanDays < 0 || r.MinDevicesPerUser < 0 {
		return common.NewError("role limits cannot be negative")
	}
	if r.MaxUserBytes > 0 && r.MinUserBytes > r.MaxUserBytes || r.MaxPlanDays > 0 && r.MinPlanDays > r.MaxPlanDays ||
		r.MaxDevicesPerUser > 0 && r.MinDevicesPerUser > r.MaxDevicesPerUser {
		return common.NewError("a role's floor cannot be above its ceiling")
	}
	if r.Tier == access.TierReseller {
		r.UsersReadScope, r.UsersWriteScope = access.ScopeOwn, access.ScopeOwn
	}
	if !access.ValidScopes(r.UsersReadScope, r.UsersWriteScope) {
		return common.NewError("a role reaches all users or its own, and changes no user it cannot see")
	}
	db := database.GetDB()
	var clash int64
	if err := db.Model(&model.Role{}).Where("name = ? AND id <> ?", r.Name, r.Id).Count(&clash).Error; err != nil {
		return err
	}
	if clash > 0 {
		return common.NewError("a role with that name already exists")
	}
	if r.Id == 0 {
		return db.Create(r).Error
	}
	var old model.Role
	if err := db.First(&old, r.Id).Error; err != nil {
		return common.NewError("role not found")
	}
	if old.Tier != r.Tier {
		var held int64
		db.Model(&model.User{}).Where("role_id = ?", r.Id).Count(&held)
		if held > 0 {
			return common.NewError("a role held by accounts keeps its tier")
		}
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(r).Error; err != nil {
			return err
		}
		return tx.Model(&model.User{}).Where("role_id = ?", r.Id).
			Update("login_epoch", gorm.Expr("login_epoch + 1")).Error
	})
}

// Delete removes a role no account holds.
func (s *RoleService) Delete(id int) error {
	db := database.GetDB()
	var holders []string
	if err := db.Model(&model.User{}).Where("role_id = ?", id).Pluck("username", &holders).Error; err != nil {
		return err
	}
	if len(holders) > 0 {
		return common.NewError("the role is held by: " + strings.Join(holders, ", "))
	}
	return db.Delete(&model.Role{}, id).Error
}
