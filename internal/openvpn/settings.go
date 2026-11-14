// Package openvpn runs OpenVPN inbounds: one openvpn server process per
// inbound, with the panel deciding every login over the management interface.
package openvpn

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

const (
	ProtoUDP = "udp"
	ProtoTCP = "tcp"

	defaultSubnet = "10.8.0.0/24"

	// OpenVPN's pool holds at most 65536 addresses and needs room for the server and clients.
	minSubnetBits = 16
	maxSubnetBits = 29
)

var defaultDNS = []string{"1.1.1.1", "8.8.8.8"}

// ServerSettings is the "server" block of an OpenVPN inbound's settings.
type ServerSettings struct {
	Proto  string   `json:"proto" example:"udp"`
	Subnet string   `json:"subnet" example:"10.8.0.0/24"`
	DNS    []string `json:"dns" example:"[\"1.1.1.1\",\"8.8.8.8\"]"`
	// PromptCredentials leaves the login out of the profile, so the app asks for it.
	PromptCredentials bool   `json:"promptCredentials" example:"false"`
	CACert            string `json:"caCert"`
	CAKey             string `json:"caKey,omitempty"`
	ServerCert        string `json:"serverCert,omitempty"`
	ServerKey         string `json:"serverKey,omitempty"`
	TLSCryptKey       string `json:"tlsCryptKey"`
}

// InboundSettings is the settings JSON of an OpenVPN inbound.
type InboundSettings struct {
	Server  *ServerSettings `json:"server"`
	Clients []model.Client  `json:"clients"`
}

// ForProfiles returns what a client profile needs, without the private keys.
func (s ServerSettings) ForProfiles() ServerSettings {
	s.CAKey = ""
	s.ServerKey = ""
	s.ServerCert = ""
	return s
}

func (s ServerSettings) hasPKI() bool {
	return s.CACert != "" && s.CAKey != "" && s.ServerCert != "" && s.ServerKey != ""
}

// OpenVPN replaces every other character of a login name with an underscore
// before the panel sees it, so such a name could never be matched.
var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.@:/=-]{1,128}$`)

// ValidateUsername reports whether a client email can be used as an OpenVPN login name.
func ValidateUsername(name string) error {
	if !usernamePattern.MatchString(name) {
		return fmt.Errorf("openvpn: %q cannot be a login name: use 1 to 128 letters, digits or _ . @ : / = -", name)
	}
	return nil
}

// ValidatePassword rejects what cannot travel in a profile or a management line.
func ValidatePassword(password string) error {
	if password == "" {
		return errors.New("openvpn: a client needs a password")
	}
	if len(password) > 128 {
		return errors.New("openvpn: a password is at most 128 bytes")
	}
	for _, r := range password {
		if r < 0x20 || r == 0x7f {
			return errors.New("openvpn: a password cannot hold control characters")
		}
	}
	return nil
}

func parseSubnet(raw string) (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
	if err != nil || !prefix.Addr().Is4() {
		return netip.Prefix{}, fmt.Errorf("openvpn: subnet %q is not an IPv4 network such as %s", raw, defaultSubnet)
	}
	if prefix.Bits() < minSubnetBits || prefix.Bits() > maxSubnetBits {
		return netip.Prefix{}, fmt.Errorf("openvpn: subnet %q must be between /%d and /%d", raw, minSubnetBits, maxSubnetBits)
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
			return nil, fmt.Errorf("openvpn: DNS server %q is not an IPv4 address", entry)
		}
		out = append(out, addr.String())
	}
	return out, nil
}

// NormalizeServer fills the defaults of a server block, validates it and gives
// it keys. Keys a save left out are taken from previous, the stored block.
func NormalizeServer(server, previous *ServerSettings) (*ServerSettings, error) {
	out := ServerSettings{}
	if server != nil {
		out = *server
	}
	out.Proto = strings.ToLower(strings.TrimSpace(out.Proto))
	if out.Proto == "" {
		out.Proto = ProtoUDP
	}
	if out.Proto != ProtoUDP && out.Proto != ProtoTCP {
		return nil, fmt.Errorf("openvpn: proto %q must be udp or tcp", out.Proto)
	}
	if strings.TrimSpace(out.Subnet) == "" {
		out.Subnet = defaultSubnet
	}
	subnet, err := parseSubnet(out.Subnet)
	if err != nil {
		return nil, err
	}
	out.Subnet = subnet.String()
	dns, err := parseDNS(out.DNS)
	if err != nil {
		return nil, err
	}
	if len(dns) == 0 {
		dns = append(dns, defaultDNS...)
	}
	out.DNS = dns

	if !out.hasPKI() && previous != nil && previous.hasPKI() {
		out.CACert, out.CAKey = previous.CACert, previous.CAKey
		out.ServerCert, out.ServerKey = previous.ServerCert, previous.ServerKey
	}
	if !out.hasPKI() {
		pki, err := generatePKI()
		if err != nil {
			return nil, err
		}
		out.CACert, out.CAKey = pki.caCert, pki.caKey
		out.ServerCert, out.ServerKey = pki.serverCert, pki.serverKey
	} else if err := checkPKI(out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.TLSCryptKey) == "" && previous != nil {
		out.TLSCryptKey = previous.TLSCryptKey
	}
	if strings.TrimSpace(out.TLSCryptKey) == "" {
		key, err := generateStaticKey()
		if err != nil {
			return nil, err
		}
		out.TLSCryptKey = key
	} else if err := checkStaticKey(out.TLSCryptKey); err != nil {
		return nil, err
	}
	return &out, nil
}

// NormalizeSettings applies NormalizeServer to a settings JSON, leaving every
// other key of it (the clients above all) as it was.
func NormalizeSettings(settings, previousSettings string) (string, error) {
	root := map[string]json.RawMessage{}
	if trimmed := strings.TrimSpace(settings); trimmed != "" && trimmed != "null" {
		if err := json.Unmarshal([]byte(trimmed), &root); err != nil {
			return "", fmt.Errorf("openvpn: invalid settings: %w", err)
		}
	}
	var server *ServerSettings
	if raw, ok := root["server"]; ok {
		if err := json.Unmarshal(raw, &server); err != nil {
			return "", fmt.Errorf("openvpn: invalid server settings: %w", err)
		}
	}
	normalized, err := NormalizeServer(server, serverOf(previousSettings))
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	root["server"] = raw
	if _, ok := root["clients"]; !ok {
		root["clients"] = json.RawMessage("[]")
	}
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func serverOf(settings string) *ServerSettings {
	if strings.TrimSpace(settings) == "" {
		return nil
	}
	var parsed InboundSettings
	if err := json.Unmarshal([]byte(settings), &parsed); err != nil {
		return nil
	}
	return parsed.Server
}

// ServerForProfiles returns an OpenVPN inbound's server block without private
// keys, or nil when the settings hold none.
func ServerForProfiles(settings string) *ServerSettings {
	server := serverOf(settings)
	if server == nil {
		return nil
	}
	redacted := server.ForProfiles()
	return &redacted
}
