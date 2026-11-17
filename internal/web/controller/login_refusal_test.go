package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/access"
	"github.com/mhsanaei/3x-ui/v3/internal/web/locale"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service/panel"
	"github.com/mhsanaei/3x-ui/v3/internal/web/session"
)

// An account with the right password that still may not sign in is told why.
func TestLoginSaysWhyAKnownAccountIsRefused(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	engine := gin.New()
	engine.Use(sessions.Sessions("manifold", cookie.NewStore([]byte("login-refusal-test-secret"))))
	engine.Use(func(c *gin.Context) {
		c.Set("I18n", func(_ locale.I18nType, key string, _ ...string) string { return key })
		c.Next()
	})
	NewIndexController(engine.Group("/"))
	db := database.GetDB()
	lock := model.Role{Name: "locked", Tier: access.TierReseller, Permissions: []string{access.UsersRead}, LockWhenLimited: true}
	if err := db.Create(&lock).Error; err != nil {
		t.Fatal(err)
	}
	const password = "long-enough-password"
	accounts := map[string]map[string]any{
		"off":     {"disabled": true},
		"limited": {"role_id": lock.Id, "suspended": access.SuspendedVolume},
		"expired": {"suspended": access.SuspendedExpired},
	}
	for name, state := range accounts {
		tier := access.TierReseller
		if name == "off" {
			tier = access.TierOperator
		}
		v, err := (&panel.AdminService{}).Add(panel.AdminInput{Username: name, Password: password, Tier: tier, ResetCycle: "none"})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&model.User{}).Where("id = ?", v.Id).Updates(state).Error; err != nil {
			t.Fatal(err)
		}
	}
	for name, want := range map[string]string{
		"off":     "pages.login.toasts.accountDisabled",
		"limited": "pages.login.toasts.accountLimited",
		"expired": "pages.login.toasts.accountExpired",
	} {
		if got := loginMessage(t, engine, name, password); got != want {
			t.Errorf("login of %q says %q, want %q", name, got, want)
		}
	}
}

func loginMessage(t *testing.T, engine *gin.Engine, username, password string) string {
	t.Helper()
	tokenRec := httptest.NewRecorder()
	engine.ServeHTTP(tokenRec, httptest.NewRequest(http.MethodGet, "/csrf-token", nil))
	var token struct{ Obj string }
	if err := json.Unmarshal(tokenRec.Body.Bytes(), &token); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"username": {username}, "password": {password}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.RemoteAddr = "198.51.100.20:40000"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set(session.CSRFHeaderName, token.Obj)
	for _, c := range tokenRec.Result().Cookies() {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	var reply struct {
		Success bool   `json:"success"`
		Msg     string `json:"msg"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
		t.Fatalf("login reply %s: %v", rec.Body.String(), err)
	}
	if reply.Success {
		t.Fatalf("%q signed in", username)
	}
	return reply.Msg
}
