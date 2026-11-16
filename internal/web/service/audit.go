package service

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

// Audit settings: change logging is off on a new panel; sign-ins and exports are kept regardless.
const (
	auditEnabledKey       = "auditEnabled"
	auditRetentionKey     = "auditRetentionDays"
	auditDetailMaxBytes   = 16 << 10
	auditRedacted         = "[redacted]"
	defaultAuditRetention = 90
)

// Audit actions recorded whatever the settings say.
const (
	AuditLogin       = "login"
	AuditLoginFailed = "login_failed"
	AuditLoginNewIP  = "login_new_ip"
	AuditExport      = "export"
	AuditTwoFactor   = "two_factor"
	AuditClear       = "clear"
)

// AuditService keeps the record of what admins do.
type AuditService struct{}

// AuditSettings is the change-logging switch and how long records are kept.
type AuditSettings struct {
	Enabled       bool `json:"enabled" example:"false"`
	RetentionDays int  `json:"retentionDays" example:"90"`
}

// Settings reads the audit settings.
func (s *AuditService) Settings() AuditSettings {
	st := SettingService{}
	enabled, _ := st.getBool(auditEnabledKey)
	days, err := st.getInt(auditRetentionKey)
	if err != nil {
		days = defaultAuditRetention
	}
	return AuditSettings{Enabled: enabled, RetentionDays: days}
}

// SaveSettings stores the audit settings.
func (s *AuditService) SaveSettings(a AuditSettings) error {
	if a.RetentionDays < 0 {
		a.RetentionDays = 0
	}
	st := SettingService{}
	if err := st.setBool(auditEnabledKey, a.Enabled); err != nil {
		return err
	}
	return st.setInt(auditRetentionKey, a.RetentionDays)
}

// Record stores one entry; a failure is logged, never returned, so it cannot undo the action.
func (s *AuditService) Record(e model.AuditEntry) {
	if e.At == 0 {
		e.At = time.Now().UnixMilli()
	}
	if len(e.Detail) > auditDetailMaxBytes {
		e.Detail = e.Detail[:auditDetailMaxBytes]
	}
	if err := database.GetDB().Create(&e).Error; err != nil {
		logger.Warning("audit: record failed:", err)
	}
}

// AuditFilter narrows the log; zero values match everything.
type AuditFilter struct {
	UserId   *int   `form:"-"`
	Admin    string `form:"admin"`
	Entity   string `form:"entity"`
	Action   string `form:"action"`
	From     int64  `form:"from"`
	To       int64  `form:"to"`
	Page     int    `form:"page"`
	PageSize int    `form:"pageSize"`
}

// AuditPage is one page of the log, newest first.
type AuditPage struct {
	Items []model.AuditEntry `json:"items"`
	Total int64              `json:"total" example:"120"`
}

// List returns the newest entries that match the filter.
func (s *AuditService) List(f AuditFilter) (*AuditPage, error) {
	q := database.GetDB().Model(&model.AuditEntry{})
	if f.Admin != "" {
		q = q.Where("username = ?", f.Admin)
	}
	if f.Entity != "" {
		q = q.Where("entity = ?", f.Entity)
	}
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	if f.From > 0 {
		q = q.Where("at >= ?", f.From)
	}
	if f.To > 0 {
		q = q.Where("at <= ?", f.To)
	}
	page := &AuditPage{Items: []model.AuditEntry{}}
	if err := q.Count(&page.Total).Error; err != nil {
		return nil, err
	}
	size := f.PageSize
	if size <= 0 || size > 200 {
		size = 50
	}
	offset := max(f.Page-1, 0) * size
	err := q.Order("at DESC").Order("id DESC").Offset(offset).Limit(size).Find(&page.Items).Error
	return page, err
}

// Clear deletes every entry; the clearing itself becomes the first entry of the new log.
func (s *AuditService) Clear(by model.AuditEntry) error {
	if err := database.GetDB().Where("1 = 1").Delete(&model.AuditEntry{}).Error; err != nil {
		return err
	}
	by.Entity, by.Action = "audit", AuditClear
	s.Record(by)
	return nil
}

// Prune deletes entries older than the retention; 0 keeps everything.
func (s *AuditService) Prune(now time.Time) error {
	days := s.Settings().RetentionDays
	if days <= 0 {
		return nil
	}
	cutoff := now.Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
	return database.GetDB().Where("at < ?", cutoff).Delete(&model.AuditEntry{}).Error
}

// SeenLoginIP reports whether the account signed in from this address before.
func (s *AuditService) SeenLoginIP(userID int, ip string) bool {
	var n int64
	database.GetDB().Model(&model.AuditEntry{}).
		Where("user_id = ? AND action IN ? AND ip = ?", userID, []string{AuditLogin, AuditLoginNewIP}, ip).
		Limit(1).Count(&n)
	return n > 0
}

var redactedKeys = map[string]bool{
	"password": true, "oldpassword": true, "newpassword": true, "privatekey": true, "secret": true,
	"token": true, "apitoken": true, "uuid": true, "id": true, "auth": true, "presharedkey": true,
	"twofactortoken": true, "twofactorcode": true, "tgbottoken": true, "discordbottoken": true,
	"smtppassword": true, "ldappassword": true, "psk": true, "key": true, "keypem": true, "certpem": true,
}

// RedactJSON hides every secret-looking value in a JSON body; a body that is not JSON is dropped.
func RedactJSON(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	out, _ := json.Marshal(redactValue(v))
	return string(out)
}

func redactValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if redactedKeys[strings.ToLower(k)] {
				if s, ok := val.(string); !ok || s != "" {
					t[k] = auditRedacted
				}
				continue
			}
			t[k] = redactValue(val)
		}
		return t
	case []any:
		for i := range t {
			t[i] = redactValue(t[i])
		}
		return t
	default:
		return v
	}
}
