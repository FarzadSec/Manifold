package wgquick

import (
	"encoding/json"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func wgInbound(t *testing.T, settings map[string]any) *model.Inbound {
	t.Helper()
	if _, ok := settings["secretKey"]; !ok {
		settings["secretKey"] = serverKey
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	return &model.Inbound{Id: 3, Tag: "inbound-51820", Port: 51820, Protocol: model.WireGuard, Settings: string(raw)}
}

func client(email, key string, enable bool, allowed ...string) map[string]any {
	return map[string]any{"email": email, "publicKey": key, "enable": enable, "allowedIPs": allowed}
}

// The server's address must sit in the subnet the panel allocates clients
// from, or the clients' own configs would point at a gateway that is not there.
func TestServerAddressFollowsTheClientSubnet(t *testing.T) {
	tests := []struct {
		name     string
		settings map[string]any
		want     string
	}{
		{name: "no subnet and no client", settings: map[string]any{}, want: "10.0.0.1/24"},
		{name: "subnet named by the inbound", settings: map[string]any{"subnetIp": "10.66.0.7", "subnetCidr": 22}, want: "10.66.0.1/22"},
		{
			name:     "subnet of the first client",
			settings: map[string]any{"clients": []any{client("alice", aliceKey, true, "0.0.0.0/0", "10.7.5.9/32")}},
			want:     "10.7.5.1/24",
		},
		{
			name:     "half-given subnet falls back to the clients",
			settings: map[string]any{"subnetIp": "10.66.0.0", "clients": []any{client("alice", aliceKey, true, "172.16.9.4")}},
			want:     "172.16.9.1/24",
		},
		{
			name:     "addresses named by the inbound win over its subnet",
			settings: map[string]any{"address": " 10.7.0.254/24 , fd42::1/64", "subnetIp": "10.7.0.0", "subnetCidr": 24},
			want:     "10.7.0.254/24, fd42::1/64",
		},
		{
			name:     "addresses that cannot be parsed are not half used",
			settings: map[string]any{"address": "10.7.0.254/24, nonsense", "subnetIp": "10.9.0.0", "subnetCidr": 24},
			want:     "10.9.0.1/24",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst, ok := InstanceFromInbound(wgInbound(t, tt.settings))
			if got := joinPrefixes(inst.Addresses); !ok || got != tt.want {
				t.Fatalf("addresses = %s (ok=%v), want %s", got, ok, tt.want)
			}
		})
	}
}

func TestInstanceHoldsOnlyPeersTheKernelCanTake(t *testing.T) {
	withPSK := client("psk", carolKey, true, "10.0.0.6")
	withPSK["preSharedKey"] = sharedKey
	badPSK := client("bad-psk", "8AEKL6YFJdNlPQ7yKqdtMz3ZkJ9oD0y0q0M0m0m0m0E=", true, "10.0.0.7/32")
	badPSK["preSharedKey"] = "not-a-key"
	inst, ok := InstanceFromInbound(wgInbound(t, map[string]any{"clients": []any{
		client("alice", aliceKey, true, "10.0.0.2/32", "fd00::2/128"),
		client("disabled", bobKey, false, "10.0.0.3/32"),
		client("no-key", "", true, "10.0.0.4/32"),
		client("only-default-route", bobKey, true, "0.0.0.0/0", "::/0"),
		client("duplicate-key", aliceKey, true, "10.0.0.5/32"),
		withPSK,
		badPSK,
	}}))
	if !ok {
		t.Fatal("InstanceFromInbound refused a usable inbound")
	}
	want := []Peer{
		{Name: "alice", PublicKey: aliceKey, AllowedIPs: []netip.Prefix{netip.MustParsePrefix("10.0.0.2/32"), netip.MustParsePrefix("fd00::2/128")}},
		{Name: "psk", PublicKey: carolKey, PresharedKey: sharedKey, AllowedIPs: []netip.Prefix{netip.MustParsePrefix("10.0.0.6/32")}},
	}
	if !slices.EqualFunc(inst.Peers, want, func(a, b Peer) bool { return a.Name == b.Name && a.fingerprint() == b.fingerprint() }) {
		t.Fatalf("peers = %+v, want %+v", inst.Peers, want)
	}
	if inst.Interface != "mfwg3" || inst.ListenPort != 51820 || inst.PrivateKey != serverKey {
		t.Fatalf("instance = %+v", inst)
	}
}

func TestInstanceIsRefusedWhenItCannotBeAnInterface(t *testing.T) {
	tests := map[string]*model.Inbound{
		"server key that is not a key":                 wgInbound(t, map[string]any{"secretKey": "c2hvcnQ="}),
		"interface name wg-quick would refuse":         wgInbound(t, map[string]any{"interface": "has space"}),
		"interface name longer than the kernel allows": wgInbound(t, map[string]any{"interface": "sixteen-chars-xx"}),
		"another protocol": func() *model.Inbound {
			ib := wgInbound(t, map[string]any{})
			ib.Protocol = model.VLESS
			return ib
		}(),
	}
	for name, ib := range tests {
		t.Run(name, func(t *testing.T) {
			if _, ok := InstanceFromInbound(ib); ok {
				t.Fatal("InstanceFromInbound accepted it")
			}
		})
	}
	named, ok := InstanceFromInbound(wgInbound(t, map[string]any{"interface": " DE "}))
	if !ok || named.Interface != "DE" {
		t.Fatalf("interface = %q (ok=%v), want the inbound's own name DE", named.Interface, ok)
	}
}

func TestInstanceCarriesTheHooksLineByLine(t *testing.T) {
	inst, ok := InstanceFromInbound(wgInbound(t, map[string]any{
		"postUp":  "iptables -A FORWARD -i %i -j ACCEPT\n\n  ip rule add from 10.0.0.0/24 table 100  \r\n",
		"preDown": "ip rule del from 10.0.0.0/24 table 100",
	}))
	if !ok {
		t.Fatal("InstanceFromInbound refused the inbound")
	}
	want := Hooks{
		PostUp:  []string{"iptables -A FORWARD -i %i -j ACCEPT", "ip rule add from 10.0.0.0/24 table 100"},
		PreDown: []string{"ip rule del from 10.0.0.0/24 table 100"},
	}
	if !slices.Equal(inst.Hooks.PostUp, want.PostUp) || !slices.Equal(inst.Hooks.PreDown, want.PreDown) ||
		len(inst.Hooks.PreUp) != 0 || len(inst.Hooks.PostDown) != 0 {
		t.Fatalf("hooks = %+v, want %+v", inst.Hooks, want)
	}
}

func TestSettingsAreCheckedBeforeTheyAreSaved(t *testing.T) {
	tests := []struct {
		name     string
		settings string
		want     string
	}{
		{name: "nothing named", settings: `{}`},
		{name: "two addresses", settings: `{"address": "10.7.0.254/24, fd42::1/64", "interface": "wg0"}`},
		{name: "interface name", settings: `{"interface": "wg main"}`, want: `wireguard: "wg main" cannot be an interface name: use 1 to 15 letters, digits or _ = + . -`},
		{name: "address that is no address", settings: `{"address": "10.7.0.300/24"}`, want: `wireguard: "10.7.0.300/24" is not an address like 10.0.0.1/24`},
		{name: "address without a prefix length", settings: `{"address": "10.7.0.1"}`, want: `wireguard: "10.7.0.1" is not an address like 10.0.0.1/24`},
		{name: "no IPv4 address", settings: `{"address": "fd42::1/64"}`, want: "wireguard: the interface needs an IPv4 address"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSettings(tt.settings)
			if got := errText(err); got != tt.want {
				t.Fatalf("ValidateSettings = %q, want %q", got, tt.want)
			}
		})
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// An inbound that names no MTU must still give its interface the size the configs it hands out
// carry (1280, as the official apps use); wg-quick's own pick follows only this host's uplink.
func TestInterfaceWithoutAnMTUFitsThePath(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings map[string]any
		want     string
	}{
		{"none named", map[string]any{}, "MTU = 1280\n"},
		{"named by the inbound", map[string]any{"mtu": 1380}, "MTU = 1380\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inst, ok := InstanceFromInbound(wgInbound(t, tc.settings))
			if !ok {
				t.Fatal("InstanceFromInbound refused the inbound")
			}
			if got := renderConfig(inst); !strings.Contains(got, "\n"+tc.want) {
				t.Fatalf("config lacks %q:\n%s", tc.want, got)
			}
		})
	}
}
