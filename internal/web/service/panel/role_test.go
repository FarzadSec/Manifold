package panel

import (
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/access"
)

// A role cannot change users it cannot see, and a reseller role always keeps to its own book.
func TestRoleScopesStayCoherent(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	svc := &RoleService{}
	if err := svc.Save(&model.Role{Name: "odd", Tier: access.TierOperator, UsersReadScope: access.ScopeOwn, UsersWriteScope: access.ScopeAll}); err == nil {
		t.Fatal("a role that reads its own users but changes all was saved")
	}
	if err := svc.Save(&model.Role{Name: "typo", Tier: access.TierOperator, UsersReadScope: "some"}); err == nil {
		t.Fatal("an unknown scope was saved")
	}
	seller := model.Role{Name: "seller", Tier: access.TierReseller, UsersReadScope: access.ScopeAll}
	if err := svc.Save(&seller); err != nil {
		t.Fatal(err)
	}
	if seller.UsersReadScope != access.ScopeOwn || seller.UsersWriteScope != access.ScopeOwn {
		t.Fatalf("reseller role scopes = %q/%q, want own/own", seller.UsersReadScope, seller.UsersWriteScope)
	}
}

func TestRoleBoundsCannotCross(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	svc := &RoleService{}
	for name, r := range map[string]model.Role{
		"quota":    {MinUserBytes: 10, MaxUserBytes: 5},
		"days":     {MinPlanDays: 40, MaxPlanDays: 30},
		"devices":  {MinDevicesPerUser: 4, MaxDevicesPerUser: 3},
		"negative": {MinPlanDays: -1},
	} {
		r.Name, r.Tier = name, access.TierOperator
		if err := svc.Save(&r); err == nil {
			t.Errorf("a role with crossed or negative %s bounds was saved", name)
		}
	}
	open := model.Role{Name: "floor only", Tier: access.TierOperator, MinUserBytes: 10, MinPlanDays: 7, MinDevicesPerUser: 2}
	if err := svc.Save(&open); err != nil {
		t.Fatalf("a role with floors and no ceilings was refused: %v", err)
	}
}

func TestAccountOverridesStayInsideTheTier(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	svc := &AdminService{}
	base := AdminInput{Password: "long-enough-password", ResetCycle: "none"}
	for name, in := range map[string]AdminInput{
		"past the tier": {Username: "r1", Tier: access.TierReseller, GrantedPermissions: []string{access.PlansWrite}},
		"both ways":     {Username: "o1", Tier: access.TierOperator, GrantedPermissions: []string{access.PlansWrite}, DeniedPermissions: []string{access.PlansWrite}},
		"unknown":       {Username: "o2", Tier: access.TierOperator, DeniedPermissions: []string{"users:fly"}},
	} {
		in.Password, in.ResetCycle = base.Password, base.ResetCycle
		if _, err := svc.Add(in); err == nil {
			t.Errorf("an account with overrides %s was saved", name)
		}
	}
	ok := AdminInput{Username: "o3", Tier: access.TierOperator, Password: base.Password, GrantedPermissions: []string{access.PlansWrite}, DeniedPermissions: []string{access.UsersWrite}}
	if _, err := svc.Add(ok); err != nil {
		t.Fatalf("overrides inside the tier were refused: %v", err)
	}
}

func TestTokenRequestCapCannotBeNegative(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	u := model.User{Username: "acc", Password: "x", Tier: access.TierOperator}
	if err := database.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := (&ApiTokenService{}).CreateBound("neg", 0, TokenAccess{UserId: u.Id, MaxRequests: -1}); err == nil {
		t.Error("a token with a negative request cap was created")
	}
}
