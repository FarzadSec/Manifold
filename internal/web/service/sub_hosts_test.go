package service

import (
	"regexp"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/access"
)

func setSubSettings(t *testing.T, kv map[string]string) {
	t.Helper()
	s := &SettingService{}
	for k, v := range kv {
		if err := s.saveSetting(k, v); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSubscriptionLinksFollowTheDomainPlan(t *testing.T) {
	setupConflictDB(t)
	setSubSettings(t, map[string]string{
		"subDomains": "*.sub.example.com\nold.example.com", "subPort": "2096", "subPath": "/sub/", "subURI": "",
	})
	seller := model.User{Username: "seller", Password: "x", Tier: access.TierReseller, SubDomain: "r.example.net"}
	if err := database.GetDB().Create(&seller).Error; err != nil {
		t.Fatal(err)
	}
	b := (&SettingService{}).NewSubURLBuilder()

	first, _, _ := b.For(5, 0, "abc")
	again, _, _ := (&SettingService{}).NewSubURLBuilder().For(5, 0, "abc")
	other, _, _ := b.For(6, 0, "def")
	own := regexp.MustCompile(`^http://(k[0-9a-z]{9})\.sub\.example\.com:2096/sub/abc$`)
	m := own.FindStringSubmatch(first)
	if m == nil || first != again {
		t.Fatalf("link = %q then %q, want one stable host of the user's own under the wildcard", first, again)
	}
	if strings.Contains(other, m[1]) {
		t.Fatalf("two users share the host %s", m[1])
	}
	if strings.Contains(first, "abc.") {
		t.Fatal("the host name gives the subscription away")
	}
	sold, _, _ := b.For(7, seller.Id, "ghi")
	if sold != "http://r.example.net:2096/sub/ghi" {
		t.Fatalf("reseller's link = %q, want its own domain", sold)
	}

	plan := LoadSubHostPlan()
	for host, want := range map[string]bool{
		m[1] + ".sub.example.com": true, "old.example.com": true, "r.example.net": true,
		"a.b.sub.example.com": false, "elsewhere.com": false,
	} {
		if got := plan.Accepts(host); got != want {
			t.Errorf("Accepts(%s) = %v, want %v", host, got, want)
		}
	}
}

// Behind a CDN the public address holds the scheme and path; a wildcard in it names each user.
func TestAWildcardPublicAddressNamesEachUser(t *testing.T) {
	setupConflictDB(t)
	setSubSettings(t, map[string]string{"subURI": "https://*.cdn.example.com/s/", "subDomains": ""})
	link, _, _ := (&SettingService{}).NewSubURLBuilder().For(9, 0, "xyz")
	if !regexp.MustCompile(`^https://k[0-9a-z]{9}\.cdn\.example\.com/s/xyz$`).MatchString(link) {
		t.Fatalf("link = %q, want a host of the user's own on the CDN domain", link)
	}
}

// The sing-box link must sit on the same per-user host as the main one, and vanish when the format is off.
func TestSingboxLinkSharesTheUsersHost(t *testing.T) {
	setupConflictDB(t)
	setSubSettings(t, map[string]string{
		"subDomains": "*.sub.example.com", "subPort": "2096", "subPath": "/sub/", "subURI": "",
		"subSingboxEnable": "true", "subSingboxPath": "/sb/", "subSingboxURI": "",
	})
	b := (&SettingService{}).NewSubURLBuilder()
	main, _, _ := b.For(5, 0, "abc")
	box := b.SingboxFor(5, 0, "abc")
	if want := strings.Replace(main, "/sub/abc", "/sb/abc", 1); box != want || box == main {
		t.Fatalf("sing-box link = %q, want %q", box, want)
	}
	setSubSettings(t, map[string]string{"subSingboxEnable": "false"})
	if off := (&SettingService{}).NewSubURLBuilder().SingboxFor(5, 0, "abc"); off != "" {
		t.Fatalf("sing-box link with the format off = %q, want none", off)
	}
}
