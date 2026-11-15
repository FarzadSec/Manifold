package sub

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/l2tp"
	"github.com/mhsanaei/3x-ui/v3/internal/openvpn"
)

// seedNativeAccount stores an OpenVPN and an L2TP inbound with one client on
// both and nothing else, as the panel does for an account sold without a proxy.
func seedNativeAccount(t *testing.T, subId string) {
	t.Helper()
	db := database.GetDB()
	client := `{"email":"dana","subId":"` + subId + `","enable":true,"password":"dana-pass-1"}`
	ovpn, err := openvpn.NormalizeSettings(`{"server":{"proto":"udp","subnet":"10.201.0.0/24"},"clients":[`+client+`]}`, "")
	if err != nil {
		t.Fatalf("openvpn settings: %v", err)
	}
	tunnel, err := l2tp.NormalizeSettings(`{"server":{"psk":"shared-key-123"},"clients":[`+client+`]}`, "")
	if err != nil {
		t.Fatalf("l2tp settings: %v", err)
	}
	record := &model.ClientRecord{Email: "dana", SubID: subId, Enable: true, Password: "dana-pass-1"}
	if err := db.Create(record).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	for _, inbound := range []*model.Inbound{
		{UserId: 1, Tag: "ovpn", Remark: "ovpn", Enable: true, Port: 21300, Protocol: model.OpenVPN, Settings: ovpn, StreamSettings: "{}"},
		{UserId: 1, Tag: "l2tp", Remark: "l2tp", Enable: true, Port: l2tp.Port, Protocol: model.L2TP, Settings: tunnel, StreamSettings: "{}"},
	} {
		if err := db.Create(inbound).Error; err != nil {
			t.Fatalf("create %s inbound: %v", inbound.Protocol, err)
		}
		if err := db.Create(&model.ClientInbound{ClientId: record.Id, InboundId: inbound.Id}).Error; err != nil {
			t.Fatalf("attach client to %s: %v", inbound.Protocol, err)
		}
	}
}

// The client's information and QR windows ask for links by subscription ID. An
// account with only OpenVPN and L2TP got none, so the windows showed nothing.
func TestClientWindowsGetTheOpenVPNProfileAndTheL2TPAccount(t *testing.T) {
	seedSubDB(t)
	seedNativeAccount(t, "native-sub")

	links, err := NewLinkProvider().SubLinksForSubId("vpn.example.com", "native-sub")
	if err != nil {
		t.Fatalf("SubLinksForSubId: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("links = %q, want the OpenVPN profile and the L2TP account", links)
	}
	profile, ok := strings.CutPrefix(links[0], openvpn.ShareLinkScheme)
	if !ok {
		t.Fatalf("first link = %q, want an openvpn:// profile", links[0])
	}
	text, err := base64.RawURLEncoding.DecodeString(profile)
	if err != nil {
		t.Fatalf("profile does not decode: %v", err)
	}
	if !strings.Contains(string(text), "\nremote vpn.example.com 21300\n") {
		t.Fatalf("profile does not name the server the admin is on:\n%s", text)
	}
	if want := "l2tp://dana:dana-pass-1@vpn.example.com?ipsec=1&psk=shared-key-123#l2tp-dana"; links[1] != want {
		t.Fatalf("second link = %q, want %q", links[1], want)
	}
}

// The subscription address of such an account answered 404 to a browser. Its
// page is where the customer reads the profile and the account; a proxy app
// asking the same address gets a body without them, because it cannot use them.
func TestSubscriptionAddressOfAnAccountWithoutProxyInbounds(t *testing.T) {
	seedSubDB(t)
	seedNativeAccount(t, "native-sub")
	gin.SetMode(gin.TestMode)
	oldDistFS := distFS
	distFS = testDistFS
	t.Cleanup(func() { distFS = oldDistFS })
	router := newSubscriptionTestRouter(subscriptionTestRouterConfig{})

	browser := httptest.NewRequest(http.MethodGet, "http://vpn.example.com/sub/native-sub", nil)
	browser.Header.Set("Accept", "text/html")
	page := httptest.NewRecorder()
	router.ServeHTTP(page, browser)
	if page.Code != http.StatusOK {
		t.Fatalf("page status = %d, want 200", page.Code)
	}
	for _, want := range []string{`"openvpn://`, `"l2tp://dana:dana-pass-1@vpn.example.com?ipsec=1\u0026psk=shared-key-123#l2tp-dana"`} {
		if !strings.Contains(page.Body.String(), want) {
			t.Fatalf("page does not carry %s:\n%s", want, page.Body.String())
		}
	}

	app := httptest.NewRequest(http.MethodGet, "http://vpn.example.com/sub/native-sub", nil)
	app.Header.Set("User-Agent", "v2rayNG/1.10.0")
	body := httptest.NewRecorder()
	router.ServeHTTP(body, app)
	if body.Code != http.StatusOK {
		t.Fatalf("body status = %d, want 200", body.Code)
	}
	if body.Body.String() != "" {
		decoded, _ := base64.StdEncoding.DecodeString(body.Body.String())
		t.Fatalf("a proxy app was handed what it cannot import: %q", decoded)
	}

	// Xray has no outbound for either protocol: a JSON config for them would be
	// a "server" whose only outbounds are direct and block. No content is a 404 there.
	asJSON := httptest.NewRecorder()
	router.ServeHTTP(asJSON, httptest.NewRequest(http.MethodGet, "http://vpn.example.com/json/native-sub", nil))
	if asJSON.Code != http.StatusNotFound || asJSON.Body.Len() != 0 {
		t.Fatalf("JSON subscription = %d, want 404 and no config for a protocol Xray cannot dial:\n%s", asJSON.Code, asJSON.Body.String())
	}
}
