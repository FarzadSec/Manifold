package egress

import (
	"net/netip"
	"slices"
	"strings"
	"testing"
)

func TestApplySteersSendsATunnelsTrafficToItsProxyPort(t *testing.T) {
	f := &fakeSystem{commands: map[string]bool{"nft": true, "ip": true}}
	useFake(t, f)
	steers := []Steer{
		{Device: "mfwg3", Port: 62301},
		{Subnet: netip.MustParsePrefix("10.21.0.0/22"), Port: 62302},
	}
	if err := ApplySteers(steers); err != nil {
		t.Fatal(err)
	}
	ruleset := f.nftInput()
	for _, want := range []string{
		"iifname \"mfwg3\" meta l4proto tcp",
		"iifname \"mfwg3\" meta l4proto udp",
		"ip saddr 10.21.0.0/22 meta l4proto tcp",
		"tproxy ip to 127.0.0.1:62301 meta mark set 0x4d520000 accept",
		"tproxy ip to 127.0.0.1:62302 meta mark set 0x4d520000 accept",
	} {
		if !strings.Contains(ruleset, want) {
			t.Fatalf("ruleset lacks %q:\n%s", want, ruleset)
		}
	}
	nftCheck(t, ruleset)
	argvs := f.argvs()
	for _, want := range []string{
		"ip -4 rule add pref 9900 fwmark 0x4d520000 lookup 19900",
		"ip -4 route replace local 0.0.0.0/0 dev lo table 19900",
	} {
		if !slices.Contains(argvs, want) {
			t.Fatalf("commands %q lack %q", argvs, want)
		}
	}

	f.calls = nil
	if err := ApplySteers(nil); err != nil {
		t.Fatal(err)
	}
	if removal := f.nftInput(); !strings.Contains(removal, "delete table inet manifold_steer") || strings.Contains(removal, "tproxy") {
		t.Fatalf("no steer left, yet the table stays:\n%s", removal)
	}
	if !slices.Contains(f.argvs(), "ip -4 route flush table 19900") {
		t.Fatalf("the local route outlives the steers: %q", f.argvs())
	}
}

func (f *fakeSystem) nftInput() string {
	for _, c := range f.calls {
		if strings.HasPrefix(c.argv, "nft ") {
			return c.stdin
		}
	}
	return ""
}

func TestApplySteersRefusesWhatWouldEnterTheRuleset(t *testing.T) {
	useFake(t, &fakeSystem{commands: map[string]bool{"nft": true, "ip": true}})
	for _, s := range []Steer{
		{Device: "bad name\"", Port: 62301},
		{Port: 62301},
		{Device: "mfwg3", Port: 0},
		{Subnet: netip.MustParsePrefix("fd00::/64"), Port: 62301},
	} {
		if err := ApplySteers([]Steer{s}); err == nil {
			t.Errorf("steer %+v was taken", s)
		}
	}
}
