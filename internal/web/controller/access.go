package controller

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xlzd/gotp"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/util/totp"
	"github.com/mhsanaei/3x-ui/v3/internal/web/access"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service/panel"
)

// AccessController serves admin accounts, roles, plans and the audit log.
type AccessController struct {
	BaseController
	adminService    panel.AdminService
	roleService     panel.RoleService
	planService     service.PlanService
	auditService    service.AuditService
	settingService  service.SettingService
	resellerService service.ResellerService
	inboundService  service.InboundService
}

// NewAccessController registers the /access routes.
func NewAccessController(g *gin.RouterGroup) *AccessController {
	a := &AccessController{}
	g.GET("/me", a.me)
	g.GET("/me/twoFactor", a.twoFactorSetup)
	g.POST("/me/twoFactor", a.twoFactorSave)
	g.GET("/admins", a.listAdmins)
	g.POST("/admins/add", a.addAdmin)
	g.POST("/admins/update/:id", a.updateAdmin)
	g.POST("/admins/del/:id", a.deleteAdmin)
	g.POST("/admins/endSessions/:id", a.endSessions)
	g.POST("/admins/handover/:id", a.handover)
	g.GET("/roles", a.listRoles)
	g.POST("/roles/add", a.saveRole)
	g.POST("/roles/update/:id", a.saveRole)
	g.POST("/roles/del/:id", a.deleteRole)
	g.GET("/plans", a.listPlans)
	g.GET("/owners", a.listOwners)
	g.POST("/plans/add", a.savePlan)
	g.POST("/plans/update/:id", a.savePlan)
	g.POST("/plans/del/:id", a.deletePlan)
	g.GET("/audit", a.listAudit)
	g.POST("/audit/clear", a.clearAudit)
	g.GET("/audit/settings", a.auditSettings)
	g.POST("/audit/settings", a.saveAuditSettings)
	return a
}

// MeView is the signed-in account: what it may reach and, for a reseller, its allowance.
type MeView struct {
	Id               int      `json:"id" example:"1"`
	Username         string   `json:"username" example:"admin"`
	Tier             string   `json:"tier" example:"admin"`
	IsOwner          bool     `json:"isOwner" example:"true"`
	RoleName         string   `json:"roleName" example:"Main admin"`
	Permissions      []string `json:"permissions" example:"[\"users:read\"]"`
	TwoFactorEnabled bool     `json:"twoFactorEnabled" example:"false"`
	VolumeBytes      int64    `json:"volumeBytes" example:"0"`
	CycleUsed        int64    `json:"cycleUsed" example:"0"`
	CycleStart       int64    `json:"cycleStart" example:"0"`
	ResetCycle       string   `json:"resetCycle" example:"none"`
	ExpiresAt        int64    `json:"expiresAt" example:"0"`
	MaxUsers         int      `json:"maxUsers" example:"0"`
	OwnedUsers       int64    `json:"ownedUsers" example:"0"`
	Suspended        string   `json:"suspended" example:""`
	MayEditUsers     bool     `json:"mayEditUsers" example:"true"`
	PlansOnly        bool     `json:"plansOnly" example:"false"`
	// Catalogue and TierCeilings feed the role and token forms.
	Catalogue    []string            `json:"catalogue" example:"[\"users:read\"]"`
	TierCeilings map[string][]string `json:"tierCeilings"`
}

func (a *AccessController) me(c *gin.Context) {
	actor := access.ActorFrom(c)
	var acc model.User
	if err := database.GetDB().First(&acc, actor.UserId).Error; err != nil {
		jsonObj(c, nil, err)
		return
	}
	owned, _ := service.CountOwned(acc.Id)
	roleName := ""
	if actor.Role != nil {
		roleName = actor.Role.Name
	}
	ceilings := map[string][]string{}
	for _, t := range []string{access.TierAdmin, access.TierOperator, access.TierReseller} {
		ceilings[t] = access.Ceiling(t)
	}
	jsonObj(c, MeView{
		Id: acc.Id, Username: acc.Username, Tier: actor.Tier, IsOwner: acc.IsOwner, RoleName: roleName,
		Permissions: actor.Perms.List(), TwoFactorEnabled: acc.TwoFactorEnabled,
		VolumeBytes: acc.VolumeBytes, CycleUsed: acc.CycleUsed, CycleStart: acc.CycleStart, ResetCycle: acc.ResetCycle,
		ExpiresAt: acc.ExpiresAt, MaxUsers: acc.MaxUsers, OwnedUsers: owned, Suspended: acc.Suspended,
		MayEditUsers: acc.MayEditUsers, PlansOnly: acc.PlansOnly,
		Catalogue: access.AllPermissions, TierCeilings: ceilings,
	}, nil)
}

// TwoFactorSetupView is a fresh secret for the account's authenticator app.
type TwoFactorSetupView struct {
	Secret string `json:"secret" example:"JBSWY3DPEHPK3PXP"`
	Uri    string `json:"uri" example:"otpauth://totp/Manifold:admin?secret=JBSWY3DPEHPK3PXP&issuer=Manifold"`
}

func (a *AccessController) twoFactorSetup(c *gin.Context) {
	actor := access.ActorFrom(c)
	secret := gotp.RandomSecret(32)
	jsonObj(c, TwoFactorSetupView{Secret: secret, Uri: gotp.NewDefaultTOTP(secret).ProvisioningUri(actor.Username, "Manifold")}, nil)
}

type twoFactorForm struct {
	Enable bool   `json:"enable"`
	Secret string `json:"secret"`
	Code   string `json:"code"`
}

// twoFactorSave turns the account's own two-factor on with a confirmed code, or off with its current one.
func (a *AccessController) twoFactorSave(c *gin.Context) {
	actor := access.ActorFrom(c)
	var form twoFactorForm
	if err := c.ShouldBindJSON(&form); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	var err error
	if form.Enable {
		if !totp.ValidSecret(form.Secret) || !totp.VerifyWithSkew(form.Secret, form.Code, time.Now()) {
			err = common.NewError("invalid two factor code")
		} else {
			err = a.settingService.SetAccountTwoFactor(actor.UserId, true, form.Secret)
		}
	} else if err = a.settingService.VerifyTwoFactorCode(actor.UserId, form.Code); err == nil {
		err = a.settingService.SetAccountTwoFactor(actor.UserId, false, "")
	}
	if err == nil {
		recordAudit(c, "admins", service.AuditTwoFactor, actor.Username, map[string]bool{"enabled": form.Enable})
	}
	jsonMsg(c, I18nWeb(c, "pages.settings.toasts.modifySettings"), err)
}

func idParam(c *gin.Context) int {
	id, _ := strconv.Atoi(c.Param("id"))
	return id
}

func (a *AccessController) listAdmins(c *gin.Context) {
	admins, err := a.adminService.List()
	jsonObj(c, admins, err)
}

func (a *AccessController) addAdmin(c *gin.Context) {
	var in panel.AdminInput
	if err := c.ShouldBindJSON(&in); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	v, err := a.adminService.Add(in)
	jsonMsgObj(c, I18nWeb(c, "pages.access.saved"), v, err)
}

func (a *AccessController) updateAdmin(c *gin.Context) {
	var in panel.AdminInput
	if err := c.ShouldBindJSON(&in); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	err := a.adminService.Update(idParam(c), in)
	if err == nil {
		err = a.resellerService.Enforce(&a.inboundService, time.Now())
	}
	jsonMsg(c, I18nWeb(c, "pages.access.saved"), err)
}

func (a *AccessController) deleteAdmin(c *gin.Context) {
	err := a.adminService.Delete(access.ActorFrom(c).UserId, idParam(c))
	if err == nil {
		notifyClientsChanged()
	}
	jsonMsg(c, I18nWeb(c, "pages.access.deleted"), err)
}

func (a *AccessController) endSessions(c *gin.Context) {
	jsonMsg(c, I18nWeb(c, "pages.access.sessionsEnded"), a.adminService.EndSessions(idParam(c)))
}

func (a *AccessController) handover(c *gin.Context) {
	if access.ActorFrom(c).ViaToken {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), common.NewError("ownership is handed over from the owner's own login"))
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.access.handedOver"), a.adminService.Handover(access.ActorFrom(c).UserId, idParam(c)))
}

func (a *AccessController) listRoles(c *gin.Context) {
	roles, err := a.roleService.List()
	jsonObj(c, roles, err)
}

func (a *AccessController) saveRole(c *gin.Context) {
	var r model.Role
	if err := c.ShouldBindJSON(&r); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	r.Id = idParam(c)
	err := a.roleService.Save(&r)
	jsonMsgObj(c, I18nWeb(c, "pages.access.saved"), r, err)
}

func (a *AccessController) deleteRole(c *gin.Context) {
	jsonMsg(c, I18nWeb(c, "pages.access.deleted"), a.roleService.Delete(idParam(c)))
}

// listPlans shows a plans-only reseller just the plans it may sell.
func (a *AccessController) listPlans(c *gin.Context) {
	plans, err := a.planService.List()
	actor := access.ActorFrom(c)
	if err == nil && actor.OwnsOnly() {
		var acc model.User
		if err = database.GetDB().First(&acc, actor.UserId).Error; err == nil && acc.PlansOnly {
			kept := plans[:0]
			for _, p := range plans {
				for _, id := range acc.AllowedPlanIds {
					if id == p.Id {
						kept = append(kept, p)
						break
					}
				}
			}
			plans = kept
		}
	}
	if plans == nil {
		plans = []model.Plan{}
	}
	jsonObj(c, plans, err)
}

func (a *AccessController) savePlan(c *gin.Context) {
	var p model.Plan
	if err := c.ShouldBindJSON(&p); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	p.Id = idParam(c)
	err := a.planService.Save(&p)
	jsonMsgObj(c, I18nWeb(c, "pages.access.saved"), p, err)
}

func (a *AccessController) deletePlan(c *gin.Context) {
	jsonMsg(c, I18nWeb(c, "pages.access.deleted"), a.planService.Delete(idParam(c)))
}

func (a *AccessController) listAudit(c *gin.Context) {
	var f service.AuditFilter
	if err := c.ShouldBindQuery(&f); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	page, err := a.auditService.List(f)
	jsonObj(c, page, err)
}

func (a *AccessController) clearAudit(c *gin.Context) {
	actor := access.ActorFrom(c)
	err := a.auditService.Clear(model.AuditEntry{UserId: actor.UserId, Username: actor.Username, Ip: getRemoteIp(c)})
	jsonMsg(c, I18nWeb(c, "pages.access.auditCleared"), err)
}

func (a *AccessController) auditSettings(c *gin.Context) {
	jsonObj(c, a.auditService.Settings(), nil)
}

func (a *AccessController) saveAuditSettings(c *gin.Context) {
	var s service.AuditSettings
	if err := c.ShouldBindJSON(&s); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.access.saved"), a.auditService.SaveSettings(s))
}

// OwnerView names an account users can belong to, for the users page's owner column and filter.
type OwnerView struct {
	Id       int    `json:"id" example:"3"`
	Username string `json:"username" example:"seller1"`
	Tier     string `json:"tier" example:"reseller"`
}

func (a *AccessController) listOwners(c *gin.Context) {
	var owners []OwnerView
	err := database.GetDB().Model(&model.User{}).Select("id", "username", "tier").Order("username").Scan(&owners).Error
	if owners == nil {
		owners = []OwnerView{}
	}
	jsonObj(c, owners, err)
}
