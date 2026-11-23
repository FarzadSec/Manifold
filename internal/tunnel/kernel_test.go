package tunnel

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

type fakeHost struct {
	files map[string]string
	runs  []string
	fail  string
	// outputs are what a command line prints.
	outputs map[string]string
}

func (f *fakeHost) run(name string, args ...string) (string, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	f.runs = append(f.runs, line)
	if f.fail != "" && strings.Contains(line, f.fail) {
		return "Error: refused", errFake
	}
	return f.outputs[line], nil
}

// changes are the commands that alter the host, without the lookups.
func (f *fakeHost) changes() []string {
	var out []string
	for _, line := range f.runs {
		if !strings.HasPrefix(line, "ip route get ") {
			out = append(out, line)
		}
	}
	return out
}

func (f *fakeHost) read(path string) (string, error) {
	v, ok := f.files[path]
	if !ok {
		return "", errFake
	}
	return v, nil
}

func (f *fakeHost) write(path, content string) error {
	f.files[path] = strings.TrimSpace(content)
	return nil
}

type fakeError struct{}

func (fakeError) Error() string { return "fake failure" }

var errFake = fakeError{}

func useHost(t *testing.T, f *fakeHost) {
	t.Helper()
	prev := host
	host = f
	t.Cleanup(func() { host = prev })
}

func normalized(t *testing.T, tun Tunnel) Tunnel {
	t.Helper()
	out, err := Normalize(tun)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestLinkCommandsBuildTheRequestedLink(t *testing.T) {
	tun := greClient()
	tun.ID, tun.Settings.LocalAddr, tun.Settings.Key, tun.Settings.MTU = 4, "203.0.113.10", 42, 1400
	tun = normalized(t, tun)
	var got []string
	for _, args := range linkCommands(tun) {
		got = append(got, strings.Join(args, " "))
	}
	want := []string{
		"link add mft4 type gre remote 198.51.100.7 local 203.0.113.10 ttl 255 key 42",
		"link set dev mft4 mtu 1400",
		"addr add 10.80.0.2/30 dev mft4",
		"link set dev mft4 alias " + aliasPrefix + linkFingerprint(tun),
		"link set dev mft4 up",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("plan:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A restarted panel must leave a link it made with the same settings alone,
// or every restart would cut the users crossing it.
func TestEnsureLinkKeepsAMatchingLinkAndRemakesAChangedOne(t *testing.T) {
	tun := greClient()
	tun.ID = 4
	tun = normalized(t, tun)
	made := tun
	made.Settings.MTU = 1476
	f := &fakeHost{files: map[string]string{
		sysNet("mft4", "ifalias"): aliasPrefix + linkFingerprint(made),
		sysNet("mft4", "flags"):   "0x1091",
	}}
	useHost(t, f)
	if err := ensureLink(tun); err != nil || len(f.changes()) != 0 {
		t.Fatalf("a matching link was touched: err %v, ran %v", err, f.runs)
	}

	tun.Settings.RemoteAddr = "198.51.100.8"
	if err := ensureLink(tun); err != nil {
		t.Fatal(err)
	}
	if ch := f.changes(); len(ch) != 6 || ch[0] != "ip link del dev mft4" || !strings.Contains(ch[1], "remote 198.51.100.8") {
		t.Fatalf("changed link was not remade: %v", f.runs)
	}

	f.runs, f.fail = nil, "addr add"
	err := ensureLink(Tunnel{ID: 4, Kind: KindGRE, Settings: TunnelSettings{RemoteAddr: "198.51.100.9", LocalIP: "10.80.0.2/30", TTL: 9}})
	if err == nil || f.runs[len(f.runs)-1] != "ip link del dev mft4" {
		t.Fatalf("a half-made link was left behind: err %v, ran %v", err, f.runs)
	}
}

// Without an MTU of its own a link takes what its uplink carries less the outer headers:
// a GRE key adds 4 bytes, and a 1450-byte cloud interface leaves less than 1500 would.
func TestLinkMTUFitsTheUplinkAndTheHeaders(t *testing.T) {
	route := "ip route get 198.51.100.7 from 203.0.113.10"
	for _, tc := range []struct {
		name, kind string
		key        uint32
		uplink     string
		want       int
	}{
		{"gre on a 1500-byte uplink", KindGRE, 0, "1500", 1476},
		{"gre with a key", KindGRE, 42, "1500", 1472},
		{"gretap with a key", KindGRETAP, 42, "1500", 1458},
		{"ipip on a 1450-byte uplink", KindIPIP, 0, "1450", 1430},
		{"uplink that cannot be read", KindGRE, 0, "", 1476},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tun := greClient()
			tun.ID, tun.Kind, tun.Settings.LocalAddr, tun.Settings.Key = 4, tc.kind, "203.0.113.10", tc.key
			tun = normalized(t, tun)
			f := &fakeHost{
				files:   map[string]string{sysNet("ens3", "mtu"): tc.uplink},
				outputs: map[string]string{route: "198.51.100.7 via 203.0.113.1 dev ens3 src 203.0.113.10 uid 0\n    cache"},
			}
			useHost(t, f)
			if err := ensureLink(tun); err != nil {
				t.Fatal(err)
			}
			want := "ip link set dev mft4 mtu " + strconv.Itoa(tc.want)
			if !slices.Contains(f.runs, want) {
				t.Fatalf("ran %q, want %q", f.runs, want)
			}
		})
	}
}

func TestIP6GRELinkLeavesRoomForItsEncapsulationLimit(t *testing.T) {
	tun := greClient()
	tun.ID, tun.Kind, tun.Settings.RemoteAddr = 4, KindIP6GRE, "2001:db8::7"
	tun = normalized(t, tun)
	f := &fakeHost{
		files:   map[string]string{sysNet("ens3", "mtu"): "1500"},
		outputs: map[string]string{"ip route get 2001:db8::7": "2001:db8::7 from :: via fe80::1 dev ens3 proto ra src 2001:db8::10 metric 100 pref medium"},
	}
	useHost(t, f)
	if err := ensureLink(tun); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(f.runs, "ip link set dev mft4 mtu 1448") {
		t.Fatalf("ran %q, want an MTU of 1448", f.runs)
	}
}

func TestOnlyThePanelsOwnLinksAreSwept(t *testing.T) {
	f := &fakeHost{files: map[string]string{
		sysNet("mft3", "ifalias"): aliasPrefix + "abc",
		sysNet("mft9", "ifalias"): "made by hand",
	}}
	prev := host
	host = linkLister{f, "1: lo: <LOOPBACK>\n7: mft3@NONE: <POINTOPOINT>\n8: mft9@NONE: <POINTOPOINT>\n9: gre0@NONE: <NOARP>"}
	t.Cleanup(func() { host = prev })
	if links := panelLinks(); len(links) != 1 || links[3] != "mft3" {
		t.Fatalf("panel links = %v, want only mft3", links)
	}
}

type linkLister struct {
	*fakeHost
	listing string
}

func (l linkLister) run(name string, args ...string) (string, error) {
	if strings.Join(args, " ") == "-o link show" {
		return l.listing, nil
	}
	return l.fakeHost.run(name, args...)
}

func TestPortForwardsSplitBothProtocols(t *testing.T) {
	tun := greClient()
	tun.Role = RoleServer
	tun = normalized(t, tun)
	got := portForwards(tun.Exposed())
	if len(got) != 3 || got[0].Proto != "tcp" || got[0].Low != 443 || got[0].TargetLow != 443 ||
		got[1].Proto != "tcp" || got[1].Low != 2201 || got[1].TargetLow != 22 || got[2].Proto != "udp" || got[2].TargetLow != 22 {
		t.Fatalf("rules = %+v", got)
	}
}
