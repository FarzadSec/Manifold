package controller

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// A Manifold panel learns a relay's version from its tunnel answers, so it can say when
// the two have drifted apart.
func TestTunnelAnswersCarryTheVersion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dir)
	dbtest.InitDB(t, filepath.Join(dir, "x-ui.db"))
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	NewTunnelController(engine.Group("/panel/api/tunnels"))
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/panel/api/tunnels/list", nil))
	if got := w.Header().Get(service.VersionHeader); got != config.GetProductVersion() || got == "" {
		t.Fatalf("the tunnel list answered with version %q, want %q", got, config.GetProductVersion())
	}
}
