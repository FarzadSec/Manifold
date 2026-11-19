package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/tunnel"
)

// fakeBenchEngine stands in for the host: which ends run, the firewall, the test port,
// and what each candidate measures. The real paths have tests of their own in tunnel.
type fakeBenchEngine struct {
	mu       sync.Mutex
	running  map[string]tunnel.Tunnel
	started  []tunnel.Tunnel
	stopped  int
	refuse   map[string]error
	firewall []netip.Addr
	closed   int
	listened bool
	serving  bool
	metrics  func(t tunnel.Tunnel) tunnel.BenchMetrics
	block    string
}

func newFakeBenchEngine() *fakeBenchEngine {
	return &fakeBenchEngine{running: map[string]tunnel.Tunnel{}, refuse: map[string]error{}}
}

type fakeBenchEnd struct {
	e   *fakeBenchEngine
	dev string
}

func (f fakeBenchEnd) Stop() {
	f.e.mu.Lock()
	defer f.e.mu.Unlock()
	if _, ok := f.e.running[f.dev]; ok {
		delete(f.e.running, f.dev)
		f.e.stopped++
	}
}

func (fakeBenchEnd) Err() error { return nil }

func (e *fakeBenchEngine) startEnd(t tunnel.Tunnel) (benchEnd, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.refuse[t.Kind]; err != nil {
		return nil, err
	}
	e.running[t.DevName] = t
	e.started = append(e.started, t)
	return fakeBenchEnd{e, t.DevName}, nil
}

type fakeCloser struct{ e *fakeBenchEngine }

func (c fakeCloser) Close() error {
	c.e.mu.Lock()
	c.e.serving = false
	c.e.mu.Unlock()
	return nil
}

func (e *fakeBenchEngine) listen(int, []byte, func(netip.Addr) bool) (io.Closer, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.listened, e.serving = true, true
	return fakeCloser{e}, nil
}

// measure answers for the end that runs, as the relay measures one candidate at a time.
func (e *fakeBenchEngine) measure(ctx context.Context, _ tunnel.BenchPlan, phase func(string)) tunnel.BenchMetrics {
	e.mu.Lock()
	var t tunnel.Tunnel
	for _, r := range e.running {
		t = r
	}
	e.mu.Unlock()
	phase(tunnel.BenchPhaseDownload)
	if e.block != "" && t.Name == "test "+e.block {
		<-ctx.Done()
		return tunnel.BenchMetrics{Error: "cancelled"}
	}
	return e.metrics(t)
}

func (e *fakeBenchEngine) openFirewall(peer []netip.Addr, _ int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.firewall = peer
	return nil
}

func (e *fakeBenchEngine) closeFirewall() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed++
}

func (*fakeBenchEngine) portsFree(int) error { return nil }
func (*fakeBenchEngine) hasBackPack() bool   { return true }
func (*fakeBenchEngine) cleanup()            {}

// pathMetrics is a path between the two servers as each tunnel would carry it.
func pathMetrics(t tunnel.Tunnel) tunnel.BenchMetrics {
	both := func(tcp, udp float64) tunnel.BenchMetrics {
		return tunnel.BenchMetrics{
			TCP: true, UDP: true, LatencyMs: 40, DownMbps: tcp, UpMbps: tcp, LoadDownMbps: tcp, LoadUpMbps: tcp, Streams: 4,
			UDPLatencyMs: 40, UDPDownMbps: udp, UDPUpMbps: udp, Bytes: 1 << 20,
		}
	}
	switch strings.TrimPrefix(t.Name, "test ") {
	case "gre":
		return both(100, 80)
	case "l3:udp:client":
		if t.Settings.MTU == 1360 {
			return both(190, 150)
		}
		return both(150, 120)
	case "reverse:tcpmux":
		m := both(160, 20)
		m.UDPLoss = 30
		return m
	case "reverse:udp":
		return tunnel.BenchMetrics{UDP: true, UDPLatencyMs: 38, UDPDownMbps: 200, UDPUpMbps: 200, Warning: "TCP: no TCP connection crossed the tunnel"}
	}
	return tunnel.BenchMetrics{Error: "the tunnel carried no connection within 15s"}
}

func benchSelfOf() benchSelf { return benchSelf{Version: "0.22.0", Edition: "full"} }

func addrsAt(v4 string) func() (string, string) { return func() (string, string) { return v4, "" } }

// peerServer serves a host's peer operations the way the panel's API does for the bench token.
func peerServer(t *testing.T, host *benchState) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		op, ok := strings.CutPrefix(r.URL.Path, "/panel/api/tunnels/bench/peer/")
		token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || !host.tokenMatches(token) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		obj, err := host.peer(op, body)
		if errors.Is(err, ErrTunnelBenchGone) {
			w.WriteHeader(http.StatusGone)
		}
		out := map[string]any{"success": err == nil, "obj": obj}
		if err != nil {
			out["msg"] = err.Error()
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newBenchPair(t *testing.T) (relay, origin *benchState, relayEngine, originEngine *fakeBenchEngine, link *TunnelBenchLink) {
	t.Helper()
	initTunnelDB(t)
	relayEngine, originEngine = newFakeBenchEngine(), newFakeBenchEngine()
	relayEngine.metrics = pathMetrics
	relay = &benchState{
		engine: relayEngine, self: benchSelfOf, addrs: addrsAt("198.51.100.7"), now: time.Now, idle: time.Minute,
		pollEvery: 5 * time.Millisecond,
	}
	srv := peerServer(t, relay)
	origin = &benchState{
		engine: originEngine, self: benchSelfOf, addrs: addrsAt("203.0.113.10"), now: time.Now, idle: time.Minute,
		pollEvery: 5 * time.Millisecond,
		client:    func(string) *http.Client { return srv.Client() },
	}
	link, err := relay.makeLink(TunnelBenchLinkRequest{Role: tunnel.RoleServer, Url: srv.URL + "/"})
	if err != nil {
		t.Fatal(err)
	}
	return relay, origin, relayEngine, originEngine, link
}

func waitBench(t *testing.T, b *benchState) TunnelBenchReport {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		run, last := b.run, b.last
		b.mu.Unlock()
		if run == nil && last != nil {
			return *last
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the test did not finish")
	return TunnelBenchReport{}
}

// A link made on the relay and run from the origin: every candidate is tried, the best are
// measured in full and tuned, and both servers end with the same picks and nothing running.
func TestTunnelBenchRunsEveryCandidateAndPicksTheBest(t *testing.T) {
	relay, origin, relayEngine, originEngine, link := newBenchPair(t)
	relayEngine.refuse[tunnel.KindIPIP] = errors.New("tunnel: ip link add mfx4 type ipip: exit status 2 (Error: Unknown device type.)")

	keys := []string{"gre", "ipip", "reverse:tcpmux", "reverse:udp", "l3:udp:client"}
	// The origin is behind a mapped address: the one given wins over the one detected.
	req := TunnelBenchStartRequest{Code: link.Code, Profile: "standard", Keys: keys, Addr4: "203.0.113.20"}
	if _, err := origin.start(req); err != nil {
		t.Fatal(err)
	}
	got := waitBench(t, origin)

	if got.State != BenchDone || !got.Initiator || got.Role != tunnel.RoleClient || got.Peer != "198.51.100.7" {
		t.Fatalf("report %+v", got)
	}
	want := map[string]string{"best": "l3:udp:client", "tcp": "reverse:tcpmux", "udp": "reverse:udp"}
	if got.Best == nil || got.BestTCP == nil || got.BestUDP == nil ||
		got.Best.Key != want["best"] || got.BestTCP.Key != want["tcp"] || got.BestUDP.Key != want["udp"] {
		t.Fatalf("picks %+v %+v %+v, want %v", got.Best, got.BestTCP, got.BestUDP, want)
	}
	// The smaller MTU carried more, so the pick takes it, with the relay to dial.
	if got.Best.MTU != 1360 || !got.Best.Tuned || got.Best.RemoteAddr != "198.51.100.7" || got.Best.Dialer != tunnel.RoleClient {
		t.Fatalf("best %+v, want l3 dialled from the client at the relay with MTU 1360", got.Best)
	}
	byKey := map[string]tunnel.BenchResult{}
	for _, r := range got.Results {
		byKey[r.Key] = r
	}
	if r := byKey["ipip"]; r.State != tunnel.BenchSkipped || r.Error != "the kernel of the other server has no ipip support" {
		t.Fatalf("ipip %s: %q", r.State, r.Error)
	}
	if r := byKey["l3:udp:client"]; r.State != tunnel.BenchOK || r.Stage != tunnel.BenchStageFull {
		t.Fatalf("the best was not measured in full: %+v", r)
	}
	// Tuning holds each tuned tunnel at its own MTU and the ones tried, scored against each other.
	var tuned []string
	for _, r := range got.Tuning {
		if r.Key == "l3:udp:client" {
			tuned = append(tuned, strconv.Itoa(r.MTU)+"="+strconv.FormatFloat(r.Score, 'f', -1, 64))
		}
	}
	if len(tuned) != 4 || tuned[0] == "0=0" || !strings.HasPrefix(tuned[2], "1360=100") {
		t.Fatalf("l3's tuning rows %v, want the default and three MTUs, 1360 the best", tuned)
	}

	// The relay holds the same outcome as its own and has nothing of the test running.
	relayLast := waitBench(t, relay)
	if relayLast.Initiator || relayLast.Role != tunnel.RoleServer || relayLast.Peer != "203.0.113.20" ||
		relayLast.State != BenchDone || relayLast.Best == nil || relayLast.Best.Key != want["best"] {
		t.Fatalf("relay's report %+v", relayLast)
	}
	for name, e := range map[string]*fakeBenchEngine{"relay": relayEngine, "origin": originEngine} {
		if len(e.running) != 0 || e.stopped != len(e.started) || e.closed == 0 {
			t.Fatalf("%s: %d ends still run, %d of %d stopped, firewall closed %d times", name, len(e.running), e.stopped, len(e.started), e.closed)
		}
	}
	if !originEngine.listened || originEngine.serving || relayEngine.listened {
		t.Fatalf("the origin answers measurements, the relay makes them: %v %v %v", originEngine.listened, originEngine.serving, relayEngine.listened)
	}
	if fw := relayEngine.firewall; len(fw) != 1 || fw[0].String() != "203.0.113.20" {
		t.Fatalf("the relay's firewall was opened for %v", fw)
	}
	if relay.tokenMatches(strings.Repeat("0", 64)) || relay.host != nil {
		t.Fatal("the relay still holds the link after the test")
	}
	var rows []model.TunnelBench
	database.GetDB().Order("id").Find(&rows)
	if len(rows) != 2 || rows[0].Initiator == rows[1].Initiator || rows[0].Best != want["best"] || rows[1].Best != want["best"] {
		t.Fatalf("history rows %+v", rows)
	}
}

// The link's token reaches the test once: a second server cannot take it over.
func TestTunnelBenchLinkServesOneServer(t *testing.T) {
	relay, _, _, _, link := newBenchPair(t)
	inv, err := tunnel.ParseBenchInvite(link.Code, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	hello := func(key string) error {
		raw, _ := json.Marshal(benchHello{ID: inv.ID, Role: tunnel.RoleClient, Addr4: "203.0.113.10", Key: key})
		_, err := relay.peer(BenchPeerHello, raw)
		return err
	}
	first, second := strings.Repeat("ab", 32), strings.Repeat("cd", 32)
	if err := hello(first); err != nil {
		t.Fatal(err)
	}
	if err := hello(first); err != nil {
		t.Fatalf("the same server greeting again was refused: %v", err)
	}
	if err := hello(second); err == nil || !strings.Contains(err.Error(), "used by another server") {
		t.Fatalf("another server took the link: %v", err)
	}
	// An end outside the catalogue's slot for its kind is refused before anything runs.
	raw, _ := json.Marshal(benchEndRequest{Op: "start", Slot: 7, Key: "gre", PairID: tunnel.NewPairID(), Token: tunnel.NewToken()})
	if _, err := relay.peer(BenchPeerEnd, raw); err == nil || !strings.Contains(err.Error(), "not at slot 7") {
		t.Fatalf("an end at another candidate's slot: %v", err)
	}
}

// A driver that goes quiet leaves nothing running on the other server.
func TestTunnelBenchEndsWhenTheOtherServerGoesQuiet(t *testing.T) {
	relay, _, relayEngine, _, link := newBenchPair(t)
	relay.idle = 50 * time.Millisecond
	inv, _ := tunnel.ParseBenchInvite(link.Code, time.Now())
	raw, _ := json.Marshal(benchHello{ID: inv.ID, Role: tunnel.RoleClient, Addr4: "203.0.113.10", Key: strings.Repeat("ab", 32)})
	if _, err := relay.peer(BenchPeerHello, raw); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(benchEndRequest{Op: "start", Slot: 0, Key: "gre", PairID: tunnel.NewPairID(), Token: tunnel.NewToken()})
	if _, err := relay.peer(BenchPeerEnd, raw); err != nil {
		t.Fatal(err)
	}
	last := waitBench(t, relay)
	if last.State != BenchFailed || !strings.Contains(last.Error, "stopped talking") {
		t.Fatalf("report %+v", last)
	}
	if len(relayEngine.running) != 0 || relayEngine.closed != 1 {
		t.Fatalf("after the driver went quiet %d ends run, firewall closed %d times", len(relayEngine.running), relayEngine.closed)
	}
}

// Cancelled on the relay, the test stops on the origin too, with the reason.
func TestTunnelBenchCancelledOnTheOtherServerStopsTheDriver(t *testing.T) {
	relay, origin, relayEngine, originEngine, link := newBenchPair(t)
	relayEngine.block = "reverse:tcpmux"
	if _, err := origin.start(TunnelBenchStartRequest{Code: link.Code, Profile: "quick", Keys: []string{"gre", "reverse:tcpmux", "l3:udp:client"}}); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); ; {
		if s := origin.view(); s.Active != nil && s.Active.Current == "reverse:tcpmux" && s.Active.Phase == tunnel.BenchPhaseDownload {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the test never reached the blocked candidate")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := relay.cancel(); err != nil {
		t.Fatal(err)
	}
	got := waitBench(t, origin)
	if got.State != BenchFailed || !strings.Contains(got.Error, "no longer holds this test") {
		t.Fatalf("origin's report %+v", got)
	}
	if len(originEngine.running) != 0 || originEngine.serving {
		t.Fatal("the origin kept its side of the test")
	}
	if relayLast := waitBench(t, relay); relayLast.State != BenchCancelled {
		t.Fatalf("relay's report %+v", relayLast)
	}
}
