package egress

import (
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"
)

type call struct {
	stdin string
	argv  string
}

type fakeSystem struct {
	commands   map[string]bool
	forward    string
	calls      []call
	forwarding bool
}

func (f *fakeSystem) run(stdin, name string, args ...string) (string, error) {
	argv := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, call{stdin: stdin, argv: argv})
	if argv == "iptables -w 5 -S FORWARD" {
		return f.forward, nil
	}
	return "", nil
}

func (f *fakeSystem) has(name string) bool { return f.commands[name] }

func (f *fakeSystem) enableIPv4Forwarding() error {
	f.forwarding = true
	return nil
}

func (f *fakeSystem) argvs() []string {
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, c.argv)
	}
	return out
}

func useFake(t *testing.T, f *fakeSystem) {
	t.Helper()
	prev := host
	host = f
	t.Cleanup(func() { host = prev })
}

func TestApplyReplacesOnlyTheOwnersRules(t *testing.T) {
	f := &fakeSystem{
		commands: map[string]bool{"nft": true, "iptables": true},
		forward: strings.Join([]string{
			"-P FORWARD DROP",
			`-A FORWARD -s 10.9.0.0/24 -m comment --comment "manifold:ovpn3" -j ACCEPT`,
			`-A FORWARD -s 10.7.0.0/24 -m comment --comment "manifold:ovpn30" -j ACCEPT`,
			"-A FORWARD -i docker0 -j ACCEPT",
		}, "\n"),
	}
	useFake(t, f)

	if err := Apply("ovpn3", netip.MustParsePrefix("10.8.0.1/24")); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	wantNAT := "add table ip manifold_ovpn3\ndelete table ip manifold_ovpn3\n" +
		"table ip manifold_ovpn3 {\n\tchain postrouting {\n" +
		"\t\ttype nat hook postrouting priority 100; policy accept;\n" +
		"\t\tip saddr 10.8.0.0/24 ip daddr != 10.8.0.0/24 masquerade\n\t}\n}\n"
	if got := f.calls[0]; got.argv != "nft -f -" || got.stdin != wantNAT {
		t.Fatalf("NAT transaction = %q via %q, want %q via nft -f -", got.stdin, got.argv, wantNAT)
	}
	if !f.forwarding {
		t.Fatal("IPv4 forwarding was not enabled")
	}
	wantRest := []string{
		"iptables -w 5 -S FORWARD",
		"iptables -w 5 -D FORWARD -s 10.9.0.0/24 -m comment --comment manifold:ovpn3 -j ACCEPT",
		"iptables -w 5 -I FORWARD 1 -s 10.8.0.0/24 -m comment --comment manifold:ovpn3 -j ACCEPT",
		"iptables -w 5 -I FORWARD 1 -d 10.8.0.0/24 -m conntrack --ctstate RELATED,ESTABLISHED -m comment --comment manifold:ovpn3 -j ACCEPT",
	}
	if got := f.argvs()[1:]; !slices.Equal(got, wantRest) {
		t.Fatalf("forward commands = %q, want %q", got, wantRest)
	}
}

func TestApplyRefusesWhatItCannotInstallSafely(t *testing.T) {
	subnet := netip.MustParsePrefix("10.8.0.0/24")
	tests := []struct {
		name    string
		owner   string
		subnet  netip.Prefix
		hasNft  bool
		wantErr error
	}{
		{name: "owner that would break out of the nft syntax", owner: "x; flush ruleset", subnet: subnet, hasNft: true},
		{name: "IPv6 subnet in an IPv4 table", owner: "ovpn3", subnet: netip.MustParsePrefix("fd00::/64"), hasNft: true},
		{name: "host without nftables", owner: "ovpn3", subnet: subnet, wantErr: ErrNoNftables},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeSystem{commands: map[string]bool{"nft": tt.hasNft, "iptables": true}}
			useFake(t, f)
			err := Apply(tt.owner, tt.subnet)
			if err == nil {
				t.Fatal("Apply succeeded")
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("Apply error = %v, want %v", err, tt.wantErr)
			}
			if len(f.calls) != 0 || f.forwarding {
				t.Fatalf("Apply touched the host before refusing: %q", f.argvs())
			}
		})
	}
}

func TestRemoveWithdrawsTableAndForwardRules(t *testing.T) {
	f := &fakeSystem{
		commands: map[string]bool{"nft": true, "iptables": true},
		forward: `-A FORWARD -d 10.8.0.0/24 -m conntrack --ctstate RELATED,ESTABLISHED -m comment --comment "manifold:l2tp1" -j ACCEPT` + "\n" +
			`-A FORWARD -s 10.8.0.0/24 -m comment --comment "manifold:ovpn3" -j ACCEPT`,
	}
	useFake(t, f)

	Remove("l2tp1")

	if got, want := f.calls[0].stdin, "add table ip manifold_l2tp1\ndelete table ip manifold_l2tp1\n"; got != want {
		t.Fatalf("NAT removal = %q, want %q", got, want)
	}
	want := []string{
		"nft -f -",
		"iptables -w 5 -S FORWARD",
		"iptables -w 5 -D FORWARD -d 10.8.0.0/24 -m conntrack --ctstate RELATED,ESTABLISHED -m comment --comment manifold:l2tp1 -j ACCEPT",
	}
	if got := f.argvs(); !slices.Equal(got, want) {
		t.Fatalf("commands = %q, want %q", got, want)
	}
}
