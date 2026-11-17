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
	ID       int
	Name     string
	Mode     string
	Members  []int
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
	if len(members) < 2 || len(members) > 16 {
		return g, errors.New("tunnel: a group joins 2 to 16 tunnels")
	}
	g.Members = members
	forwards, err := normalizeForwards(KindL3, g.Forwards)
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

func groupOwner(id int) string { return fmt.Sprintf("tg%d", id) }

// chooseTargets is what a group forwards to now: in failover the first member that
// answers, in balance every member that answers; when none answers, all of them, so a
// probe that cannot run never takes the ports away.
func chooseTargets(mode string, members []egress.Target, healthy []bool) []egress.Target {
	var live []egress.Target
	for i, m := range members {
		if healthy[i] {
			live = append(live, m)
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

// applyGroupForwards and removeGroupForwards change the relay's rules; tests replace them.
var (
	applyGroupForwards  = egress.ApplyBalancedForwards
	removeGroupForwards = egress.RemoveTables
)

func targetsKey(targets []egress.Target, rules []egress.PortForward) string {
	return fmt.Sprint(targets, rules)
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
		var members []egress.Target
		var healthy []bool
		for _, id := range g.Members {
			e := m.entries[id]
			if e == nil || e.disabled || e.err != nil || !CanJoinGroup(e.t) {
				continue
			}
			peer, err := netip.ParseAddr(e.t.Settings.PeerIP)
			if err != nil {
				continue
			}
			members = append(members, egress.Target{Device: e.t.Device(), Addr: peer})
			state := m.statusLocked(e).State
			healthy = append(healthy, state == StateUp || state == StateDegraded)
		}
		if len(members) == 0 {
			continue
		}
		wanted[g.ID] = true
		targets := chooseTargets(g.Mode, members, healthy)
		rules := portForwards(Tunnel{Settings: TunnelSettings{Forwards: g.Forwards}})
		key := targetsKey(targets, rules)
		if m.groups[g.ID] == key {
			continue
		}
		if err := applyGroupForwards(groupOwner(g.ID), targets, rules, true); err != nil {
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
