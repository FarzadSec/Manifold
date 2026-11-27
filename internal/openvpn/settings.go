// Package openvpn runs OpenVPN inbounds: one openvpn server process per
// inbound, with the panel deciding every login over the management interface.
package openvpn

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/pathmtu"
	"github.com/mhsanaei/3x-ui/v3/internal/util/random"
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

// How the control channel is protected; old routers know neither key file.
const (
	TLSModeCrypt = "tls-crypt"
	TLSModeAuth  = "tls-auth"
	TLSModeNone  = "none"
)

// The data ciphers a profile may be limited to, in OpenVPN's own preference order.
var allowedDataCiphers = []string{"AES-256-GCM", "AES-128-GCM", "CHACHA20-POLY1305"}

const (
	minMTU, maxMTU       = 576, 9000
	minMSSFix, maxMSSFix = 512, 9000
	maxDirectivesBytes   = 4096
	// RenegNever switches renegotiation off; 0 leaves OpenVPN's hourly default.
	RenegNever               = -1
	minRenegSec, maxRenegSec = 60, 7 * 24 * 3600
	maxSearchDomains         = 8
)

var domainPattern = regexp.MustCompile(`^(?i)[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)

// ServerSettings is the "server" block of an OpenVPN inbound's settings.
type ServerSettings struct {
	Proto  string   `json:"proto" example:"udp"`
	Subnet string   `json:"subnet" example:"10.8.0.0/24"`
	DNS    []string `json:"dns" example:"[\"1.1.1.1\",\"8.8.8.8\"]"`
	// PromptCredentials leaves the login out of the profile, so the app asks for it.
	PromptCredentials bool `json:"promptCredentials" example:"false"`
	// MTU is tun-mtu on both ends and MSSFix their mssfix; zero is what dataChannelSizes picks.
	MTU    int `json:"mtu,omitempty" example:"1420"`
	MSSFix int `json:"mssfix,omitempty" example:"1360"`
	// DataCiphers limits the negotiable ciphers; empty offers every allowed one.
	DataCiphers []string `json:"dataCiphers,omitempty" example:"[\"AES-256-GCM\"]"`
	// SplitTunnel stops pushing the default route, so only the VPN subnet goes through it.
	SplitTunnel bool   `json:"splitTunnel,omitempty" example:"false"`
	TLSMode     string `json:"tlsMode,omitempty" example:"tls-crypt"`
	// TLSVersionMin is the oldest TLS the server accepts, 1.2 when empty.
	TLSVersionMin string `json:"tlsVersionMin,omitempty" example:"1.2"`
	RenegSec      int    `json:"renegSec,omitempty" example:"0"`
	MaxClients    int    `json:"maxClients,omitempty" example:"0"`
	// SearchDomains are pushed so a short name resolves inside them.
	SearchDomains []string `json:"searchDomains,omitempty" example:"[\"corp.example\"]"`
	// ServerName is the server certificate's common name, which profiles verify.
	ServerName string `json:"serverName,omitempty" example:"manifold-openvpn-server"`
	// ClientDirectives are extra lines for every profile of this inbound.
	ClientDirectives string `json:"clientDirectives,omitempty" example:"sndbuf 0"`
	CACert           string `json:"caCert"`
	CAKey            string `json:"caKey,omitempty"`
	ServerCert       string `json:"serverCert,omitempty"`
	ServerKey        string `json:"serverKey,omitempty"`
	TLSCryptKey      string `json:"tlsCryptKey"`
}

// InboundSettings is the settings JSON of an OpenVPN inbound.
type InboundSettings struct {
	Server  *ServerSettings `json:"server"`
	Clients []model.Client  `json:"clients"`
}

// dataChannelSizes are the tun-mtu and mssfix of both ends: the inbound's own or, over UDP, what
// fits pathmtu.Path, mssfix keeping TCP inside it; TCP carries any size, so OpenVPN keeps its own.
func (s ServerSettings) dataChannelSizes() (mtu, mssfix int) {
	mtu, mssfix = s.MTU, s.MSSFix
	if s.Proto == ProtoTCP {
		return mtu, mssfix
	}
	if mtu == 0 {
		mtu = pathmtu.OpenVPN
	}
	if mssfix == 0 {
		mssfix = min(mtu+pathmtu.OpenVPNOverhead, pathmtu.OpenVPNPayload)
	}
	return mtu, mssfix
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

func parseDataCiphers(raw []string) ([]string, error) {
	var out []string
	for _, entry := range raw {
		name := strings.ToUpper(strings.TrimSpace(entry))
		if name == "" || slices.Contains(out, name) {
			continue
		}
		if !slices.Contains(allowedDataCiphers, name) {
			return nil, fmt.Errorf("openvpn: data cipher %q must be one of %s", entry, strings.Join(allowedDataCiphers, ", "))
		}
		out = append(out, name)
	}
	return out, nil
}

func parseTLSMode(raw string) (string, error) {
	switch mode := strings.ToLower(strings.TrimSpace(raw)); mode {
	case "":
		return TLSModeCrypt, nil
	case TLSModeCrypt, TLSModeAuth, TLSModeNone:
		return mode, nil
	default:
		return "", fmt.Errorf("openvpn: tlsMode %q must be %s, %s or %s", raw, TLSModeCrypt, TLSModeAuth, TLSModeNone)
	}
}

// Lines the panel writes into every profile itself; a second copy would contradict it.
var panelOwnedDirectives = []string{
	"client", "remote", "proto", "port", "dev", "dev-type", "ca", "cert", "key", "pkcs12",
	"tls-crypt", "tls-crypt-v2", "tls-auth", "key-direction", "auth-user-pass", "verify-x509-name",
	"script-security", "up", "down", "route-up", "ipchange", "tls-verify", "plugin",
}

var directivePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// parseDirectives checks the extra profile lines: one directive per line, no inline blocks.
func parseDirectives(raw string) (string, error) {
	if len(raw) > maxDirectivesBytes {
		return "", fmt.Errorf("openvpn: the extra profile lines are longer than %d bytes", maxDirectivesBytes)
	}
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r", ""), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		name, _, _ := strings.Cut(line, " ")
		name = strings.TrimSpace(strings.Split(name, "\t")[0])
		if strings.ContainsAny(line, "<>") || !directivePattern.MatchString(name) {
			return "", fmt.Errorf("openvpn: %q is not one OpenVPN directive", line)
		}
		if slices.Contains(panelOwnedDirectives, name) {
			return "", fmt.Errorf("openvpn: %q is written by the panel and cannot be an extra line", name)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), nil
}

func checkRange(name string, value, low, high int) error {
	if value != 0 && (value < low || value > high) {
		return fmt.Errorf("openvpn: %s %d must be 0 for the default or between %d and %d", name, value, low, high)
	}
	return nil
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
	if err := checkRange("mtu", out.MTU, minMTU, maxMTU); err != nil {
		return nil, err
	}
	if err := checkRange("mssfix", out.MSSFix, minMSSFix, maxMSSFix); err != nil {
		return nil, err
	}
	if out.DataCiphers, err = parseDataCiphers(out.DataCiphers); err != nil {
		return nil, err
	}
	if out.TLSMode, err = parseTLSMode(out.TLSMode); err != nil {
		return nil, err
	}
	if out.ClientDirectives, err = parseDirectives(out.ClientDirectives); err != nil {
		return nil, err
	}
	if err := checkSessionOptions(&out); err != nil {
		return nil, err
	}

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
	}
	if out.ServerName, err = checkPKI(out); err != nil {
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

// normalizeClients checks every client's login name and gives a password to
// the ones saved without one, keeping the fields this package does not know.
func normalizeClients(raw json.RawMessage) (json.RawMessage, error) {
	clients := []map[string]any{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &clients); err != nil {
			return nil, fmt.Errorf("openvpn: invalid clients: %w", err)
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

// checkSessionOptions validates the TLS floor, renegotiation, client cap and search domains.
func checkSessionOptions(s *ServerSettings) error {
	switch s.TLSVersionMin = strings.TrimSpace(s.TLSVersionMin); s.TLSVersionMin {
	case "", "1.2", "1.3":
	default:
		return fmt.Errorf("openvpn: tlsVersionMin %q must be 1.2 or 1.3", s.TLSVersionMin)
	}
	if s.RenegSec != 0 && s.RenegSec != RenegNever && (s.RenegSec < minRenegSec || s.RenegSec > maxRenegSec) {
		return fmt.Errorf("openvpn: renegSec %d must be -1 for never, 0 for the default or between %d and %d", s.RenegSec, minRenegSec, maxRenegSec)
	}
	if s.MaxClients < 0 {
		return errors.New("openvpn: maxClients cannot be negative")
	}
	domains, err := parseSearchDomains(s.SearchDomains)
	s.SearchDomains = domains
	return err
}

func parseSearchDomains(raw []string) ([]string, error) {
	var out []string
	for _, entry := range raw {
		name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(entry), "."))
		if name == "" || slices.Contains(out, name) {
			continue
		}
		if len(name) > 253 || !domainPattern.MatchString(name) {
			return nil, fmt.Errorf("openvpn: search domain %q is not a domain name", entry)
		}
		out = append(out, name)
	}
	if len(out) > maxSearchDomains {
		return nil, fmt.Errorf("openvpn: at most %d search domains", maxSearchDomains)
	}
	return out, nil
}

// TunnelSubnet is the network an inbound's server hands its clients addresses from.
func TunnelSubnet(settings string) (netip.Prefix, bool) {
	server := serverOf(settings)
	if server == nil {
		return netip.Prefix{}, false
	}
	subnet, err := parseSubnet(server.Subnet)
	return subnet, err == nil
}

// DefaultSubnet is the network a server without one is given.
func DefaultSubnet() netip.Prefix { return netip.MustParsePrefix(defaultSubnet) }
