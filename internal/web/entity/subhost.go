package entity

import (
	"net"
	"strings"
)

// ParseSubDomains reads the domains setting: one host per line or comma, no port, no path.
func ParseSubDomains(raw string) []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == ',' || r == '\r' }) {
		host := strings.ToLower(strings.TrimSpace(part))
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		out = append(out, host)
	}
	return out
}

// ValidSubDomain reports a host a link can be built on: a name or address, or "*." and a name.
func ValidSubDomain(host string) bool {
	name := strings.TrimPrefix(host, "*.")
	if name == "" || strings.ContainsAny(name, "/:* ") || len(name) > 253 {
		return false
	}
	if net.ParseIP(name) != nil {
		return !strings.HasPrefix(host, "*.")
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
	}
	return true
}
