package tunnel

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/egress"
)

type groupCalls struct {
	applied []string
	removed []string
}

func fakeGroupRules(t *testing.T) *groupCalls {
	t.Helper()
	calls := &groupCalls{}
	prevApply, prevRemove := applyGroupForwards, removeGroupForwards
	applyGroupForwards = func(owner string, targets []egress.Target, rules []egress.PortForward, _, _ bool) error {
		var devs []string
		for _, tg := range targets {
			devs = append(devs, tg.Device)
		}
		calls.applied = append(calls.applied, fmt.Sprintf("%s:%s:%d", owner, strings.Join(devs, ","), len(rules)))
		return nil
	}
	removeGroupForwards = func(owner string) { calls.removed = append(calls.removed, owner) }
	t.Cleanup(func() { applyGroupForwards, removeGroupForwards = prevApply, prevRemove })
	return calls
}

// relayEnd is a running relay end of a GRE tunnel whose probe saw the far side answer or not.
func relayEnd(id int, answering bool) *entry {
	return relayEndRTT(id, answering, 20*time.Millisecond)
}

func relayEndRTT(id int, answering bool, rtt time.Duration) *entry {
	e := &entry{t: Tunnel{
		ID: id, Kind: KindGRE, Role: RoleServer, Enabled: true,
		Settings: TunnelSettings{PeerIP: fmt.Sprintf("10.8%d.0.2", id), LocalIP: fmt.Sprintf("10.8%d.0.1/30", id)},
	}, prober: &prober{}}
	for range 5 {
		e.prober.window.add(result{rtt: rtt, lost: !answering})
	}
	return e
}

// A degraded tunnel (loss or delay over the limits) kept its share of every client while a healthy
// one sat beside it: everyone hashed onto it got the slow link.
func TestGroupsLeaveADegradedTunnelWhileAHealthyOneCarries(t *testing.T) {
	calls := fakeGroupRules(t)
	m := newManager(t.TempDir(), func() string { return "" })
	slow := 2 * time.Second
	m.entries = map[int]*entry{1: relayEndRTT(1, true, slow), 2: relayEnd(2, true), 3: relayEnd(3, true)}
	forwards := []PortForward{{Listen: "8007", Proto: ProtoUDP}}
	g := Group{ID: 7, Name: "lb", Mode: ModeBalance, Members: []int{1, 2}, Backups: []int{3}, Forwards: forwards, Enabled: true}
	step := func(want string) {
		t.Helper()
		m.ReconcileGroups([]Group{g})
		if got := calls.applied[len(calls.applied)-1]; got != want {
			t.Fatalf("forwarding to %s, want %s", got, want)
		}
	}
	step("tg7:mft2:1")
	m.entries[2] = relayEndRTT(2, true, slow)
	step("tg7:mft3:1")
	m.entries[3] = relayEnd(3, false)
	step("tg7:mft1,mft2:1")
}

func TestGroupsFollowTheTunnelsThatAnswer(t *testing.T) {
	calls := fakeGroupRules(t)
	m := newManager(t.TempDir(), func() string { return "" })
	m.entries = map[int]*entry{1: relayEnd(1, true), 2: relayEnd(2, true), 3: relayEnd(3, true)}
	forwards := []PortForward{{Listen: "443", Proto: ProtoTCP}}
	failover := Group{ID: 1, Name: "fo", Mode: ModeFailover, Members: []int{2, 1}, Forwards: forwards, Enabled: true}
	balance := Group{ID: 2, Name: "lb", Mode: ModeBalance, Members: []int{1, 2, 3}, Forwards: forwards, Enabled: true}

	m.ReconcileGroups([]Group{failover, balance})
	want := []string{"tg1:mft2:1", "tg2:mft1,mft2,mft3:1"}
	if strings.Join(calls.applied, " ") != strings.Join(want, " ") {
		t.Fatalf("applied %v, want %v", calls.applied, want)
	}
	m.ReconcileGroups([]Group{failover, balance})
	if len(calls.applied) != 2 {
		t.Fatalf("rules were rewritten with nothing changed: %v", calls.applied)
	}

	m.entries[2] = relayEnd(2, false)
	m.ReconcileGroups([]Group{failover, balance})
	want = append(want, "tg1:mft1:1", "tg2:mft1,mft3:1")
	if strings.Join(calls.applied, " ") != strings.Join(want, " ") {
		t.Fatalf("after tunnel 2 stopped answering: %v, want %v", calls.applied, want)
	}

	for _, id := range []int{1, 3} {
		m.entries[id] = relayEnd(id, false)
	}
	m.ReconcileGroups([]Group{failover, balance})
	want = append(want, "tg1:mft2:1", "tg2:mft1,mft2,mft3:1")
	if strings.Join(calls.applied, " ") != strings.Join(want, " ") {
		t.Fatalf("with no tunnel answering the ports went to %v, want back to every member", calls.applied)
	}

	m.ReconcileGroups([]Group{failover})
	if strings.Join(calls.removed, " ") != "tg2" {
		t.Fatalf("removed %v, want the dropped group's rules", calls.removed)
	}
}

func TestNormalizeGroupNeedsTwoTunnelsAndAPort(t *testing.T) {
	if _, err := NormalizeGroup(Group{Name: "g", Members: []int{1, 1}, Forwards: []PortForward{{Listen: "443"}}}); err == nil {
		t.Fatal("a group of one tunnel was taken")
	}
	if _, err := NormalizeGroup(Group{Name: "g", Members: []int{1, 2}}); err == nil {
		t.Fatal("a group without ports was taken")
	}
	g, err := NormalizeGroup(Group{Name: " g ", Members: []int{1, 2}, Forwards: []PortForward{{Listen: "443"}}})
	if err != nil || g.Mode != ModeFailover || g.Name != "g" || g.Forwards[0].Proto != ProtoTCP {
		t.Fatalf("normalized %+v %v", g, err)
	}
}

func TestBackupsCarryTheGroupOnlyWhenNoMemberAnswers(t *testing.T) {
	calls := fakeGroupRules(t)
	m := newManager(t.TempDir(), func() string { return "" })
	m.entries = map[int]*entry{1: relayEnd(1, true), 2: relayEnd(2, true), 3: relayEnd(3, true), 4: relayEnd(4, true)}
	forwards := []PortForward{{Listen: "443", Proto: ProtoTCP}}
	g := Group{ID: 5, Name: "lb", Mode: ModeBalance, Members: []int{1, 2}, Backups: []int{3, 4}, Forwards: forwards, Enabled: true}
	step := func(want string) {
		t.Helper()
		m.ReconcileGroups([]Group{g})
		if got := calls.applied[len(calls.applied)-1]; got != want {
			t.Fatalf("forwarding to %s, want %s", got, want)
		}
	}
	step("tg5:mft1,mft2:1")
	m.entries[1] = relayEnd(1, false)
	step("tg5:mft2:1")
	m.entries[2] = relayEnd(2, false)
	step("tg5:mft3,mft4:1")
	g.Mode = ModeFailover
	step("tg5:mft3:1")
	m.entries[3] = relayEnd(3, false)
	step("tg5:mft4:1")
	// Nothing answers: every primary member, so a probe that cannot run never takes the ports away.
	m.entries[4] = relayEnd(4, false)
	g.Mode = ModeBalance
	step("tg5:mft1,mft2:1")
	m.entries[2] = relayEnd(2, true)
	step("tg5:mft2:1")
}

func TestStickyGroupHashesTheClientsAddress(t *testing.T) {
	var sticky []bool
	prev := applyGroupForwards
	applyGroupForwards = func(_ string, _ []egress.Target, _ []egress.PortForward, _, s bool) error {
		sticky = append(sticky, s)
		return nil
	}
	t.Cleanup(func() { applyGroupForwards = prev })
	m := newManager(t.TempDir(), func() string { return "" })
	m.entries = map[int]*entry{1: relayEnd(1, true), 2: relayEnd(2, true)}
	m.ReconcileGroups([]Group{{ID: 1, Name: "ipsec", Mode: ModeBalance, Members: []int{1, 2}, Sticky: true, Forwards: []PortForward{{Listen: "500", Proto: ProtoUDP}}, Enabled: true}})
	if len(sticky) != 1 || !sticky[0] {
		t.Fatalf("sticky passed as %v", sticky)
	}
}

func TestNormalizeGroupKeepsBackupsApartFromMembers(t *testing.T) {
	g, err := NormalizeGroup(Group{Name: "g", Members: []int{1}, Backups: []int{2, 1, 2, 0}, Forwards: []PortForward{{Listen: "443"}}})
	if err != nil {
		t.Fatalf("one member and one backup: %v", err)
	}
	if len(g.Members) != 1 || len(g.Backups) != 1 || g.Backups[0] != 2 {
		t.Fatalf("normalized members %v backups %v", g.Members, g.Backups)
	}
	if _, err := NormalizeGroup(Group{Name: "g", Backups: []int{1, 2}, Forwards: []PortForward{{Listen: "443"}}}); err == nil {
		t.Fatal("a group of backups alone was taken")
	}
	many := make([]int, 17)
	for i := range many {
		many[i] = i + 1
	}
	if _, err := NormalizeGroup(Group{Name: "g", Members: many[:10], Backups: many[10:], Forwards: []PortForward{{Listen: "443"}}}); err == nil {
		t.Fatal("17 tunnels were taken")
	}
}

// A group forwards with nftables whatever its tunnels are: UDP alone and ESP go, a remapped range cannot.
func TestGroupForwardsAreRulesOfTheRelayItself(t *testing.T) {
	g, err := NormalizeGroup(Group{Name: "ipsec", Members: []int{1, 2}, Forwards: []PortForward{{Listen: "500", Proto: ProtoUDP}, {Listen: "4500", Proto: ProtoUDP}, {Proto: ProtoESP}}})
	if err != nil || len(g.Forwards) != 3 {
		t.Fatalf("UDP alone and ESP in a group: %+v %v", g.Forwards, err)
	}
	if _, err := NormalizeGroup(Group{Name: "g", Members: []int{1, 2}, Forwards: []PortForward{{Listen: "1000-1009", Target: "2000-2009"}}}); err == nil {
		t.Fatal("a remapped range was taken, which the group's rules would send to the same ports")
	}
}
