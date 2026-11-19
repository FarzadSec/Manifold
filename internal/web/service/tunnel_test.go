package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/tunnel"
)

type quietEngine struct{}

func (quietEngine) Reconcile([]tunnel.Tunnel)       {}
func (quietEngine) ReconcileGroups([]tunnel.Group)  {}
func (quietEngine) Statuses() []tunnel.TunnelStatus { return nil }

func initTunnelDB(t *testing.T) *TunnelService {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dir)
	dbtest.InitDB(t, filepath.Join(dir, "x-ui.db"))
	prev := tunnelManager
	tunnelManager = func() tunnelEngine { return quietEngine{} }
	t.Cleanup(func() { tunnelManager = prev })
	return &TunnelService{}
}

type recordingEngine struct{ applied chan []tunnel.Tunnel }

func (e recordingEngine) Reconcile(desired []tunnel.Tunnel) { e.applied <- desired }
func (recordingEngine) Statuses() []tunnel.TunnelStatus     { return nil }
func (recordingEngine) ReconcileGroups([]tunnel.Group)      {}

// A save is applied by the server's request loop, and the loop ends with the
// server's context, so no reconcile outlives a shutdown or the database.
func TestASavedTunnelIsAppliedByTheRequestLoopUntilItStops(t *testing.T) {
	s := initTunnelDB(t)
	engine := recordingEngine{applied: make(chan []tunnel.Tunnel, 4)}
	tunnelManager = func() tunnelEngine { return engine }
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { s.ServeReconcileRequests(ctx); close(stopped) }()
	t.Cleanup(func() { cancel(); <-stopped })

	v, err := s.Add(relayEnd(8443, tunnel.PortForward{Listen: "443"}))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	for applied := false; !applied; {
		select {
		case desired := <-engine.applied:
			applied = len(desired) == 1 && desired[0].ID == v.Id
		case <-deadline:
			t.Fatalf("tunnel %d was not applied", v.Id)
		}
	}

	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the request loop kept running after its context ended")
	}
	// A request queued before the stop may have been applied already; only later ones count.
	for drained := false; !drained; {
		select {
		case <-engine.applied:
		default:
			drained = true
		}
	}
	if err := s.Delete(v.Id); err != nil {
		t.Fatal(err)
	}
	select {
	case desired := <-engine.applied:
		t.Fatalf("applied %+v after the loop stopped", desired)
	case <-time.After(200 * time.Millisecond):
	}
}

func relayEnd(port int, forwards ...tunnel.PortForward) TunnelRequest {
	return TunnelRequest{
		Name: "relay", Kind: tunnel.KindReverse, Role: tunnel.RoleServer, Enable: true,
		Settings: tunnel.TunnelSettings{Transport: "tcpmux", Port: port, Forwards: forwards},
	}
}

func TestTunnelSaveRefusesWhatAnotherUserHolds(t *testing.T) {
	s := initTunnelDB(t)
	if _, err := s.Add(relayEnd(8443, tunnel.PortForward{Listen: "443"})); err != nil {
		t.Fatal(err)
	}
	gre := TunnelRequest{
		Name: "gre", Kind: tunnel.KindGRE, Role: tunnel.RoleClient, Enable: true,
		Settings: tunnel.TunnelSettings{RemoteAddr: "198.51.100.7", LocalIP: "10.80.0.2/30", PeerIP: "10.80.0.1"},
	}
	if _, err := s.Add(gre); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Create(&model.Inbound{Port: 9443, Enable: true, Protocol: model.VLESS, Tag: "in-9443"}).Error; err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		req     TunnelRequest
		wantErr string
	}{
		{"tunnel port of another tunnel", relayEnd(8443), "port 8443 is already used by tunnel"},
		{"forwarded port of another tunnel", relayEnd(8444, tunnel.PortForward{Listen: "440-445"}), "port 443 is already used by tunnel"},
		{"port of an inbound", relayEnd(9443), "port 9443 is used by an inbound"},
		{"SSH", relayEnd(8445, tunnel.PortForward{Listen: "22"}), "port 22 belongs to SSH"},
		{"overlapping tunnel addresses", TunnelRequest{
			Name: "gre2", Kind: tunnel.KindGRE, Role: tunnel.RoleClient,
			Settings: tunnel.TunnelSettings{RemoteAddr: "198.51.100.8", LocalIP: "10.80.0.1/29", PeerIP: "10.80.0.3"},
		}, `overlap those of tunnel "gre"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.Add(tt.req)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// The owning panel updates its relay by sending the code again: the relay must
// change its end in place, not add a second one on the same ports.
func TestImportReplacesTheEndOfTheSamePair(t *testing.T) {
	owner := initTunnelDB(t)
	v, err := owner.Add(TunnelRequest{
		Name: "iran", Kind: tunnel.KindReverse, Role: tunnel.RoleClient, Enable: true,
		Settings: tunnel.TunnelSettings{Transport: "tcpmux", Port: 8443, RemoteAddr: "relay.example.com", Forwards: []tunnel.PortForward{{Listen: "443"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := owner.Pairing(v.Id, "203.0.113.10")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Delete(&model.TunnelEnd{}, v.Id).Error; err != nil {
		t.Fatal(err)
	}

	imported, err := owner.Import(first.Code)
	if err != nil {
		t.Fatal(err)
	}
	if imported.Role != tunnel.RoleServer || imported.PairId != v.PairId {
		t.Fatalf("imported end = %+v", imported)
	}
	end, _ := tunnelFromRow(&model.TunnelEnd{Id: imported.Id, PairId: imported.PairId, Name: "iran", Kind: imported.Kind, Role: imported.Role, Settings: `{"transport":"tcpmux","port":8443,"forwards":[{"listen":"443"},{"listen":"8080"}]}`})
	end.Role = tunnel.RoleClient
	end.Settings.RemoteAddr = "relay.example.com"
	second, err := tunnel.PairingCode(end, "203.0.113.10")
	if err != nil {
		t.Fatal(err)
	}
	again, err := owner.Import(second)
	if err != nil {
		t.Fatal(err)
	}
	var count int64
	database.GetDB().Model(&model.TunnelEnd{}).Count(&count)
	if again.Id != imported.Id || count != 1 || len(again.Settings.Forwards) != 2 {
		t.Fatalf("second import: id %d (want %d), %d rows, forwards %+v", again.Id, imported.Id, count, again.Settings.Forwards)
	}
}

type fakeRelay struct {
	mu    sync.Mutex
	calls []string
	codes []string
}

func (f *fakeRelay) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer relay-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = r.ParseForm()
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		if code := r.PostForm.Get("code"); code != "" {
			f.codes = append(f.codes, code)
		}
		if id := r.PostForm.Get("pairId"); id != "" {
			f.calls[len(f.calls)-1] += " " + id
		}
		f.mu.Unlock()
		w.Header().Set(VersionHeader, "0.13.0")
		_, _ = w.Write([]byte(`{"success":true,"msg":"","obj":null}`))
	})
}

func TestARelayedTunnelIsSetUpAndRemovedOnTheRelay(t *testing.T) {
	s := initTunnelDB(t)
	relay := &fakeRelay{}
	srv := httptest.NewServer(relay.handler())
	t.Cleanup(srv.Close)
	row, err := s.SaveRelay(0, "uk", srv.URL+"/secret", "relay-secret")
	if err != nil {
		t.Fatal(err)
	}
	if row.Url != srv.URL+"/secret/" {
		t.Fatalf("url = %q, want the base path to end in a slash", row.Url)
	}
	v, err := s.Add(TunnelRequest{
		Name: "uk", Kind: tunnel.KindGRE, Role: tunnel.RoleClient, Enable: true, RelayId: row.Id,
		Settings: tunnel.TunnelSettings{
			RemoteAddr: "198.51.100.7", PublicAddr: "203.0.113.10", LocalIP: "10.80.0.2/30", PeerIP: "10.80.0.1",
			Forwards: []tunnel.PortForward{{Listen: "443"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(relay.codes) != 1 {
		t.Fatalf("the relay got %d codes, want 1 (calls %v)", len(relay.codes), relay.calls)
	}
	other, err := tunnel.ParsePairingCode(relay.codes[0])
	if err != nil {
		t.Fatal(err)
	}
	if other.Role != tunnel.RoleServer || other.Settings.RemoteAddr != "203.0.113.10" || other.Settings.PeerIP != "10.80.0.2" {
		t.Fatalf("relay's end = %+v", other)
	}
	var seen model.TunnelRelay
	database.GetDB().First(&seen, row.Id)
	if seen.Version != "0.13.0" {
		t.Fatalf("the relay's version is recorded as %q after it answered as 0.13.0", seen.Version)
	}
	if err := s.Delete(v.Id); err != nil {
		t.Fatal(err)
	}
	want := "POST /secret/panel/api/tunnels/withdraw " + v.PairId
	if last := relay.calls[len(relay.calls)-1]; last != want {
		t.Fatalf("last relay call = %q, want %q", last, want)
	}
}

// strongSwan on this server holds UDP 500 and 4500 for its L2TP inbound; a relay forward there
// would take IPsec away from that inbound's users.
func TestTunnelCannotForwardIPsecPortsOfThisServersL2TP(t *testing.T) {
	s := initTunnelDB(t)
	ipsec := relayEnd(8446, tunnel.PortForward{Listen: "500", Proto: tunnel.ProtoBoth})
	if _, err := s.Add(ipsec); err != nil {
		t.Fatalf("UDP 500 with no L2TP inbound here: %v", err)
	}
	if err := database.GetDB().Create(&model.Inbound{Port: 1701, Enable: true, Protocol: model.L2TP, Tag: "l2tp"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(relayEnd(8447, tunnel.PortForward{Listen: "4500", Proto: tunnel.ProtoBoth})); err == nil || !strings.Contains(err.Error(), "port 4500 belongs to this server's L2TP inbound") {
		t.Fatalf("error = %v", err)
	}
}

func TestGroupKeepsItsBackupsAndStickiness(t *testing.T) {
	s := initTunnelDB(t)
	var ids []int
	for i := range 3 {
		v, err := s.Add(TunnelRequest{
			Name: fmt.Sprintf("gre%d", i), Kind: tunnel.KindGRE, Role: tunnel.RoleServer, Enable: true,
			Settings: tunnel.TunnelSettings{RemoteAddr: fmt.Sprintf("198.51.100.%d", 10+i), LocalIP: fmt.Sprintf("10.8%d.0.1/30", i), PeerIP: fmt.Sprintf("10.8%d.0.2", i)},
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, v.Id)
	}
	saved, err := s.SaveGroup(0, TunnelGroupRequest{Name: "ipsec", Mode: tunnel.ModeBalance, Members: ids[:2], Backups: ids[2:], Sticky: true, Enable: true,
		Forwards: []tunnel.PortForward{{Listen: "500", Proto: tunnel.ProtoUDP}, {Listen: "4500", Proto: tunnel.ProtoUDP}}})
	if err != nil {
		t.Fatal(err)
	}
	groups, err := s.ListGroups()
	if err != nil || len(groups) != 1 || !groups[0].Sticky || len(groups[0].Backups) != 1 || groups[0].Backups[0] != ids[2] || groups[0].Id != saved.Id {
		t.Fatalf("groups = %+v (%v)", groups, err)
	}
	if err := s.Delete(ids[2]); err == nil || !strings.Contains(err.Error(), "ipsec") {
		t.Fatalf("deleting a backup the group lists: %v", err)
	}
}
