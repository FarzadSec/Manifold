package panel

import (
	"path/filepath"
	"testing"

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
