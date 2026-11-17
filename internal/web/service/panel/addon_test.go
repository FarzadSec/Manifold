package panel

import (
	"slices"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/access"
)

func TestAddonGetsANarrowedTokenAndAWebhookAndLosesBoth(t *testing.T) {
	setupAPITokenTestDB(t)
	db := database.GetDB()
	var owner model.User
	if err := db.First(&owner).Error; err != nil {
		t.Fatal(err)
	}
	actor := &access.Actor{UserId: owner.Id, Owner: true, Tier: owner.Tier}
	s := &AddonService{}
	manifest := `{"id":"mirza-bot","name":"Mirza Bot","version":"1.0.0","permissions":["users:read","users:write"],
		"events":["user.limited","user.expired"],"webhook":"https://bot.example.com/manifold"}`
	got, err := s.Install([]byte(manifest), actor)
	if err != nil {
		t.Fatal(err)
	}
	if got.Token == "" || len(got.WebhookSecret) < 32 {
		t.Fatalf("the token and the signing secret are shown once at install: %+v", got)
	}
	var tok model.ApiToken
	db.First(&tok, got.Addon.TokenId)
	if tok.Name != "addon:mirza-bot" || tok.UserId != owner.Id || !slices.Equal(tok.Permissions, []string{"users:read", "users:write"}) {
		t.Fatalf("token = %+v", tok)
	}
	if row, ok := (&ApiTokenService{}).MatchToken(got.Token); !ok || row.Id != tok.Id {
		t.Fatal("the token shown does not sign in")
	}
	var hook model.Webhook
	db.First(&hook, got.Addon.WebhookId)
	if hook.URL != "https://bot.example.com/manifold" || !slices.Equal(hook.Events, []string{"user.limited", "user.expired"}) {
		t.Fatalf("webhook = %+v", hook)
	}

	again, err := s.Install([]byte(strings.Replace(manifest, `"users:read","users:write"`, `"users:read"`, 1)), actor)
	if err != nil {
		t.Fatal(err)
	}
	db.First(&tok, got.Addon.TokenId)
	if again.Token != "" || again.Addon.TokenId != tok.Id || !slices.Equal(tok.Permissions, []string{"users:read"}) {
		t.Fatalf("an update kept token %d with %v and showed %q", again.Addon.TokenId, tok.Permissions, again.Token)
	}

	if err := s.SetEnabled(got.Addon.Id, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := (&ApiTokenService{}).MatchToken(got.Token); ok {
		t.Fatal("a disabled addon's token still signs in")
	}
	if err := s.Uninstall(got.Addon.Id); err != nil {
		t.Fatal(err)
	}
	var left int64
	db.Model(&model.ApiToken{}).Where("id = ?", tok.Id).Count(&left)
	var hooks int64
	db.Model(&model.Webhook{}).Where("id = ?", hook.Id).Count(&hooks)
	if left+hooks != 0 {
		t.Fatalf("after uninstall %d tokens and %d webhooks are left", left, hooks)
	}
}

func TestAddonCannotAskForMoreThanTheInstallerHas(t *testing.T) {
	setupAPITokenTestDB(t)
	operator := &access.Actor{UserId: 1, Tier: access.TierOperator, Perms: access.Set{"users:read": {}}}
	if _, err := (&AddonService{}).Install([]byte(`{"id":"x-bot","permissions":["users:write"]}`), operator); err == nil {
		t.Fatal("an addon got users:write from an account that has only users:read")
	}
	for _, bad := range []string{
		`{"id":"Bad Id","permissions":["users:read"]}`,
		`{"id":"x-bot","permissions":["nope"]}`,
		`{"id":"x-bot","permissions":["users:read"],"events":["user.limited"]}`,
		`{"id":"x-bot","permissions":["users:read"],"webhook":"ftp://x"}`,
	} {
		if _, err := ParseAddonManifest([]byte(bad)); err == nil {
			t.Errorf("manifest %s was taken", bad)
		}
	}
}
