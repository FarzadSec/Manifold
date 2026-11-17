package panel

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/util/crypto"
	"github.com/mhsanaei/3x-ui/v3/internal/util/random"
	"github.com/mhsanaei/3x-ui/v3/internal/web/access"
)

type ApiTokenService struct{}

const apiTokenLength = 48

type ApiTokenView struct {
	Id        int    `json:"id" example:"2"`
	Name      string `json:"name" example:"central-panel-a"`
	Token     string `json:"token,omitempty" example:"new-token-string"`
	Enabled   bool   `json:"enabled" example:"true"`
	CreatedAt int64  `json:"createdAt" example:"1736000000"`
	Scope     string `json:"scope" example:"admin"`
	ExpiresAt int64  `json:"expiresAt" example:"0"`
	// Acts-as account, tier and permissions narrow an admin-scope token; empty permissions mean no restriction.
	UserId      int      `json:"userId" example:"1"`
	Username    string   `json:"username" example:"admin"`
	Tier        string   `json:"tier" example:"admin"`
	Permissions []string `json:"permissions" example:"[\"users:read\"]"`
	RateLimit   int      `json:"rateLimit" example:"120"`
	// Requests over the token's life: the cap (0 for none), how many it made, and its last use.
	MaxRequests   int64 `json:"maxRequests" example:"10000"`
	RequestCount  int64 `json:"requestCount" example:"42"`
	DeleteOnLimit bool  `json:"deleteOnLimit" example:"false"`
	LastUsedAt    int64 `json:"lastUsedAt" example:"1736000000000"`
}

func apiTokenCreatedAtSeconds(createdAt int64) int64 {
	if createdAt >= model.ApiTokenUnixMillisecondsThreshold {
		return createdAt / 1000
	}
	return createdAt
}

// toView builds the metadata view returned by List. It never carries the
// token value: only a SHA-256 hash is stored, and the plaintext is shown
// exactly once at creation time.
func toView(t *model.ApiToken) *ApiTokenView {
	perms := t.Permissions
	if perms == nil {
		perms = []string{}
	}
	return &ApiTokenView{
		Id:          t.Id,
		Name:        t.Name,
		Enabled:     t.Enabled,
		CreatedAt:   apiTokenCreatedAtSeconds(t.CreatedAt),
		Scope:       t.Scope,
		ExpiresAt:   t.ExpiresAt,
		UserId:      t.UserId,
		Tier:        t.Tier,
		Permissions: perms,
		RateLimit:   t.RateLimit,

		MaxRequests:   t.MaxRequests,
		RequestCount:  t.RequestCount,
		DeleteOnLimit: t.DeleteOnLimit,
		LastUsedAt:    t.LastUsedAt,
	}
}

// TokenAccess binds an admin-scope token to an account and narrows it.
type TokenAccess struct {
	UserId      int
	Tier        string
	Permissions []string
	RateLimit   int
	// MaxRequests caps the token's requests over its life; DeleteOnLimit removes it at the cap.
	MaxRequests   int64
	DeleteOnLimit bool
}

// CreateBound creates an admin-scope token that acts as one account, never above it.
func (s *ApiTokenService) CreateBound(name string, expiresAt int64, ta TokenAccess) (*ApiTokenView, error) {
	var user model.User
	if err := database.GetDB().First(&user, ta.UserId).Error; err != nil {
		return nil, common.NewError("the account the token acts as does not exist")
	}
	if ta.Tier == "" {
		ta.Tier = user.Tier
	}
	if !access.ValidTier(ta.Tier) || access.TierRank(ta.Tier) > access.TierRank(user.Tier) {
		return nil, common.NewError("a token's tier cannot be above its account's")
	}
	for _, p := range ta.Permissions {
		if !access.Known(p) {
			return nil, common.NewError("unknown permission: " + p)
		}
	}
	if ta.RateLimit < 0 || ta.MaxRequests < 0 {
		return nil, common.NewError("request limits cannot be negative")
	}
	view, err := s.Create(name, model.ApiScopeAdmin, expiresAt)
	if err != nil {
		return nil, err
	}
	if err := database.GetDB().Model(&model.ApiToken{}).Where("id = ?", view.Id).Updates(map[string]any{
		"user_id": ta.UserId, "tier": ta.Tier, "permissions": jsonStrings(ta.Permissions), "rate_limit": ta.RateLimit,
		"max_requests": ta.MaxRequests, "delete_on_limit": ta.DeleteOnLimit,
	}).Error; err != nil {
		return nil, err
	}
	view.UserId, view.Username, view.Tier, view.RateLimit = ta.UserId, user.Username, ta.Tier, ta.RateLimit
	view.MaxRequests, view.DeleteOnLimit = ta.MaxRequests, ta.DeleteOnLimit
	view.Permissions = append([]string{}, ta.Permissions...)
	return view, nil
}

func jsonStrings(v []string) string {
	if len(v) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// DeleteForUser removes every token that acts as the account, when the account is deleted.
func (s *ApiTokenService) DeleteForUser(userId int) error {
	return database.GetDB().Where("user_id = ?", userId).Delete(&model.ApiToken{}).Error
}

// NormalizeScope validates a requested scope, defaulting empty to admin so
// callers that omit it keep the legacy full-access behavior.
func NormalizeScope(scope string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "", model.ApiScopeAdmin:
		return model.ApiScopeAdmin, nil
	case model.ApiScopeMonitor:
		return model.ApiScopeMonitor, nil
	case model.ApiScopeNodeSync:
		return model.ApiScopeNodeSync, nil
	case model.ApiScopeNodeAdmin:
		return model.ApiScopeNodeAdmin, nil
	case model.ApiScopeTunnel:
		return model.ApiScopeTunnel, nil
	default:
		return "", common.NewError("scope must be 'admin', 'monitor', 'node-sync', 'node-admin' or 'tunnel'")
	}
}

func (s *ApiTokenService) List() ([]*ApiTokenView, error) {
	db := database.GetDB()
	var rows []*model.ApiToken
	if err := db.Model(model.ApiToken{}).Order("id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	var users []model.User
	if err := db.Model(&model.User{}).Select("id", "username", "is_owner").Find(&users).Error; err != nil {
		return nil, err
	}
	names, owner := map[int]string{}, ""
	for _, u := range users {
		names[u.Id] = u.Username
		if u.IsOwner {
			owner = u.Username
		}
	}
	out := make([]*ApiTokenView, 0, len(rows))
	for _, r := range rows {
		v := toView(r)
		if v.Username = names[r.UserId]; r.UserId == 0 {
			v.Username = owner
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *ApiTokenService) Create(name, scope string, expiresAt int64) (*ApiTokenView, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, common.NewError("token name is required")
	}
	if len(name) > 64 {
		return nil, common.NewError("token name must be 64 characters or fewer")
	}
	normScope, err := NormalizeScope(scope)
	if err != nil {
		return nil, err
	}
	if expiresAt < 0 || (expiresAt != 0 && expiresAt <= nowMilli()) {
		return nil, common.NewError("expiresAt must be 0 (never) or a future unix-ms timestamp")
	}
	db := database.GetDB()
	var count int64
	if err := db.Model(model.ApiToken{}).Where("name = ?", name).Count(&count).Error; err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, common.NewError("a token with that name already exists")
	}
	plaintext := random.Seq(apiTokenLength)
	row := &model.ApiToken{
		Name:      name,
		Token:     crypto.HashTokenSHA256(plaintext),
		Enabled:   true,
		Scope:     normScope,
		ExpiresAt: expiresAt,
	}
	if err := db.Create(row).Error; err != nil {
		return nil, err
	}
	view := toView(row)
	view.Token = plaintext
	return view, nil
}

// RecreateByName replaces any token with this name, keeping exactly one so a
// repeatedly-run caller cannot accumulate credentials it can never revoke.
func (s *ApiTokenService) RecreateByName(name, scope string) (*ApiTokenView, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, common.NewError("token name is required")
	}
	// Same column, same limit as Create: the CLI now feeds this operator input.
	if len(name) > 64 {
		return nil, common.NewError("token name must be 64 characters or fewer")
	}
	givenScope := ""
	if strings.TrimSpace(scope) != "" {
		var err error
		if givenScope, err = NormalizeScope(scope); err != nil {
			return nil, err
		}
	}
	plaintext := random.Seq(apiTokenLength)
	row := &model.ApiToken{Name: name, Token: crypto.HashTokenSHA256(plaintext), Enabled: true, Scope: givenScope}
	if err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		var replaced []model.ApiToken
		if err := tx.Where("name = ?", name).Order("id asc").Limit(1).Find(&replaced).Error; err != nil {
			return err
		}
		if len(replaced) > 0 {
			// A rotation keeps the deadline the token was issued with; reviving an
			// expired one would silently hand back a credential that never expires.
			if replaced[0].ExpiresAt != 0 && nowMilli() >= replaced[0].ExpiresAt {
				return common.NewErrorf("token %q has expired; create a new token from the panel or the API instead", name)
			}
			row.ExpiresAt = replaced[0].ExpiresAt
		}
		if row.Scope == "" {
			// An empty Scope takes the column default of admin, so a rotated
			// monitor or node-sync token would silently gain full access.
			row.Scope = model.ApiScopeAdmin
			if len(replaced) > 0 {
				if !model.IsKnownApiScope(replaced[0].Scope) {
					return common.NewErrorf("token %q has unknown scope %q", name, replaced[0].Scope)
				}
				row.Scope = replaced[0].Scope
			}
		}
		if err := tx.Where("name = ?", name).Delete(model.ApiToken{}).Error; err != nil {
			return err
		}
		return tx.Create(row).Error
	}); err != nil {
		return nil, err
	}
	view := toView(row)
	view.Token = plaintext
	return view, nil
}

func (s *ApiTokenService) Delete(id int) error {
	if id <= 0 {
		return common.NewError("invalid token id")
	}
	db := database.GetDB()
	return db.Where("id = ?", id).Delete(model.ApiToken{}).Error
}

func (s *ApiTokenService) DeleteExpectedScope(id int, expectedScope string) error {
	if id <= 0 {
		return common.NewError("invalid token id")
	}
	scope, err := requireExpectedScope(expectedScope)
	if err != nil {
		return err
	}
	res := database.GetDB().Where("id = ? AND scope = ?", id, scope).Delete(model.ApiToken{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("token not found with expected scope")
	}
	return nil
}

func (s *ApiTokenService) SetEnabled(id int, enabled bool) error {
	if id <= 0 {
		return common.NewError("invalid token id")
	}
	db := database.GetDB()
	res := db.Model(model.ApiToken{}).Where("id = ?", id).Update("enabled", enabled)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("token not found")
	}
	return nil
}

func (s *ApiTokenService) SetEnabledExpectedScope(id int, expectedScope string, enabled bool) error {
	if id <= 0 {
		return common.NewError("invalid token id")
	}
	scope, err := requireExpectedScope(expectedScope)
	if err != nil {
		return err
	}
	res := database.GetDB().Model(model.ApiToken{}).Where("id = ? AND scope = ?", id, scope).Update("enabled", enabled)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("token not found with expected scope")
	}
	return nil
}

func nowMilli() int64 { return time.Now().UnixMilli() }

// DisableExpectedScope fails closed unless the stored scope matches the caller,
// preventing rotation from revoking a newly minted token after a wrong ID.
func (s *ApiTokenService) DisableExpectedScope(id int, expectedScope string) error {
	if id <= 0 {
		return common.NewError("invalid token id")
	}
	return s.SetEnabledExpectedScope(id, expectedScope, false)
}

func requireExpectedScope(expectedScope string) (string, error) {
	if strings.TrimSpace(expectedScope) == "" {
		return "", common.NewError("expected scope is required")
	}
	scope, err := NormalizeScope(expectedScope)
	if err != nil {
		return "", err
	}
	return scope, nil
}

// MatchToken seeks the enabled row by the presented value's SHA-256 (comparing hashes leaks nothing
// usable), then checks scope and expiry so a corrupt value is never treated as admin.
func (s *ApiTokenService) MatchToken(presented string) (*model.ApiToken, bool) {
	if presented == "" {
		return nil, false
	}
	db := database.GetDB()
	var row model.ApiToken
	if err := db.Model(model.ApiToken{}).
		Where("token = ? AND enabled = ?", crypto.HashTokenSHA256(presented), true).
		First(&row).Error; err != nil {
		return nil, false
	}
	if !model.IsKnownApiScope(row.Scope) {
		return nil, false
	}
	if row.ExpiresAt != 0 && nowMilli() >= row.ExpiresAt {
		return nil, false
	}
	return &row, true
}

// ErrTokenUsedUp refuses a token that has made every request it was given.
var ErrTokenUsedUp = errors.New("this API token has used up its requests")

// Spend counts one request against a capped token, deleting it at the cap when asked to, and
// notes an uncapped token's last use at most once a minute.
func (s *ApiTokenService) Spend(t *model.ApiToken, now time.Time) error {
	db := database.GetDB()
	ms := now.UnixMilli()
	if t.MaxRequests == 0 {
		if ms-t.LastUsedAt >= int64(time.Minute/time.Millisecond) {
			if err := db.Model(&model.ApiToken{}).Where("id = ?", t.Id).Update("last_used_at", ms).Error; err != nil {
				logger.Warning("api token: last use not recorded:", err)
			}
		}
		return nil
	}
	res := db.Model(&model.ApiToken{}).Where("id = ? AND request_count < max_requests", t.Id).
		Updates(map[string]any{"request_count": gorm.Expr("request_count + 1"), "last_used_at": ms})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrTokenUsedUp
	}
	if t.DeleteOnLimit && t.RequestCount+1 >= t.MaxRequests {
		return db.Delete(&model.ApiToken{}, t.Id).Error
	}
	return nil
}

// Match is the legacy boolean form for callers that do not need scope.
func (s *ApiTokenService) Match(presented string) bool {
	_, ok := s.MatchToken(presented)
	return ok
}
