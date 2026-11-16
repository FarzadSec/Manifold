package controller

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

// The interface hides Xray and inbounds on a relay only because the page says
// it is the tunnel edition; any other install must not be told so.
func TestPageNamesTheTunnelEditionOnlyOnARelay(t *testing.T) {
	oldDistFS := distFS
	distFS = fstest.MapFS{"dist/index.html": &fstest.MapFile{Data: []byte("<html><head></head><body></body></html>")}}
	t.Cleanup(func() { distFS = oldDistFS })
	gin.SetMode(gin.TestMode)

	serve := func() string {
		engine := gin.New()
		engine.Use(sessions.Sessions("manifold", cookie.NewStore([]byte("edition-test-secret"))))
		engine.GET("/", func(c *gin.Context) { serveDistPage(c, "index.html") })
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		return w.Body.String()
	}
	if body := serve(); strings.Contains(body, "X_UI_EDITION") {
		t.Fatalf("a full install was named an edition:\n%s", body)
	}
	t.Setenv("XUI_EDITION", "tunnel")
	if body := serve(); !strings.Contains(body, `window.X_UI_EDITION="tunnel"`) {
		t.Fatalf("the relay's page does not name its edition:\n%s", body)
	}
}
