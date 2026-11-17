package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/access"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service/panel"
)

type accessFixture struct {
	engine  *gin.Engine
	inbound *model.Inbound
	owner   model.User
}

type apiReply struct {
	Success bool            `json:"success"`
	Msg     string          `json:"msg"`
	Obj     json.RawMessage `json:"obj"`
}

func newAccessFixture(t *testing.T) *accessFixture {
	t.Helper()
	t.Setenv("XUI_DB_FOLDER", t.TempDir())
	engine := newTestEngine(t)
	db := database.GetDB()
	var owner model.User
	if err := db.Where("is_owner = ?", true).First(&owner).Error; err != nil {
		owner = model.User{Username: "owner", Password: "x", Tier: access.TierAdmin, IsOwner: true, MayEditUsers: true}
		if err := db.Create(&owner).Error; err != nil {
			t.Fatal(err)
		}
	}
	ib := &model.Inbound{
		UserId: owner.Id, Tag: "in-vless", Remark: "vless", Enable: true, Port: 41001, Protocol: model.VLESS,
		Settings: `{"clients":[],"decryption":"none"}`, StreamSettings: `{"network":"tcp","security":"none"}`,
	}
	if err := db.Create(ib).Error; err != nil {
		t.Fatal(err)
	}
	return &accessFixture{engine: engine, inbound: ib, owner: owner}
}

func (f *accessFixture) account(t *testing.T, u model.User) model.User {
	t.Helper()
	u.Password = "x"
	if err := database.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

func (f *accessFixture) token(t *testing.T, name string, ta panel.TokenAccess) string {
	t.Helper()
	view, err := (&panel.ApiTokenService{}).CreateBound(name, 0, ta)
	if err != nil {
		t.Fatalf("CreateBound: %v", err)
	}
	return view.Token
}

func (f *accessFixture) call(t *testing.T, token, method, path, body string) (int, apiReply) {
	t.Helper()
	req := httptest.NewRequest(method, "/panel/api"+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.engine.ServeHTTP(rec, req)
	var reply apiReply
	_ = json.Unmarshal(rec.Body.Bytes(), &reply)
	return rec.Code, reply
}

func (f *accessFixture) addClient(t *testing.T, token, email string) apiReply {
	t.Helper()
	body := `{"client":{"email":"` + email + `","id":"` + uuidFor(email) + `","enable":true},"inboundIds":[` + itoa(f.inbound.Id) + `]}`
	_, reply := f.call(t, token, http.MethodPost, "/clients/add", body)
	return reply
}

func uuidFor(email string) string {
	const base = "00000000-0000-4000-8000-000000000000"
	return base[:len(base)-len(email)] + strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'f' || r >= '0' && r <= '9' {
			return r
		}
		return 'a'
	}, email)
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func ownerOf(t *testing.T, email string) int {
	t.Helper()
	var rec model.ClientRecord
	if err := database.GetDB().Where("email = ?", email).First(&rec).Error; err != nil {
		t.Fatalf("load %s: %v", email, err)
	}
	return rec.OwnerId
}

// A reseller works only on its own book: lists hold its users alone, another user cannot be
// read or deleted, and the pool and staff routes stay shut.
func TestResellerSeesAndChangesOnlyItsOwnUsers(t *testing.T) {
	f := newAccessFixture(t)
	seller := f.account(t, model.User{Username: "seller", Tier: access.TierReseller, MayEditUsers: true})
	adminTok := f.token(t, "admin", panel.TokenAccess{UserId: f.owner.Id})
	sellerTok := f.token(t, "seller", panel.TokenAccess{UserId: seller.Id})

	if reply := f.addClient(t, adminTok, "house"); !reply.Success {
		t.Fatalf("admin create: %s", reply.Msg)
	}
	if reply := f.addClient(t, sellerTok, "bought"); !reply.Success {
		t.Fatalf("reseller create: %s", reply.Msg)
	}
	if got := ownerOf(t, "bought"); got != seller.Id {
		t.Fatalf("reseller's user owned by %d, want %d", got, seller.Id)
	}
	if got := ownerOf(t, "house"); got != 0 {
		t.Fatalf("main admin's user owned by %d, want the panel", got)
	}

	_, reply := f.call(t, sellerTok, http.MethodGet, "/clients/list", "")
	var rows []struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(reply.Obj, &rows); err != nil || len(rows) != 1 || rows[0].Email != "bought" {
		t.Fatalf("reseller list = %s, want only its own user", reply.Obj)
	}
	if _, reply := f.call(t, sellerTok, http.MethodGet, "/clients/get/house", ""); reply.Success {
		t.Fatal("reseller read another owner's user")
	}
	if _, reply := f.call(t, sellerTok, http.MethodPost, "/clients/del/house", ""); reply.Success {
		t.Fatal("reseller deleted another owner's user")
	}
	if ownerOf(t, "house") != 0 {
		t.Fatal("the panel's user changed hands")
	}
	if code, _ := f.call(t, sellerTok, http.MethodGet, "/inbounds/list", ""); code != http.StatusForbidden {
		t.Fatalf("reseller reached the inbound pool: %d", code)
	}
	if code, _ := f.call(t, sellerTok, http.MethodPost, "/clients/resetAllTraffics", ""); code != http.StatusForbidden {
		t.Fatalf("reseller reached a staff route: %d", code)
	}
}

// Taking a name another owner already uses would hand the reseller that user.
func TestResellerCannotTakeAnotherOwnersName(t *testing.T) {
	f := newAccessFixture(t)
	seller := f.account(t, model.User{Username: "seller", Tier: access.TierReseller, MayEditUsers: true})
	adminTok := f.token(t, "admin", panel.TokenAccess{UserId: f.owner.Id})
	sellerTok := f.token(t, "seller", panel.TokenAccess{UserId: seller.Id})
	f.addClient(t, adminTok, "house")
	if reply := f.addClient(t, sellerTok, "house"); reply.Success {
		t.Fatal("reseller created a user under a name the panel owns")
	}
	if ownerOf(t, "house") != 0 {
		t.Fatal("the panel's user passed to the reseller")
	}
}

// The account's user limit and the may-edit switch hold at the API, not only in the form.
func TestResellerAllowanceLimitsHoldAtTheAPI(t *testing.T) {
	f := newAccessFixture(t)
	seller := f.account(t, model.User{Username: "seller", Tier: access.TierReseller, MaxUsers: 1, MayEditUsers: false})
	sellerTok := f.token(t, "seller", panel.TokenAccess{UserId: seller.Id})
	if reply := f.addClient(t, sellerTok, "first"); !reply.Success {
		t.Fatalf("first user: %s", reply.Msg)
	}
	if reply := f.addClient(t, sellerTok, "second"); reply.Success {
		t.Fatal("reseller went past its user limit")
	}
	if _, reply := f.call(t, sellerTok, http.MethodPost, "/clients/del/first", ""); reply.Success {
		t.Fatal("a reseller that may not edit deleted its user")
	}
}

// A plans-only reseller creates from its plans and gets the plan's limits whatever it asks for.
func TestPlansOnlyResellerGetsThePlansLimits(t *testing.T) {
	f := newAccessFixture(t)
	plan := model.Plan{Name: "30d", TotalBytes: 5 << 30, Days: 30, LimitIp: 2, UserLimit: 3, AllInbounds: true, NamePrefix: "r_"}
	if err := (&service.PlanService{}).Save(&plan); err != nil {
		t.Fatal(err)
	}
	seller := f.account(t, model.User{Username: "seller", Tier: access.TierReseller, MayEditUsers: true, PlansOnly: true, AllowedPlanIds: []int{plan.Id}})
	sellerTok := f.token(t, "seller", panel.TokenAccess{UserId: seller.Id})
	if reply := f.addClient(t, sellerTok, "free"); reply.Success {
		t.Fatal("a plans-only reseller created a user without a plan")
	}
	body := `{"planId":` + itoa(plan.Id) + `,"client":{"email":"ali","id":"` + uuidFor("ali") + `","enable":true,"totalGB":0,"limitIp":0,"userLimit":0},"inboundIds":[]}`
	if _, reply := f.call(t, sellerTok, http.MethodPost, "/clients/add", body); !reply.Success {
		t.Fatalf("create from plan: %s", reply.Msg)
	}
	var rec model.ClientRecord
	if err := database.GetDB().Where("email = ?", "r_ali").First(&rec).Error; err != nil {
		t.Fatalf("the plan's name prefix was not applied: %v", err)
	}
	if rec.TotalGB != 5<<30 || rec.LimitIP != 2 || rec.UserLimit != 3 || rec.ExpiryTime <= 0 || rec.OwnerId != seller.Id {
		t.Fatalf("created %+v, want the plan's limits and the reseller as owner", rec)
	}
	for _, edit := range []string{`{"totalGB":0}`, `{"userLimit":0}`} {
		if _, reply := f.call(t, sellerTok, http.MethodPost, "/clients/update/r_ali", edit); reply.Success {
			t.Fatalf("a plans-only reseller lifted its user's limit with %s", edit)
		}
	}
}

// A token never reaches further than its permissions, nor an operator the panel's administration.
func TestTokensAndOperatorsStayInsideTheirPermissions(t *testing.T) {
	f := newAccessFixture(t)
	op := f.account(t, model.User{Username: "op", Tier: access.TierOperator, MayEditUsers: true})
	opTok := f.token(t, "op", panel.TokenAccess{UserId: op.Id})
	readOnly := f.token(t, "ro", panel.TokenAccess{UserId: f.owner.Id, Permissions: []string{access.UsersRead}})

	if code, _ := f.call(t, opTok, http.MethodGet, "/access/admins", ""); code != http.StatusForbidden {
		t.Fatalf("operator reached the admin accounts: %d", code)
	}
	if code, _ := f.call(t, opTok, http.MethodGet, "/inbounds/list", ""); code != http.StatusOK {
		t.Fatalf("operator was kept from the inbound pool: %d", code)
	}
	if code, _ := f.call(t, readOnly, http.MethodGet, "/clients/list", ""); code != http.StatusOK {
		t.Fatalf("read-only token could not read users: %d", code)
	}
	if code, _ := f.call(t, readOnly, http.MethodPost, "/clients/add", `{}`); code != http.StatusForbidden {
		t.Fatalf("read-only token created a user: %d", code)
	}
}

// A token past its requests per minute is refused until the window turns.
func TestTokenRateLimit(t *testing.T) {
	f := newAccessFixture(t)
	tok := f.token(t, "slow", panel.TokenAccess{UserId: f.owner.Id, RateLimit: 2})
	for i := range 2 {
		if code, _ := f.call(t, tok, http.MethodGet, "/access/me", ""); code != http.StatusOK {
			t.Fatalf("request %d refused: %d", i+1, code)
		}
	}
	if code, _ := f.call(t, tok, http.MethodGet, "/access/me", ""); code != http.StatusTooManyRequests {
		t.Fatalf("third request = %d, want 429", code)
	}
}

// An export carries every subscription link it lists, so a reseller's covers its own users only.
func TestResellerExportsOnlyItsOwnUsers(t *testing.T) {
	f := newAccessFixture(t)
	seller := f.account(t, model.User{Username: "seller", Tier: access.TierReseller, MayEditUsers: true})
	adminTok := f.token(t, "admin", panel.TokenAccess{UserId: f.owner.Id})
	sellerTok := f.token(t, "seller", panel.TokenAccess{UserId: seller.Id})
	if reply := f.addClient(t, adminTok, "house"); !reply.Success {
		t.Fatalf("admin create: %s", reply.Msg)
	}
	if reply := f.addClient(t, sellerTok, "bought"); !reply.Success {
		t.Fatalf("reseller create: %s", reply.Msg)
	}
	_, reply := f.call(t, sellerTok, http.MethodPost, "/clients/exportCsv", `{"all":true}`)
	var text string
	if err := json.Unmarshal(reply.Obj, &text); err != nil || !reply.Success {
		t.Fatalf("reseller export: %s %s", reply.Msg, reply.Obj)
	}
	if !strings.Contains(text, "\nbought,") || strings.Contains(text, "house") {
		t.Fatalf("reseller export =\n%s\nwant only its own user", text)
	}
	if _, reply := f.call(t, sellerTok, http.MethodPost, "/clients/configPack", `{"emails":["house"]}`); reply.Success {
		t.Fatal("reseller exported another owner's configs")
	}
	_, reply = f.call(t, adminTok, http.MethodPost, "/clients/exportCsv", `{"all":true}`)
	_ = json.Unmarshal(reply.Obj, &text)
	if !strings.Contains(text, "house") || !strings.Contains(text, "bought,true,,seller,") {
		t.Fatalf("admin export =\n%s\nwant both users with the reseller as owner", text)
	}
}

// An operator role may reach every user, read all but change only its own, or keep to its own
// book like a reseller; routes that act on every user at once stay shut to what it cannot reach.
func TestOperatorRoleScopesWhichUsersItReaches(t *testing.T) {
	f := newAccessFixture(t)
	adminTok := f.token(t, "admin", panel.TokenAccess{UserId: f.owner.Id})
	if reply := f.addClient(t, adminTok, "panel-user"); !reply.Success {
		t.Fatalf("admin add: %s", reply.Msg)
	}
	operator := func(name, read, write string) string {
		role := model.Role{Name: name, Tier: access.TierOperator, Permissions: []string{access.UsersWrite}, UsersReadScope: read, UsersWriteScope: write}
		if err := database.GetDB().Create(&role).Error; err != nil {
			t.Fatal(err)
		}
		acc := f.account(t, model.User{Username: name, Tier: access.TierOperator, RoleId: role.Id})
		tok := f.token(t, name, panel.TokenAccess{UserId: acc.Id})
		if reply := f.addClient(t, tok, name+"-user"); !reply.Success {
			t.Fatalf("%s add: %s", name, reply.Msg)
		}
		return tok
	}
	listed := func(tok string) []string {
		_, reply := f.call(t, tok, http.MethodGet, "/clients/list", "")
		var rows []struct {
			Email string `json:"email"`
		}
		_ = json.Unmarshal(reply.Obj, &rows)
		out := []string{}
		for _, r := range rows {
			out = append(out, r.Email)
		}
		return out
	}
	editOK := func(tok, email string) bool {
		_, reply := f.call(t, tok, http.MethodPost, "/clients/update/"+email, `{"comment":"seen"}`)
		return reply.Success
	}
	reachable := func(tok, method, path string) bool {
		code, reply := f.call(t, tok, method, path, `{}`)
		return code != http.StatusForbidden && !strings.Contains(reply.Msg, "not permitted")
	}

	full := operator("full", "", "")
	if got := listed(full); len(got) != 2 || !editOK(full, "panel-user") || !reachable(full, http.MethodPost, "/clients/delDepleted") {
		t.Fatalf("an all-users operator: sees %v, or was refused an edit or a bulk route", got)
	}

	support := operator("support", access.ScopeAll, access.ScopeOwn)
	if got := listed(support); len(got) != 3 {
		t.Fatalf("a read-all operator sees %v, want every user", got)
	}
	if editOK(support, "panel-user") || !editOK(support, "support-user") {
		t.Fatal("a change-own operator changed another's user, or not its own")
	}
	if reachable(support, http.MethodPost, "/clients/delDepleted") || !reachable(support, http.MethodGet, "/clients/groups") {
		t.Fatal("a change-own operator reached a bulk change, or was kept from reading groups")
	}

	book := operator("book", access.ScopeOwn, access.ScopeOwn)
	if got := listed(book); len(got) != 1 || got[0] != "book-user" {
		t.Fatalf("an own-users operator sees %v, want only its own", got)
	}
	if reachable(book, http.MethodGet, "/clients/groups") {
		t.Fatal("an own-users operator read every group")
	}
}
