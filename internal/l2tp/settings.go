package l2tp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/random"
)

const (
	// Port is where every L2TP client dials; none lets its user choose another.
	Port = 1701

	defaultSubnet = "10.9.0.0/24"
	// xl2tpd walks its pool for every call, and the pool needs room for the server and clients.
	minSubnetBits = 16
	maxSubnetBits = 29

	// 1280 passes through IPsec, L2TP and PPP headers on any path a phone meets.
	defaultMTU = 1280
	minMTU     = 576
	maxMTU     = 1460

	generatedKeyLength = 24
)

var (
	defaultDNS = []string{"1.1.1.1", "8.8.8.8"}
	// The key is typed by hand on every device and written into a swanctl string.
	pskPattern = regexp.MustCompile(`^[A-Za-z0-9!@#$%^&*()_+=.,:;~-]{8,64}$`)
	// pppd reads a backslash as the end of a Windows domain, which no account has.
	usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.@:/=-]{1,128}$`)
)

// ServerSettings is the "server" block of an L2TP inbound's settings.
type ServerSettings struct {
	// IPsec set to false also accepts sessions that are not encrypted; absent means true.
	IPsec  *bool    `json:"ipsec,omitempty" example:"true"`
	PSK    string   `json:"psk" example:"hT7mQ2xLr9VbK4nZ8cWdE5aY"`
	Subnet string   `json:"subnet" example:"10.9.0.0/24"`
	DNS    []string `json:"dns" example:"[\"1.1.1.1\",\"8.8.8.8\"]"`
	MTU    int      `json:"mtu" example:"1280"`
}

// InboundSettings is the settings JSON of an L2TP inbound.
type InboundSettings struct {
	Server  *ServerSettings `json:"server"`
	Clients []model.Client  `json:"clients"`
}

// RequiresIPsec reports whether sessions must arrive through IPsec. Either
// way the server offers it, so the key is part of every account.
func (s ServerSettings) RequiresIPsec() bool { return s.IPsec == nil || *s.IPsec }

// ValidatePSK reports whether key can be an inbound's IPsec key.
func ValidatePSK(key string) error {
	if !pskPattern.MatchString(key) {
		return errors.New("l2tp: the IPsec key must be 8 to 64 letters, digits or ! @ # $ % ^ & * ( ) _ + = . , : ; ~ -")
	}
	return nil
}

// ValidateUsername reports whether a client email can be used as an L2TP login name.
func ValidateUsername(name string) error {
	if !usernamePattern.MatchString(name) {
		return fmt.Errorf("l2tp: %q cannot be a login name: use 1 to 128 letters, digits or _ . @ : / = -", name)
	}
	return nil
}

// ValidatePassword rejects what a person could not type into a VPN dialog.
func ValidatePassword(password string) error {
	if password == "" {
		return errors.New("l2tp: a client needs a password")
	}
	if len(password) > 128 {
		return errors.New("l2tp: a password is at most 128 bytes")
	}
	for _, r := range password {
		if r < 0x20 || r == 0x7f {
			return errors.New("l2tp: a password cannot hold control characters")
		}
	}
	return nil
}

func parseSubnet(raw string) (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
	if err != nil || !prefix.Addr().Is4() {
		return netip.Prefix{}, fmt.Errorf("l2tp: subnet %q is not an IPv4 network such as %s", raw, defaultSubnet)
	}
	if prefix.Bits() < minSubnetBits || prefix.Bits() > maxSubnetBits {
		return netip.Prefix{}, fmt.Errorf("l2tp: subnet %q must be between /%d and /%d", raw, minSubnetBits, maxSubnetBits)
	}
	return prefix.Masked(), nil
}

func parseDNS(raw []string) ([]string, error) {
	out := make([]string, 0, len(raw))
	for _, entry := range raw {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		addr, err := netip.ParseAddr(entry)
		if err != nil || !addr.Is4() {
			return nil, fmt.Errorf("l2tp: DNS server %q is not an IPv4 address", entry)
		}
		out = append(out, addr.String())
	}
	if len(out) > 2 {
		return nil, errors.New("l2tp: PPP hands a client at most two DNS servers")
	}
	return out, nil
}

// NormalizeServer fills the defaults of a server block and validates it. A
// key the save left out is the stored one; a new inbound gets a generated key.
func NormalizeServer(server, previous *ServerSettings) (*ServerSettings, error) {
	out := ServerSettings{}
	if server != nil {
		out = *server
	}
	out.PSK = strings.TrimSpace(out.PSK)
	if out.PSK == "" && previous != nil {
		out.PSK = previous.PSK
	}
	if out.PSK == "" {
		out.PSK = random.Seq(generatedKeyLength)
	}
	if err := ValidatePSK(out.PSK); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.Subnet) == "" {
		out.Subnet = defaultSubnet
	}
	subnet, err := parseSubnet(out.Subnet)
	if err != nil {
		return nil, err
	}
	out.Subnet = subnet.String()
	if out.DNS == nil {
		out.DNS = defaultDNS
	}
	if out.DNS, err = parseDNS(out.DNS); err != nil {
		return nil, err
	}
	if out.MTU == 0 {
		out.MTU = defaultMTU
	}
	if out.MTU < minMTU || out.MTU > maxMTU {
		return nil, fmt.Errorf("l2tp: the MTU must be between %d and %d", minMTU, maxMTU)
	}
	return &out, nil
}

// NormalizeSettings applies NormalizeServer to a settings JSON and checks its
// clients, leaving every key this package does not know as it was.
func NormalizeSettings(settings, previousSettings string) (string, error) {
	root := map[string]json.RawMessage{}
	if trimmed := strings.TrimSpace(settings); trimmed != "" && trimmed != "null" {
		if err := json.Unmarshal([]byte(trimmed), &root); err != nil {
			return "", fmt.Errorf("l2tp: invalid settings: %w", err)
		}
	}
	var server *ServerSettings
	if raw, ok := root["server"]; ok {
		if err := json.Unmarshal(raw, &server); err != nil {
			return "", fmt.Errorf("l2tp: invalid server settings: %w", err)
		}
	}
	normalized, err := NormalizeServer(server, ServerOf(previousSettings))
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	root["server"] = raw
	clients, err := normalizeClients(root["clients"])
	if err != nil {
		return "", err
	}
	root["clients"] = clients
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// normalizeClients checks every client's login and gives a password to the
// ones saved without one, keeping the fields this package does not know.
func normalizeClients(raw json.RawMessage) (json.RawMessage, error) {
	clients := []map[string]any{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &clients); err != nil {
			return nil, fmt.Errorf("l2tp: invalid clients: %w", err)
		}
	}
	for _, client := range clients {
		email, _ := client["email"].(string)
		if err := ValidateUsername(email); err != nil {
			return nil, err
		}
		password, _ := client["password"].(string)
		if password == "" {
			password = random.Seq(16)
			client["password"] = password
		}
		if err := ValidatePassword(password); err != nil {
			return nil, err
		}
	}
	return json.Marshal(clients)
}

// ServerOf returns the server block of an L2TP inbound's settings, or nil.
func ServerOf(settings string) *ServerSettings {
	if strings.TrimSpace(settings) == "" {
		return nil
	}
	var parsed InboundSettings
	if err := json.Unmarshal([]byte(settings), &parsed); err != nil {
		return nil
	}
	return parsed.Server
}

// TunnelSubnet is the network the inbound's sessions are given addresses from.
func TunnelSubnet(settings string) (netip.Prefix, bool) {
	server := ServerOf(settings)
	if server == nil {
		return netip.Prefix{}, false
	}
	subnet, err := parseSubnet(server.Subnet)
	return subnet, err == nil
}
