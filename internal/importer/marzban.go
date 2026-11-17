package importer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// marzbanSign is the 10-character signature Marzban appends to a subscription token.
func marzbanSign(body, secret string) string {
	sum := sha256.Sum256([]byte(body + secret))
	return base64.URLEncoding.EncodeToString(sum[:])[:10]
}

// MarzbanToken is a token as Marzban and PasarGuard make it, for tests and for links
// the panel hands out to imported users.
func MarzbanToken(username, secret string, at time.Time) string {
	body := strings.TrimRight(base64.URLEncoding.EncodeToString([]byte(username+","+strconv.FormatInt(at.Unix(), 10))), "=")
	return body + marzbanSign(body, secret)
}

// ParseMarzbanToken checks a Marzban or PasarGuard subscription token against the old
// panel's secret and returns the username and when the token was made. Both the
// current short tokens and the older HS256 JWTs are read.
func ParseMarzbanToken(token, secret string) (string, time.Time, bool) {
	if len(token) < 15 || secret == "" {
		return "", time.Time{}, false
	}
	if strings.HasPrefix(token, "ey") {
		return parseSubJWT(token, secret)
	}
	body, sig := token[:len(token)-10], token[len(token)-10:]
	if !hmac.Equal([]byte(sig), []byte(marzbanSign(body, secret))) {
		return "", time.Time{}, false
	}
	raw, err := base64.URLEncoding.DecodeString(body + strings.Repeat("=", (4-len(body)%4)%4))
	if err != nil {
		return "", time.Time{}, false
	}
	name, at, ok := strings.Cut(string(raw), ",")
	if !ok {
		return "", time.Time{}, false
	}
	sec, err := strconv.ParseInt(at, 10, 64)
	if err != nil {
		return "", time.Time{}, false
	}
	return name, time.Unix(sec, 0), true
}

func parseSubJWT(token, secret string) (string, time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", time.Time{}, false
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(parts[0] + "." + parts[1]))
	want := base64.RawURLEncoding.EncodeToString(m.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(strings.TrimRight(parts[2], "="))) {
		return "", time.Time{}, false
	}
	head, _ := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[0], "="))
	var h struct{ Alg string }
	if json.Unmarshal(head, &h) != nil || h.Alg != "HS256" {
		return "", time.Time{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return "", time.Time{}, false
	}
	var p struct {
		Sub    string  `json:"sub"`
		Access string  `json:"access"`
		Iat    float64 `json:"iat"`
	}
	if json.Unmarshal(raw, &p) != nil || p.Access != "subscription" || p.Sub == "" {
		return "", time.Time{}, false
	}
	return p.Sub, time.Unix(int64(p.Iat), 0), true
}

// marzbanCreds reads Marzban's proxies rows or PasarGuard's proxy_settings JSON.
func marzbanCreds(settings map[string]json.RawMessage) map[string]Cred {
	out := map[string]Cred{}
	for kind, raw := range settings {
		p := protocolName(kind)
		if p == "" {
			continue
		}
		var c Cred
		if json.Unmarshal(raw, &c) == nil && (c.ID != "" || c.Password != "") {
			out[p] = c
		}
	}
	return out
}
