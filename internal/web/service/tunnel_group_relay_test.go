package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/tunnel"
)

// groupRelay records what the managing panel sends a relay, tunnels and groups alike.
type groupRelay struct {
	mu        sync.Mutex
	groups    []string
	withdrawn []string
	missing   bool
}

func (f *groupRelay) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/tunnels/groups/import") && f.missing:
			w.WriteHeader(http.StatusNotFound)
			return
		case strings.HasSuffix(r.URL.Path, "/tunnels/groups/import"):
			f.groups = append(f.groups, r.PostForm.Get("group"))
		case strings.HasSuffix(r.URL.Path, "/tunnels/groups/withdraw"):
			f.withdrawn = append(f.withdrawn, r.PostForm.Get("pairId"))
		}
		_, _ = w.Write([]byte(`{"success":true,"msg":"","obj":null}`))
	})
}

func relayedGre(n, relayID int) TunnelRequest {
	return TunnelRequest{
		Name: fmt.Sprintf("path-%d", n), Kind: tunnel.KindGRE, Role: tunnel.RoleClient, Enable: true, RelayId: relayID,
		Settings: tunnel.TunnelSettings{
			RemoteAddr: fmt.Sprintf("198.51.100.%d", n), PublicAddr: "203.0.113.10",
			LocalIP: fmt.Sprintf("10.8%d.0.2/30", n), PeerIP: fmt.Sprintf("10.8%d.0.1", n),
		},
	}
}

// The panel that manages a relay builds the balancer from its own ends of that relay's
// tunnels; the relay forwards the ports, so the group is placed there and not applied here.
func TestAGroupOfARelaysTunnelsIsPlacedOnThatRelay(t *testing.T) {
	s := initTunnelDB(t)
	var applied []tunnel.Group
	tunnelManager = func() tunnelEngine { return groupEngine{groups: &applied} }
	relay := &groupRelay{}
	srv := httptest.NewServer(relay.handler())
	t.Cleanup(srv.Close)
	uk, err := s.SaveRelay(0, "uk", srv.URL, "relay-secret")
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Add(relayedGre(1, uk.Id))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Add(relayedGre(2, uk.Id))
	if err != nil {
		t.Fatal(err)
	}
	backup, err := s.Add(relayedGre(3, uk.Id))
	if err != nil {
		t.Fatal(err)
	}
	// The relay's port, not this server's: an inbound here on 443 is no conflict.
	seedInboundConflict(t, "here-443", "", 443, model.VLESS, "", `{"clients":[]}`)

	g, err := s.SaveGroup(0, TunnelGroupRequest{
		Name: "uk paths", Mode: tunnel.ModeBalance, RelayId: uk.Id, Members: []int{a.Id, b.Id}, Backups: []int{backup.Id},
		Sticky: true, Forwards: []tunnel.PortForward{{Listen: "443"}, {Listen: "2053", Proto: tunnel.ProtoUDP}}, Enable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(relay.groups) != 1 {
		t.Fatalf("the relay got %d groups, want 1", len(relay.groups))
	}
	var placed PlacedTunnelGroup
	if err := json.Unmarshal([]byte(relay.groups[0]), &placed); err != nil {
		t.Fatal(err)
	}
	want := PlacedTunnelGroup{
		PairID: g.PairId, Name: "uk paths", Mode: tunnel.ModeBalance, Members: []string{a.PairId, b.PairId}, Backups: []string{backup.PairId},
		Sticky: true, Forwards: []tunnel.PortForward{{Listen: "443", Proto: tunnel.ProtoTCP}, {Listen: "2053", Proto: tunnel.ProtoUDP}}, Enable: true,
	}
	if g.PairId == "" || !groupsEqual(placed, want) {
		t.Fatalf("the relay got %+v\nwant %+v", placed, want)
	}
	s.Reconcile()
	if len(applied) != 0 {
		t.Fatalf("this server applied the relay's group: %+v", applied)
	}

	if err := s.DeleteGroup(g.Id); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(relay.withdrawn, []string{g.PairId}) {
		t.Fatalf("withdrawn from the relay: %v, want the group's pair id", relay.withdrawn)
	}
}

func groupsEqual(a, b PlacedTunnelGroup) bool {
	ra, _ := json.Marshal(a)
	rb, _ := json.Marshal(b)
	return string(ra) == string(rb)
}

func TestARelayGroupTakesOnlyThatRelaysTunnels(t *testing.T) {
	s := initTunnelDB(t)
	relay := &groupRelay{}
	srv := httptest.NewServer(relay.handler())
	t.Cleanup(srv.Close)
	uk, _ := s.SaveRelay(0, "uk", srv.URL, "relay-secret")
	de, _ := s.SaveRelay(0, "de", srv.URL, "relay-secret")
	a, err := s.Add(relayedGre(1, uk.Id))
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.Add(relayedGre(2, de.Id))
	if err != nil {
		t.Fatal(err)
	}
	unrelayed := relayedGre(3, 0)
	local, err := s.Add(unrelayed)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		member int
		want   string
	}{
		{other.Id, `tunnel "path-2" goes to another relay`},
		{local.Id, `tunnel "path-3" goes to another relay`},
	} {
		_, err := s.SaveGroup(0, TunnelGroupRequest{Name: "x", RelayId: uk.Id, Members: []int{a.Id, tc.member}, Forwards: []tunnel.PortForward{{Listen: "443"}}, Enable: true})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("member %d: err = %v, want %q", tc.member, err, tc.want)
		}
	}
	relay.missing = true
	b, _ := s.Add(relayedGre(4, uk.Id))
	_, err = s.SaveGroup(0, TunnelGroupRequest{Name: "y", RelayId: uk.Id, Members: []int{a.Id, b.Id}, Forwards: []tunnel.PortForward{{Listen: "443"}}, Enable: true})
	if err == nil || !strings.Contains(err.Error(), "update the relay") {
		t.Fatalf("a relay without groups: err = %v, want it told to update", err)
	}
}

// The relay maps the group's tunnels by pair id to its own ends and runs it like its own.
func TestARelayRunsAPlacedGroupOnItsOwnEnds(t *testing.T) {
	s := initTunnelDB(t)
	var applied []tunnel.Group
	tunnelManager = func() tunnelEngine { return groupEngine{groups: &applied} }
	one, err := s.Add(greRelay(1))
	if err != nil {
		t.Fatal(err)
	}
	two, err := s.Add(greRelay(2))
	if err != nil {
		t.Fatal(err)
	}
	placed := PlacedTunnelGroup{
		PairID: "0123456789abcdef01234567", Name: "from the panel", Mode: tunnel.ModeFailover,
		Members: []string{two.PairId, one.PairId}, Forwards: []tunnel.PortForward{{Listen: "443"}}, Enable: true,
	}
	v, err := s.ImportGroup(placed)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(v.Members, []int{two.Id, one.Id}) || v.PairId != placed.PairID || v.RelayId != 0 {
		t.Fatalf("stored %+v", v)
	}
	s.Reconcile()
	if len(applied) != 1 || applied[0].ID != v.Id {
		t.Fatalf("the relay runs %+v", applied)
	}

	placed.Mode, placed.Members = tunnel.ModeBalance, []string{one.PairId, two.PairId}
	again, err := s.ImportGroup(placed)
	if err != nil {
		t.Fatal(err)
	}
	var count int64
	database.GetDB().Model(&model.TunnelGroup{}).Count(&count)
	if again.Id != v.Id || count != 1 || again.Mode != tunnel.ModeBalance {
		t.Fatalf("a second import made %d groups, id %d (want %d)", count, again.Id, v.Id)
	}

	placed.Members = []string{one.PairId, "ffffffffffffffffffffffff"}
	if _, err := s.ImportGroup(placed); err == nil || !strings.Contains(err.Error(), "this relay has no end of tunnel ffffffffffffffffffffffff") {
		t.Fatalf("a group over an unknown tunnel: %v", err)
	}
	if err := s.WithdrawGroup(placed.PairID); err != nil {
		t.Fatal(err)
	}
	database.GetDB().Model(&model.TunnelGroup{}).Count(&count)
	if count != 0 {
		t.Fatalf("%d groups after the withdrawal", count)
	}
}
