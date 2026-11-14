// Package wgquick runs WireGuard inbounds on the kernel's WireGuard through
// the standard tools: a wg-quick config per inbound, peers applied with wg set.
package wgquick

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// defaultSubnet is where the panel allocates client addresses when an inbound
// names no subnet (service.defaultWireguardBase); the server takes its first host.
const defaultSubnet = "10.0.0.0/24"

// wg-quick's own rule for an interface name.
var interfacePattern = regexp.MustCompile(`^[a-zA-Z0-9_=+.-]{1,15}$`)

// Peer is one account's device on an interface.
type Peer struct {
	Name         string
	PublicKey    string
	PresharedKey string
	AllowedIPs   []netip.Prefix
}

// Instance is the desired state of one WireGuard inbound's interface.
type Instance struct {
	Id         int
	Tag        string
	Interface  string
	ListenPort int
	PrivateKey string
	Address    netip.Prefix
	MTU        int
	Peers      []Peer
}

type inboundSettings struct {
	SecretKey  string         `json:"secretKey"`
	MTU        int            `json:"mtu"`
	SubnetIP   string         `json:"subnetIp"`
	SubnetCIDR int            `json:"subnetCidr"`
	Interface  string         `json:"interface"`
	Clients    []model.Client `json:"clients"`
}

// DefaultInterface is the interface of an inbound that names none.
func DefaultInterface(inboundID int) string { return fmt.Sprintf("mfwg%d", inboundID) }

// ValidateInterface reports whether name can be a WireGuard interface.
func ValidateInterface(name string) error {
	if !interfacePattern.MatchString(name) {
		return fmt.Errorf("wireguard: %q cannot be an interface name: use 1 to 15 letters, digits or _ = + . -", name)
	}
	return nil
}

func validKey(key string) bool {
	raw, err := base64.StdEncoding.DecodeString(key)
	return err == nil && len(raw) == 32
}

// serverAddress is the interface's own address: the first host of the subnet
// the inbound names, else of the /24 its first client lives in, else of the default.
func serverAddress(s inboundSettings) netip.Prefix {
	subnet := netip.MustParsePrefix(defaultSubnet)
	explicit, err := netip.ParsePrefix(fmt.Sprintf("%s/%d", strings.TrimSpace(s.SubnetIP), s.SubnetCIDR))
	// A subnet needs room for the server and a client; "ip/0" is a half-filled form.
	if err == nil && explicit.Addr().Is4() && s.SubnetCIDR >= 1 && s.SubnetCIDR <= 30 {
		subnet = explicit.Masked()
	} else if first, ok := firstClientAddress(s.Clients); ok {
		subnet, _ = first.Prefix(24)
	}
	return netip.PrefixFrom(subnet.Addr().Next(), subnet.Bits())
}

func firstClientAddress(clients []model.Client) (netip.Addr, bool) {
	for _, c := range clients {
		for _, entry := range c.AllowedIPs {
			if prefix, ok := parseAllowedIP(entry); ok && prefix.Addr().Is4() && !prefix.Addr().IsUnspecified() {
				return prefix.Addr(), true
			}
		}
	}
	return netip.Addr{}, false
}

// parseAllowedIP reads "10.0.0.2/32" or a bare address, which means that one host.
func parseAllowedIP(entry string) (netip.Prefix, bool) {
	entry = strings.TrimSpace(entry)
	if prefix, err := netip.ParsePrefix(entry); err == nil {
		return prefix.Masked(), true
	}
	if addr, err := netip.ParseAddr(entry); err == nil {
		return netip.PrefixFrom(addr, addr.BitLen()), true
	}
	return netip.Prefix{}, false
}

// InstanceFromInbound derives the interface to run from a WireGuard inbound,
// with one peer per enabled client that has a key and an address. It reports
// false when the inbound cannot be an interface at all.
func InstanceFromInbound(ib *model.Inbound) (Instance, bool) {
	if ib == nil || ib.Protocol != model.WireGuard {
		return Instance{}, false
	}
	var parsed inboundSettings
	if err := json.Unmarshal([]byte(ib.Settings), &parsed); err != nil || !validKey(parsed.SecretKey) {
		return Instance{}, false
	}
	name := strings.TrimSpace(parsed.Interface)
	if name == "" {
		name = DefaultInterface(ib.Id)
	}
	if ValidateInterface(name) != nil || ib.Port <= 0 || ib.Port > 65535 {
		return Instance{}, false
	}
	inst := Instance{
		Id:         ib.Id,
		Tag:        ib.Tag,
		Interface:  name,
		ListenPort: ib.Port,
		PrivateKey: parsed.SecretKey,
		Address:    serverAddress(parsed),
		MTU:        max(parsed.MTU, 0),
	}
	seen := map[string]bool{}
	for _, c := range parsed.Clients {
		if !c.Enable || !validKey(c.PublicKey) || seen[c.PublicKey] {
			continue
		}
		if c.PreSharedKey != "" && !validKey(c.PreSharedKey) {
			continue
		}
		var allowed []netip.Prefix
		for _, entry := range c.AllowedIPs {
			// A client's own 0.0.0.0/0 is what it routes into the tunnel, not what it is.
			if prefix, ok := parseAllowedIP(entry); ok && prefix.Bits() > 0 {
				allowed = append(allowed, prefix)
			}
		}
		if len(allowed) == 0 {
			continue
		}
		seen[c.PublicKey] = true
		inst.Peers = append(inst.Peers, Peer{
			Name:         c.Email,
			PublicKey:    c.PublicKey,
			PresharedKey: c.PreSharedKey,
			AllowedIPs:   allowed,
		})
	}
	return inst, true
}

func egressOwner(id int) string { return fmt.Sprintf("wg%d", id) }

func (p Peer) allowedIPs() string {
	parts := make([]string, 0, len(p.AllowedIPs))
	for _, prefix := range p.AllowedIPs {
		parts = append(parts, prefix.String())
	}
	return strings.Join(parts, ",")
}

func (p Peer) fingerprint() string {
	return p.PublicKey + "\x00" + p.PresharedKey + "\x00" + p.allowedIPs()
}

// structuralFingerprint moves with what only bringing the interface down and
// up can apply; peers are applied to the live interface.
func (inst Instance) structuralFingerprint() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		inst.Interface,
		fmt.Sprint(inst.ListenPort),
		inst.PrivateKey,
		inst.Address.String(),
		fmt.Sprint(inst.MTU),
	}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func (inst Instance) peersByKey() map[string]Peer {
	out := make(map[string]Peer, len(inst.Peers))
	for _, p := range inst.Peers {
		out[p.PublicKey] = p
	}
	return out
}

func sortedKeys(peers map[string]Peer) []string {
	keys := make([]string, 0, len(peers))
	for key := range peers {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
