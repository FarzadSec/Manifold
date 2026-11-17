package sub

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func setSubSetting(t *testing.T, key, value string) {
	t.Helper()
	db := database.GetDB()
	db.Where("key = ?", key).Delete(&model.Setting{})
	if err := db.Create(&model.Setting{Key: key, Value: value}).Error; err != nil {
		t.Fatal(err)
	}
}

func getPage(t *testing.T, router *gin.Engine, path string, html bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://sub.example.com"+path, nil)
	if html {
		req.Header.Set("Accept", "text/html")
	}
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	return resp
}

// A picked theme draws the page, in the visitor's language, with app links html/template
// did not defang, and serves its stylesheet but never its page source or files outside it.
func TestPickedThemeDrawsThePageAndServesItsFiles(t *testing.T) {
	seedSubDB(t)
	seedSubInbound(t, "s1", "edge", 4490, 1, `{"network":"tcp","security":"none"}`)
	gin.SetMode(gin.TestMode)
	oldDistFS := distFS
	distFS = testDistFS
	t.Cleanup(func() { distFS = oldDistFS })
	router := newSubscriptionTestRouter(subscriptionTestRouterConfig{})

	if body := getPage(t, router, "/sub/s1", true).Body.String(); !strings.Contains(body, "__SUB_PAGE_DATA__") {
		t.Fatalf("with no theme the panel's own page must answer:\n%.300s", body)
	}

	setSubSetting(t, "subTheme", "builtin:aurora")
	resp := getPage(t, router, "/sub/s1?lang=fa", true)
	body := resp.Body.String()
	for _, want := range []string{`<title>Aurora</title>`, `dir="rtl"`, "کپی لینک اشتراک", `href="happ://add/http://`, `href="s1/theme/style.css"`, "edge"} {
		if !strings.Contains(body, want) {
			t.Errorf("themed page lacks %q", want)
		}
	}
	if strings.Contains(body, "ZgotmplZ") || strings.Contains(body, "__SUB_PAGE_DATA__") {
		t.Fatalf("themed page has a defanged link or fell back to the panel page:\n%s", body)
	}

	css := getPage(t, router, "/sub/s1/theme/style.css", false)
	if css.Code != http.StatusOK || css.Header().Get("Content-Type") != "text/css; charset=utf-8" {
		t.Fatalf("stylesheet = %d %q, want 200 text/css", css.Code, css.Header().Get("Content-Type"))
	}
	for _, path := range []string{"/sub/s1/theme/index.html", "/sub/s1/theme/..%2f..%2fx.css"} {
		if code := getPage(t, router, path, false).Code; code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", path, code)
		}
	}
	if code := getPage(t, router, "/json/s1/theme/style.css", false).Code; code != http.StatusOK {
		t.Errorf("the page under the JSON path cannot load its stylesheet: %d", code)
	}

	folder := t.TempDir()
	for name, content := range map[string]string{"index.html": `<p>folder {{ .sId }}</p>`, ".secret.css": "x", "logo.svg": "<svg/>"} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	setSubSetting(t, "subThemeDir", folder)
	if body := getPage(t, router, "/sub/s1", true).Body.String(); body != "<p>folder s1</p>" {
		t.Fatalf("a custom theme folder must override the picked theme, got %q", body)
	}
	if code := getPage(t, router, "/sub/s1/theme/logo.svg", false).Code; code != http.StatusOK {
		t.Errorf("the folder theme's image = %d, want 200", code)
	}
	if code := getPage(t, router, "/sub/s1/theme/.secret.css", false).Code; code != http.StatusNotFound {
		t.Errorf("a hidden file of the folder theme = %d, want 404", code)
	}
}
