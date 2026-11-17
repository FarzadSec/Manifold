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
	applyGroupForwards = func(owner string, targets []egress.Target, rules []egress.PortForward, _ bool) error {
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
	e := &entry{t: Tunnel{
		ID: id, Kind: KindGRE, Role: RoleServer, Enabled: true,
		Settings: TunnelSettings{PeerIP: fmt.Sprintf("10.8%d.0.2", id), LocalIP: fmt.Sprintf("10.8%d.0.1/30", id)},
	}, prober: &prober{}}
	for range 5 {
		e.prober.window.add(result{rtt: 20 * time.Millisecond, lost: !answering})
	}
	return e
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
