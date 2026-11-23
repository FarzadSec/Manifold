package egress

import (
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestApplyForwardsSendsThePortsThroughTheTunnel(t *testing.T) {
	f := &fakeSystem{commands: map[string]bool{"nft": true, "iptables": true}}
	useFake(t, f)
	rules := []PortForward{
		{Proto: "tcp", Low: 443, High: 443, TargetLow: 443},
		{Proto: "udp", Low: 2201, High: 2201, TargetLow: 22},
		{Proto: "tcp", Low: 10000, High: 10009, TargetLow: 10000},
	}
	if err := ApplyForwards("tf3", "mft3", netip.MustParseAddr("10.80.0.2"), rules, true); err != nil {
		t.Fatal(err)
	}
	want := "add table ip6 manifold_tf3\ndelete table ip6 manifold_tf3\n" +
		"add table ip manifold_tf3\ndelete table ip manifold_tf3\ntable ip manifold_tf3 {\n" +
		"\tchain prerouting {\n\t\ttype nat hook prerouting priority dstnat; policy accept;\n" +
		"\t\tiifname != \"mft3\" tcp dport 443 dnat to 10.80.0.2:443\n" +
		"\t\tiifname != \"mft3\" udp dport 2201 dnat to 10.80.0.2:22\n" +
		"\t\tiifname != \"mft3\" tcp dport 10000-10009 dnat to 10.80.0.2\n\t}\n" +
		"\tchain postrouting {\n\t\ttype nat hook postrouting priority srcnat; policy accept;\n\t\toifname \"mft3\" masquerade\n\t}\n" +
		"\tchain mss {\n\t\ttype filter hook forward priority mangle; policy accept;\n" +
		"\t\tiifname \"mft3\" tcp flags & (syn | rst) == syn tcp option maxseg size set rt mtu\n" +
		"\t\toifname \"mft3\" tcp flags & (syn | rst) == syn tcp option maxseg size set rt mtu\n\t}\n}\n"
	if f.calls[0].stdin != want {
		t.Fatalf("nft transaction:\n%s\nwant:\n%s", f.calls[0].stdin, want)
	}
	nftCheck(t, want[strings.Index(want, "add table ip manifold_tf3"):])
	if !f.forwarding {
		t.Fatal("IPv4 forwarding was not turned on")
	}
	got := strings.Join(f.argvs(), "\n")
	if !strings.Contains(got, `iptables -w 5 -I FORWARD 1 -o mft3 -m comment --comment manifold:tf3 -j ACCEPT`) {
		t.Fatalf("no FORWARD accept for the tunnel:\n%s", got)
	}
}

// Keeping the client's address means no masquerade on the relay: the client end
// routes its replies back through the tunnel instead.
func TestApplyForwardsWithoutMasqueradeKeepsTheSource(t *testing.T) {
	f := &fakeSystem{commands: map[string]bool{"nft": true}}
	useFake(t, f)
	if err := ApplyForwards("tf3", "mft3", netip.MustParseAddr("10.80.0.2"), []PortForward{{Proto: "tcp", Low: 443, High: 443}}, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.calls[0].stdin, "masquerade") {
		t.Fatalf("masquerade installed:\n%s", f.calls[0].stdin)
	}
	// Without masquerade the client end advertises its uplink's MSS, so only the relay can fit it to the link.
	if !strings.Contains(f.calls[0].stdin, "\t\tiifname \"mft3\" tcp flags & (syn | rst) == syn tcp option maxseg size set rt mtu\n") {
		t.Fatalf("the replies' MSS is not clamped:\n%s", f.calls[0].stdin)
	}
	if err := ApplyReplyMark("tr3", "mft3", 0x4d460003); err != nil {
		t.Fatal(err)
	}
	mark := f.calls[len(f.calls)-1].stdin
	for _, want := range []string{`iifname "mft3" ct state new ct mark set 0x4d460003`, "ct mark 0x4d460003 meta mark set 0x4d460003", "type route hook output"} {
		if !strings.Contains(mark, want) {
			t.Errorf("reply marking lacks %q:\n%s", want, mark)
		}
	}
}

func TestApplyForwardsRefusesWhatNftCouldNotTake(t *testing.T) {
	useFake(t, &fakeSystem{commands: map[string]bool{"nft": true}})
	tests := []struct {
		rule    PortForward
		wantErr string
	}{
		{PortForward{Proto: "sctp", Low: 1, High: 1}, `protocol "sctp"`},
		{PortForward{Proto: "tcp", Low: 10, High: 5}, "is not a port range"},
		{PortForward{Proto: "tcp", Low: 10, High: 20, TargetLow: 30}, "same ports only"},
	}
	for _, tt := range tests {
		err := ApplyForwards("tf1", "mft1", netip.MustParseAddr("10.0.0.2"), []PortForward{tt.rule}, true)
		if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("rule %+v: error %v, want %q", tt.rule, err, tt.wantErr)
		}
	}
}

func TestApplyBalancedForwardsSpreadsNewConnectionsOverTheTunnels(t *testing.T) {
	f := &fakeSystem{commands: map[string]bool{"nft": true, "iptables": true}}
	useFake(t, f)
	targets := []Target{{"mft1", netip.MustParseAddr("10.80.0.2")}, {"mft2", netip.MustParseAddr("10.81.0.2")}}
	rules := []PortForward{
		{Proto: "tcp", Low: 443, High: 443, TargetLow: 443},
		{Proto: "udp", Low: 2201, High: 2201, TargetLow: 22},
		{Proto: "tcp", Low: 10000, High: 10009, TargetLow: 10000},
	}
	if err := ApplyBalancedForwards("tg7", targets, rules, true, false); err != nil {
		t.Fatal(err)
	}
	want := "add table ip6 manifold_tg7\ndelete table ip6 manifold_tg7\n" +
		"add table ip manifold_tg7\ndelete table ip manifold_tg7\ntable ip manifold_tg7 {\n" +
		"\tchain prerouting {\n\t\ttype nat hook prerouting priority dstnat; policy accept;\n" +
		"\t\tiifname != { \"mft1\", \"mft2\" } tcp dport 443 dnat to numgen inc mod 2 map { 0 : 10.80.0.2, 1 : 10.81.0.2 }\n" +
		"\t\tiifname != { \"mft1\", \"mft2\" } udp dport 2201 dnat ip to numgen inc mod 2 map { 0 : 10.80.0.2 . 22, 1 : 10.81.0.2 . 22 }\n" +
		"\t\tiifname != { \"mft1\", \"mft2\" } tcp dport 10000-10009 dnat to numgen inc mod 2 map { 0 : 10.80.0.2, 1 : 10.81.0.2 }\n\t}\n" +
		"\tchain postrouting {\n\t\ttype nat hook postrouting priority srcnat; policy accept;\n\t\toifname { \"mft1\", \"mft2\" } masquerade\n\t}\n" +
		"\tchain mss {\n\t\ttype filter hook forward priority mangle; policy accept;\n" +
		"\t\tiifname { \"mft1\", \"mft2\" } tcp flags & (syn | rst) == syn tcp option maxseg size set rt mtu\n" +
		"\t\toifname { \"mft1\", \"mft2\" } tcp flags & (syn | rst) == syn tcp option maxseg size set rt mtu\n\t}\n}\n"
	if f.calls[0].stdin != want {
		t.Fatalf("nft transaction:\n%s\nwant:\n%s", f.calls[0].stdin, want)
	}
	got := strings.Join(f.argvs(), "\n")
	for _, dev := range []string{"mft1", "mft2"} {
		if !strings.Contains(got, "-I FORWARD 1 -o "+dev+" -m comment --comment manifold:tg7 -j ACCEPT") {
			t.Fatalf("no FORWARD accept for %s:\n%s", dev, got)
		}
	}
	nftCheck(t, strings.TrimPrefix(want, "add table ip6 manifold_tg7\ndelete table ip6 manifold_tg7\nadd table ip manifold_tg7\ndelete table ip manifold_tg7\n"))

	f.calls = nil
	if err := ApplyBalancedForwards("tg7", targets[1:], rules[:1], true, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.calls[0].stdin, "iifname != \"mft2\" tcp dport 443 dnat to 10.81.0.2:443\n") {
		t.Fatalf("one target left should be a plain forward:\n%s", f.calls[0].stdin)
	}
	if err := ApplyBalancedForwards("tg7", []Target{targets[0], {"mft3", netip.MustParseAddr("fd00::2")}}, rules, true, false); err == nil {
		t.Fatal("targets of two families were taken")
	}
}

// nftCheck has nft itself parse a ruleset where nft and the rights to run it are there.
func nftCheck(t *testing.T, ruleset string) {
	t.Helper()
	path, err := exec.LookPath("nft")
	if err != nil || os.Geteuid() != 0 {
		t.Log("nft is not available to check the ruleset")
		return
	}
	cmd := exec.CommandContext(t.Context(), path, "-c", "-f", "-")
	cmd.Stdin = strings.NewReader(ruleset)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("nft refused the ruleset: %v\n%s\n%s", err, out, ruleset)
	}
}

// L2TP/IPsec without NAT traversal sends ESP, IP protocol 50, which has no port.
func TestApplyForwardsCarriesESPThroughTheTunnel(t *testing.T) {
	f := &fakeSystem{commands: map[string]bool{"nft": true, "iptables": true}}
	useFake(t, f)
	rules := []PortForward{{Proto: "udp", Low: 500, High: 500}, {Proto: "udp", Low: 4500, High: 4500}, {Proto: "esp"}}
	if err := ApplyForwards("tf3", "mft3", netip.MustParseAddr("10.80.0.2"), rules, false); err != nil {
		t.Fatal(err)
	}
	ruleset := f.calls[0].stdin
	if !strings.Contains(ruleset, "\t\tiifname != \"mft3\" meta l4proto esp dnat to 10.80.0.2\n") {
		t.Fatalf("no ESP forward:\n%s", ruleset)
	}
	nftCheck(t, ruleset[strings.Index(ruleset, "add table ip manifold_tf3"):])
}

// IKE on UDP 500 and NAT traversal on UDP 4500 must reach the same origin, so a sticky group
// sends each client address to one tunnel instead of taking turns.
func TestStickyGroupKeepsEachClientOnOneTunnel(t *testing.T) {
	f := &fakeSystem{commands: map[string]bool{"nft": true, "iptables": true}}
	useFake(t, f)
	targets := []Target{{"mft1", netip.MustParseAddr("10.80.0.2")}, {"mft2", netip.MustParseAddr("10.81.0.2")}}
	rules := []PortForward{{Proto: "udp", Low: 500, High: 500, TargetLow: 500}, {Proto: "udp", Low: 4501, High: 4501, TargetLow: 4500}}
	if err := ApplyBalancedForwards("tg7", targets, rules, true, true); err != nil {
		t.Fatal(err)
	}
	ruleset := f.calls[0].stdin
	for _, want := range []string{
		"udp dport 500 dnat to jhash ip saddr mod 2 map { 0 : 10.80.0.2, 1 : 10.81.0.2 }\n",
		"udp dport 4501 dnat ip to jhash ip saddr mod 2 map { 0 : 10.80.0.2 . 4500, 1 : 10.81.0.2 . 4500 }\n",
	} {
		if !strings.Contains(ruleset, want) {
			t.Fatalf("sticky ruleset lacks %q:\n%s", want, ruleset)
		}
	}
	nftCheck(t, ruleset[strings.Index(ruleset, "add table ip manifold_tg7"):])
}
