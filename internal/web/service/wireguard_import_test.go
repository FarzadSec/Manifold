package service

import (
	"encoding/json"
	"errors"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	wgutil "github.com/mhsanaei/3x-ui/v3/internal/util/wireguard"
	"github.com/mhsanaei/3x-ui/v3/internal/wgquick"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func wgKeypair(t *testing.T) (private, public string) {
	t.Helper()
	private, public, err := wgutil.GenerateWireguardKeypair()
	if err != nil {
		t.Fatal(err)
	}
	return private, public
}

func prefixes(entries ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(entries))
	for _, entry := range entries {
		out = append(out, netip.MustParsePrefix(entry))
	}
	return out
}

type importedSettings struct {
	SecretKey  string         `json:"secretKey"`
	Interface  string         `json:"interface"`
	Address    string         `json:"address"`
	SubnetIP   string         `json:"subnetIp"`
	SubnetCIDR int            `json:"subnetCidr"`
	MTU        int            `json:"mtu"`
	DNS        string         `json:"dns"`
	PostUp     string         `json:"postUp"`
	PreDown    string         `json:"preDown"`
	Clients    []model.Client `json:"clients"`
}

func settingsOf(t *testing.T, inbound *model.Inbound) importedSettings {
	t.Helper()
	var parsed importedSettings
	if err := json.Unmarshal([]byte(inbound.Settings), &parsed); err != nil {
		t.Fatalf("settings: %v\n%s", err, inbound.Settings)
	}
	return parsed
}

func noEmailTaken(string) bool { return false }

func TestWireGuardImportKeepsWhatTheServerAndItsPeersAre(t *testing.T) {
	alicePrivate, alicePublic := wgKeypair(t)
	_, bobPublic := wgKeypair(t)
	someoneElsesPrivate, _ := wgKeypair(t)
	_, carolPublic := wgKeypair(t)

	cfg := wgquick.ForeignConfig{
		Interface: "wg0", ListenPort: 51820, PrivateKey: wgTestServerKey, MTU: 1380,
		Addresses: prefixes("10.0.0.1/24"),
		Hooks: wgquick.Hooks{
			PostUp:  []string{"iptables -A FORWARD -i %i -j ACCEPT", "ip rule add from 10.0.0.0/24 table 100"},
			PreDown: []string{"ip rule del from 10.0.0.0/24 table 100"},
		},
		Peers: []wgquick.ForeignPeer{
			{Name: "name in the config", PublicKey: alicePublic, PresharedKey: wgTestCarolKey, AllowedIPs: prefixes("10.0.0.2/32", "fd42::2/128")},
			{PublicKey: bobPublic, AllowedIPs: prefixes("10.0.0.3/32")},
		},
		Dropped: []string{"FwMark"},
	}
	known := []wgquick.DashboardPeer{
		{PublicKey: alicePublic, PrivateKey: alicePrivate, Name: "Alice Smith", DNS: "1.1.1.1", Keepalive: 21, Up: 100, Down: 200},
		{PublicKey: bobPublic, PrivateKey: someoneElsesPrivate, DNS: "1.1.1.1"},
		{PublicKey: carolPublic, Name: "alice smith", DNS: "9.9.9.9", AllowedIPs: []string{"10.0.0.4/32"}, Up: 5, Restricted: true},
	}

	inbound, result := buildWireGuardImport(cfg, known, func(email string) bool { return email == "wg0-10.0.0.3" })

	if inbound.Protocol != model.WireGuard || inbound.Port != 51820 || !inbound.Enable || inbound.Remark != "wg0" {
		t.Fatalf("inbound = %+v", inbound)
	}
	got := settingsOf(t, inbound)
	if got.SecretKey != wgTestServerKey || got.Interface != "wg0" || got.MTU != 1380 || got.DNS != "1.1.1.1" ||
		got.SubnetIP != "10.0.0.0" || got.SubnetCIDR != 24 || got.Address != "" {
		t.Fatalf("server settings = %+v", got)
	}
	if got.PostUp != "iptables -A FORWARD -i %i -j ACCEPT\nip rule add from 10.0.0.0/24 table 100" || got.PreDown != "ip rule del from 10.0.0.0/24 table 100" {
		t.Fatalf("hooks = %q / %q", got.PostUp, got.PreDown)
	}
	if len(got.Clients) != 3 {
		t.Fatalf("clients = %+v", got.Clients)
	}
	alice, bob, carol := got.Clients[0], got.Clients[1], got.Clients[2]
	if alice.Email != "Alice-Smith" || alice.PrivateKey != alicePrivate || alice.PublicKey != alicePublic || alice.PreSharedKey != wgTestCarolKey ||
		!slices.Equal(alice.AllowedIPs, []string{"10.0.0.2/32", "fd42::2/128"}) || !alice.Enable || alice.KeepAliveSeconds() != 21 || alice.SubID == "" {
		t.Fatalf("alice = %+v", alice)
	}
	// A private key that is not this peer's would hand out a config that never connects.
	if bob.Email != "wg0-10.0.0.3-2" || bob.PrivateKey != "" || bob.PublicKey != bobPublic || !bob.Enable {
		t.Fatalf("bob = %+v", bob)
	}
	if carol.Email != "alice-smith-2" || carol.Enable || !slices.Equal(carol.AllowedIPs, []string{"10.0.0.4/32"}) || carol.SubID == alice.SubID {
		t.Fatalf("carol = %+v", carol)
	}
	wantStats := []xray.ClientTraffic{
		{Email: "Alice-Smith", Enable: true, Up: 100, Down: 200},
		{Email: "wg0-10.0.0.3-2", Enable: true},
		{Email: "alice-smith-2", Enable: false, Up: 5},
	}
	if !reflect.DeepEqual(inbound.ClientStats, wantStats) {
		t.Fatalf("client stats:\n got  %+v\n want %+v", inbound.ClientStats, wantStats)
	}
	wantResult := WireGuardImportResult{Interface: "wg0", Clients: 3, Disabled: 1, WithoutKey: 2, Dropped: []string{"FwMark"}}
	if !reflect.DeepEqual(result, wantResult) {
		t.Fatalf("result = %+v, want %+v", result, wantResult)
	}
}

func TestWireGuardImportKeepsAServerAddressThatIsNotTheFirstOfItsSubnet(t *testing.T) {
	tests := []struct {
		name      string
		addresses []netip.Prefix
		address   string
		subnetIP  string
		cidr      int
	}{
		{"last host", prefixes("10.9.0.254/24"), "10.9.0.254/24", "10.9.0.0", 24},
		{"with an IPv6 address", prefixes("fd42::1/64", "10.9.0.1/22"), "fd42::1/64, 10.9.0.1/22", "10.9.0.0", 22},
		{"a lone host address leaves room to allocate", prefixes("10.9.0.1/32"), "10.9.0.1/32", "10.9.0.0", 24},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inbound, _ := buildWireGuardImport(wgquick.ForeignConfig{
				Interface: "wg0", ListenPort: 51820, PrivateKey: wgTestServerKey, Addresses: tt.addresses,
			}, nil, noEmailTaken)
			got := settingsOf(t, inbound)
			if got.Address != tt.address || got.SubnetIP != tt.subnetIP || got.SubnetCIDR != tt.cidr {
				t.Fatalf("address %q subnet %s/%d, want %q %s/%d", got.Address, got.SubnetIP, got.SubnetCIDR, tt.address, tt.subnetIP, tt.cidr)
			}
			inst, ok := wgquick.InstanceFromInbound(&model.Inbound{Id: 1, Port: 51820, Protocol: model.WireGuard, Settings: inbound.Settings})
			if !ok || !slices.Equal(inst.Addresses, tt.addresses) {
				t.Fatalf("the inbound would run with %v, the server had %v", inst.Addresses, tt.addresses)
			}
		})
	}
}

func TestWireGuardImportLeavesOutPeersThatCannotBeClients(t *testing.T) {
	_, routedNowhere := wgKeypair(t)
	_, fine := wgKeypair(t)
	inbound, result := buildWireGuardImport(wgquick.ForeignConfig{
		Interface: "wg0", ListenPort: 51820, PrivateKey: wgTestServerKey, Addresses: prefixes("10.0.0.1/24"),
		Peers: []wgquick.ForeignPeer{
			{Name: "nowhere", PublicKey: routedNowhere},
			{Name: "tab\tand / slash", PublicKey: fine, AllowedIPs: prefixes("10.0.0.2/32")},
		},
	}, []wgquick.DashboardPeer{
		{PublicKey: "not a key", Name: "broken", AllowedIPs: []string{"10.0.0.7/32"}, Restricted: true},
		{PublicKey: wgTestBobKey, Name: "no address", Restricted: true},
	}, noEmailTaken)

	clients := settingsOf(t, inbound).Clients
	if len(clients) != 1 || clients[0].Email != "tab-and-slash" {
		t.Fatalf("clients = %+v, want only tab-and-slash", clients)
	}
	wantSkipped := []string{
		"nowhere: has no address in the tunnel",
		"broken: has no usable public key",
		"no address: has no address in the tunnel",
	}
	if !slices.Equal(result.Skipped, wantSkipped) || result.Clients != 1 {
		t.Fatalf("result = %+v, want skipped %q", result, wantSkipped)
	}
}

type fakeWireGuardHost struct {
	config   wgquick.ForeignConfig
	released []string
	restored int
}

func (h *fakeWireGuardHost) ReadForeign(name string) (wgquick.ForeignConfig, error) {
	if name != h.config.Interface {
		return wgquick.ForeignConfig{}, errors.New("wireguard: there is no config for " + name + " in /etc/wireguard")
	}
	return h.config, nil
}

func (h *fakeWireGuardHost) Release(name string) (func(), error) {
	h.released = append(h.released, name)
	return func() { h.restored++ }, nil
}

func importableHost(t *testing.T) (*fakeWireGuardHost, string) {
	t.Helper()
	_, alicePublic := wgKeypair(t)
	return &fakeWireGuardHost{config: wgquick.ForeignConfig{
		Interface: "wg0", ListenPort: 48030, PrivateKey: wgTestServerKey, Addresses: prefixes("10.0.0.1/24"),
		Peers: []wgquick.ForeignPeer{{Name: "alice", PublicKey: alicePublic, AllowedIPs: prefixes("10.0.0.2/32")}},
	}}, alicePublic
}

func TestImportWireGuardStoresTheInboundItsClientsAndTheirUsage(t *testing.T) {
	setupConflictDB(t)
	svc := &InboundService{}
	host, alicePublic := importableHost(t)
	// The panel already has an "alice": the imported peer must not be merged into that account.
	seedInboundConflict(t, "vless-1", "", 48031, model.VLESS, "", `{"clients":[{"id":"1b0d8d0e-0000-4000-8000-000000000001","email":"alice","enable":true}]}`)
	if err := database.GetDB().Create(&model.ClientRecord{Email: "alice", SubID: "existing-sub", Enable: true}).Error; err != nil {
		t.Fatal(err)
	}

	result, err := svc.importWireGuard(host, 1, "wg0", []wgquick.DashboardPeer{{PublicKey: alicePublic, Up: 1 << 20, Down: 3 << 20}})
	if err != nil {
		t.Fatalf("importWireGuard: %v", err)
	}
	if !slices.Equal(host.released, []string{"wg0"}) || host.restored != 0 {
		t.Fatalf("released %v, restored %d times", host.released, host.restored)
	}
	stored, err := svc.GetInbound(result.InboundId)
	if err != nil {
		t.Fatalf("the inbound was not stored: %v", err)
	}
	clients := settingsOf(t, stored).Clients
	if stored.Port != 48030 || len(clients) != 1 || clients[0].Email != "alice-2" || clients[0].PublicKey != alicePublic {
		t.Fatalf("stored inbound port %d clients %+v", stored.Port, clients)
	}
	var traffic xray.ClientTraffic
	if err := database.GetDB().Where("email = ?", "alice-2").First(&traffic).Error; err != nil {
		t.Fatalf("no traffic row for the imported client: %v", err)
	}
	if traffic.Up != 1<<20 || traffic.Down != 3<<20 || traffic.InboundId != stored.Id || !traffic.Enable {
		t.Fatalf("traffic = %+v", traffic)
	}
}

func TestImportWireGuardPutsTheInterfaceBackWhenTheInboundCannotBeStored(t *testing.T) {
	setupConflictDB(t)
	svc := &InboundService{}
	host, _ := importableHost(t)
	seedInboundConflict(t, "wg-holds-the-port", "", 48030, model.WireGuard, "", wgTestSettings(""))

	_, err := svc.importWireGuard(host, 1, "wg0", nil)
	if err == nil || !strings.Contains(err.Error(), "48030") {
		t.Fatalf("error = %v, want the port conflict", err)
	}
	if host.restored != 1 {
		t.Fatalf("the interface was restored %d times, want once", host.restored)
	}
}

func TestImportWireGuardDoesNotTouchAnInterfaceItCannotRead(t *testing.T) {
	setupConflictDB(t)
	host, _ := importableHost(t)
	_, err := (&InboundService{}).importWireGuard(host, 1, "wg7", nil)
	if err == nil || !strings.Contains(err.Error(), "no config for wg7") || len(host.released) != 0 {
		t.Fatalf("error = %v, released = %v", err, host.released)
	}
}

// A server taken over with its address in the middle of the subnet must not see
// that address handed to the next client.
func TestNewWireGuardClientsNeverGetTheServerAddress(t *testing.T) {
	settings := `{"subnetIp":"10.7.0.0","subnetCidr":24,"address":"10.7.0.2/24, fd42::1/64"}`
	clients := []model.Client{{Email: "new"}}
	if err := defaultWireguardClients(settings, nil, clients, nil, nil); err != nil {
		t.Fatalf("defaultWireguardClients: %v", err)
	}
	if got := clients[0].AllowedIPs; !slices.Equal(got, []string{"10.7.0.3/32"}) {
		t.Fatalf("allocated %v, want 10.7.0.3/32: 10.7.0.2 is the server", got)
	}
}
