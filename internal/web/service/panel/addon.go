package panel

import (
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/eventbus"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/web/access"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// AddonManifest is what an addon publishes about itself (addon.json).
type AddonManifest struct {
	Id          string   `json:"id" example:"mirza-bot"`
	Name        string   `json:"name" example:"Mirza Bot"`
	Version     string   `json:"version" example:"1.0.0"`
	Description string   `json:"description,omitempty" example:"Sells subscriptions in Telegram"`
	Homepage    string   `json:"homepage,omitempty" example:"https://github.com/example/bot"`
	Permissions []string `json:"permissions" example:"[\"users:read\",\"users:write\"]"`
	Events      []string `json:"events,omitempty" example:"[\"user.limited\"]"`
	Webhook     string   `json:"webhook,omitempty" example:"https://bot.example.com/manifold"`
	PanelURL    string   `json:"panelUrl,omitempty" example:"https://bot.example.com/admin"`
}

// AddonView is an installed addon with its manifest.
type AddonView struct {
	model.Addon
	Manifest AddonManifest `json:"manifest"`
}

// AddonInstalled is an install's answer: the token and signing secret, shown once.
type AddonInstalled struct {
	Addon         AddonView `json:"addon"`
	Token         string    `json:"token,omitempty" example:"new-token-string"`
	WebhookSecret string    `json:"webhookSecret,omitempty" example:"4f1c…"`
}

// AddonService installs addons: a token narrowed to what each asks, and a webhook.
type AddonService struct{}

var addonIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,47}$`)

func addonView(a model.Addon) AddonView {
	var m AddonManifest
	_ = json.Unmarshal([]byte(a.Manifest), &m)
	if m.Permissions == nil {
		m.Permissions = []string{}
	}
	return AddonView{Addon: a, Manifest: m}
}

// ParseAddonManifest checks a manifest before anything is created for it.
func ParseAddonManifest(raw []byte) (AddonManifest, error) {
	var m AddonManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, common.NewError("the manifest is not JSON:", err)
	}
	m.Id, m.Name = strings.TrimSpace(m.Id), strings.TrimSpace(m.Name)
	if !addonIDPattern.MatchString(m.Id) {
		return m, common.NewError("an addon id is 2 to 48 lowercase letters, digits and dashes")
	}
	if m.Name == "" {
		m.Name = m.Id
	}
	if len(m.Permissions) == 0 {
		return m, common.NewError("an addon names the permissions it needs")
	}
	for _, p := range m.Permissions {
		if !access.Known(p) {
			return m, common.NewError("unknown permission:", p)
		}
	}
	for _, e := range m.Events {
		if !slices.Contains(eventbus.AllEventTypes, eventbus.EventType(e)) {
			return m, common.NewError("unknown event:", e)
		}
	}
	if len(m.Events) > 0 && m.Webhook == "" {
		return m, common.NewError("an addon that hears events gives a webhook URL")
	}
	for _, link := range []string{m.Webhook, m.PanelURL, m.Homepage} {
		if link == "" {
			continue
		}
		if u, err := url.Parse(link); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return m, common.NewError("not an http(s) address:", link)
		}
	}
	return m, nil
}

func (s *AddonService) List() ([]AddonView, error) {
	var rows []model.Addon
	if err := database.GetDB().Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]AddonView, 0, len(rows))
	for _, r := range rows {
		out = append(out, addonView(r))
	}
	return out, nil
}

// Install creates an addon's token, acting as the installing account and never above
// it, and its webhook; installing the same id again updates both in place.
func (s *AddonService) Install(raw []byte, actor *access.Actor) (AddonInstalled, error) {
	m, err := ParseAddonManifest(raw)
	if err != nil {
		return AddonInstalled{}, err
	}
	if actor == nil {
		return AddonInstalled{}, common.NewError("no account to install the addon as")
	}
	for _, p := range m.Permissions {
		if !actor.Owner && !actor.Perms.Has(p) {
			return AddonInstalled{}, common.NewError("the addon asks for", p, "which this account does not have")
		}
	}
	manifest, _ := json.Marshal(m)
	db := database.GetDB()
	var row model.Addon
	db.Where("addon_id = ?", m.Id).First(&row)
	out := AddonInstalled{}
	tokens := &ApiTokenService{}
	if row.Id == 0 {
		tok, err := tokens.CreateBound("addon:"+m.Id, 0, TokenAccess{UserId: actor.UserId, Permissions: m.Permissions})
		if err != nil {
			return out, err
		}
		row.TokenId, out.Token = tok.Id, tok.Token
	} else if err := db.Model(&model.ApiToken{}).Where("id = ?", row.TokenId).
		Update("permissions", jsonStrings(m.Permissions)).Error; err != nil {
		return out, err
	}
	hooks := &service.WebhookService{}
	if m.Webhook != "" {
		in := service.WebhookInput{Id: row.WebhookId, Name: "addon:" + m.Id, Enable: true, Kind: "http", URL: m.Webhook, Events: m.Events}
		view, err := hooks.Save(in, actor)
		if err != nil && row.WebhookId != 0 {
			in.Id = 0
			view, err = hooks.Save(in, actor)
		}
		if err != nil {
			if out.Token != "" {
				_ = tokens.Delete(row.TokenId)
			}
			return out, err
		}
		row.WebhookId, out.WebhookSecret = view.Id, view.Secret
	} else if row.WebhookId != 0 {
		_ = hooks.Delete(row.WebhookId, actor)
		row.WebhookId = 0
	}
	row.AddonId, row.Name, row.Version, row.Manifest, row.Enable = m.Id, m.Name, m.Version, string(manifest), true
	if row.InstalledBy == 0 {
		row.InstalledBy = actor.UserId
	}
	if err := db.Save(&row).Error; err != nil {
		return out, err
	}
	out.Addon = addonView(row)
	return out, nil
}

// SetEnabled turns an addon's token and webhook on or off together.
func (s *AddonService) SetEnabled(id int, enable bool) error {
	db := database.GetDB()
	var row model.Addon
	if err := db.First(&row, id).Error; err != nil {
		return common.NewError("no such addon")
	}
	if err := (&ApiTokenService{}).SetEnabled(row.TokenId, enable); err != nil {
		return err
	}
	if row.WebhookId != 0 {
		db.Model(&model.Webhook{}).Where("id = ?", row.WebhookId).Update("enable", enable)
	}
	return db.Model(&row).Update("enable", enable).Error
}

// Uninstall removes an addon with its token and webhook, so it can no longer act.
func (s *AddonService) Uninstall(id int) error {
	db := database.GetDB()
	var row model.Addon
	if err := db.First(&row, id).Error; err != nil {
		return common.NewError("no such addon")
	}
	if err := (&ApiTokenService{}).Delete(row.TokenId); err != nil {
		return err
	}
	if row.WebhookId != 0 {
		db.Where("webhook_id = ?", row.WebhookId).Delete(&model.WebhookDelivery{})
		db.Delete(&model.Webhook{}, row.WebhookId)
	}
	return db.Delete(&row).Error
}
