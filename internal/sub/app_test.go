package sub

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/l2tp"
	"github.com/mhsanaei/3x-ui/v3/internal/openvpn"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

const (
	appSubID  = "app-sub-1"
	appUser   = "dana@example.com"
	appDevice = "device-0123456789"
)

// seedAppAccount stores one account with OpenVPN and L2TP on this panel and
// VLESS on a node, the mix a multi-protocol plan is sold as.
func seedAppAccount(t *testing.T, appEnabled bool) int {
	t.Helper()
	seedSubDB(t)
	db := database.GetDB()
	if err := db.Create(&model.Setting{Key: "appEnable", Value: map[bool]string{true: "true", false: "false"}[appEnabled]}).Error; err != nil {
		t.Fatalf("set appEnable: %v", err)
	}
	node := &model.Node{Name: "de-fra-1", Address: "de.example.com", Port: 2053, Enable: true}
	if err := db.Create(node).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}
	const uuid = "11111111-2222-4333-8444-555555555555"
	client := `{"email":"` + appUser + `","subId":"` + appSubID + `","enable":true,"password":"dana-pass-1","id":"` + uuid + `"}`
	ovpn, err := openvpn.NormalizeSettings(`{"server":{"proto":"udp","subnet":"10.201.0.0/24"},"clients":[`+client+`]}`, "")
	if err != nil {
		t.Fatalf("openvpn settings: %v", err)
	}
	tunnel, err := l2tp.NormalizeSettings(`{"server":{"psk":"shared-key-123"},"clients":[`+client+`]}`, "")
	if err != nil {
		t.Fatalf("l2tp settings: %v", err)
	}
	record := &model.ClientRecord{Email: appUser, SubID: appSubID, UUID: uuid, Enable: true, Password: "dana-pass-1", LimitHwid: 2}
	if err := db.Create(record).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	nodeID := node.Id
	for _, inbound := range []*model.Inbound{
		{UserId: 1, Tag: "ovpn", Remark: "ovpn", Enable: true, Port: 21300, Protocol: model.OpenVPN, Settings: ovpn, StreamSettings: "{}"},
		{UserId: 1, Tag: "l2tp", Remark: "l2tp", Enable: true, Port: l2tp.Port, Protocol: model.L2TP, Settings: tunnel, StreamSettings: "{}"},
		{
			UserId: 1, Tag: "vless-de", Remark: "vless-de", Enable: true, Port: 8443, Protocol: model.VLESS, NodeID: &nodeID,
			Settings:       `{"clients":[` + client + `],"decryption":"none"}`,
			StreamSettings: `{"network":"tcp","security":"none"}`,
		},
	} {
		if err := db.Create(inbound).Error; err != nil {
			t.Fatalf("create %s inbound: %v", inbound.Protocol, err)
		}
		if err := db.Create(&model.ClientInbound{ClientId: record.Id, InboundId: inbound.Id}).Error; err != nil {
			t.Fatalf("attach client to %s: %v", inbound.Protocol, err)
		}
	}
	return nodeID
}

func newAppTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewSUBController(router.Group("/"), WithSUBPath("/sub/"), WithSUBJsonPath("/json/"), WithSUBJsonEnabled(true))
	return router
}

type appCall struct {
	method, path, token, hwid string
	body                      any
}

func callApp(t *testing.T, router *gin.Engine, call appCall) (int, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	if call.body != nil {
		if err := json.NewEncoder(&body).Encode(call.body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(call.method, "http://vpn.example.com"+call.path, &body)
	req.Header.Set("User-Agent", "DTClient/0.1.0")
	if call.token != "" {
		req.Header.Set("Authorization", "Bearer "+call.token)
	}
	if call.hwid != "" {
		req.Header.Set("X-HWID", call.hwid)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	out := map[string]any{}
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s %s answered non-JSON %d: %s", call.method, call.path, rec.Code, rec.Body.String())
		}
	}
	return rec.Code, out
}

func appLogin(username string) map[string]any {
	return map[string]any{"username": username, "deviceId": appDevice, "deviceName": "office-pc", "os": "windows", "osVersion": "11", "appVersion": "0.1.0"}
}

func loginApp(t *testing.T, router *gin.Engine, prefix string) string {
	t.Helper()
	code, out := callApp(t, router, appCall{method: http.MethodPost, path: prefix + appSubID + "/app/v1/login", body: appLogin(appUser)})
	token, _ := out["token"].(string)
	if code != http.StatusOK || token == "" {
		t.Fatalf("login = %d %v", code, out)
	}
	return token
}

func TestAppProfileCarriesTheAccountItsLocationsAndEveryProtocol(t *testing.T) {
	seedAppAccount(t, true)
	inboundID := func(tag string) int {
		var ib model.Inbound
		if err := database.GetDB().Where("tag = ?", tag).First(&ib).Error; err != nil {
			t.Fatalf("inbound %s: %v", tag, err)
		}
		return ib.Id
	}
	appSvc := &service.AppService{}
	iran, err := appSvc.SaveLocation(model.AppLocation{Name: "Iran", CountryCode: "IR", Timezone: "Asia/Tehran", InboundIds: []int{inboundID("ovpn")}})
	if err != nil {
		t.Fatalf("save Iran: %v", err)
	}
	germany, err := appSvc.SaveLocation(model.AppLocation{Name: "Germany", CountryCode: "DE", InboundIds: []int{inboundID("vless-de")}})
	if err != nil {
		t.Fatalf("save Germany: %v", err)
	}
	if _, err := appSvc.SaveLocation(model.AppLocation{Name: "Unused", CountryCode: "NL"}); err != nil {
		t.Fatalf("save an empty location: %v", err)
	}
	router := newAppTestRouter()
	token := loginApp(t, router, "/sub/")

	code, profile := callApp(t, router, appCall{method: http.MethodGet, path: "/sub/" + appSubID + "/app/v1/profile", token: token, hwid: appDevice})
	if code != http.StatusOK {
		t.Fatalf("profile = %d %v", code, profile)
	}
	account, _ := profile["account"].(map[string]any)
	if account["username"] != appUser || account["deviceLimit"] != float64(2) || account["devices"] != float64(1) || account["enabled"] != true {
		t.Fatalf("account = %v", account)
	}

	locations, _ := profile["locations"].([]any)
	if len(locations) != 2 {
		t.Fatalf("locations = %v, want the two that serve this account", locations)
	}
	first, _ := locations[0].(map[string]any)
	second, _ := locations[1].(map[string]any)
	if first["id"] != float64(iran.Id) || first["countryCode"] != "IR" || first["timezone"] != "Asia/Tehran" {
		t.Fatalf("Iran = %v", first)
	}
	if second["id"] != float64(germany.Id) || second["name"] != "Germany" {
		t.Fatalf("Germany = %v", second)
	}
	if _, leaks := first["inboundIds"]; leaks {
		t.Fatal("the app is told the panel's inbound ids")
	}

	byProtocol := map[string]map[string]any{}
	for _, raw := range profile["configs"].([]any) {
		cfg := raw.(map[string]any)
		byProtocol[cfg["protocol"].(string)] = cfg
	}
	vless, ovpn, l2tpCfg := byProtocol["vless"], byProtocol["openvpn"], byProtocol["l2tp"]
	if vless == nil || ovpn == nil || l2tpCfg == nil || len(byProtocol) != 3 {
		t.Fatalf("configs by protocol = %v, want vless, openvpn and l2tp", byProtocol)
	}
	if vless["locationId"] != float64(germany.Id) || !strings.HasPrefix(vless["link"].(string), "vless://") {
		t.Fatalf("vless config = %v", vless)
	}
	outbound, _ := vless["outbound"].(map[string]any)
	if outbound["protocol"] != "vless" || !strings.Contains(mustJSON(t, outbound), `"de.example.com"`) {
		t.Fatalf("vless outbound = %v, want an Xray outbound to the node", outbound)
	}
	if _, placed := l2tpCfg["locationId"]; placed {
		t.Fatalf("the l2tp inbound serves no location, yet its config has %v", l2tpCfg["locationId"])
	}
	if ovpn["locationId"] != float64(iran.Id) || ovpn["outbound"] != nil || ovpn["name"] != "ovpn" || !strings.HasPrefix(ovpn["link"].(string), openvpn.ShareLinkScheme) {
		t.Fatalf("openvpn config = %v", ovpn)
	}
	if want := "l2tp://dana%40example.com:dana-pass-1@vpn.example.com?ipsec=1&psk=shared-key-123#l2tp-dana@example.com"; l2tpCfg["link"] != want {
		t.Fatalf("l2tp link = %v, want %s", l2tpCfg["link"], want)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestAppLoginAnswersOnEverySubscriptionPath(t *testing.T) {
	seedAppAccount(t, true)
	router := newAppTestRouter()
	token := loginApp(t, router, "/json/")
	if code, out := callApp(t, router, appCall{method: http.MethodGet, path: "/json/" + appSubID + "/app/v1/profile", token: token, hwid: appDevice}); code != http.StatusOK {
		t.Fatalf("profile on the JSON path = %d %v", code, out)
	}
}

func TestAppLoginRefusesAWrongUsernameAndThrottlesGuessing(t *testing.T) {
	seedAppAccount(t, true)
	router := newAppTestRouter()
	path := "/sub/" + appSubID + "/app/v1/login"
	for i := range 5 {
		code, out := callApp(t, router, appCall{method: http.MethodPost, path: path, body: appLogin("guess-" + string(rune('a'+i)))})
		if code != http.StatusUnauthorized || out["error"] != "bad_login" {
			t.Fatalf("guess %d = %d %v, want 401 bad_login", i+1, code, out)
		}
	}
	code, out := callApp(t, router, appCall{method: http.MethodPost, path: path, body: appLogin(appUser)})
	if code != http.StatusTooManyRequests || out["error"] != "too_many_attempts" {
		t.Fatalf("login after five wrong usernames = %d %v, want 429", code, out)
	}
}

func TestAppSessionNeedsItsTokenAndDevice(t *testing.T) {
	seedAppAccount(t, true)
	router := newAppTestRouter()
	token := loginApp(t, router, "/sub/")
	profile := "/sub/" + appSubID + "/app/v1/profile"
	for _, tc := range []struct {
		name, token, hwid string
	}{
		{"no token", "", appDevice},
		{"another device", token, "device-9999999999"},
		{"no device", token, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out := callApp(t, router, appCall{method: http.MethodGet, path: profile, token: tc.token, hwid: tc.hwid})
			if code != http.StatusUnauthorized || out["error"] != "bad_session" {
				t.Fatalf("profile = %d %v, want 401 bad_session", code, out)
			}
		})
	}

	if code, out := callApp(t, router, appCall{method: http.MethodPost, path: "/sub/" + appSubID + "/app/v1/logout", token: token, hwid: appDevice}); code != http.StatusOK {
		t.Fatalf("logout = %d %v", code, out)
	}
	if code, _ := callApp(t, router, appCall{method: http.MethodGet, path: profile, token: token, hwid: appDevice}); code != http.StatusUnauthorized {
		t.Fatalf("profile after logout = %d, want 401", code)
	}
}

func TestAppAccessOffClosesLoginAndOpenSessions(t *testing.T) {
	seedAppAccount(t, false)
	router := newAppTestRouter()
	code, out := callApp(t, router, appCall{method: http.MethodPost, path: "/sub/" + appSubID + "/app/v1/login", body: appLogin(appUser)})
	if code != http.StatusForbidden || out["error"] != "app_disabled" {
		t.Fatalf("login with app access off = %d %v, want 403 app_disabled", code, out)
	}

	if err := database.GetDB().Model(&model.Setting{}).Where("key = ?", "appEnable").Update("value", "true").Error; err != nil {
		t.Fatalf("turn app access on: %v", err)
	}
	token := loginApp(t, router, "/sub/")
	if err := database.GetDB().Model(&model.Setting{}).Where("key = ?", "appEnable").Update("value", "false").Error; err != nil {
		t.Fatalf("turn app access off: %v", err)
	}
	code, out = callApp(t, router, appCall{method: http.MethodGet, path: "/sub/" + appSubID + "/app/v1/profile", token: token, hwid: appDevice})
	if code != http.StatusForbidden || out["error"] != "app_disabled" {
		t.Fatalf("profile with app access off = %d %v, want 403 app_disabled", code, out)
	}
}

func TestAppLoginPastTheDeviceLimit(t *testing.T) {
	seedAppAccount(t, true)
	router := newAppTestRouter()
	clients := &service.ClientService{}
	for _, hwid := range []string{"iphone-1111111", "android-222222"} {
		if res, err := clients.EnforceHwidForSubID(appSubID, service.HwidRequest{Hwid: hwid}); err != nil || !res.Allowed {
			t.Fatalf("seed device %s: %+v %v", hwid, res, err)
		}
	}
	code, out := callApp(t, router, appCall{method: http.MethodPost, path: "/sub/" + appSubID + "/app/v1/login", body: appLogin(appUser)})
	if code != http.StatusForbidden || out["error"] != "device_limit" {
		t.Fatalf("login past the device limit = %d %v, want 403 device_limit", code, out)
	}
}

func appProfile(t *testing.T) map[string]any {
	t.Helper()
	router := newAppTestRouter()
	token := loginApp(t, router, "/sub/")
	code, profile := callApp(t, router, appCall{method: http.MethodGet, path: "/sub/" + appSubID + "/app/v1/profile", token: token, hwid: appDevice})
	if code != http.StatusOK {
		t.Fatalf("profile = %d %v", code, profile)
	}
	return profile
}

func TestAppProfileCarriesThePanelsNoticesNewestFirst(t *testing.T) {
	seedAppAccount(t, true)
	appSvc := &service.AppService{}
	older, err := appSvc.PublishNotice("سرور آلمان امشب ساعت ۲ به‌روزرسانی می‌شود", service.AppNoticeInfo)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	newer, err := appSvc.PublishNotice("قطعی سرور هلند تا ساعت ۱۸", service.AppNoticeCritical)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	notices, _ := appProfile(t)["notices"].([]any)
	if len(notices) != 2 {
		t.Fatalf("notices = %v, want both", notices)
	}
	first, _ := notices[0].(map[string]any)
	second, _ := notices[1].(map[string]any)
	if first["id"] != float64(newer.Id) || first["level"] != "critical" || first["body"] != newer.Body || first["createdAt"] != float64(newer.CreatedAt) {
		t.Fatalf("first notice = %v, want the critical one", first)
	}
	if second["id"] != float64(older.Id) || second["level"] != "info" {
		t.Fatalf("second notice = %v", second)
	}
}

// The app signs in with the account, so it never has a password to prompt for.
func TestAppProfileGivesOpenVPNTheLoginEvenWhenTheServerPromptsForIt(t *testing.T) {
	seedAppAccount(t, true)
	db := database.GetDB()
	var ib model.Inbound
	if err := db.Where("tag = ?", "ovpn").First(&ib).Error; err != nil {
		t.Fatalf("ovpn inbound: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal([]byte(ib.Settings), &settings); err != nil {
		t.Fatalf("ovpn settings: %v", err)
	}
	settings["server"].(map[string]any)["promptCredentials"] = true
	if err := db.Model(&ib).Update("settings", mustJSON(t, settings)).Error; err != nil {
		t.Fatalf("turn promptCredentials on: %v", err)
	}

	for _, raw := range appProfile(t)["configs"].([]any) {
		cfg := raw.(map[string]any)
		if cfg["protocol"] != "openvpn" {
			continue
		}
		profile, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(cfg["link"].(string), openvpn.ShareLinkScheme))
		if err != nil {
			t.Fatalf("decode profile: %v", err)
		}
		if !strings.Contains(string(profile), "<auth-user-pass>\n"+appUser+"\ndana-pass-1\n</auth-user-pass>") {
			t.Fatalf("the app's OpenVPN profile has no login:\n%s", profile)
		}
		return
	}
	t.Fatal("no openvpn config in the profile")
}
