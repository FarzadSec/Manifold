package tunnel

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// BenchScheme marks a tunnel test link: what the other server needs to run a test with this one.
const BenchScheme = "manifold-tunnel-test://"

// BenchInvite is a tunnel test link. The server that imports it runs the test and drives
// this one through its panel's API with Token, which reaches nothing but the test.
type BenchInvite struct {
	Version int    `json:"v"`
	ID      string `json:"id"`
	URL     string `json:"url"`
	Token   string `json:"token"`
	// Pin is the SHA-256 of the panel's certificate, so a self-signed one is trusted too.
	Pin   string `json:"pin,omitempty"`
	Role  string `json:"role"`
	Addr4 string `json:"addr4,omitempty"`
	Addr6 string `json:"addr6,omitempty"`
	// Port is the first of the BenchPortSpan ports the test may use on both servers.
	Port    int    `json:"port"`
	Expires int64  `json:"exp"`
	Host    string `json:"host,omitempty"`
}

var benchTokenPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Check refuses a link that is damaged, expired or would point the test anywhere odd.
func (i BenchInvite) Check(now time.Time) error {
	switch {
	case i.Version != 1:
		return errors.New("tunnel: this test link is from a newer panel; update this one")
	case !pairIDPattern.MatchString(i.ID):
		return errors.New("tunnel: the test link is damaged")
	case !benchTokenPattern.MatchString(i.Token):
		return errors.New("tunnel: the test link is damaged")
	case i.Role != RoleClient && i.Role != RoleServer:
		return errors.New("tunnel: the test link is damaged")
	case i.Port < 1024 || i.Port+BenchPortSpan-1 > 65535:
		return fmt.Errorf("tunnel: the test link's ports %d to %d are out of range", i.Port, i.Port+BenchPortSpan-1)
	case now.UnixMilli() > i.Expires:
		return errors.New("tunnel: the test link has expired; make a new one on the other server")
	}
	u, err := url.Parse(i.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("tunnel: the test link's panel address is not an http or https URL")
	}
	if i.Addr4 == "" && i.Addr6 == "" {
		return errors.New("tunnel: the test link carries no address for the other server")
	}
	if a, err := netip.ParseAddr(i.Addr4); i.Addr4 != "" && (err != nil || !a.Is4()) {
		return fmt.Errorf("tunnel: %q in the test link is not an IPv4 address", i.Addr4)
	}
	if a, err := netip.ParseAddr(i.Addr6); i.Addr6 != "" && (err != nil || !a.Is6() || a.Is4In6()) {
		return fmt.Errorf("tunnel: %q in the test link is not an IPv6 address", i.Addr6)
	}
	return nil
}

// Code encodes the link to hand to the other server.
func (i BenchInvite) Code() (string, error) {
	raw, err := json.Marshal(i)
	if err != nil {
		return "", err
	}
	return BenchScheme + base64URL(raw), nil
}

// ParseBenchInvite reads a tunnel test link the other server made.
func ParseBenchInvite(code string, now time.Time) (BenchInvite, error) {
	body, ok := strings.CutPrefix(strings.TrimSpace(code), BenchScheme)
	if !ok {
		return BenchInvite{}, fmt.Errorf("tunnel: a test link starts with %s", BenchScheme)
	}
	raw, err := decodeBase64URL(body)
	if err != nil {
		return BenchInvite{}, errors.New("tunnel: the test link is damaged")
	}
	var i BenchInvite
	if err := json.Unmarshal(raw, &i); err != nil {
		return BenchInvite{}, errors.New("tunnel: the test link is damaged")
	}
	if err := i.Check(now); err != nil {
		return BenchInvite{}, err
	}
	return i, nil
}
