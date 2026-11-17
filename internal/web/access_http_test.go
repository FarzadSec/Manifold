package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

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

// A role bounds what its accounts hand out, on creation, on an edit and on a bulk change,
// whatever the tier: a quota, a plan length and a device count each between a floor and a ceiling.
func TestRoleLimitsBoundWhatItsAccountsHandOut(t *testing.T) {
	f := newAccessFixture(t)
	const gb = int64(1) << 30
	role := model.Role{
		Name: "bounded", Tier: access.TierOperator, Permissions: []string{access.UsersWrite},
		MinUserBytes: gb, MaxUserBytes: 10 * gb, MinPlanDays: 7, MaxPlanDays: 30, MinDevicesPerUser: 1, MaxDevicesPerUser: 3,
	}
	if err := database.GetDB().Create(&role).Error; err != nil {
		t.Fatal(err)
	}
	acc := f.account(t, model.User{Username: "bounded", Tier: access.TierOperator, RoleId: role.Id})
	tok := f.token(t, "bounded", panel.TokenAccess{UserId: acc.Id})
	day := int64(24 * 60 * 60 * 1000)
	expiry := func(days int64) int64 { return time.Now().UnixMilli() + days*day }
	add := func(email string, total, expiryMs int64, hwid int) apiReply {
		body := `{"client":{"email":"` + email + `","id":"` + uuidFor(email) + `","enable":true,"totalGB":` + strconv.FormatInt(total, 10) +
			`,"expiryTime":` + strconv.FormatInt(expiryMs, 10) + `,"limitHwid":` + strconv.Itoa(hwid) + `},"inboundIds":[` + itoa(f.inbound.Id) + `]}`
		_, reply := f.call(t, tok, http.MethodPost, "/clients/add", body)
		return reply
	}
	if reply := add("fits", 5*gb, expiry(10), 2); !reply.Success {
		t.Fatalf("a user inside every bound was refused: %s", reply.Msg)
	}
	for name, reply := range map[string]apiReply{
		"unlimited quota": add("q0", 0, expiry(10), 2),
		"quota above":     add("q1", 20*gb, expiry(10), 2),
		"quota below":     add("q2", gb/2, expiry(10), 2),
		"plan too short":  add("d1", 5*gb, expiry(3), 2),
		"plan too long":   add("d2", 5*gb, expiry(40), 2),
		"no expiry":       add("d3", 5*gb, 0, 2),
		"devices above":   add("h1", 5*gb, expiry(10), 5),
		"no device limit": add("h2", 5*gb, expiry(10), 0),
	} {
		if reply.Success {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, reply := f.call(t, tok, http.MethodPost, "/clients/update/fits", `{"totalGB":`+strconv.FormatInt(20*gb, 10)+`}`); reply.Success {
		t.Error("an edit past the quota ceiling was accepted")
	}
	if _, reply := f.call(t, tok, http.MethodPost, "/clients/update/fits", `{"comment":"vip"}`); !reply.Success {
		t.Errorf("an edit that changes no limit was refused: %s", reply.Msg)
	}
	if _, reply := f.call(t, tok, http.MethodPost, "/clients/bulkAdjust", `{"emails":["fits"],"addDays":30}`); reply.Success {
		t.Error("a bulk change past the longest plan was accepted")
	}
	adminTok := f.token(t, "admin", panel.TokenAccess{UserId: f.owner.Id})
	if reply := f.addClient(t, adminTok, "legacy"); !reply.Success {
		t.Fatalf("admin add: %s", reply.Msg)
	}
	if _, reply := f.call(t, tok, http.MethodPost, "/clients/update/legacy", `{"comment":"vip","totalGB":0,"expiryTime":0,"limitHwid":0}`); !reply.Success {
		t.Errorf("an edit repeating an unbounded user's own limits was refused: %s", reply.Msg)
	}
}

// A role names the plans and inbounds its accounts sell from; a reseller's own lists narrow it.
func TestRoleChoosesThePlansAndInboundsItsAccountsSell(t *testing.T) {
	f := newAccessFixture(t)
	db := database.GetDB()
	extra := func(tag string, port int) int {
		ib := model.Inbound{
			UserId: f.owner.Id, Tag: tag, Remark: tag, Enable: true, Port: port, Protocol: model.VLESS,
			Settings: `{"clients":[],"decryption":"none"}`, StreamSettings: `{"network":"tcp","security":"none"}`,
		}
		if err := db.Create(&ib).Error; err != nil {
			t.Fatal(err)
		}
		return ib.Id
	}
	a, b, cIb := f.inbound.Id, extra("in-b", 41002), extra("in-c", 41003)
	sold := model.Plan{Name: "sold", TotalBytes: 5 << 30, Days: 30, AllInbounds: true}
	other := model.Plan{Name: "other", TotalBytes: 5 << 30, Days: 30, AllInbounds: true}
	for _, p := range []*model.Plan{&sold, &other} {
		if err := (&service.PlanService{}).Save(p); err != nil {
			t.Fatal(err)
		}
	}
	role := model.Role{
		Name: "desk", Tier: access.TierOperator, Permissions: []string{access.UsersRead, access.UsersWrite},
		PlansOnly: true, AllowedPlanIds: []int{sold.Id}, InboundsLimited: true, AllowedInboundIds: []int{b, cIb},
	}
	if err := db.Create(&role).Error; err != nil {
		t.Fatal(err)
	}
	desk := f.account(t, model.User{Username: "desk", Tier: access.TierOperator, RoleId: role.Id})
	tok := f.token(t, "desk", panel.TokenAccess{UserId: desk.Id})
	fromPlan := func(tok, email string, plan int, ids string) apiReply {
		body := `{"planId":` + itoa(plan) + `,"client":{"email":"` + email + `","id":"` + uuidFor(email) + `","enable":true},"inboundIds":[` + ids + `]}`
		_, reply := f.call(t, tok, http.MethodPost, "/clients/add", body)
		return reply
	}
	if reply := f.addClient(t, tok, "free"); reply.Success {
		t.Error("a plans-only role created a user without a plan")
	}
	if reply := fromPlan(tok, "o1", other.Id, ""); reply.Success {
		t.Error("a plans-only role sold a plan it was not given")
	}
	if reply := fromPlan(tok, "s1", sold.Id, ""); !reply.Success {
		t.Fatalf("selling the role's plan was refused: %s", reply.Msg)
	}
	if got := inboundsOf(t, "s1"); !slices.Equal(got, []int{b, cIb}) {
		t.Errorf("the role's user is on inbounds %v, want %v", got, []int{b, cIb})
	}
	if _, reply := f.call(t, tok, http.MethodPost, "/clients/update/s1", `{"totalGB":0}`); reply.Success {
		t.Error("a plans-only role lifted its user's quota")
	}
	if _, reply := f.call(t, tok, http.MethodPost, "/clients/update/s1", `{"comment":"vip"}`); !reply.Success {
		t.Errorf("a plans-only role could not rename-level edit its user: %s", reply.Msg)
	}
	if got := listedIDs(t, f, tok, "/access/plans"); !slices.Equal(got, []int{sold.Id}) {
		t.Errorf("the role lists plans %v, want only %d", got, sold.Id)
	}
	if got := listedIDs(t, f, tok, "/inbounds/options"); !slices.Equal(got, []int{b, cIb}) {
		t.Errorf("the role is offered inbounds %v, want %v", got, []int{b, cIb})
	}
	planner := model.Role{
		Name: "planner", Tier: access.TierOperator, Permissions: []string{access.UsersRead, access.UsersWrite, access.PlansWrite},
		PlansOnly: true, AllowedPlanIds: []int{sold.Id},
	}
	if err := db.Create(&planner).Error; err != nil {
		t.Fatal(err)
	}
	plannerAcc := f.account(t, model.User{Username: "planner", Tier: access.TierOperator, RoleId: planner.Id})
	plannerTok := f.token(t, "planner", panel.TokenAccess{UserId: plannerAcc.Id})
	if got, want := listedIDs(t, f, plannerTok, "/access/plans"), []int{sold.Id, other.Id}; !slices.Equal(got, want) {
		t.Errorf("a role that manages plans lists %v, want every plan %v", got, want)
	}

	open := model.Role{Name: "shop", Tier: access.TierReseller, Permissions: []string{access.UsersRead, access.UsersWrite}, InboundsLimited: true, AllowedInboundIds: []int{b, cIb}}
	if err := db.Create(&open).Error; err != nil {
		t.Fatal(err)
	}
	seller := f.account(t, model.User{Username: "shop", Tier: access.TierReseller, RoleId: open.Id, MayEditUsers: true, InboundsLimited: true, AllowedInboundIds: []int{a, b}})
	sellerTok := f.token(t, "shop", panel.TokenAccess{UserId: seller.Id})
	if got := listedIDs(t, f, sellerTok, "/inbounds/options"); !slices.Equal(got, []int{b}) {
		t.Errorf("the reseller is offered inbounds %v, want only %d, on both its own and its role's lists", got, b)
	}
	body := func(email, ids string) string {
		return `{"client":{"email":"` + email + `","id":"` + uuidFor(email) + `","enable":true},"inboundIds":[` + ids + `]}`
	}
	if _, reply := f.call(t, sellerTok, http.MethodPost, "/clients/add", body("r1", itoa(a)+","+itoa(cIb))); reply.Success {
		t.Error("the reseller put a user on inbounds outside one of its two lists")
	}
	if _, reply := f.call(t, sellerTok, http.MethodPost, "/clients/add", body("r2", itoa(a)+","+itoa(b)+","+itoa(cIb))); !reply.Success {
		t.Fatalf("add: %s", reply.Msg)
	}
	if got := inboundsOf(t, "r2"); !slices.Equal(got, []int{b}) {
		t.Errorf("the reseller's user is on inbounds %v, want only %d", got, b)
	}
}

func inboundsOf(t *testing.T, email string) []int {
	t.Helper()
	var ids []int
	if err := database.GetDB().Model(&model.ClientInbound{}).
		Joins("JOIN clients ON clients.id = client_inbounds.client_id").
		Where("clients.email = ?", email).Order("client_inbounds.inbound_id").
		Pluck("client_inbounds.inbound_id", &ids).Error; err != nil {
		t.Fatal(err)
	}
	return ids
}

func listedIDs(t *testing.T, f *accessFixture, tok, path string) []int {
	t.Helper()
	_, reply := f.call(t, tok, http.MethodGet, path, "")
	var items []struct {
		Id int `json:"id"`
	}
	if err := json.Unmarshal(reply.Obj, &items); err != nil {
		t.Fatalf("%s: %v (%s)", path, err, reply.Msg)
	}
	ids := make([]int, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.Id)
	}
	slices.Sort(ids)
	return ids
}

// An account's own grants and denials change what its role gives it, never past its tier,
// and never for the panel owner.
func TestAccountOverridesChangeWhatItsRoleGives(t *testing.T) {
	f := newAccessFixture(t)
	db := database.GetDB()
	reader := model.Role{Name: "reader", Tier: access.TierOperator, Permissions: []string{access.UsersRead}}
	if err := db.Create(&reader).Error; err != nil {
		t.Fatal(err)
	}
	tokenFor := func(u model.User) string {
		return f.token(t, u.Username, panel.TokenAccess{UserId: f.account(t, u).Id})
	}
	plain := tokenFor(model.User{Username: "plain", Tier: access.TierOperator, RoleId: reader.Id})
	planner := tokenFor(model.User{Username: "planner", Tier: access.TierOperator, RoleId: reader.Id, GrantedPermissions: []string{access.PlansWrite}})
	greedy := tokenFor(model.User{Username: "greedy", Tier: access.TierReseller, GrantedPermissions: []string{access.PlansWrite}})
	blind := tokenFor(model.User{Username: "blind", Tier: access.TierOperator, DeniedPermissions: []string{access.UsersRead}})
	addPlan := func(tok, name string) int {
		code, _ := f.call(t, tok, http.MethodPost, "/access/plans/add", `{"name":"`+name+`","days":30}`)
		return code
	}
	if code := addPlan(plain, "p1"); code != http.StatusForbidden {
		t.Errorf("a read-only role added a plan (status %d)", code)
	}
	if code := addPlan(planner, "p2"); code != http.StatusOK {
		t.Errorf("an account granted plans:write could not add a plan (status %d)", code)
	}
	if code := addPlan(greedy, "p3"); code != http.StatusForbidden {
		t.Errorf("a reseller granted a permission past its tier added a plan (status %d)", code)
	}
	if code, _ := f.call(t, blind, http.MethodGet, "/clients/list", ""); code != http.StatusForbidden {
		t.Errorf("an account denied users:read still listed users (status %d)", code)
	}
	if code, _ := f.call(t, blind, http.MethodPost, "/clients/add", `{}`); code != http.StatusForbidden {
		t.Errorf("an account denied users:read still added users (status %d)", code)
	}
	if err := db.Model(&model.User{}).Where("id = ?", f.owner.Id).
		Update("denied_permissions", `["users:write"]`).Error; err != nil {
		t.Fatal(err)
	}
	if reply := f.addClient(t, f.token(t, "owner", panel.TokenAccess{UserId: f.owner.Id}), "kept"); !reply.Success {
		t.Errorf("a denial locked the panel owner out: %s", reply.Msg)
	}
}

// A switched-off account is refused at once, and so is one out of allowance when its role
// locks it; switching an account off turns its users off when its role says so.
func TestSwitchedOffAndLockedAccountsAreRefused(t *testing.T) {
	f := newAccessFixture(t)
	db := database.GetDB()
	perms := []string{access.UsersRead, access.UsersWrite}
	lock := model.Role{Name: "locked", Tier: access.TierReseller, Permissions: perms, LockWhenLimited: true}
	shut := model.Role{Name: "shut", Tier: access.TierReseller, Permissions: perms, UsersOffWhenDisabled: true}
	for _, r := range []*model.Role{&lock, &shut} {
		if err := db.Create(r).Error; err != nil {
			t.Fatal(err)
		}
	}
	tokenFor := func(u model.User) (model.User, string) {
		acc := f.account(t, u)
		return acc, f.token(t, u.Username, panel.TokenAccess{UserId: acc.Id})
	}
	_, off := tokenFor(model.User{Username: "off", Tier: access.TierOperator, Disabled: true})
	_, locked := tokenFor(model.User{Username: "locked", Tier: access.TierReseller, RoleId: lock.Id, Suspended: access.SuspendedVolume})
	_, limited := tokenFor(model.User{Username: "limited", Tier: access.TierReseller, RoleId: shut.Id, Suspended: access.SuspendedVolume})
	for name, want := range map[string]int{off: http.StatusUnauthorized, locked: http.StatusUnauthorized, limited: http.StatusOK} {
		if code, _ := f.call(t, name, http.MethodGet, "/clients/list", ""); code != want {
			t.Errorf("a request answered %d, want %d", code, want)
		}
	}

	seller, sellerTok := tokenFor(model.User{Username: "seller", Tier: access.TierReseller, RoleId: shut.Id, MayEditUsers: true})
	if reply := f.addClient(t, sellerTok, "bought"); !reply.Success {
		t.Fatalf("add: %s", reply.Msg)
	}
	adminTok := f.token(t, "admin", panel.TokenAccess{UserId: f.owner.Id})
	update := func(disabled bool) {
		t.Helper()
		body := `{"username":"seller","tier":"reseller","roleId":` + itoa(shut.Id) + `,"resetCycle":"none","mayEditUsers":true,"disabled":` + strconv.FormatBool(disabled) + `}`
		if _, reply := f.call(t, adminTok, http.MethodPost, "/access/admins/update/"+itoa(seller.Id), body); !reply.Success {
			t.Fatalf("update: %s", reply.Msg)
		}
	}
	enabled := func() bool {
		var rec model.ClientRecord
		if err := db.Where("email = ?", "bought").First(&rec).Error; err != nil {
			t.Fatal(err)
		}
		return rec.Enable
	}
	update(true)
	if enabled() {
		t.Error("switching the account off left its user on, though its role turns users off")
	}
	if code, _ := f.call(t, sellerTok, http.MethodGet, "/clients/list", ""); code != http.StatusUnauthorized {
		t.Errorf("the switched-off account's token still answered %d", code)
	}
	update(false)
	if !enabled() {
		t.Error("switching the account back on did not bring its user back")
	}
	ownerBody := `{"username":"` + f.owner.Username + `","tier":"admin","resetCycle":"none","disabled":true}`
	if _, reply := f.call(t, adminTok, http.MethodPost, "/access/admins/update/"+itoa(f.owner.Id), ownerBody); reply.Success {
		t.Error("the panel owner was switched off")
	}
}
