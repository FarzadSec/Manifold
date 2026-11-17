package sub

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/importer"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// Links the old panel handed out keep working after an import, and nothing else does.
func TestOldPanelLinksReachTheImportedUser(t *testing.T) {
	seedSubDB(t)
	seedSubInbound(t, "s1", "vl", 4492, 1, `{"network":"tcp","security":"none"}`)
	db := database.GetDB()
	revoked := time.Now().Add(-time.Hour)
	db.Create(&[]model.SubAlias{
		{Alias: "token:alice", SubId: "s1", Source: "marzban", NotBefore: revoked.UnixMilli()},
		{Alias: "dave/0123456789abcdef", SubId: "s1", Source: "marzneshin"},
	})
	db.Create(&model.LegacySubSecret{Source: "marzban", Secret: "sekret"})
	service.SubAliasesChanged()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewSUBController(router.Group("/"), WithSUBEncryption(false), WithSUBClashEnabled(true))
	get := func(path string) (int, string) {
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "http://sub.example.com"+path, nil))
		return resp.Code, resp.Body.String()
	}
	fresh := importer.MarzbanToken("alice", "sekret", time.Now())
	for _, path := range []string{"/sub/" + fresh, "/sub/dave/0123456789abcdef"} {
		if code, body := get(path); code != http.StatusOK || !strings.Contains(body, "vless://") {
			t.Fatalf("%s: %d %s", path, code, body)
		}
	}
	if code, body := get("/sub/" + fresh + "/clash"); code != http.StatusOK || !strings.Contains(body, "proxies:") {
		t.Fatalf("token/clash: %d %s", code, body)
	}
	for _, path := range []string{
		"/sub/" + importer.MarzbanToken("alice", "sekret", revoked.Add(-time.Minute)),
		"/sub/" + importer.MarzbanToken("alice", "guessed", time.Now()),
		"/sub/token:alice",
		"/sub/dave/wrongkey",
	} {
		if _, body := get(path); strings.Contains(body, "vless://") {
			t.Fatalf("%s served the subscription", path)
		}
	}
}
