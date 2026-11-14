package wgquick

import (
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	serverKey = "OEyW2GYBFVHTgB0a4gYZ4pXW0vlwtWRmKEB5H3xPz1Y="
	aliceKey  = "j+YN0164vTj8VigZXhwG7W55DcuT//8qERucl32kzVM="
	bobKey    = "qraEGD5B2ve022rwYQM+vw1sQgGeGJEDbzystC+MbDw="
	carolKey  = "UNIsSne3GWwPunxmglRqjTPNZo6Zw0BQIIRw+p0a8HM="
	sharedKey = "7m2nJ0yY2b0m8a1kq3n3Zb9mT5W6c7d8e9f0a1b2c3Q="
)

type command struct {
	stdin string
	argv  string
}

// fakeSystem is a host with files, links and the WireGuard tools.
type fakeSystem struct {
	files    map[string]string
	links    map[string]bool
	missing  map[string]bool
	dumps    map[string]string
	commands []command
}

func newFakeSystem() *fakeSystem {
	return &fakeSystem{files: map[string]string{}, links: map[string]bool{}, missing: map[string]bool{}, dumps: map[string]string{}}
}

func (f *fakeSystem) run(stdin, name string, args ...string) (string, error) {
	argv := strings.Join(append([]string{name}, args...), " ")
	f.commands = append(f.commands, command{stdin: stdin, argv: argv})
	switch {
	case name == "wg-quick" && args[0] == "up":
		f.links[strings.TrimSuffix(filepath.Base(args[1]), ".conf")] = true
	case name == "wg-quick" && args[0] == "down":
		delete(f.links, strings.TrimSuffix(filepath.Base(args[1]), ".conf"))
	case name == "wg" && args[0] == "show":
		return f.dumps[args[1]], nil
	}
	return "", nil
}

func (f *fakeSystem) has(name string) bool        { return !f.missing[name] }
func (f *fakeSystem) linkExists(name string) bool { return f.links[name] }
func (f *fakeSystem) removeFile(path string)      { delete(f.files, path) }
func (f *fakeSystem) writeFile(path, content string) error {
	f.files[path] = content
	return nil
}

func (f *fakeSystem) rename(from, to string) error {
	content, ok := f.files[from]
	if !ok {
		return fmt.Errorf("rename %s: no such file", from)
	}
	delete(f.files, from)
	f.files[to] = content
	return nil
}

func (f *fakeSystem) readFile(path string) (string, bool) {
	content, ok := f.files[path]
	return content, ok
}

func (f *fakeSystem) configs(dir string) []string {
	var out []string
	for path := range f.files {
		if filepath.Dir(path) == dir && strings.HasSuffix(path, ".conf") {
			out = append(out, path)
		}
	}
	slices.Sort(out)
	return out
}

// taken returns the commands run since the last call.
func (f *fakeSystem) taken() []string {
	out := make([]string, 0, len(f.commands))
	for _, c := range f.commands {
		out = append(out, c.argv)
	}
	f.commands = nil
	return out
}

type fakeEgress struct{ applied map[string]string }

func (e *fakeEgress) apply(owner, device string) error {
	e.applied[owner] = device
	return nil
}

func (e *fakeEgress) remove(owner string) { delete(e.applied, owner) }

func testManager() (*Manager, *fakeSystem, *fakeEgress) {
	sys := newFakeSystem()
	eg := &fakeEgress{applied: map[string]string{}}
	m := newManager("/etc/wireguard", sys)
	m.applyEgress, m.removeEgress = eg.apply, eg.remove
	return m, sys, eg
}

func peer(name, key, address string) Peer {
	return Peer{Name: name, PublicKey: key, AllowedIPs: []netip.Prefix{netip.MustParsePrefix(address)}}
}

func testInstance(peers ...Peer) Instance {
	return Instance{
		Id:         3,
		Tag:        "inbound-51820",
		Interface:  "mfwg3",
		ListenPort: 51820,
		PrivateKey: serverKey,
		Addresses:  []netip.Prefix{netip.MustParsePrefix("10.0.0.1/24")},
		MTU:        1420,
		Peers:      peers,
	}
}

func equalCommands(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("commands:\n  got  %q\n  want %q", got, want)
	}
}

func TestEnsureBringsUpAnInterfaceFromItsConfig(t *testing.T) {
	m, sys, eg := testManager()
	bob := peer("bob", bobKey, "10.0.0.3/32")
	bob.PresharedKey = sharedKey

	if err := m.Ensure(testInstance(peer("alice", aliceKey, "10.0.0.2/32"), bob)); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	wantConfig := "# Managed by Manifold: inbound 3. Changes made here are overwritten.\n" +
		"[Interface]\n" +
		"PrivateKey = " + serverKey + "\n" +
		"Address = 10.0.0.1/24\n" +
		"ListenPort = 51820\n" +
		"MTU = 1420\n" +
		"Table = off\n" +
		"\n# alice\n[Peer]\nPublicKey = " + aliceKey + "\nAllowedIPs = 10.0.0.2/32\n" +
		"\n# bob\n[Peer]\nPublicKey = " + bobKey + "\nPresharedKey = " + sharedKey + "\nAllowedIPs = 10.0.0.3/32\n"
	if got := sys.files["/etc/wireguard/mfwg3.conf"]; got != wantConfig {
		t.Fatalf("config:\n%s\nwant:\n%s", got, wantConfig)
	}
	equalCommands(t, sys.taken(), []string{
		"wg-quick up /etc/wireguard/mfwg3.conf",
		"ip route replace 10.0.0.2/32 dev mfwg3",
		"ip route replace 10.0.0.3/32 dev mfwg3",
	})
	if eg.applied["wg3"] != "mfwg3" {
		t.Fatalf("egress = %v, want wg3 on mfwg3", eg.applied)
	}

	if err := m.Ensure(testInstance(peer("alice", aliceKey, "10.0.0.2/32"), bob)); err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if again := sys.taken(); len(again) != 0 {
		t.Fatalf("an unchanged inbound ran %q", again)
	}
}

// Restarting the interface would drop every connected peer for an account change.
func TestEnsureAppliesPeerChangesToTheLiveInterface(t *testing.T) {
	m, sys, _ := testManager()
	bob := peer("bob", bobKey, "10.0.0.3/32")
	if err := m.Ensure(testInstance(peer("alice", aliceKey, "10.0.0.2/32"), bob)); err != nil {
		t.Fatal(err)
	}
	sys.taken()

	// Bob gets a preshared key and another address: his old route must not stay behind.
	changedBob := peer("bob", bobKey, "10.0.0.9/32")
	changedBob.PresharedKey = sharedKey
	if err := m.Ensure(testInstance(changedBob, peer("carol", carolKey, "10.0.0.4/32"))); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	equalCommands(t, sys.taken(), []string{
		"wg show mfwg3 dump",
		"wg set mfwg3 peer " + aliceKey + " remove",
		"ip route del 10.0.0.2/32 dev mfwg3",
		"wg set mfwg3 peer " + bobKey + " remove",
		"ip route del 10.0.0.3/32 dev mfwg3",
		"wg set mfwg3 peer " + bobKey + " preshared-key /dev/stdin allowed-ips 10.0.0.9/32",
		"ip route replace 10.0.0.9/32 dev mfwg3",
		"wg set mfwg3 peer " + carolKey + " allowed-ips 10.0.0.4/32",
		"ip route replace 10.0.0.4/32 dev mfwg3",
	})
	config := sys.files["/etc/wireguard/mfwg3.conf"]
	if strings.Contains(config, aliceKey) || !strings.Contains(config, carolKey) || !strings.Contains(config, "PresharedKey = "+sharedKey) {
		t.Fatalf("the config was not rewritten for the new peers:\n%s", config)
	}
}

func TestEnsurePassesThePresharedKeyOnStdinOnly(t *testing.T) {
	m, sys, _ := testManager()
	if err := m.Ensure(testInstance()); err != nil {
		t.Fatal(err)
	}
	bob := peer("bob", bobKey, "10.0.0.3/32")
	bob.PresharedKey = sharedKey
	sys.commands = nil
	if err := m.Ensure(testInstance(bob)); err != nil {
		t.Fatal(err)
	}
	for _, c := range sys.commands {
		if strings.Contains(c.argv, sharedKey) {
			t.Fatalf("the preshared key is visible in the process list: %q", c.argv)
		}
		if strings.Contains(c.argv, "preshared-key") && c.stdin != sharedKey+"\n" {
			t.Fatalf("stdin = %q, want the preshared key", c.stdin)
		}
	}
}

func TestEnsureRestartsTheInterfaceOnlyForWhatNeedsIt(t *testing.T) {
	m, sys, _ := testManager()
	inst := testInstance(peer("alice", aliceKey, "10.0.0.2/32"))
	if err := m.Ensure(inst); err != nil {
		t.Fatal(err)
	}
	sys.taken()

	inst.ListenPort = 51821
	if err := m.Ensure(inst); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	equalCommands(t, sys.taken(), []string{
		"wg show mfwg3 dump",
		"wg-quick down /etc/wireguard/mfwg3.conf",
		"wg-quick up /etc/wireguard/mfwg3.conf",
		"ip route replace 10.0.0.2/32 dev mfwg3",
	})
	if !strings.Contains(sys.files["/etc/wireguard/mfwg3.conf"], "ListenPort = 51821\n") {
		t.Fatal("the interface was restarted from the old config")
	}
}

func TestConfigCarriesEveryAddressAndHook(t *testing.T) {
	m, sys, _ := testManager()
	inst := testInstance()
	inst.Addresses = append(inst.Addresses, netip.MustParsePrefix("fd42::1/64"))
	inst.Hooks = Hooks{
		PreUp:    []string{"sysctl -w net.ipv4.ip_forward=1"},
		PostUp:   []string{"iptables -A FORWARD -i %i -j ACCEPT", "ip rule add from 10.0.0.0/24 table 100"},
		PreDown:  []string{"ip rule del from 10.0.0.0/24 table 100"},
		PostDown: []string{"iptables -D FORWARD -i %i -j ACCEPT"},
	}
	if err := m.Ensure(inst); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	want := "# Managed by Manifold: inbound 3. Changes made here are overwritten.\n" +
		"[Interface]\n" +
		"PrivateKey = " + serverKey + "\n" +
		"Address = 10.0.0.1/24, fd42::1/64\n" +
		"ListenPort = 51820\n" +
		"MTU = 1420\n" +
		"Table = off\n" +
		"PreUp = sysctl -w net.ipv4.ip_forward=1\n" +
		"PostUp = iptables -A FORWARD -i %i -j ACCEPT\n" +
		"PostUp = ip rule add from 10.0.0.0/24 table 100\n" +
		"PreDown = ip rule del from 10.0.0.0/24 table 100\n" +
		"PostDown = iptables -D FORWARD -i %i -j ACCEPT\n"
	if got := sys.files["/etc/wireguard/mfwg3.conf"]; got != want {
		t.Fatalf("config:\n%s\nwant:\n%s", got, want)
	}
}

// A hook only runs when wg-quick brings the interface up or down.
func TestEnsureRestartsTheInterfaceForAChangedHook(t *testing.T) {
	m, sys, _ := testManager()
	inst := testInstance()
	if err := m.Ensure(inst); err != nil {
		t.Fatal(err)
	}
	sys.taken()

	inst.Hooks.PostUp = []string{"ip rule add from 10.0.0.0/24 table 100"}
	if err := m.Ensure(inst); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	equalCommands(t, sys.taken(), []string{
		"wg show mfwg3 dump",
		"wg-quick down /etc/wireguard/mfwg3.conf",
		"wg-quick up /etc/wireguard/mfwg3.conf",
	})
}

// An interface the admin or another tool wrote must never be taken over silently.
func TestEnsureRefusesAConfigItDidNotWrite(t *testing.T) {
	for name, existing := range map[string]string{
		"hand-written config":       "[Interface]\nPrivateKey = x\n",
		"config of another inbound": "# Managed by Manifold: inbound 9. Changes made here are overwritten.\n[Interface]\n",
	} {
		t.Run(name, func(t *testing.T) {
			m, sys, eg := testManager()
			sys.files["/etc/wireguard/mfwg3.conf"] = existing

			err := m.Ensure(testInstance(peer("alice", aliceKey, "10.0.0.2/32")))
			if !errors.Is(err, ErrForeignConfig) {
				t.Fatalf("Ensure error = %v, want ErrForeignConfig", err)
			}
			if sys.files["/etc/wireguard/mfwg3.conf"] != existing || len(sys.taken()) != 0 || len(eg.applied) != 0 {
				t.Fatal("the foreign interface was touched")
			}
		})
	}
}

func TestEnsureReportsMissingTools(t *testing.T) {
	m, sys, _ := testManager()
	sys.missing["wg-quick"] = true
	if err := m.Ensure(testInstance()); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("Ensure error = %v, want ErrNotInstalled", err)
	}
	if len(sys.files) != 0 {
		t.Fatal("a config was written on a host that cannot run it")
	}
}

func dump(rows ...string) string {
	return serverKey + "\tpublic\t51820\toff\n" + strings.Join(rows, "\n") + "\n"
}

func dumpRow(key string, handshake time.Time, rx, tx int64) string {
	stamp := int64(0)
	if !handshake.IsZero() {
		stamp = handshake.Unix()
	}
	return fmt.Sprintf("%s\t(none)\t203.0.113.9:51000\t10.0.0.2/32\t%d\t%d\t%d\toff", key, stamp, rx, tx)
}

func TestTrafficIsBilledOncePerByteAndPerPeer(t *testing.T) {
	m, sys, _ := testManager()
	now := time.Unix(1_800_000_000, 0)
	m.now = func() time.Time { return now }
	if err := m.Ensure(testInstance(peer("alice", aliceKey, "10.0.0.2/32"), peer("bob", bobKey, "10.0.0.3/32"))); err != nil {
		t.Fatal(err)
	}

	sys.dumps["mfwg3"] = dump(
		dumpRow(aliceKey, now.Add(-30*time.Second), 1000, 5000),
		dumpRow(bobKey, now.Add(-10*time.Minute), 10, 20),
		dumpRow(carolKey, now, 777, 777),
	)
	traffic, online := m.CollectTraffic()
	slices.SortFunc(traffic, func(a, b Traffic) int { return strings.Compare(a.Email, b.Email) })
	want := []Traffic{
		{Tag: "inbound-51820", Email: "alice", Up: 1000, Down: 5000},
		{Tag: "inbound-51820", Email: "bob", Up: 10, Down: 20},
	}
	if !slices.Equal(traffic, want) {
		t.Fatalf("first collection = %+v, want %+v", traffic, want)
	}
	// Bob's last handshake is older than a rekey interval, so he is not connected.
	if !slices.Equal(online, []string{"alice"}) {
		t.Fatalf("online = %v, want [alice]", online)
	}

	sys.dumps["mfwg3"] = dump(dumpRow(aliceKey, now, 1500, 5000), dumpRow(bobKey, time.Time{}, 10, 20))
	traffic, _ = m.CollectTraffic()
	if want := []Traffic{{Tag: "inbound-51820", Email: "alice", Up: 500, Down: 0}}; !slices.Equal(traffic, want) {
		t.Fatalf("second collection = %+v, want %+v", traffic, want)
	}

	// A peer removed and added again starts its kernel counters from zero.
	if err := m.Ensure(testInstance(peer("bob", bobKey, "10.0.0.3/32"))); err != nil {
		t.Fatal(err)
	}
	if err := m.Ensure(testInstance(peer("alice", aliceKey, "10.0.0.2/32"), peer("bob", bobKey, "10.0.0.3/32"))); err != nil {
		t.Fatal(err)
	}
	sys.dumps["mfwg3"] = dump(dumpRow(aliceKey, now, 40, 60), dumpRow(bobKey, time.Time{}, 10, 20))
	traffic, _ = m.CollectTraffic()
	if want := []Traffic{{Tag: "inbound-51820", Email: "alice", Up: 40, Down: 60}}; !slices.Equal(traffic, want) {
		t.Fatalf("collection after re-adding the peer = %+v, want %+v", traffic, want)
	}
}

// What a peer used since the last collection must not be lost when it is removed.
func TestRemovedPeerIsBilledForItsLastTraffic(t *testing.T) {
	m, sys, _ := testManager()
	if err := m.Ensure(testInstance(peer("alice", aliceKey, "10.0.0.2/32"))); err != nil {
		t.Fatal(err)
	}
	sys.dumps["mfwg3"] = dump(dumpRow(aliceKey, time.Time{}, 300, 900))
	if err := m.Ensure(testInstance()); err != nil {
		t.Fatal(err)
	}
	sys.dumps["mfwg3"] = dump()
	traffic, _ := m.CollectTraffic()
	if want := []Traffic{{Tag: "inbound-51820", Email: "alice", Up: 300, Down: 900}}; !slices.Equal(traffic, want) {
		t.Fatalf("traffic = %+v, want %+v", traffic, want)
	}
}

func TestReconcileStopsWhatIsNoLongerWanted(t *testing.T) {
	m, sys, eg := testManager()
	if err := m.Ensure(testInstance(peer("alice", aliceKey, "10.0.0.2/32"))); err != nil {
		t.Fatal(err)
	}
	// Left by a previous run of the panel, for an inbound deleted meanwhile.
	sys.files["/etc/wireguard/mfwg8.conf"] = "# Managed by Manifold: inbound 8. Changes made here are overwritten.\n[Interface]\n"
	sys.links["mfwg8"] = true
	eg.applied["wg8"] = "mfwg8"
	sys.files["/etc/wireguard/wg0.conf"] = "[Interface]\nPrivateKey = admin\n"
	sys.links["wg0"] = true
	sys.taken()

	m.Reconcile(nil)

	equalCommands(t, sys.taken(), []string{
		"wg show mfwg3 dump",
		"wg-quick down /etc/wireguard/mfwg3.conf",
		"wg-quick down /etc/wireguard/mfwg8.conf",
	})
	if configs := sys.configs("/etc/wireguard"); !slices.Equal(configs, []string{"/etc/wireguard/wg0.conf"}) {
		t.Fatalf("configs left = %v, want only the admin's wg0", configs)
	}
	if !sys.links["wg0"] || len(eg.applied) != 0 {
		t.Fatalf("links = %v, egress = %v: the admin's interface must stay, ours must go", sys.links, eg.applied)
	}
}

func TestEnsureReplacesItsOwnInterfaceLeftByACrash(t *testing.T) {
	m, sys, _ := testManager()
	sys.files["/etc/wireguard/mfwg3.conf"] = "# Managed by Manifold: inbound 3. Changes made here are overwritten.\n[Interface]\nListenPort = 1\n"
	sys.links["mfwg3"] = true

	if err := m.Ensure(testInstance(peer("alice", aliceKey, "10.0.0.2/32"))); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	equalCommands(t, sys.taken(), []string{
		"wg-quick down /etc/wireguard/mfwg3.conf",
		"wg-quick up /etc/wireguard/mfwg3.conf",
		"ip route replace 10.0.0.2/32 dev mfwg3",
	})
}
