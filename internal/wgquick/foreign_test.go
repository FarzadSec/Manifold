package wgquick

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

const dashboardConfig = `[Interface]
Address = 10.0.0.1/24, fd42::1/64
SaveConfig = true
PostUp = iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE; # nat for the clients
PostUp = ip rule add from 10.0.0.0/24 table 100
PreDown = ip rule del from 10.0.0.0/24 table 100
ListenPort = 51820
FwMark = 0x200
privatekey = ` + serverKey + `
MTU = 1380

#Name# = Alice's phone
[Peer]
PublicKey = ` + aliceKey + `
AllowedIPs = 10.0.0.2/32
AllowedIPs = fd42::2/128
Endpoint = 203.0.113.9:51000
PersistentKeepalive = 21

### Client bob
[peer]
PublicKey = ` + bobKey + ` # laptop
PresharedKey = ` + sharedKey + `
AllowedIPs = 10.0.0.3
`

func TestForeignConfigIsReadTheWayWgQuickReadsIt(t *testing.T) {
	cfg, err := ParseForeignConfig("wg0", dashboardConfig)
	if err != nil {
		t.Fatalf("ParseForeignConfig: %v", err)
	}
	if cfg.Interface != "wg0" || cfg.ListenPort != 51820 || cfg.PrivateKey != serverKey || cfg.MTU != 1380 {
		t.Fatalf("interface = %+v", cfg)
	}
	if got := joinPrefixes(cfg.Addresses); got != "10.0.0.1/24, fd42::1/64" {
		t.Fatalf("addresses = %s", got)
	}
	wantHooks := Hooks{
		PostUp:  []string{"iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE;", "ip rule add from 10.0.0.0/24 table 100"},
		PreDown: []string{"ip rule del from 10.0.0.0/24 table 100"},
	}
	if !reflect.DeepEqual(cfg.Hooks, wantHooks) {
		t.Fatalf("hooks = %+v, want %+v", cfg.Hooks, wantHooks)
	}
	if !slices.Equal(cfg.Dropped, []string{"FwMark"}) {
		t.Fatalf("dropped = %v, want FwMark", cfg.Dropped)
	}
	if len(cfg.Peers) != 2 {
		t.Fatalf("peers = %+v", cfg.Peers)
	}
	alice, bob := cfg.Peers[0], cfg.Peers[1]
	if alice.Name != "Alice's phone" || alice.PublicKey != aliceKey || alice.Keepalive != 21 ||
		joinPrefixes(alice.AllowedIPs) != "10.0.0.2/32, fd42::2/128" {
		t.Fatalf("alice = %+v", alice)
	}
	if bob.Name != "bob" || bob.PublicKey != bobKey || bob.PresharedKey != sharedKey || joinPrefixes(bob.AllowedIPs) != "10.0.0.3/32" {
		t.Fatalf("bob = %+v", bob)
	}
}

func TestPeerNamesAreReadFromTheCommentsOtherToolsWrite(t *testing.T) {
	tests := map[string]string{
		"#Name# = alice":                "alice",
		"# Name = alice":                "alice",
		"# name: alice":                 "alice",
		"### Client alice":              "alice",
		"# Client: alice (3f2a-77)":     "alice",
		"### begin alice ###":           "alice",
		"# just a remark":               "",
		"#PresharedKey = " + sharedKey:  "",
		"# Name =":                      "",
		"### Client two words are kept": "two words are kept",
	}
	for comment, want := range tests {
		t.Run(comment, func(t *testing.T) {
			cfg, err := ParseForeignConfig("wg0", "[Interface]\nPrivateKey = "+serverKey+"\nListenPort = 1\nAddress = 10.0.0.1/24\n"+
				comment+"\n[Peer]\nPublicKey = "+aliceKey+"\nAllowedIPs = 10.0.0.2/32\n")
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.Peers[0].Name; got != want {
				t.Fatalf("name = %q, want %q", got, want)
			}
		})
	}
}

func TestForeignConfigThatCannotBecomeAnInboundIsRefused(t *testing.T) {
	iface := "[Interface]\nPrivateKey = " + serverKey + "\nListenPort = 51820\nAddress = 10.0.0.1/24\n"
	tests := []struct {
		name, content, want string
	}{
		{"no private key", "[Interface]\nListenPort = 51820\nAddress = 10.0.0.1/24\n", "wireguard: wg0 has no usable private key"},
		{"no listen port", "[Interface]\nPrivateKey = " + serverKey + "\nAddress = 10.0.0.1/24\n", "wireguard: wg0 has no listen port, so it is not a server"},
		{"no address", "[Interface]\nPrivateKey = " + serverKey + "\nListenPort = 51820\n", "wireguard: wg0 has no IPv4 address"},
		{"only an IPv6 address", "[Interface]\nPrivateKey = " + serverKey + "\nListenPort = 51820\nAddress = fd42::1/64\n", "wireguard: the interface needs an IPv4 address"},
		{"peer without a key", iface + "[Peer]\nAllowedIPs = 10.0.0.2/32\n", "wireguard: wg0 has a peer without a usable public key"},
		{"peer with a broken preshared key", iface + "[Peer]\nPublicKey = " + aliceKey + "\nPresharedKey = short\nAllowedIPs = 10.0.0.2/32\n", "wireguard: wg0 has a peer with an unusable preshared key"},
		{"the same peer twice", iface + "[Peer]\nPublicKey = " + aliceKey + "\nAllowedIPs = 10.0.0.2/32\n[Peer]\nPublicKey = " + aliceKey + "\nAllowedIPs = 10.0.0.3/32\n", "wireguard: wg0 lists the peer " + aliceKey + " twice"},
		{"a managed config", "# Managed by Manifold: inbound 4. Changes made here are overwritten.\n" + iface, "wireguard: wg0 is already managed by this panel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseForeignConfig("wg0", tt.content)
			if got := errText(err); got != tt.want {
				t.Fatalf("error = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestForeignListsOnlyWhatThisPanelDidNotWrite(t *testing.T) {
	m, sys, _ := testManager()
	if err := m.Ensure(testInstance()); err != nil {
		t.Fatal(err)
	}
	sys.files["/etc/wireguard/wg0.conf"] = dashboardConfig
	sys.files["/etc/wireguard/broken.conf"] = "[Interface]\nListenPort = 4\n"
	sys.files["/etc/wireguard/wg0.conf.before-manifold"] = dashboardConfig
	sys.links["wg0"] = true

	got := m.Foreign()
	if len(got) != 2 {
		t.Fatalf("foreign = %+v, want broken and wg0", got)
	}
	if got[0].Config.Interface != "broken" || got[0].Problem != "wireguard: broken has no usable private key" || got[0].Up {
		t.Fatalf("broken = %+v", got[0])
	}
	if got[1].Config.Interface != "wg0" || got[1].Problem != "" || !got[1].Up || len(got[1].Config.Peers) != 2 {
		t.Fatalf("wg0 = %+v", got[1])
	}
}

func TestReleaseStopsAForeignInterfaceAndKeepsItsConfigAside(t *testing.T) {
	m, sys, _ := testManager()
	sys.files["/etc/wireguard/wg0.conf"] = dashboardConfig
	sys.links["wg0"] = true

	restore, err := m.Release("wg0")
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	equalCommands(t, sys.taken(), []string{"wg-quick down /etc/wireguard/wg0.conf"})
	if _, still := sys.files["/etc/wireguard/wg0.conf"]; still || sys.files["/etc/wireguard/wg0.conf.before-manifold"] != dashboardConfig {
		t.Fatalf("files after release = %v", sys.files)
	}

	restore()
	equalCommands(t, sys.taken(), []string{"wg-quick up /etc/wireguard/wg0.conf"})
	if sys.files["/etc/wireguard/wg0.conf"] != dashboardConfig {
		t.Fatal("the config was not put back")
	}
	if _, left := sys.files["/etc/wireguard/wg0.conf.before-manifold"]; left {
		t.Fatal("the copy outlived the restore")
	}
}

func TestReleaseLeavesAManagedOrMissingInterfaceAlone(t *testing.T) {
	m, sys, _ := testManager()
	if err := m.Ensure(testInstance()); err != nil {
		t.Fatal(err)
	}
	sys.taken()
	for name, want := range map[string]string{
		"mfwg3":   "wireguard: mfwg3 is already managed by this panel",
		"nothing": "wireguard: there is no config for nothing in /etc/wireguard",
		"a/b":     `wireguard: "a/b" cannot be an interface name: use 1 to 15 letters, digits or _ = + . -`,
	} {
		if _, err := m.Release(name); errText(err) != want {
			t.Fatalf("Release(%s) = %v, want %s", name, err, want)
		}
	}
	if len(sys.taken()) != 0 {
		t.Fatal("a command was run for an interface that was refused")
	}
}

// The table layout is WGDashboard 4.3.5's (WireguardConfiguration.py): one table per
// interface keyed by public key, usage in GiB, restricted peers parked in a second table.
func dashboardDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wgdashboard.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	columns := `(id VARCHAR(255) NOT NULL PRIMARY KEY, private_key VARCHAR(255), DNS TEXT, endpoint_allowed_ip TEXT,
		name TEXT, total_receive FLOAT, total_sent FLOAT, total_data FLOAT, endpoint VARCHAR(255), status VARCHAR(255),
		latest_handshake VARCHAR(255), allowed_ip VARCHAR(255), cumu_receive FLOAT, cumu_sent FLOAT, cumu_data FLOAT,
		mtu INTEGER, keepalive INTEGER, notes TEXT, remote_endpoint VARCHAR(255), preshared_key VARCHAR(255))`
	statements := []string{
		`CREATE TABLE "wg0" ` + columns,
		`CREATE TABLE "wg0_restrict_access" ` + columns,
		`INSERT INTO "wg0" (id, private_key, DNS, name, total_receive, total_sent, cumu_receive, cumu_sent, allowed_ip, mtu, keepalive, preshared_key)
			VALUES ('` + aliceKey + `', 'alice-private', '1.1.1.1', 'Alice', 0.5, 1.5, 1, 2, '10.0.0.2/32', 1380, 21, '')`,
		`INSERT INTO "wg0" (id, name) VALUES ('` + bobKey + `', NULL)`,
		`INSERT INTO "wg0_restrict_access" (id, private_key, name, total_receive, total_sent, cumu_receive, cumu_sent, allowed_ip, preshared_key)
			VALUES ('` + carolKey + `', '', 'Carol', 0, 0, 0.25, 0, '10.0.0.4/32, fd42::4/128', '` + sharedKey + `')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%v\n%s", err, statement)
		}
	}
	return path
}

func TestDashboardPeersCarryNamesKeysAndUsage(t *testing.T) {
	peers, err := ReadDashboardPeers(dashboardDB(t), "wg0")
	if err != nil {
		t.Fatalf("ReadDashboardPeers: %v", err)
	}
	const gib = 1 << 30
	want := []DashboardPeer{
		{PublicKey: aliceKey, PrivateKey: "alice-private", Name: "Alice", DNS: "1.1.1.1", AllowedIPs: []string{"10.0.0.2/32"}, Keepalive: 21, MTU: 1380, Up: 1.5 * gib, Down: 3.5 * gib},
		{PublicKey: bobKey},
		{PublicKey: carolKey, Name: "Carol", PresharedKey: sharedKey, AllowedIPs: []string{"10.0.0.4/32", "fd42::4/128"}, Up: 0.25 * gib, Restricted: true},
	}
	if !reflect.DeepEqual(peers, want) {
		t.Fatalf("peers:\n got  %+v\n want %+v", peers, want)
	}
}

func TestDashboardWithoutTheInterfaceHasNoPeers(t *testing.T) {
	peers, err := ReadDashboardPeers(dashboardDB(t), "wg9")
	if err != nil || len(peers) != 0 {
		t.Fatalf("peers = %v, err = %v; want none", peers, err)
	}
	if _, err := ReadDashboardPeers(filepath.Join(t.TempDir(), "absent.db"), "wg0"); err == nil || !strings.Contains(err.Error(), "absent.db") {
		t.Fatalf("error for a missing database = %v, want one naming the file", err)
	}
}

func TestDashboardDatabaseIsFoundThroughItsServiceUnit(t *testing.T) {
	unit := "[Unit]\nAfter=network-online.target\n\n[Service]\nType=forking\nWorkingDirectory = /opt/WGDashboard/src\nExecStart=/opt/WGDashboard/src/wgd.sh start\n"
	if got := dashboardDBOfUnit(unit); got != "/opt/WGDashboard/src/db/wgdashboard.db" {
		t.Fatalf("database = %q", got)
	}
	if got := dashboardDBOfUnit("[Service]\nExecStart=/bin/true\n"); got != "" {
		t.Fatalf("database of a unit without a working directory = %q", got)
	}
}
