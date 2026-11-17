// Package importer reads the users of another panel (Marzban, PasarGuard, Marzneshin,
// 3x-ui/x-ui, s-ui, Hiddify, Remnawave) into one shape the panel can create.
package importer

import (
	"strings"
)

// Cred is a user's credential for one protocol on the old panel.
type Cred struct {
	ID       string `json:"id,omitempty"`
	Flow     string `json:"flow,omitempty"`
	Password string `json:"password,omitempty"`
	Method   string `json:"method,omitempty"`
}

// User is one account of the old panel.
type User struct {
	Username  string          `json:"username"`
	Enabled   bool            `json:"enabled"`
	DataLimit int64           `json:"dataLimit"`
	Up        int64           `json:"up"`
	Down      int64           `json:"down"`
	Expire    int64           `json:"expire"`
	Note      string          `json:"note,omitempty"`
	TgID      int64           `json:"tgId,omitempty"`
	LimitIP   int             `json:"limitIp,omitempty"`
	Reset     string          `json:"reset,omitempty"`
	Creds     map[string]Cred `json:"creds"`
	// SubID is kept as the user's sub id when the old link path ends in it.
	SubID string `json:"subId,omitempty"`
	// Aliases are other paths the old panel served this user's subscription at.
	Aliases []string `json:"aliases,omitempty"`
	// TokenName and RevokedAt let a signed Marzban/PasarGuard token find the user.
	TokenName string `json:"tokenName,omitempty"`
	RevokedAt int64  `json:"revokedAt,omitempty"`
}

// Result is what a source holds.
type Result struct {
	Source string `json:"source"`
	Users  []User `json:"-"`
	// TokenSecret verifies the old panel's signed subscription tokens.
	TokenSecret string   `json:"-"`
	Warnings    []string `json:"warnings"`
}

// Protocols counts the users that have a credential for each protocol.
func (r *Result) Protocols() map[string]int {
	out := map[string]int{}
	for _, u := range r.Users {
		for p := range u.Creds {
			out[p]++
		}
	}
	return out
}

// Protocol names used across sources.
const (
	VLESS       = "vless"
	VMess       = "vmess"
	Trojan      = "trojan"
	Shadowsocks = "shadowsocks"
	Hysteria2   = "hysteria2"
)

func protocolName(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "vless":
		return VLESS
	case "vmess":
		return VMess
	case "trojan":
		return Trojan
	case "shadowsocks", "ss":
		return Shadowsocks
	case "hysteria2", "hy2":
		return Hysteria2
	}
	return ""
}

// resetName maps an old panel's reset strategy onto the panel's cycle names.
func resetName(s string) string {
	switch strings.ToLower(s) {
	case "day", "daily":
		return "daily"
	case "week", "weekly":
		return "weekly"
	case "month", "monthly", "calendar_month":
		return "monthly"
	}
	return ""
}
