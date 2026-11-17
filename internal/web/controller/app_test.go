package controller

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func appLocationsCall(t *testing.T, router *gin.Engine, method, path, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	return responseObj(t, resp.Body.String())
}

// The panel names a location, links inbounds of any protocol to it, lists it and removes it.
func TestAppLocationsSaveListAndDelete(t *testing.T) {
	t.Setenv("XUI_DB_FOLDER", t.TempDir())
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewAppController(router.Group("/panel/api/app"))
	vless := &model.Inbound{Tag: "vless-de", Port: 443, Protocol: model.VLESS, Enable: true, Settings: `{"clients":[]}`}
	ovpn := &model.Inbound{Tag: "ovpn-de", Port: 1194, Protocol: model.OpenVPN, Enable: true, Settings: `{"clients":[]}`}
	for _, ib := range []*model.Inbound{vless, ovpn} {
		if err := database.GetDB().Create(ib).Error; err != nil {
			t.Fatal(err)
		}
	}
	ids := strconv.Itoa(vless.Id) + "," + strconv.Itoa(ovpn.Id)

	saved := appLocationsCall(t, router, http.MethodPost, "/panel/api/app/locations",
		`{"name":"Germany","countryCode":"de","city":"Frankfurt","timezone":"Europe/Berlin","inboundIds":[`+ids+`]}`)
	obj, _ := saved["obj"].(map[string]any)
	if saved["success"] != true || obj["id"] == nil {
		t.Fatalf("save = %v", saved)
	}
	if refused := appLocationsCall(t, router, http.MethodPost, "/panel/api/app/locations", `{"name":"x","countryCode":"DEU"}`); refused["success"] != false {
		t.Fatalf("a three-letter country was saved: %v", refused)
	}

	list := appLocationsCall(t, router, http.MethodGet, "/panel/api/app/locations", "")
	locations, _ := list["obj"].([]any)
	if len(locations) != 1 {
		t.Fatalf("locations = %v, want Germany only", list)
	}
	germany := locations[0].(map[string]any)
	inbounds, _ := germany["inboundIds"].([]any)
	if germany["countryCode"] != "DE" || len(inbounds) != 2 || inbounds[0] != float64(vless.Id) || inbounds[1] != float64(ovpn.Id) {
		t.Fatalf("Germany = %v", germany)
	}

	id := strconv.Itoa(int(obj["id"].(float64)))
	if deleted := appLocationsCall(t, router, http.MethodPost, "/panel/api/app/locations/del/"+id, ""); deleted["success"] != true {
		t.Fatalf("delete = %v", deleted)
	}
	if left, _ := appLocationsCall(t, router, http.MethodGet, "/panel/api/app/locations", "")["obj"].([]any); len(left) != 0 {
		t.Fatalf("after delete: %v", left)
	}
}
