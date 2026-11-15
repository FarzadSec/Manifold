package l2tp

import (
	"net/url"
	"testing"
)

// The link is read back by the panel's pages and by the bot, so it must
// survive every character a password or a key may hold.
func TestShareLinkCarriesWhatAPersonTypesIntoTheirDevice(t *testing.T) {
	tests := []struct {
		name    string
		account Account
		host    string
	}{
		{"plain", Account{Host: "vpn.example.com", Username: "alice", Password: "secret-pass", PSK: "shared-key-123", RequiresIPsec: true, Remark: "Berlin"}, "vpn.example.com"},
		{"characters with a meaning in URLs", Account{Host: "203.0.113.9", Username: "alice@shop/1", Password: "p@ss:w/rd#1 ?&=", PSK: "k&e=y+1;2,3~", RequiresIPsec: true, Remark: "DE #1 / fast"}, "203.0.113.9"},
		{"IPv6 server", Account{Host: "2001:db8::1", Username: "alice", Password: "secret-pass", PSK: "shared-key-123", RequiresIPsec: true}, "2001:db8::1"},
		{"IPsec on offer but not required", Account{Host: "vpn.example.com", Username: "alice", Password: "secret-pass", PSK: "shared-key-123"}, "vpn.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link := ShareLink(tt.account)
			parsed, err := url.Parse(link)
			if err != nil {
				t.Fatalf("%q does not parse: %v", link, err)
			}
			password, _ := parsed.User.Password()
			if parsed.Scheme != "l2tp" || parsed.Hostname() != tt.host || parsed.User.Username() != tt.account.Username ||
				password != tt.account.Password || parsed.Fragment != tt.account.Remark {
				t.Fatalf("link %q reads back as host %q user %q password %q remark %q", link, parsed.Hostname(), parsed.User.Username(), password, parsed.Fragment)
			}
			query := parsed.Query()
			if query.Get("psk") != tt.account.PSK {
				t.Fatalf("link %q carries the key %q, want %q", link, query.Get("psk"), tt.account.PSK)
			}
			if wantIPsec := map[bool]string{true: "1", false: "0"}[tt.account.RequiresIPsec]; query.Get("ipsec") != wantIPsec {
				t.Fatalf("link %q says ipsec=%q, want %s", link, query.Get("ipsec"), wantIPsec)
			}
		})
	}
}
