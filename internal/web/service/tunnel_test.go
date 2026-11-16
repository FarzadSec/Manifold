package service

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/tunnel"
)

type quietEngine struct{}

func (quietEngine) Reconcile([]tunnel.Tunnel)       {}
func (quietEngine) Statuses() []tunnel.TunnelStatus { return nil }

func initTunnelDB(t *testing.T) *TunnelService {
	t.Helper()
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	prev := tunnelManager
	tunnelManager = func() tunnelEngine { return quietEngine{} }
	t.Cleanup(func() { tunnelManager = prev })
	return &TunnelService{}
}

func relayEnd(port int, forwards ...tunnel.PortForward) TunnelRequest {
	return TunnelRequest{Name: "relay", Kind: tunnel.KindReverse, Role: tunnel.RoleServer, Enable: true,
		Settings: tunnel.TunnelSettings{Transport: "tcpmux", Port: port, Forwards: forwards}}
}

func TestTunnelSaveRefusesWhatAnotherUserHolds(t *testing.T) {
	s := initTunnelDB(t)
	if _, err := s.Add(relayEnd(8443, tunnel.PortForward{Listen: "443"})); err != nil {
		t.Fatal(err)
	}
	gre := TunnelRequest{Name: "gre", Kind: tunnel.KindGRE, Role: tunnel.RoleClient, Enable: true,
		Settings: tunnel.TunnelSettings{RemoteAddr: "198.51.100.7", LocalIP: "10.80.0.2/30", PeerIP: "10.80.0.1"}}
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
		{"overlapping tunnel addresses", TunnelRequest{Name: "gre2", Kind: tunnel.KindGRE, Role: tunnel.RoleClient,
			Settings: tunnel.TunnelSettings{RemoteAddr: "198.51.100.8", LocalIP: "10.80.0.1/29", PeerIP: "10.80.0.3"}}, `overlap those of tunnel "gre"`},
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
	v, err := owner.Add(TunnelRequest{Name: "iran", Kind: tunnel.KindReverse, Role: tunnel.RoleClient, Enable: true,
		Settings: tunnel.TunnelSettings{Transport: "tcpmux", Port: 8443, RemoteAddr: "relay.example.com", Forwards: []tunnel.PortForward{{Listen: "443"}}}})
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
	v, err := s.Add(TunnelRequest{Name: "uk", Kind: tunnel.KindGRE, Role: tunnel.RoleClient, Enable: true, RelayId: row.Id,
		Settings: tunnel.TunnelSettings{RemoteAddr: "198.51.100.7", PublicAddr: "203.0.113.10", LocalIP: "10.80.0.2/30", PeerIP: "10.80.0.1",
			Forwards: []tunnel.PortForward{{Listen: "443"}}}})
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
	if err := s.Delete(v.Id); err != nil {
		t.Fatal(err)
	}
	want := "POST /secret/panel/api/tunnels/withdraw " + v.PairId
	if last := relay.calls[len(relay.calls)-1]; last != want {
		t.Fatalf("last relay call = %q, want %q", last, want)
	}
}
