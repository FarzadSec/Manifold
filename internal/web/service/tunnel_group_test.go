package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/tunnel"
)

type groupEngine struct {
	quietEngine
	groups *[]tunnel.Group
}

func (e groupEngine) ReconcileGroups(g []tunnel.Group) { *e.groups = g }

func greRelay(n int) TunnelRequest {
	return TunnelRequest{
		Name: fmt.Sprintf("origin-%d", n), Kind: tunnel.KindGRE, Role: tunnel.RoleServer, Enable: true,
		Settings: tunnel.TunnelSettings{
			RemoteAddr: fmt.Sprintf("198.51.100.%d", n), LocalIP: fmt.Sprintf("10.8%d.0.1/30", n), PeerIP: fmt.Sprintf("10.8%d.0.2", n),
		},
	}
}

func TestTunnelGroupsShareThePortsOfSeveralOrigins(t *testing.T) {
	s := initTunnelDB(t)
	var applied []tunnel.Group
	tunnelManager = func() tunnelEngine { return groupEngine{groups: &applied} }
	a, err := s.Add(greRelay(1))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Add(greRelay(2))
	if err != nil {
		t.Fatal(err)
	}
	own := greRelay(9)
	own.Role, own.Name = tunnel.RoleClient, "this-panel-end"
	client, err := s.Add(own)
	if err != nil {
		t.Fatal(err)
	}
	web := []tunnel.PortForward{{Listen: "443"}}
	g, err := s.SaveGroup(0, TunnelGroupRequest{Name: "origins", Mode: "balance", Members: []int{a.Id, b.Id}, Forwards: web, Enable: true})
	if err != nil {
		t.Fatal(err)
	}
	s.Reconcile()
	if len(applied) != 1 || applied[0].ID != g.Id || applied[0].Mode != tunnel.ModeBalance || applied[0].Forwards[0].Listen != "443" {
		t.Fatalf("the engine was given %+v", applied)
	}

	for name, attempt := range map[string]func() error{
		"a tunnel forwarding a group's port": func() error {
			_, err := s.Add(relayEnd(9443, tunnel.PortForward{Listen: "443"}))
			return err
		},
		"a second group on the same port": func() error {
			_, err := s.SaveGroup(0, TunnelGroupRequest{Name: "other", Members: []int{a.Id, b.Id}, Forwards: web, Enable: true})
			return err
		},
		"the panel's own end of a tunnel, which is no relay": func() error {
			_, err := s.SaveGroup(0, TunnelGroupRequest{Name: "x", Members: []int{a.Id, client.Id}, Forwards: []tunnel.PortForward{{Listen: "8080"}}, Enable: true})
			return err
		},
		"deleting a tunnel the group uses": func() error { return s.Delete(a.Id) },
	} {
		if err := attempt(); err == nil {
			t.Errorf("%s was allowed", name)
		}
	}
	if _, err := s.SaveGroup(g.Id, TunnelGroupRequest{Name: "origins", Mode: "failover", Members: []int{b.Id, a.Id}, Forwards: web, Enable: true}); err != nil {
		t.Fatalf("re-saving the group over its own port: %v", err)
	}
	if err := s.DeleteGroup(g.Id); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(a.Id); err != nil || !strings.Contains(fmt.Sprint(groupsUsing(a.Id)), "[]") {
		t.Fatalf("deleting a tunnel no group uses: %v", err)
	}
}
