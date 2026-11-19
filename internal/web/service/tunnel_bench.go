package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/tunnel"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// States of a tunnel test, as both servers report it.
const (
	BenchWaiting   = "waiting"
	BenchRunning   = "running"
	BenchDone      = "done"
	BenchFailed    = "failed"
	BenchCancelled = "cancelled"
)

// TunnelBenchReport is a tunnel test as this server sees it, under way or finished. Both
// servers hold one; the server that runs the test sends its own to the other as it goes.
type TunnelBenchReport struct {
	Id          string               `json:"id" example:"5d1c0a9e7b2f4c11a0e3d8f6"`
	State       string               `json:"state" example:"running"`
	Stage       string               `json:"stage" example:"screen"`
	Initiator   bool                 `json:"initiator" example:"true"`
	Role        string               `json:"role" example:"client"`
	Peer        string               `json:"peer" example:"198.51.100.7"`
	PeerVersion string               `json:"peerVersion" example:"0.22.0"`
	Profile     string               `json:"profile" example:"standard"`
	Current     string               `json:"current" example:"reverse:tcpmux"`
	Phase       string               `json:"phase" example:"download"`
	Done        int                  `json:"done" example:"12"`
	Total       int                  `json:"total" example:"36"`
	Results     []tunnel.BenchResult `json:"results"`
	Tuning      []tunnel.BenchResult `json:"tuning"`
	Best        *TunnelBenchPick     `json:"best,omitempty"`
	BestTCP     *TunnelBenchPick     `json:"bestTcp,omitempty"`
	BestUDP     *TunnelBenchPick     `json:"bestUdp,omitempty"`
	Warnings    []string             `json:"warnings"`
	Error       string               `json:"error,omitempty" example:""`
	Bytes       int64                `json:"bytes" example:"1073741824"`
	StartedAt   int64                `json:"startedAt" example:"1763300000000"`
	FinishedAt  int64                `json:"finishedAt" example:"1763300900000"`
}

// TunnelBenchPick is a tunnel the test recommends, with the settings it found best, as
// the tunnel form of the panel that serves the accounts (the client end) takes them.
type TunnelBenchPick struct {
	Key       string  `json:"key" example:"l3:udp:client"`
	Kind      string  `json:"kind" example:"l3"`
	Transport string  `json:"transport,omitempty" example:""`
	Carrier   string  `json:"carrier,omitempty" example:"udp"`
	Dialer    string  `json:"dialer,omitempty" example:"client"`
	MTU       int     `json:"mtu,omitempty" example:"1360"`
	Score     float64 `json:"score" example:"92.4"`
	// RemoteAddr is what the client end dials: the relay's address, or empty when it listens.
	RemoteAddr string `json:"remoteAddr,omitempty" example:"198.51.100.7"`
	// Tuned says the MTU came from the tuning round rather than the kind's default.
	Tuned bool `json:"tuned" example:"true"`
}

// TunnelBenchLink is a test link this server made, waiting for the other to import it.
type TunnelBenchLink struct {
	Code    string `json:"code" example:"manifold-tunnel-test://eyJ2Ijox..."`
	Role    string `json:"role" example:"server"`
	Url     string `json:"url" example:"https://relay.example.com:2053/path/"`
	Addr4   string `json:"addr4" example:"198.51.100.7"`
	Addr6   string `json:"addr6" example:""`
	Port    int    `json:"port" example:"62140"`
	Expires int64  `json:"expires" example:"1763301800000"`
}

// TunnelBenchLinkRequest is what making a test link takes; empty fields are detected.
type TunnelBenchLinkRequest struct {
	Role    string `json:"role" example:"server"`
	Url     string `json:"url" example:"https://relay.example.com:2053/path/"`
	Addr4   string `json:"addr4" example:"198.51.100.7"`
	Addr6   string `json:"addr6" example:""`
	Minutes int    `json:"minutes" example:"30"`
}

// TunnelBenchStartRequest starts a test from the other server's link; empty addresses
// of this server are detected, as for a link.
type TunnelBenchStartRequest struct {
	Code    string   `json:"code" example:"manifold-tunnel-test://eyJ2Ijox..."`
	Profile string   `json:"profile" example:"standard"`
	Keys    []string `json:"keys" example:"[\"gre\",\"reverse:tcpmux\"]"`
	Addr4   string   `json:"addr4" example:"203.0.113.10"`
	Addr6   string   `json:"addr6" example:""`
}

// TunnelBenchProfile is one way to run a test: how long and how hard each round pushes.
type TunnelBenchProfile struct {
	Name      string `json:"name" example:"standard"`
	Finalists int    `json:"finalists" example:"5"`
	Tuned     int    `json:"tuned" example:"2"`
	// MaxBytes is the most data the test can move between the two servers, each candidate
	// at its cap; a slower path moves less.
	MaxBytes int64 `json:"maxBytes" example:"6442450944"`
	Minutes  int   `json:"minutes" example:"14"`
}

// TunnelBenchState is what the tunnel test page shows: the test under way or its link,
// the last one finished here, and what a new one can try.
type TunnelBenchState struct {
	Active    *TunnelBenchReport      `json:"active,omitempty"`
	Link      *TunnelBenchLink        `json:"link,omitempty"`
	Last      *TunnelBenchReport      `json:"last,omitempty"`
	Catalogue []tunnel.BenchCandidate `json:"catalogue"`
	Profiles  []TunnelBenchProfile    `json:"profiles"`
}

type benchProfile struct {
	name             string
	screen, full     tunnel.BenchPlan
	finalists, tuned int
}

const mib = 1 << 20

var benchProfiles = []benchProfile{
	{
		name:      "quick",
		screen:    tunnel.BenchPlan{Setup: 12 * time.Second, Phase: time.Second, Cap: 8 * mib, UDPCap: 4 * mib, Pings: 10, UDPPings: 25, UDPSteps: 1},
		full:      tunnel.BenchPlan{Setup: 15 * time.Second, Phase: 3 * time.Second, Cap: 32 * mib, UDPCap: 32 * mib, Streams: 4},
		finalists: 3,
	},
	{
		name:      "standard",
		screen:    tunnel.BenchPlan{Setup: 15 * time.Second, Phase: 1500 * time.Millisecond, Cap: 16 * mib, UDPCap: 8 * mib, Pings: 10, UDPPings: 25, UDPSteps: 1},
		full:      tunnel.BenchPlan{Setup: 15 * time.Second, Phase: 6 * time.Second, Cap: 64 * mib, UDPCap: 64 * mib, Streams: 8},
		finalists: 5, tuned: 2,
	},
	{
		name:      "thorough",
		screen:    tunnel.BenchPlan{Setup: 20 * time.Second, Phase: 2 * time.Second, Cap: 32 * mib, UDPCap: 16 * mib, Pings: 20, UDPPings: 50, UDPSteps: 2},
		full:      tunnel.BenchPlan{Setup: 20 * time.Second, Phase: 10 * time.Second, Cap: 128 * mib, UDPCap: 128 * mib, Streams: 16},
		finalists: 8, tuned: 3,
	},
}

func profileNamed(name string) (benchProfile, bool) {
	i := slices.IndexFunc(benchProfiles, func(p benchProfile) bool { return p.name == name })
	if i < 0 {
		return benchProfile{}, false
	}
	return benchProfiles[i], true
}

// planBytes is the most one measurement moves: a stream each way, the load's streams each
// way, and UDP each way.
func planBytes(p tunnel.BenchPlan) int64 {
	n := 2 * p.Cap
	if p.Streams > 0 {
		n += 2 * p.Cap
	}
	udp := p.UDPCap
	if udp <= 0 {
		udp = p.Cap
	}
	return n + 2*udp
}

// planSeconds is about how long one measurement of a candidate that answers takes.
func planSeconds(p tunnel.BenchPlan) float64 {
	pings, udpPings := 20, 50
	if p.Pings > 0 {
		pings = p.Pings
	}
	if p.UDPPings > 0 {
		udpPings = p.UDPPings
	}
	phases := 2.0
	if p.Streams > 0 {
		phases = 3
	}
	return 4 + float64(pings)*0.1 + float64(udpPings)*0.02 + (phases+2)*p.Phase.Seconds()
}

func (p benchProfile) view(candidates int) TunnelBenchProfile {
	finalists := min(p.finalists+2, candidates)
	runs := p.tuned * 3
	bytes := int64(candidates)*planBytes(p.screen) + int64(finalists+runs)*planBytes(p.full)
	secs := float64(candidates)*planSeconds(p.screen) + float64(finalists+runs)*planSeconds(p.full)
	return TunnelBenchProfile{Name: p.name, Finalists: p.finalists, Tuned: p.tuned, MaxBytes: bytes, Minutes: int(secs/60) + 1}
}

// benchSelf is what a test needs to know of this server; tests substitute it. The
// addresses are looked up only when the admin gave none.
type benchSelf struct {
	Version string
	Edition string
	Used    []netip.Prefix
}

func osBenchAddrs() (string, string) {
	return (&TunnelService{}).PublicAddress(), localIPv6()
}

func osBenchSelf() benchSelf {
	self := benchSelf{Version: config.GetProductVersion(), Edition: "full"}
	if config.IsTunnelEdition() {
		self.Edition = "relay"
	}
	var rows []model.TunnelEnd
	if err := database.GetDB().Find(&rows).Error; err == nil {
		for i := range rows {
			if t, err := tunnelFromRow(&rows[i]); err == nil {
				if p, err := netip.ParsePrefix(t.Settings.LocalIP); err == nil {
					self.Used = append(self.Used, p.Masked())
				}
			}
		}
	}
	return self
}

// localIPv6 is the global IPv6 address this server sends from: connecting a UDP socket
// picks the route's source address and sends nothing.
func localIPv6() string {
	c, err := (&net.Dialer{}).DialContext(context.Background(), "udp6", "[2001:4860:4860::8888]:53")
	if err != nil {
		return ""
	}
	defer c.Close()
	a, ok := netip.AddrFromSlice(c.LocalAddr().(*net.UDPAddr).IP)
	if !ok || !a.IsGlobalUnicast() || netip.MustParsePrefix("fc00::/7").Contains(a) {
		return ""
	}
	return a.String()
}

// benchHost is this server's side of a link it made: waiting, then under test.
type benchHost struct {
	inv     tunnel.BenchInvite
	claimed bool
	key     []byte
	driver  tunnel.BenchSide
	self    tunnel.BenchSide
	net4    netip.Prefix
	net6    netip.Prefix
	server  interface{ Close() error }
	ends    map[int]benchEnd
	built   map[int]tunnel.Tunnel
	job     *benchJob
	seen    time.Time
	report  TunnelBenchReport
}

// benchJob is a measurement this server runs as the relay, for the server that drives.
type benchJob struct {
	slot    int
	done    bool
	phase   string
	metrics tunnel.BenchMetrics
	cancel  func()
}

// benchRun is a test this server runs from the other server's link.
type benchRun struct {
	cancel func()
	report TunnelBenchReport
	done   chan struct{}
}

// benchState is one server's tunnel test: at most one link or one run at a time.
type benchState struct {
	mu     sync.Mutex
	engine benchEngine
	self   func() benchSelf
	addrs  func() (v4, v6 string)
	client func(pin string) *http.Client
	now    func() time.Time
	host   *benchHost
	run    *benchRun
	last   *TunnelBenchReport
	idle   time.Duration
	// pollEvery is how often the driver asks the relay how its measurement goes.
	pollEvery time.Duration
	cleaned   bool
}

var defaultBench = &benchState{
	engine:    osBenchEngine{},
	self:      osBenchSelf,
	addrs:     osBenchAddrs,
	client:    benchHTTPClient,
	now:       time.Now,
	idle:      90 * time.Second,
	pollEvery: time.Second,
}

// TunnelBenchService runs tunnel tests between this server and another.
type TunnelBenchService struct{}

func (TunnelBenchService) state() *benchState { return defaultBench }

// Cleanup takes down what a test this panel never finished left on the host, at start.
func (s TunnelBenchService) Cleanup() { s.state().cleanupOnce() }

func (b *benchState) cleanupOnce() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.cleaned && b.host == nil && b.run == nil {
		b.engine.cleanup()
		b.cleaned = true
	}
}

// Shutdown ends the test under way when the panel stops, telling the other server.
func (s TunnelBenchService) Shutdown() { s.state().shutdown() }

func (b *benchState) shutdown() {
	b.mu.Lock()
	run := b.run
	if b.host != nil {
		b.closeHostLocked(BenchCancelled, "the panel of this server stopped")
	}
	b.mu.Unlock()
	if run != nil {
		run.cancel()
		select {
		case <-run.done:
		case <-time.After(30 * time.Second):
		}
	}
}

// State is the test under way or waiting here, the last one finished, and the choices.
func (s TunnelBenchService) State() TunnelBenchState { return s.state().view() }

func (b *benchState) view() TunnelBenchState {
	b.mu.Lock()
	defer b.mu.Unlock()
	cat := tunnel.BenchCatalogue()
	out := TunnelBenchState{Catalogue: cat}
	for _, p := range benchProfiles {
		out.Profiles = append(out.Profiles, p.view(len(cat)))
	}
	switch {
	case b.run != nil:
		r := copyReport(b.run.report)
		out.Active = &r
	case b.host != nil:
		r := copyReport(b.host.report)
		out.Active = &r
		if !b.host.claimed {
			code, _ := b.host.inv.Code()
			out.Link = &TunnelBenchLink{
				Code: code, Role: b.host.inv.Role, Url: b.host.inv.URL, Addr4: b.host.inv.Addr4, Addr6: b.host.inv.Addr6,
				Port: b.host.inv.Port, Expires: b.host.inv.Expires,
			}
		}
	}
	if b.last != nil {
		r := copyReport(*b.last)
		out.Last = &r
	}
	return out
}

func copyReport(r TunnelBenchReport) TunnelBenchReport {
	r.Results = slices.Clone(r.Results)
	r.Tuning = slices.Clone(r.Tuning)
	r.Warnings = slices.Clone(r.Warnings)
	return r
}

var errBenchBusy = common.NewError("a tunnel test is already under way on this server; cancel it first")

// MakeLink makes a test link for the other server to import, and waits for it.
func (s TunnelBenchService) MakeLink(req TunnelBenchLinkRequest) (*TunnelBenchLink, error) {
	return s.state().makeLink(req)
}

func (b *benchState) makeLink(req TunnelBenchLinkRequest) (*TunnelBenchLink, error) {
	if req.Role != tunnel.RoleClient && req.Role != tunnel.RoleServer {
		return nil, common.NewError("choose whether this server is the panel that serves the accounts or the relay")
	}
	minutes := req.Minutes
	if minutes <= 0 {
		minutes = 30
	}
	if minutes > 24*60 {
		return nil, common.NewError("a test link lasts at most a day")
	}
	addr4, addr6 := strings.TrimSpace(req.Addr4), strings.TrimSpace(req.Addr6)
	if addr4 == "" && addr6 == "" {
		addr4, addr6 = b.addrs()
	}
	panelURL, pin, err := benchPanelURL(req.Url)
	if err != nil {
		return nil, err
	}
	token := make([]byte, 32)
	_, _ = rand.Read(token)
	inv := tunnel.BenchInvite{
		Version: 1, ID: tunnel.NewPairID(), URL: panelURL, Token: hex.EncodeToString(token), Pin: pin,
		Role: req.Role, Addr4: addr4, Addr6: addr6, Expires: b.now().Add(time.Duration(minutes) * time.Minute).UnixMilli(),
	}
	if hn, err := os.Hostname(); err == nil {
		inv.Host = hn
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.run != nil || (b.host != nil && b.host.claimed) {
		return nil, errBenchBusy
	}
	if b.host != nil {
		b.closeHostLocked(BenchCancelled, "replaced by a new link")
	}
	if inv.Port, err = b.freeBase(); err != nil {
		return nil, err
	}
	if err := inv.Check(b.now()); err != nil {
		return nil, common.NewError(err.Error())
	}
	b.host = &benchHost{
		inv: inv, ends: map[int]benchEnd{}, built: map[int]tunnel.Tunnel{}, self: tunnel.BenchSide{Role: inv.Role, Addr4: addr4, Addr6: addr6},
		report: TunnelBenchReport{
			Id: inv.ID, State: BenchWaiting, Role: inv.Role, Results: []tunnel.BenchResult{}, Tuning: []tunnel.BenchResult{},
			Warnings: []string{}, StartedAt: b.now().UnixMilli(),
		},
	}
	go b.watch(b.host)
	code, err := inv.Code()
	if err != nil {
		return nil, err
	}
	return &TunnelBenchLink{Code: code, Role: inv.Role, Url: inv.URL, Addr4: addr4, Addr6: addr6, Port: inv.Port, Expires: inv.Expires}, nil
}

// freeBase picks the first port of a span that nothing on this server uses.
func (b *benchState) freeBase() (int, error) {
	base, err := tunnel.PickBenchBase(b.engine.ephemeralPorts(), b.engine.portsFree)
	if err != nil {
		return 0, common.NewError(err.Error())
	}
	return base, nil
}

// benchPanelURL is how the other server reaches this panel, and its certificate's pin.
func benchPanelURL(requested string) (string, string, error) {
	var settings SettingService
	certFile, _ := settings.GetCertFile()
	raw := strings.TrimSpace(requested)
	if raw == "" {
		scheme := "http"
		if certFile != "" {
			scheme = "https"
		}
		host, _ := settings.GetWebDomain()
		if host == "" {
			host = (&TunnelService{}).PublicAddress()
		}
		if host == "" {
			return "", "", common.NewError("this panel's address could not be found; enter it")
		}
		port, _ := settings.GetPort()
		path, _ := settings.GetBasePath()
		raw = (&url.URL{Scheme: scheme, Host: net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port)), Path: path}).String()
	}
	u, err := normalizeRelayURL(raw)
	if err != nil {
		return "", "", common.NewError("the panel address must be its URL, such as https://relay.example.com:2053/path/")
	}
	pin := ""
	if strings.HasPrefix(u, "https://") && certFile != "" {
		pin = certFilePin(certFile)
	}
	return u, pin, nil
}

func certFilePin(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		return ""
	}
	sum := sha256.Sum256(block.Bytes)
	return base64.StdEncoding.EncodeToString(sum[:])
}

// TokenMatches reports whether a bearer token is the credential of this server's test link.
func (s TunnelBenchService) TokenMatches(token string) bool { return s.state().tokenMatches(token) }

func (b *benchState) tokenMatches(token string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	h := b.host
	if h == nil || token == "" || (!h.claimed && b.now().UnixMilli() > h.inv.Expires) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(h.inv.Token)) == 1
}

// Cancel stops the test under way or withdraws the link waiting here.
func (s TunnelBenchService) Cancel() error { return s.state().cancel() }

func (b *benchState) cancel() error {
	b.mu.Lock()
	switch {
	case b.run != nil:
		run := b.run
		b.mu.Unlock()
		run.cancel()
		<-run.done
		return nil
	case b.host != nil:
		b.closeHostLocked(BenchCancelled, "cancelled on this server")
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()
	return common.NewError("no tunnel test is under way on this server")
}

// watch ends a link nobody imported in time, and a test whose driver went quiet.
func (b *benchState) watch(h *benchHost) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		b.mu.Lock()
		if b.host != h {
			b.mu.Unlock()
			return
		}
		now := b.now()
		switch {
		case !h.claimed && now.UnixMilli() > h.inv.Expires:
			b.host = nil
			b.mu.Unlock()
			return
		case h.claimed && now.Sub(h.seen) > b.idle:
			b.closeHostLocked(BenchFailed, "the other server stopped talking to this one")
			b.mu.Unlock()
			return
		}
		b.mu.Unlock()
	}
}

// closeHostLocked takes down this server's side of a link and keeps its report.
func (b *benchState) closeHostLocked(state, reason string) {
	h := b.host
	if h == nil {
		return
	}
	b.host = nil
	if h.job != nil && !h.job.done {
		h.job.cancel()
	}
	for _, end := range h.ends {
		end.Stop()
	}
	if h.server != nil {
		_ = h.server.Close()
	}
	if h.claimed {
		b.engine.closeFirewall()
	}
	if !h.claimed {
		return
	}
	r := h.report
	if r.State == BenchWaiting || r.State == BenchRunning {
		r.State = state
		if state != BenchDone && r.Error == "" {
			r.Error = reason
		}
	}
	if r.FinishedAt == 0 {
		r.FinishedAt = b.now().UnixMilli()
	}
	r.Current, r.Phase = "", ""
	b.last = &r
	saveBenchReport(r)
}

// saveBenchReport keeps a finished test in this server's history.
func saveBenchReport(r TunnelBenchReport) {
	raw, err := json.Marshal(r)
	if err != nil {
		logger.Warning("tunnel test: ", err)
		return
	}
	row := &model.TunnelBench{
		BenchId: r.Id, Initiator: r.Initiator, Side: r.Role, Peer: r.Peer, State: r.State,
		Report: string(raw), FinishedAt: r.FinishedAt,
	}
	if r.Best != nil {
		row.Best = r.Best.Key
	}
	if err := database.GetDB().Create(row).Error; err != nil {
		logger.Warning("tunnel test: saving the report failed: ", err)
	}
}

// History lists the tests this server took part in, newest first.
func (s TunnelBenchService) History() ([]model.TunnelBench, error) {
	var rows []model.TunnelBench
	err := database.GetDB().Order("id desc").Limit(100).Find(&rows).Error
	return rows, err
}

// Report reads one finished test of the history.
func (s TunnelBenchService) Report(id int) (*TunnelBenchReport, error) {
	row := &model.TunnelBench{}
	if err := database.GetDB().First(row, id).Error; err != nil {
		return nil, err
	}
	var r TunnelBenchReport
	if err := json.Unmarshal([]byte(row.Report), &r); err != nil {
		return nil, fmt.Errorf("tunnel test %d: %w", id, err)
	}
	return &r, nil
}

// Delete removes a test from the history.
func (s TunnelBenchService) Delete(id int) error {
	return database.GetDB().Delete(&model.TunnelBench{}, id).Error
}

// pickFor turns a result into the tunnel the client end would create, given the relay's addresses.
func pickFor(r tunnel.BenchResult, score float64, relay tunnel.BenchSide, tuned bool) *TunnelBenchPick {
	p := &TunnelBenchPick{
		Key: r.Key, Kind: r.Kind, Transport: r.Transport, Carrier: r.Carrier, Dialer: r.Dialer,
		MTU: r.MTU, Score: score, Tuned: tuned,
	}
	addr := relay.Addr4
	if r.NeedsIPv6() {
		addr = relay.Addr6
	}
	if r.Kind != tunnel.KindL3 || r.Dialer == tunnel.RoleClient {
		p.RemoteAddr = addr
	}
	return p
}
