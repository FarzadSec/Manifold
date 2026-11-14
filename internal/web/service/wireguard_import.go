package service

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"unicode"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/random"
	wgutil "github.com/mhsanaei/3x-ui/v3/internal/util/wireguard"
	"github.com/mhsanaei/3x-ui/v3/internal/wgquick"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

// WireGuardImportable is an interface of this server that can become an inbound.
type WireGuardImportable struct {
	Name    string `json:"name" example:"wg0"`
	Port    int    `json:"port" example:"51820"`
	Address string `json:"address" example:"10.0.0.1/24"`
	Peers   int    `json:"peers" example:"42"`
	// Restricted counts the peers WGDashboard keeps switched off for this interface.
	Restricted int  `json:"restricted" example:"3"`
	Up         bool `json:"up" example:"true"`
	// Problem says why the interface cannot be imported; empty when it can.
	Problem string `json:"problem" example:""`
}

// WireGuardImportSources is what this server offers to import.
type WireGuardImportSources struct {
	ToolsInstalled bool `json:"toolsInstalled" example:"true"`
	// DashboardDB is the WGDashboard database peer names and usage are read from, if one was found.
	DashboardDB      string                `json:"dashboardDb" example:"/root/WGDashboard/src/db/wgdashboard.db"`
	DashboardRunning bool                  `json:"dashboardRunning" example:"false"`
	Interfaces       []WireGuardImportable `json:"interfaces"`
}

// WireGuardImportResult says what an imported interface became.
type WireGuardImportResult struct {
	Interface string `json:"interface" example:"wg0"`
	InboundId int    `json:"inboundId" example:"7"`
	Clients   int    `json:"clients" example:"42"`
	// Disabled counts the clients imported switched off, as WGDashboard had restricted them.
	Disabled int `json:"disabled" example:"3"`
	// WithoutKey counts the clients whose private key is unknown, so the panel cannot show their config.
	WithoutKey int `json:"withoutKey" example:"5"`
	// Skipped lists the peers left out, each with the reason.
	Skipped []string `json:"skipped" example:"[\"old-laptop: has no address in the tunnel\"]"`
	// Dropped lists the interface settings an inbound does not carry over.
	Dropped []string `json:"dropped" example:"[\"FwMark\"]"`
}

// wireGuardHost is the part of the server an import reads and changes.
type wireGuardHost interface {
	ReadForeign(name string) (wgquick.ForeignConfig, error)
	Release(name string) (restore func(), err error)
}

// importedEmail turns a peer's name into a client email: the panel allows no
// spaces, slashes or control characters in one.
func importedEmail(name, fallback string) string {
	var b strings.Builder
	gap := false
	for _, r := range strings.TrimSpace(name) {
		if r == '/' || r == '\\' || r < 0x20 || r == 0x7f || unicode.IsSpace(r) {
			gap = true
			continue
		}
		if gap && b.Len() > 0 {
			b.WriteByte('-')
		}
		gap = false
		b.WriteRune(r)
	}
	if email := b.String(); email != "" && email != "." && email != ".." {
		return email
	}
	return fallback
}

type wireGuardImportBuilder struct {
	iface   string
	taken   func(email string) bool
	used    map[string]bool
	clients []model.Client
	stats   []xray.ClientTraffic
	result  WireGuardImportResult
}

func (b *wireGuardImportBuilder) freeEmail(wanted string) string {
	email := wanted
	for n := 2; b.used[strings.ToLower(email)] || b.taken(email); n++ {
		email = fmt.Sprintf("%s-%d", wanted, n)
	}
	b.used[strings.ToLower(email)] = true
	return email
}

func (b *wireGuardImportBuilder) skip(name, publicKey, reason string) {
	if name == "" {
		name = publicKey
	}
	b.result.Skipped = append(b.result.Skipped, name+": "+reason)
}

func (b *wireGuardImportBuilder) add(name, publicKey, presharedKey string, allowedIPs []string, known wgquick.DashboardPeer, enable bool) {
	if _, err := wgutil.KeyToHex(publicKey); err != nil {
		b.skip(name, publicKey, "has no usable public key")
		return
	}
	normalized, err := normalizeWireguardAllowedIPs(allowedIPs)
	if err != nil || len(normalized) == 0 {
		b.skip(name, publicKey, "has no address in the tunnel")
		return
	}
	fallback := b.iface + "-" + wireguardHostAddr(normalized[0]).String()
	client := model.Client{
		Email:        b.freeEmail(importedEmail(name, fallback)),
		PublicKey:    publicKey,
		PreSharedKey: presharedKey,
		AllowedIPs:   normalized,
		Enable:       enable,
		SubID:        random.Seq(16),
	}
	if name != "" && name != client.Email {
		client.Comment = name
	}
	// A private key that is not this peer's would produce a config that never connects.
	if derived, err := wgutil.PublicKeyFromPrivate(known.PrivateKey); err == nil && derived == publicKey {
		client.PrivateKey = known.PrivateKey
	} else {
		b.result.WithoutKey++
	}
	if known.Keepalive > 0 {
		keepAlive := model.KeepAliveValue(fmt.Sprint(known.Keepalive))
		client.KeepAlive = &keepAlive
	}
	if !enable {
		b.result.Disabled++
	}
	b.result.Clients++
	b.clients = append(b.clients, client)
	b.stats = append(b.stats, xray.ClientTraffic{Email: client.Email, Enable: enable, Up: known.Up, Down: known.Down})
}

// importedSubnet is where new clients of an imported server get their
// addresses: the server's own IPv4 network, or its /24 when it holds a lone address.
func importedSubnet(addresses []netip.Prefix) netip.Prefix {
	for _, address := range addresses {
		if !address.Addr().Is4() {
			continue
		}
		if address.Bits() > 30 {
			subnet, _ := address.Addr().Prefix(24)
			return subnet
		}
		return address.Masked()
	}
	return netip.Prefix{}
}

func mostCommonDNS(known []wgquick.DashboardPeer) string {
	counts := map[string]int{}
	best := ""
	for _, peer := range known {
		if peer.DNS == "" {
			continue
		}
		counts[peer.DNS]++
		if counts[peer.DNS] > counts[best] {
			best = peer.DNS
		}
	}
	return best
}

// buildWireGuardImport turns an existing interface and what WGDashboard knows
// about its peers into the inbound that replaces it, keys and addresses unchanged.
func buildWireGuardImport(cfg wgquick.ForeignConfig, known []wgquick.DashboardPeer, emailTaken func(string) bool) (*model.Inbound, WireGuardImportResult) {
	b := &wireGuardImportBuilder{iface: cfg.Interface, taken: emailTaken, used: map[string]bool{}}
	b.result.Interface = cfg.Interface
	b.result.Dropped = cfg.Dropped

	knownByKey := make(map[string]wgquick.DashboardPeer, len(known))
	for _, peer := range known {
		if _, dup := knownByKey[peer.PublicKey]; !dup || !peer.Restricted {
			knownByKey[peer.PublicKey] = peer
		}
	}
	onInterface := make(map[string]bool, len(cfg.Peers))
	for _, peer := range cfg.Peers {
		onInterface[peer.PublicKey] = true
		extra := knownByKey[peer.PublicKey]
		name := extra.Name
		if name == "" {
			name = peer.Name
		}
		allowed := make([]string, 0, len(peer.AllowedIPs))
		for _, prefix := range peer.AllowedIPs {
			allowed = append(allowed, prefix.String())
		}
		b.add(name, peer.PublicKey, peer.PresharedKey, allowed, extra, true)
	}
	for _, peer := range known {
		if peer.Restricted && !onInterface[peer.PublicKey] {
			b.add(peer.Name, peer.PublicKey, peer.PresharedKey, peer.AllowedIPs, peer, false)
		}
	}

	subnet := importedSubnet(cfg.Addresses)
	settings := map[string]any{
		"secretKey":  cfg.PrivateKey,
		"interface":  cfg.Interface,
		"subnetIp":   subnet.Addr().String(),
		"subnetCidr": subnet.Bits(),
		"clients":    b.clients,
	}
	// An inbound's server takes the first address of its subnet unless told otherwise.
	if len(cfg.Addresses) != 1 || cfg.Addresses[0] != netip.PrefixFrom(subnet.Addr().Next(), subnet.Bits()) {
		named := make([]string, 0, len(cfg.Addresses))
		for _, address := range cfg.Addresses {
			named = append(named, address.String())
		}
		settings["address"] = strings.Join(named, ", ")
	}
	if cfg.MTU > 0 {
		settings["mtu"] = cfg.MTU
	}
	if dns := mostCommonDNS(known); dns != "" {
		settings["dns"] = dns
	}
	for key, commands := range map[string][]string{
		"preUp": cfg.Hooks.PreUp, "postUp": cfg.Hooks.PostUp, "preDown": cfg.Hooks.PreDown, "postDown": cfg.Hooks.PostDown,
	} {
		if len(commands) > 0 {
			settings[key] = strings.Join(commands, "\n")
		}
	}
	raw, _ := json.MarshalIndent(settings, "", "  ")
	return &model.Inbound{
		Enable:      true,
		Remark:      cfg.Interface,
		Port:        cfg.ListenPort,
		Protocol:    model.WireGuard,
		Settings:    string(raw),
		ClientStats: b.stats,
	}, b.result
}

// takenClientEmails reports, without regard to case, whether an email already
// names an account or a traffic row of this panel.
func takenClientEmails() (func(email string) bool, error) {
	db := database.GetDB()
	var accounts, rows []string
	if err := db.Model(&model.ClientRecord{}).Pluck("email", &accounts).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&xray.ClientTraffic{}).Pluck("email", &rows).Error; err != nil {
		return nil, err
	}
	taken := make(map[string]bool, len(accounts)+len(rows))
	for _, email := range append(accounts, rows...) {
		taken[strings.ToLower(email)] = true
	}
	return func(email string) bool { return taken[strings.ToLower(email)] }, nil
}

func (s *InboundService) importWireGuard(host wireGuardHost, userID int, name string, known []wgquick.DashboardPeer) (WireGuardImportResult, error) {
	cfg, err := host.ReadForeign(name)
	if err != nil {
		return WireGuardImportResult{Interface: name}, err
	}
	emailTaken, err := takenClientEmails()
	if err != nil {
		return WireGuardImportResult{Interface: name}, err
	}
	inbound, result := buildWireGuardImport(cfg, known, emailTaken)
	inbound.UserId = userID
	// The inbound runs under the interface's own name, so the old config has to make way first.
	restore, err := host.Release(name)
	if err != nil {
		return result, err
	}
	stored, _, err := s.AddInbound(inbound)
	if err != nil {
		restore()
		return result, err
	}
	result.InboundId = stored.Id
	return result, nil
}

// ImportWireGuard makes an inbound out of a WireGuard interface that already
// runs on this server; its peers keep their keys and addresses. dashboardDB
// names WGDashboard's database, or is empty to look for it.
func (s *InboundService) ImportWireGuard(userID int, name, dashboardDB string) (WireGuardImportResult, error) {
	if dashboardDB == "" {
		dashboardDB = wgquick.FindDashboardDB()
	}
	var known []wgquick.DashboardPeer
	if dashboardDB != "" {
		peers, err := wgquick.ReadDashboardPeers(dashboardDB, name)
		if err != nil {
			return WireGuardImportResult{Interface: name}, err
		}
		known = peers
	}
	return s.importWireGuard(wgquick.GetManager(), userID, name, known)
}

// WireGuardImportSources lists the interfaces of this server that are not
// inbounds yet, and where WGDashboard's data about them would come from.
func (s *InboundService) WireGuardImportSources(dashboardDB string) WireGuardImportSources {
	manager := wgquick.GetManager()
	if dashboardDB == "" {
		dashboardDB = wgquick.FindDashboardDB()
	}
	sources := WireGuardImportSources{
		ToolsInstalled:   manager.Available(),
		DashboardDB:      dashboardDB,
		DashboardRunning: dashboardDB != "" && wgquick.DashboardRunning(),
		Interfaces:       []WireGuardImportable{},
	}
	for _, foreign := range manager.Foreign() {
		entry := WireGuardImportable{
			Name:    foreign.Config.Interface,
			Port:    foreign.Config.ListenPort,
			Peers:   len(foreign.Config.Peers),
			Up:      foreign.Up,
			Problem: foreign.Problem,
		}
		if len(foreign.Config.Addresses) > 0 {
			entry.Address = foreign.Config.Addresses[0].String()
		}
		if dashboardDB != "" && foreign.Problem == "" {
			if peers, err := wgquick.ReadDashboardPeers(dashboardDB, entry.Name); err == nil {
				for _, peer := range peers {
					if peer.Restricted {
						entry.Restricted++
					}
				}
			}
		}
		sources.Interfaces = append(sources.Interfaces, entry)
	}
	return sources
}
