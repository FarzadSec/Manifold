package tunnel

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/egress"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

// How a group spreads its ports over its tunnels.
const (
	// ModeFailover sends everything to the first member that answers, in order.
	ModeFailover = "failover"
	// ModeBalance gives each new connection to the next member that answers.
	ModeBalance = "balance"
)

// Group is a set of this relay's tunnels to several origins that share forwarded ports.
type Group struct {
	ID      int
	Name    string
	Mode    string
	Members []int
	// Backups carry the ports only while no member answers; Sticky keeps each client on one tunnel.
	Backups  []int
	Sticky   bool
	Forwards []PortForward
	Enabled  bool
}

// NormalizeGroup fills defaults and checks a group before it is saved.
func NormalizeGroup(g Group) (Group, error) {
	g.Name = strings.TrimSpace(g.Name)
	if g.Name == "" || len(g.Name) > 64 {
		return g, errors.New("tunnel: a group name is 1 to 64 characters")
	}
	if g.Mode == "" {
		g.Mode = ModeFailover
	}
	if g.Mode != ModeFailover && g.Mode != ModeBalance {
		return g, fmt.Errorf("tunnel: mode %q must be %s or %s", g.Mode, ModeFailover, ModeBalance)
	}
	members := make([]int, 0, len(g.Members))
	for _, id := range g.Members {
		if id > 0 && !slices.Contains(members, id) {
			members = append(members, id)
		}
	}
	backups := make([]int, 0, len(g.Backups))
	for _, id := range g.Backups {
		if id > 0 && !slices.Contains(members, id) && !slices.Contains(backups, id) {
			backups = append(backups, id)
		}
	}
	if len(members) == 0 || len(members)+len(backups) < 2 || len(members)+len(backups) > 16 {
		return g, errors.New("tunnel: a group joins 2 to 16 tunnels, at least one of them not a backup")
	}
	g.Members, g.Backups = members, backups
	// A group's ports are DNAT rules to its tunnels' addresses, as a kernel link's are.
	forwards, err := normalizeForwards(KindGRE, g.Forwards)
	if err != nil {
		return g, err
	}
	if len(forwards) == 0 {
		return g, errors.New("tunnel: a group forwards at least one port")
	}
	g.Forwards = forwards
	return g, nil
}

// ListenPorts are the ports a group takes on the relay.
func (g Group) ListenPorts() map[int]string {
	return Tunnel{Role: RoleServer, Settings: TunnelSettings{Forwards: g.Forwards}}.ListenPorts()
}

// CanJoinGroup reports whether a tunnel end can carry a group's ports: a relay end with
// an interface and an address on the far side.
func CanJoinGroup(t Tunnel) bool {
	return t.Role == RoleServer && t.HasInterface()
}

// CanCarryRelayGroup reports whether this panel's end can be named in a group it places on the
// relay: the relay then balances over its own end of the same link.
func CanCarryRelayGroup(t Tunnel) bool {
	return t.Role == RoleClient && t.HasInterface()
}

func groupOwner(id int) string { return fmt.Sprintf("tg%d", id) }

// carryHealth is how well a group's tunnel carries traffic now, best last.
type carryHealth int

const (
	carryDown carryHealth = iota
	carryDegraded
	carryUp
)

// chooseTargets is what a group forwards to now: the members fully up, else the backups that are, else
// the members and then the backups that answer with loss or delay, else every member, so a probe that
// cannot run never takes the ports away; failover takes the first.
func chooseTargets(mode string, members, backups []egress.Target, memberHealth, backupHealth []carryHealth) []egress.Target {
	var live []egress.Target
	for _, step := range []struct {
		targets []egress.Target
		health  []carryHealth
		want    carryHealth
	}{
		{members, memberHealth, carryUp},
		{backups, backupHealth, carryUp},
		{members, memberHealth, carryDegraded},
		{backups, backupHealth, carryDegraded},
	} {
		if live = withHealth(step.targets, step.health, step.want); len(live) > 0 {
			break
		}
	}
	if len(live) == 0 {
		live = slices.Clone(members)
	}
	if mode == ModeFailover {
		return live[:1]
	}
	return live
}

func withHealth(targets []egress.Target, health []carryHealth, want carryHealth) []egress.Target {
	var live []egress.Target
	for i, t := range targets {
		if health[i] == want {
			live = append(live, t)
		}
	}
	return live
}

// groupTargetsLocked lists the tunnels of ids that can carry a group, and how well each carries now.
func (m *Manager) groupTargetsLocked(ids []int) ([]egress.Target, []carryHealth) {
	var targets []egress.Target
	var health []carryHealth
	for _, id := range ids {
		e := m.entries[id]
		if e == nil || e.disabled || e.err != nil || !CanJoinGroup(e.t) {
			continue
		}
		peer, err := netip.ParseAddr(e.t.Settings.PeerIP)
		if err != nil {
			continue
		}
		targets = append(targets, egress.Target{Device: e.t.Device(), Addr: peer})
		switch m.statusLocked(e).State {
		case StateUp:
			health = append(health, carryUp)
		case StateDegraded:
			health = append(health, carryDegraded)
		default:
			health = append(health, carryDown)
		}
	}
	return targets, health
}

// applyGroupForwards and removeGroupForwards change the relay's rules; tests replace them.
var (
	applyGroupForwards  = egress.ApplyBalancedForwards
	removeGroupForwards = egress.RemoveTables
)

func targetsKey(targets []egress.Target, rules []egress.PortForward, sticky bool) string {
	return fmt.Sprint(targets, rules, sticky)
}

// ReconcileGroups points each group's ports at the members that answer now; it is run
// after every tunnel reconcile and on a timer, and changes rules only when the set moves.
func (m *Manager) ReconcileGroups(groups []Group) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.groups == nil {
		m.groups = map[int]string{}
	}
	wanted := map[int]bool{}
	for _, g := range groups {
		if !g.Enabled {
			continue
		}
		members, health := m.groupTargetsLocked(g.Members)
		backups, backupHealth := m.groupTargetsLocked(g.Backups)
		if len(members) == 0 {
			members, health, backups, backupHealth = backups, backupHealth, nil, nil
		}
		if len(members) == 0 {
			continue
		}
		wanted[g.ID] = true
		targets := chooseTargets(g.Mode, members, backups, health, backupHealth)
		rules := portForwards(g.Forwards)
		key := targetsKey(targets, rules, g.Sticky)
		if m.groups[g.ID] == key {
			continue
		}
		if err := applyGroupForwards(groupOwner(g.ID), targets, rules, true, g.Sticky); err != nil {
			logger.Warning("tunnel group ", g.ID, " (", g.Name, "): ", err)
			delete(m.groups, g.ID)
			continue
		}
		if m.groups[g.ID] != "" {
			logger.Info("tunnel group ", g.Name, " now forwards to ", len(targets), " of ", len(members), " tunnels")
		}
		m.groups[g.ID] = key
	}
	for id := range m.groups {
		if !wanted[id] {
			removeGroupForwards(groupOwner(id))
			delete(m.groups, id)
		}
	}
}
