package sub

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/openvpn"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

const (
	filesSubID = "files-sub-1"
	filesUser  = "erin_42"
	filesPass  = "erin-pass-7"
)

// seedFilesAccount stores one account on OpenVPN and WireGuard in two locations
// (Netherlands sorted before Germany, Germany with two OpenVPN servers), one
// OpenVPN server in no location and a VLESS inbound the file pages must skip.
func seedFilesAccount(t *testing.T) map[string]int {
	t.Helper()
	seedSubDB(t)
	db := database.GetDB()
	const uuid = "11111111-2222-4333-8444-666666666666"
	serverPriv, _ := mustWireguardKeypair(t)
	clientPriv, _ := mustWireguardKeypair(t)
	client := fmt.Sprintf(`{"email":%q,"subId":%q,"enable":true,"password":%q,"id":%q}`, filesUser, filesSubID, filesPass, uuid)
	ovpn := func(subnet string) string {
		s, err := openvpn.NormalizeSettings(`{"server":{"proto":"udp","subnet":"`+subnet+`"},"clients":[`+client+`]}`, "")
		if err != nil {
			t.Fatalf("openvpn settings: %v", err)
		}
		return s
	}
	wg := func(address string) string {
		return fmt.Sprintf(`{"secretKey":%q,"clients":[{"email":%q,"subId":%q,"privateKey":%q,"allowedIPs":[%q],"enable":true}]}`,
			serverPriv, filesUser, filesSubID, clientPriv, address)
	}
	record := &model.ClientRecord{Email: filesUser, SubID: filesSubID, UUID: uuid, Enable: true, Password: filesPass, PrivateKey: clientPriv, AllowedIPs: "10.0.0.5/32"}
	if err := db.Create(record).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	ids := map[string]int{}
	for _, inbound := range []*model.Inbound{
		{UserId: 1, Tag: "ovpn-de-1", Remark: "DE UDP", Enable: true, Listen: "203.0.113.10", Port: 21300, Protocol: model.OpenVPN, Settings: ovpn("10.201.0.0/24"), StreamSettings: "{}"},
		{UserId: 1, Tag: "vless", Remark: "vless", Enable: true, Port: 8443, Protocol: model.VLESS, Settings: `{"clients":[` + client + `],"decryption":"none"}`, StreamSettings: `{"network":"tcp","security":"none"}`},
		{UserId: 1, Tag: "wg-de", Remark: "DE WG", Enable: true, Listen: "203.0.113.10", Port: 51820, Protocol: model.WireGuard, Settings: wg("10.0.0.5/32")},
		{UserId: 1, Tag: "ovpn-nl", Remark: "NL UDP", Enable: true, Listen: "203.0.113.20", Port: 21301, Protocol: model.OpenVPN, Settings: ovpn("10.202.0.0/24"), StreamSettings: "{}"},
		{UserId: 1, Tag: "wg-nl", Remark: "NL WG", Enable: true, Listen: "203.0.113.20", Port: 51821, Protocol: model.WireGuard, Settings: wg("10.0.1.5/32")},
		{UserId: 1, Tag: "ovpn-de-2", Remark: "DE TCP", Enable: true, Listen: "203.0.113.11", Port: 21302, Protocol: model.OpenVPN, Settings: ovpn("10.203.0.0/24"), StreamSettings: "{}"},
		{UserId: 1, Tag: "ovpn-free", Remark: "Backup", Enable: true, Listen: "203.0.113.30", Port: 21303, Protocol: model.OpenVPN, Settings: ovpn("10.204.0.0/24"), StreamSettings: "{}"},
	} {
		if err := db.Create(inbound).Error; err != nil {
			t.Fatalf("create %s: %v", inbound.Tag, err)
		}
		if err := db.Create(&model.ClientInbound{ClientId: record.Id, InboundId: inbound.Id}).Error; err != nil {
			t.Fatalf("attach %s: %v", inbound.Tag, err)
		}
		ids[inbound.Tag] = inbound.Id
	}
	appSvc := &service.AppService{}
	for _, loc := range []model.AppLocation{
		{Name: "Germany", CountryCode: "DE", City: "Frankfurt", SortOrder: 2, InboundIds: []int{ids["ovpn-de-1"], ids["ovpn-de-2"], ids["wg-de"]}},
		{Name: "Netherlands", CountryCode: "NL", SortOrder: 1, InboundIds: []int{ids["ovpn-nl"], ids["wg-nl"]}},
	} {
		saved, err := appSvc.SaveLocation(loc)
		if err != nil {
			t.Fatalf("save %s: %v", loc.Name, err)
		}
		ids["loc-"+loc.Name] = saved.Id
	}
	return ids
}

func newFilesRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewSUBController(router.Group("/"), WithSUBPath("/subs/"), WithSUBClashEnabled(true), WithSUBEncryption(false))
	return router
}

func getFiles(t *testing.T, router *gin.Engine, path, lang string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://vpn.example.com"+path, nil)
	if lang != "" {
		req.Header.Set("Accept-Language", lang)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// The bot names and orders files from these entries, so each one has to be the
// subscription's own link with the inbound and location it came from.
func TestSubEntriesMapEachSubscriptionLinkToItsInboundAndLocation(t *testing.T) {
	ids := seedFilesAccount(t)
	provider := NewLinkProvider()
	links, err := provider.SubLinksForSubId("vpn.example.com", filesSubID)
	if err != nil {
		t.Fatalf("sub links: %v", err)
	}
	entries, err := provider.SubEntriesForSubId("vpn.example.com", filesSubID)
	if err != nil {
		t.Fatalf("sub entries: %v", err)
	}
	if len(entries) != len(links) || len(links) != 7 {
		t.Fatalf("entries = %d, links = %d, want 7 each", len(entries), len(links))
	}
	tagOf := map[int]string{}
	for tag, id := range ids {
		if !strings.HasPrefix(tag, "loc-") {
			tagOf[id] = tag
		}
	}
	want := map[string]service.SubEntry{
		"ovpn-de-1": {Protocol: "openvpn", LocationId: ids["loc-Germany"], Location: "Germany", CountryCode: "DE", City: "Frankfurt", SortOrder: 2},
		"ovpn-de-2": {Protocol: "openvpn", LocationId: ids["loc-Germany"], Location: "Germany", CountryCode: "DE", City: "Frankfurt", SortOrder: 2},
		"wg-de":     {Protocol: "wireguard", LocationId: ids["loc-Germany"], Location: "Germany", CountryCode: "DE", City: "Frankfurt", SortOrder: 2},
		"ovpn-nl":   {Protocol: "openvpn", LocationId: ids["loc-Netherlands"], Location: "Netherlands", CountryCode: "NL", SortOrder: 1},
		"wg-nl":     {Protocol: "wireguard", LocationId: ids["loc-Netherlands"], Location: "Netherlands", CountryCode: "NL", SortOrder: 1},
		"ovpn-free": {Protocol: "openvpn"},
		"vless":     {Protocol: "vless"},
	}
	for i, entry := range entries {
		if entry.Link != links[i] {
			t.Fatalf("entry %d link = %q, want the subscription's %q", i, entry.Link, links[i])
		}
		tag := tagOf[entry.InboundId]
		expected, ok := want[tag]
		if !ok {
			t.Fatalf("entry %d has inbound %d, which the account is not on", i, entry.InboundId)
		}
		expected.Link, expected.InboundId = entry.Link, entry.InboundId
		if entry != expected {
			entry.Link, expected.Link = "", ""
			t.Fatalf("entry for %s = %+v, want %+v", tag, entry, expected)
		}
		delete(want, tag)
	}
}

func TestOpenVPNPageListsLocationsInOrderWithOneLoginAndServesEachProfile(t *testing.T) {
	ids := seedFilesAccount(t)
	router := newFilesRouter()
	entries, err := NewLinkProvider().SubEntriesForSubId("vpn.example.com", filesSubID)
	if err != nil {
		t.Fatalf("sub entries: %v", err)
	}
	profileOf := map[int]string{}
	for _, e := range entries {
		if raw, ok := strings.CutPrefix(e.Link, openvpn.ShareLinkScheme); ok {
			body, err := base64.RawURLEncoding.DecodeString(raw)
			if err != nil {
				t.Fatalf("decode profile: %v", err)
			}
			profileOf[e.InboundId] = string(body)
		}
	}

	rec := getFiles(t, router, "/subs/"+filesSubID+"/openvpn", "")
	page := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("page = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	nl, de, backup := strings.Index(page, "Netherlands"), strings.Index(page, "Germany"), strings.Index(page, "Backup")
	if nl < 0 || de < 0 || backup < 0 || !(nl < de && de < backup) {
		t.Fatalf("locations out of order (Netherlands %d, Germany %d, no location %d)", nl, de, backup)
	}
	if strings.Count(page, filesPass) != 1 || !strings.Contains(page, filesUser) || !strings.Contains(page, `dir="rtl"`) {
		t.Fatalf("page must show the login once, in Persian by default:\n%s", page)
	}
	if strings.Contains(page, "vless") {
		t.Fatalf("the OpenVPN page lists a V2Ray inbound")
	}
	for _, h := range []string{"Referrer-Policy", "X-Robots-Tag"} {
		if rec.Header().Get(h) == "" {
			t.Fatalf("header %s missing", h)
		}
	}

	for n, want := range []struct{ tag, name string }{
		{"ovpn-nl", "Netherlands.ovpn"}, {"ovpn-de-1", "Germany.ovpn"}, {"ovpn-de-2", "Germany-2.ovpn"}, {"ovpn-free", "Backup.ovpn"},
	} {
		rec := getFiles(t, router, fmt.Sprintf("/subs/%s/openvpn/%d", filesSubID, n+1), "")
		if rec.Code != http.StatusOK {
			t.Fatalf("file %d = %d", n+1, rec.Code)
		}
		if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="`+want.name+`"` {
			t.Fatalf("file %d disposition = %q, want %s", n+1, got, want.name)
		}
		if rec.Header().Get("Content-Type") != "application/x-openvpn-profile" || rec.Body.String() != profileOf[ids[want.tag]] {
			t.Fatalf("file %d is not %s's profile", n+1, want.tag)
		}
	}
	if rec := getFiles(t, router, "/subs/"+filesSubID+"/openvpn/5", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("file past the last = %d, want 404", rec.Code)
	}
	if page := getFiles(t, router, "/subs/"+filesSubID+"/openvpn", "en-US,en;q=0.9").Body.String(); !strings.Contains(page, `dir="ltr"`) {
		t.Fatalf("an English browser gets the Persian page")
	}
}

func TestWireGuardPageServesEachConfigWithItsQRAndAllAsOneArchive(t *testing.T) {
	seedFilesAccount(t)
	router := newFilesRouter()
	page := getFiles(t, router, "/subs/"+filesSubID+"/wireguard", "").Body.String()
	if strings.Count(page, "data:image/png;base64,") != 2 || strings.Index(page, "Netherlands") > strings.Index(page, "Germany") {
		t.Fatalf("want two QR codes, Netherlands first:\n%s", page)
	}
	if !strings.Contains(page, `href="wireguard/all.zip"`) {
		t.Fatalf("page has no link to the archive")
	}
	first := getFiles(t, router, "/subs/"+filesSubID+"/wireguard/1", "")
	if first.Header().Get("Content-Disposition") != `attachment; filename="Netherlands.conf"` || !strings.Contains(first.Body.String(), "Address = 10.0.1.5/32") {
		t.Fatalf("file 1 = %q %q", first.Header().Get("Content-Disposition"), first.Body.String())
	}
	archive := getFiles(t, router, "/subs/"+filesSubID+"/wireguard/all.zip", "")
	zr, err := zip.NewReader(bytes.NewReader(archive.Body.Bytes()), int64(archive.Body.Len()))
	if err != nil {
		t.Fatalf("archive: %v (%d)", err, archive.Code)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		body, err := io.ReadAll(rc)
		if closeErr := rc.Close(); err != nil || closeErr != nil {
			t.Fatalf("read %s: %v %v", f.Name, err, closeErr)
		}
		if !strings.HasPrefix(string(body), "[Interface]") {
			t.Fatalf("%s is not a WireGuard config", f.Name)
		}
	}
	if strings.Join(names, ",") != "Netherlands.conf,Germany.conf" {
		t.Fatalf("archive holds %v", names)
	}
}

// The pages hand out what the subscription itself would: nothing for an
// unknown id, and the subscription path's other routes keep working.
func TestFilePagesKeepTheSubscriptionsVisibilityAndRoutes(t *testing.T) {
	seedFilesAccount(t)
	router := newFilesRouter()
	for _, path := range []string{"/subs/unknown/openvpn", "/subs/unknown/wireguard", "/subs/unknown/openvpn/1", "/subs/unknown/wireguard/all.zip"} {
		if rec := getFiles(t, router, path, ""); rec.Code != http.StatusNotFound {
			t.Fatalf("%s = %d, want 404", path, rec.Code)
		}
	}
	if rec := getFiles(t, router, "/subs/"+filesSubID, ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "vless://") {
		t.Fatalf("raw subscription = %d", rec.Code)
	}
	if rec := getFiles(t, router, "/subs/"+filesSubID+"/clash", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "proxies:") {
		t.Fatalf("two-part clash route = %d", rec.Code)
	}
}
