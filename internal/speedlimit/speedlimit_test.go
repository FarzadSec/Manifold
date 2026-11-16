package speedlimit

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"
)

type fakeSystem struct {
	noNft   bool
	missing bool
	fail    func(stdin string) bool
	applied []string
	listed  int
}

func (f *fakeSystem) run(stdin, name string, args ...string) (string, error) {
	if len(args) > 0 && args[0] == "-t" {
		f.listed++
		if f.missing {
			return "Error: No such file or directory", errors.New("exit status 1")
		}
		return "", nil
	}
	if f.fail != nil && f.fail(stdin) {
		return "Error: conflicting intervals", errors.New("exit status 1")
	}
	f.applied = append(f.applied, stdin)
	return "", nil
}

func (f *fakeSystem) has(string) bool { return !f.noNft }

func newFake(f *fakeSystem) (*Limiter, *time.Time) {
	now := time.Unix(1_800_000_000, 0)
	return &Limiter{sys: f, now: func() time.Time { return now }}, &now
}

func pfx(s string) netip.Prefix { return netip.MustParsePrefix(s) }

func TestApplyInstallsLimitsAndMapsForEveryAccount(t *testing.T) {
	f := &fakeSystem{}
	l, _ := newFake(f)
	err := l.Apply([]Account{
		{ID: 12, Mbps: 20, Addrs: []netip.Prefix{pfx("10.8.0.5/32"), pfx("fd00::5/128")}},
		{ID: 7, Mbps: 1, Ifaces: []string{"ppp3"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.applied) != 1 {
		t.Fatalf("applied %d transactions, want 1", len(f.applied))
	}
	tx := f.applied[0]
	for _, want := range []string{
		"add table inet manifold_speed\ndelete table inet manifold_speed\n",
		"limit u12_up { rate over 2500000 bytes/second burst 1250000 bytes }",
		"limit u12_down { rate over 2500000 bytes/second burst 1250000 bytes }",
		// One megabit would allow a 62500-byte burst, under a single 64 KiB GSO packet.
		"limit u7_up { rate over 125000 bytes/second burst 131072 bytes }",
		`map mark_up { type mark : limit; elements = { 0x4e000007 : "u7_up", 0x4e00000c : "u12_up" }; }`,
		`map mark_down { type mark : limit; elements = { 0x4e000007 : "u7_down", 0x4e00000c : "u12_down" }; }`,
		`map addr4_down { type ipv4_addr : limit; flags interval; elements = { 10.8.0.5/32 : "u12_down" }; }`,
		`map addr6_up { type ipv6_addr : limit; flags interval; elements = { fd00::5/128 : "u12_up" }; }`,
		`map iface_down { type ifname : limit; elements = { "ppp3" : "u7_down" }; }`,
	} {
		if !strings.Contains(tx, want) {
			t.Errorf("transaction lacks %q:\n%s", want, tx)
		}
	}
}

func TestApplyIsSilentWhenNothingChanged(t *testing.T) {
	f := &fakeSystem{}
	l, _ := newFake(f)
	accounts := []Account{{ID: 3, Mbps: 10, Addrs: []netip.Prefix{pfx("10.66.66.2/32")}}}
	if err := l.Apply(accounts); err != nil {
		t.Fatal(err)
	}
	if err := l.Apply(accounts); err != nil {
		t.Fatal(err)
	}
	if len(f.applied) != 1 {
		t.Fatalf("applied %d transactions for an unchanged set, want 1", len(f.applied))
	}
}

// A session coming or going must not refill everyone's bucket by replacing the table.
func TestApplyChangesOnlyWhatMoved(t *testing.T) {
	f := &fakeSystem{}
	l, _ := newFake(f)
	if err := l.Apply([]Account{
		{ID: 3, Mbps: 10, Addrs: []netip.Prefix{pfx("10.8.0.2/32")}},
		{ID: 4, Mbps: 10},
	}); err != nil {
		t.Fatal(err)
	}
	if err := l.Apply([]Account{
		{ID: 3, Mbps: 10, Addrs: []netip.Prefix{pfx("10.8.0.9/32")}},
		{ID: 4, Mbps: 30},
	}); err != nil {
		t.Fatal(err)
	}
	got := f.applied[1]
	want := strings.Join([]string{
		"delete element inet manifold_speed mark_up { 0x4e000004 }",
		"delete element inet manifold_speed mark_down { 0x4e000004 }",
		"delete element inet manifold_speed addr4_up { 10.8.0.2/32 }",
		"delete element inet manifold_speed addr4_down { 10.8.0.2/32 }",
		"delete limit inet manifold_speed u4_up",
		"delete limit inet manifold_speed u4_down",
		"add limit inet manifold_speed u4_up { rate over 3750000 bytes/second burst 1875000 bytes }",
		"add limit inet manifold_speed u4_down { rate over 3750000 bytes/second burst 1875000 bytes }",
		`add element inet manifold_speed mark_up { 0x4e000004 : "u4_up" }`,
		`add element inet manifold_speed mark_down { 0x4e000004 : "u4_down" }`,
		`add element inet manifold_speed addr4_up { 10.8.0.9/32 : "u3_up" }`,
		`add element inet manifold_speed addr4_down { 10.8.0.9/32 : "u3_down" }`,
	}, "\n") + "\n"
	if got != want {
		t.Fatalf("delta:\n%s\nwant:\n%s", got, want)
	}
}

func TestApplyRemovesTheTableWhenNoAccountIsLimited(t *testing.T) {
	f := &fakeSystem{}
	l, _ := newFake(f)
	if err := l.Apply(nil); err != nil {
		t.Fatal(err)
	}
	if err := l.Apply([]Account{{ID: 1, Mbps: 0}}); err != nil {
		t.Fatal(err)
	}
	if len(f.applied) != 1 || f.applied[0] != "add table inet manifold_speed\ndelete table inet manifold_speed\n" {
		t.Fatalf("applied %q, want one removal of what an earlier run left behind", f.applied)
	}
}

func TestApplyReinstallsATableFlushedFromOutside(t *testing.T) {
	f := &fakeSystem{}
	l, now := newFake(f)
	accounts := []Account{{ID: 5, Mbps: 8}}
	if err := l.Apply(accounts); err != nil {
		t.Fatal(err)
	}
	f.missing = true
	*now = now.Add(30 * time.Second)
	if err := l.Apply(accounts); err != nil {
		t.Fatal(err)
	}
	if f.listed != 0 || len(f.applied) != 1 {
		t.Fatalf("checked %d times and applied %d before the check is due", f.listed, len(f.applied))
	}
	*now = now.Add(time.Minute)
	if err := l.Apply(accounts); err != nil {
		t.Fatal(err)
	}
	if len(f.applied) != 2 || !strings.HasPrefix(f.applied[1], "add table inet manifold_speed\n") {
		t.Fatalf("applied %q, want the whole table again", f.applied)
	}
}

func TestApplyFallsBackToTheWholeTableWhenADeltaIsRefused(t *testing.T) {
	f := &fakeSystem{}
	l, _ := newFake(f)
	if err := l.Apply([]Account{{ID: 5, Mbps: 8}}); err != nil {
		t.Fatal(err)
	}
	f.fail = func(stdin string) bool {
		return strings.HasPrefix(stdin, "add limit") || strings.HasPrefix(stdin, "delete")
	}
	if err := l.Apply([]Account{{ID: 5, Mbps: 8}, {ID: 6, Mbps: 9}}); err != nil {
		t.Fatal(err)
	}
	last := f.applied[len(f.applied)-1]
	if !strings.HasPrefix(last, "add table inet manifold_speed\n") || !strings.Contains(last, "limit u6_up") {
		t.Fatalf("last transaction %q, want the whole table with the new account", last)
	}
}

func TestDesiredKeepsAClaimedAddressWithTheLowerID(t *testing.T) {
	s := desired([]Account{
		{ID: 9, Mbps: 5, Addrs: []netip.Prefix{pfx("10.9.0.5/32")}, Ifaces: []string{"ppp1", `bad"name`}},
		{ID: 2, Mbps: 5, Addrs: []netip.Prefix{pfx("10.9.0.0/24")}, Ifaces: []string{"ppp1"}},
		{ID: 1 << 24, Mbps: 5},
	})
	if got := s.addr4; len(got) != 1 || got[pfx("10.9.0.0/24")] != 2 {
		t.Fatalf("addr4 = %v, want only the /24 of account 2", got)
	}
	if got := s.ifaces; len(got) != 1 || got["ppp1"] != 2 {
		t.Fatalf("ifaces = %v, want ppp1 for account 2 and no unquotable name", got)
	}
	if _, ok := s.rates[1<<24]; ok {
		t.Fatal("an id past 24 bits got a limit its mark cannot carry")
	}
}

func TestApplyReportsAHostWithoutNft(t *testing.T) {
	l, _ := newFake(&fakeSystem{noNft: true})
	if err := l.Apply([]Account{{ID: 1, Mbps: 1}}); !errors.Is(err, ErrNoNftables) {
		t.Fatalf("Apply = %v, want ErrNoNftables", err)
	}
	if err := l.Apply(nil); err != nil {
		t.Fatalf("Apply(nil) without nft = %v, want nothing to remove", err)
	}
}
