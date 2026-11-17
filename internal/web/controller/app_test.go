package controller

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
)

func TestAppLocationsSaveAndList(t *testing.T) {
	t.Setenv("XUI_DB_FOLDER", t.TempDir())
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewAppController(router.Group("/panel/api/app"))

	resp := subBalancerPost(t, router, "/panel/api/app/locations", "nodeId=0&name=Iran&countryCode=ir&city=Tehran&timezone=Asia%2FTehran")
	if saved := responseObj(t, resp.Body.String()); saved["success"] != true {
		t.Fatalf("save = %s", resp.Body.String())
	}
	resp = subBalancerPost(t, router, "/panel/api/app/locations", "nodeId=0&countryCode=IRN")
	if refused := responseObj(t, resp.Body.String()); refused["success"] != false {
		t.Fatalf("a three-letter country was saved: %s", resp.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/panel/api/app/locations", nil)
	list := httptest.NewRecorder()
	router.ServeHTTP(list, req)
	locations, _ := responseObj(t, list.Body.String())["obj"].([]any)
	if len(locations) != 1 {
		t.Fatalf("locations = %s, want this panel only", list.Body.String())
	}
	panel := locations[0].(map[string]any)
	if panel["nodeId"] != float64(0) || panel["countryCode"] != "IR" || panel["city"] != "Tehran" || panel["timezone"] != "Asia/Tehran" {
		t.Fatalf("this panel's location = %v", panel)
	}
}
