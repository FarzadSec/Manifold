package wgquick

import (
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// A released config is kept under this suffix; wg-quick only reads "*.conf".
const releasedSuffix = ".before-manifold"

// How WGDashboard, wireguard-install, wg-easy and PiVPN note a peer's name above it.
var peerNameComments = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^#\s*name#?\s*[=:]\s*(.+)$`),
	regexp.MustCompile(`(?i)^#+\s*client:?\s+(.+?)(\s+\([^)]*\))?$`),
	regexp.MustCompile(`(?i)^#+\s*begin\s+(.+?)\s*#+$`),
}

// ForeignPeer is a peer of an interface this panel did not set up.
type ForeignPeer struct {
	Name         string
	PublicKey    string
	PresharedKey string
	AllowedIPs   []netip.Prefix
	Keepalive    int
}

// ForeignConfig is a wg-quick config written by the admin or by another tool.
type ForeignConfig struct {
	Interface  string
	ListenPort int
	PrivateKey string
	Addresses  []netip.Prefix
	MTU        int
	Hooks      Hooks
	Peers      []ForeignPeer
	// Dropped names the settings an inbound cannot carry over, such as FwMark.
	Dropped []string
}

// Foreign is a config in the WireGuard directory that an inbound could take over.
type Foreign struct {
	Config ForeignConfig
	Up     bool
	// Problem says why the config cannot become an inbound, or is empty.
	Problem string
}

func peerNameOf(comment string) string {
	for _, pattern := range peerNameComments {
		if match := pattern.FindStringSubmatch(comment); match != nil {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
}

// ParseForeignConfig reads a config the way wg-quick does and reports why it
// cannot become an inbound, if it cannot.
func ParseForeignConfig(name, content string) (ForeignConfig, error) {
	cfg := ForeignConfig{Interface: name}
	if _, ours := configOwner(content); ours {
		return cfg, fmt.Errorf("wireguard: %s is already managed by this panel", name)
	}
	var addresses []string
	section, pendingName := "", ""
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#") {
			if found := peerNameOf(line); found != "" {
				pendingName = found
			}
			continue
		}
		if cut := strings.Index(line, "#"); cut >= 0 {
			line = strings.TrimSpace(line[:cut])
		}
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = strings.ToLower(line)
			if section == "[peer]" {
				cfg.Peers = append(cfg.Peers, ForeignPeer{Name: pendingName})
			}
			pendingName = ""
			continue
		}
		key, value, isSetting := strings.Cut(line, "=")
		if !isSetting {
			continue
		}
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		switch section {
		case "[interface]":
			cfg.readInterfaceSetting(key, value, &addresses)
		case "[peer]":
			cfg.Peers[len(cfg.Peers)-1].readSetting(key, value)
		}
	}
	return cfg, cfg.check(strings.Join(addresses, ","))
}

func (cfg *ForeignConfig) readInterfaceSetting(key, value string, addresses *[]string) {
	switch key {
	case "privatekey":
		cfg.PrivateKey = value
	case "listenport":
		cfg.ListenPort, _ = strconv.Atoi(value)
	case "address":
		*addresses = append(*addresses, value)
	case "mtu":
		cfg.MTU, _ = strconv.Atoi(value)
	case "preup":
		cfg.Hooks.PreUp = append(cfg.Hooks.PreUp, value)
	case "postup":
		cfg.Hooks.PostUp = append(cfg.Hooks.PostUp, value)
	case "predown":
		cfg.Hooks.PreDown = append(cfg.Hooks.PreDown, value)
	case "postdown":
		cfg.Hooks.PostDown = append(cfg.Hooks.PostDown, value)
	case "fwmark":
		if value != "" && value != "off" && value != "0" {
			cfg.Dropped = append(cfg.Dropped, "FwMark")
		}
	case "table":
		// The panel adds each peer's route itself, in the main table.
		if value != "" && value != "auto" && value != "off" {
			cfg.Dropped = append(cfg.Dropped, "Table")
		}
	}
}

func (p *ForeignPeer) readSetting(key, value string) {
	switch key {
	case "publickey":
		p.PublicKey = value
	case "presharedkey":
		p.PresharedKey = value
	case "persistentkeepalive":
		p.Keepalive, _ = strconv.Atoi(value)
	case "allowedips":
		for _, entry := range strings.Split(value, ",") {
			if prefix, ok := parseAllowedIP(entry); ok {
				p.AllowedIPs = append(p.AllowedIPs, prefix)
			}
		}
	}
}

func (cfg *ForeignConfig) check(addresses string) error {
	name := cfg.Interface
	if !validKey(cfg.PrivateKey) {
		return fmt.Errorf("wireguard: %s has no usable private key", name)
	}
	if cfg.ListenPort <= 0 || cfg.ListenPort > 65535 {
		return fmt.Errorf("wireguard: %s has no listen port, so it is not a server", name)
	}
	parsed, err := ParseAddresses(addresses)
	if err != nil {
		return err
	}
	if len(parsed) == 0 {
		return fmt.Errorf("wireguard: %s has no IPv4 address", name)
	}
	cfg.Addresses = parsed
	seen := map[string]bool{}
	for _, p := range cfg.Peers {
		if !validKey(p.PublicKey) {
			return fmt.Errorf("wireguard: %s has a peer without a usable public key", name)
		}
		if p.PresharedKey != "" && !validKey(p.PresharedKey) {
			return fmt.Errorf("wireguard: %s has a peer with an unusable preshared key", name)
		}
		if seen[p.PublicKey] {
			return fmt.Errorf("wireguard: %s lists the peer %s twice", name, p.PublicKey)
		}
		seen[p.PublicKey] = true
	}
	return nil
}

// Foreign lists the interfaces of the WireGuard directory that this panel did
// not write, sorted by name.
func (m *Manager) Foreign() []Foreign {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Foreign
	for _, path := range m.sys.configs(m.dir) {
		content, _ := m.sys.readFile(path)
		if _, ours := configOwner(content); ours {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(path), ".conf")
		cfg, err := ParseForeignConfig(name, content)
		entry := Foreign{Config: cfg, Up: m.sys.linkExists(name)}
		if err != nil {
			entry.Problem = err.Error()
		}
		out = append(out, entry)
	}
	slices.SortFunc(out, func(a, b Foreign) int { return strings.Compare(a.Config.Interface, b.Config.Interface) })
	return out
}

// ReadForeign returns the config of one foreign interface.
func (m *Manager) ReadForeign(name string) (ForeignConfig, error) {
	if err := ValidateInterface(name); err != nil {
		return ForeignConfig{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	content, exists := m.sys.readFile(configPath(m.dir, name))
	if !exists {
		return ForeignConfig{}, fmt.Errorf("wireguard: there is no config for %s in %s", name, m.dir)
	}
	return ParseForeignConfig(name, content)
}

// Release takes a foreign interface down and moves its config aside, so that
// an inbound can run under its name. The returned function puts both back.
func (m *Manager) Release(name string) (restore func(), err error) {
	if err := ValidateInterface(name); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	path := configPath(m.dir, name)
	content, exists := m.sys.readFile(path)
	if !exists {
		return nil, fmt.Errorf("wireguard: there is no config for %s in %s", name, m.dir)
	}
	if _, ours := configOwner(content); ours {
		return nil, fmt.Errorf("wireguard: %s is already managed by this panel", name)
	}
	wasUp := m.sys.linkExists(name)
	if wasUp {
		if out, err := m.sys.run("", "wg-quick", "down", path); err != nil {
			return nil, fmt.Errorf("wireguard: stopping %s: %w: %s", name, err, lastLine(out))
		}
	}
	aside := path + releasedSuffix
	if err := m.sys.rename(path, aside); err != nil {
		if wasUp {
			_, upErr := m.sys.run("", "wg-quick", "up", path)
			err = errors.Join(err, upErr)
		}
		return nil, fmt.Errorf("wireguard: moving the config of %s aside: %w", name, err)
	}
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if err := m.sys.rename(aside, path); err != nil {
			return
		}
		if wasUp {
			_, _ = m.sys.run("", "wg-quick", "up", path)
		}
	}, nil
}
