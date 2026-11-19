package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/tunnel"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// Start runs a test from the other server's link in the background; State follows it.
func (s TunnelBenchService) Start(req TunnelBenchStartRequest) (*TunnelBenchReport, error) {
	return s.state().start(req)
}

func selectCandidates(keys []string) ([]tunnel.BenchCandidate, error) {
	cat := tunnel.BenchCatalogue()
	if len(keys) == 0 {
		return cat, nil
	}
	for _, k := range keys {
		if _, ok := tunnel.BenchCandidateByKey(k); !ok {
			return nil, common.NewErrorf("%q is not a tunnel the test knows", k)
		}
	}
	return slices.DeleteFunc(cat, func(c tunnel.BenchCandidate) bool { return !slices.Contains(keys, c.Key) }), nil
}

func (b *benchState) start(req TunnelBenchStartRequest) (*TunnelBenchReport, error) {
	inv, err := tunnel.ParseBenchInvite(req.Code, b.now())
	if err != nil {
		return nil, common.NewError(err.Error())
	}
	name := req.Profile
	if name == "" {
		name = "standard"
	}
	prof, ok := profileNamed(name)
	if !ok {
		return nil, common.NewErrorf("%q is not a test profile", req.Profile)
	}
	cands, err := selectCandidates(req.Keys)
	if err != nil {
		return nil, err
	}
	if len(cands) == 0 {
		return nil, common.NewError("choose at least one tunnel to test")
	}
	if req.Addr4 != "" || req.Addr6 != "" {
		if err := parseSideAddrs(strings.TrimSpace(req.Addr4), strings.TrimSpace(req.Addr6)); err != nil {
			return nil, common.NewError(err.Error())
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.run != nil || b.host != nil {
		return nil, errBenchBusy
	}
	if err := b.engine.portsFree(inv.Port); err != nil {
		return nil, common.NewError(err.Error() + "; make a new link on the other server")
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &benchRun{cancel: cancel, done: make(chan struct{}), report: TunnelBenchReport{
		Id: inv.ID, State: BenchRunning, Stage: tunnel.BenchStageScreen, Initiator: true, Role: tunnel.Opposite(inv.Role),
		Peer: firstNonEmpty(inv.Addr4, inv.Addr6), Profile: prof.name, Total: len(cands),
		Results: []tunnel.BenchResult{}, Tuning: []tunnel.BenchResult{}, Warnings: []string{}, StartedAt: b.now().UnixMilli(),
	}}
	b.run = run
	d := &benchDriver{
		b: b, run: run, inv: inv, prof: prof, role: run.report.Role, kick: make(chan struct{}, 1),
		peer:  benchPeer{base: inv.URL, token: inv.Token, client: b.client(inv.Pin)},
		addr4: strings.TrimSpace(req.Addr4), addr6: strings.TrimSpace(req.Addr6),
	}
	go d.drive(ctx, cands)
	r := copyReport(run.report)
	return &r, nil
}

// benchDriver runs one test from the server that imported the link.
type benchDriver struct {
	b          *benchState
	run        *benchRun
	inv        tunnel.BenchInvite
	prof       benchProfile
	peer       benchPeer
	role       string
	addr4      string
	addr6      string
	self       tunnel.BenchSide
	other      tunnel.BenchSide
	key        []byte
	net4, net6 netip.Prefix
	selfBP     bool
	peerBP     bool
	kick       chan struct{}
	// greeted and fatal are read by the pusher as well.
	greeted atomic.Bool
	fatalMu sync.Mutex
	fatal   error
}

func (d *benchDriver) setFatal(err error) {
	d.fatalMu.Lock()
	defer d.fatalMu.Unlock()
	if d.fatal == nil {
		d.fatal = err
	}
}

func (d *benchDriver) fatalErr() error {
	d.fatalMu.Lock()
	defer d.fatalMu.Unlock()
	return d.fatal
}

func (d *benchDriver) drive(ctx context.Context, cands []tunnel.BenchCandidate) {
	defer close(d.run.done)
	pushCtx, stopPush := context.WithCancel(ctx)
	pushed := make(chan struct{})
	go func() {
		defer close(pushed)
		d.pusher(pushCtx)
	}()
	err := d.execute(ctx, cands)
	stopPush()
	<-pushed
	state, msg := BenchDone, ""
	switch {
	case ctx.Err() != nil:
		state, msg = BenchCancelled, "cancelled on the server that ran the test"
	case err != nil:
		state, msg = BenchFailed, err.Error()
	}
	d.finish(state, msg)
}

// update changes the report under the state's lock.
func (d *benchDriver) update(change func(r *TunnelBenchReport)) {
	d.b.mu.Lock()
	change(&d.run.report)
	d.b.mu.Unlock()
}

func (d *benchDriver) snapshot() TunnelBenchReport {
	d.b.mu.Lock()
	defer d.b.mu.Unlock()
	return copyReport(d.run.report)
}

func (d *benchDriver) warn(msg string) {
	d.update(func(r *TunnelBenchReport) { r.Warnings = append(r.Warnings, msg) })
}

// pusher keeps the other server's copy of the report current, and the test alive there
// while this server measures on its own.
func (d *benchDriver) pusher(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.kick:
		case <-ticker.C:
		}
		if !d.greeted.Load() {
			continue
		}
		r := d.snapshot()
		if err := d.peer.call(ctx, BenchPeerReport, r, nil); errors.Is(err, errBenchPeerGone) {
			d.setFatal(err)
		}
	}
}

func (d *benchDriver) push() {
	select {
	case d.kick <- struct{}{}:
	default:
	}
}

func (d *benchDriver) setPhase(phase string) {
	d.update(func(r *TunnelBenchReport) { r.Phase = phase })
	d.push()
}

func (d *benchDriver) execute(ctx context.Context, cands []tunnel.BenchCandidate) error {
	self := d.b.self()
	if d.addr4 == "" && d.addr6 == "" {
		d.addr4, d.addr6 = d.b.addrs()
	}
	if d.addr4 == "" && d.addr6 == "" {
		return errors.New("this server's public address could not be found; enter it")
	}
	d.self = tunnel.BenchSide{Role: d.role, Addr4: d.addr4, Addr6: d.addr6}
	d.key = make([]byte, 32)
	_, _ = rand.Read(d.key)
	used := make([]string, 0, len(self.Used))
	for _, p := range self.Used {
		used = append(used, p.String())
	}
	var hello benchHelloReply
	err := d.peer.call(ctx, BenchPeerHello, benchHello{
		ID: d.inv.ID, Version: self.Version, Role: d.role, Addr4: d.addr4, Addr6: d.addr6,
		Key: hex.EncodeToString(d.key), Used: used,
	}, &hello)
	if err != nil {
		return fmt.Errorf("the other server: %w", err)
	}
	d.greeted.Store(true)
	if d.net4, err = netip.ParsePrefix(hello.Net4); err != nil {
		return errors.New("the other server sent no tunnel addresses for the test")
	}
	if d.net6, err = netip.ParsePrefix(hello.Net6); err != nil {
		return errors.New("the other server sent no tunnel addresses for the test")
	}
	d.other = tunnel.BenchSide{Role: d.inv.Role, Addr4: firstNonEmpty(hello.Addr4, d.inv.Addr4), Addr6: firstNonEmpty(hello.Addr6, d.inv.Addr6)}
	d.selfBP, d.peerBP = d.b.engine.hasBackPack(), hello.BackPack
	d.update(func(r *TunnelBenchReport) {
		r.PeerVersion, r.Peer = hello.Version, firstNonEmpty(d.other.Addr4, d.other.Addr6)
		for _, w := range hello.Warnings {
			r.Warnings = append(r.Warnings, "On the other server: "+w)
		}
		if hello.Version != self.Version {
			r.Warnings = append(r.Warnings, fmt.Sprintf("the other server runs Manifold %s and this one %s; update both so they test alike", hello.Version, self.Version))
		}
	})
	if err := d.b.engine.openFirewall(sideAddrs(d.other), d.inv.Port); err != nil {
		d.warn("the firewall of this server could not be opened for the test: " + err.Error())
	}
	defer d.b.engine.closeFirewall()
	if d.role == tunnel.RoleClient {
		n4, n6 := d.net4, d.net6
		srv, err := d.b.engine.listen(d.inv.Port, d.key, func(a netip.Addr) bool { return a.IsLoopback() || n4.Contains(a) || n6.Contains(a) })
		if err != nil {
			return err
		}
		defer srv.Close()
	}
	d.push()

	results := make([]tunnel.BenchResult, len(cands))
	for i := range results {
		results[i] = tunnel.BenchResult{BenchCandidate: cands[i], State: tunnel.BenchPending, Stage: tunnel.BenchStageScreen}
	}
	for i, c := range cands {
		if err := d.stop(ctx); err != nil {
			return err
		}
		results[i] = d.measure(ctx, c, d.prof.screen, tunnel.BenchStageScreen)
		d.record(results)
	}
	tunnel.RankBench(results)
	finalists := pickFinalists(results, d.prof.finalists)
	d.update(func(r *TunnelBenchReport) {
		r.Stage, r.Total = tunnel.BenchStageFull, r.Done+len(finalists)+d.prof.tuned*3
	})
	for _, i := range finalists {
		if err := d.stop(ctx); err != nil {
			return err
		}
		results[i] = d.measure(ctx, results[i].BenchCandidate, d.prof.full, tunnel.BenchStageFull)
		d.record(results)
	}
	final := make([]tunnel.BenchResult, len(finalists))
	for j, i := range finalists {
		final[j] = results[i]
	}
	picks := tunnel.RankBench(final)
	for j, i := range finalists {
		results[i] = final[j]
	}
	d.record(results)

	tuned := map[int]tunnel.BenchResult{}
	d.update(func(r *TunnelBenchReport) { r.Stage = tunnel.BenchStageTune })
	for _, j := range tuneOrder(final, d.prof.tuned) {
		group := []tunnel.BenchResult{final[j]}
		for _, mtu := range final[j].MTUVariants() {
			if err := d.stop(ctx); err != nil {
				return err
			}
			c := final[j].BenchCandidate
			c.MTU = mtu
			group = append(group, d.measure(ctx, c, d.prof.full, tunnel.BenchStageTune))
			d.record(results)
		}
		best := tunnel.RankBench(group).Best
		d.update(func(r *TunnelBenchReport) { r.Tuning = append(r.Tuning, group...) })
		// A smaller MTU must win clearly: two runs of one tunnel differ by a few points anyway.
		if best > 0 && group[best].Score >= group[0].Score+3 {
			tuned[j] = group[best]
		}
	}
	d.update(func(r *TunnelBenchReport) {
		relay := d.other
		if d.role == tunnel.RoleServer {
			relay = d.self
		}
		pick := func(j int) *TunnelBenchPick {
			if j < 0 {
				return nil
			}
			if t, ok := tuned[j]; ok {
				return pickFor(t, final[j].Score, relay, true)
			}
			return pickFor(final[j], final[j].Score, relay, false)
		}
		r.Best, r.BestTCP, r.BestUDP = pick(picks.Best), pick(picks.TCP), pick(picks.UDP)
		if r.BestTCP != nil {
			r.BestTCP.Score = final[picks.TCP].TCPScore
		}
		if r.BestUDP != nil {
			r.BestUDP.Score = final[picks.UDP].UDPScore
		}
		r.Total = r.Done
	})
	if picks.Best < 0 {
		return errors.New("no tunnel carried anything between the two servers; see each tunnel's reason")
	}
	return nil
}

// stop reports why the test must end now: it was cancelled, or the other server left it.
func (d *benchDriver) stop(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return d.fatalErr()
}

func (d *benchDriver) record(results []tunnel.BenchResult) {
	d.update(func(r *TunnelBenchReport) { r.Results = slices.Clone(results) })
	d.push()
}

// pickFinalists is the best few overall, and the best two for TCP and for UDP alone, so
// each pick is made from candidates measured in full.
func pickFinalists(results []tunnel.BenchResult, n int) []int {
	var out []int
	add := func(score func(tunnel.BenchResult) float64, count int) {
		order := make([]int, 0, len(results))
		for i, r := range results {
			if r.State == tunnel.BenchOK && score(r) > 0 {
				order = append(order, i)
			}
		}
		sort.SliceStable(order, func(a, b int) bool { return score(results[order[a]]) > score(results[order[b]]) })
		for _, i := range order[:min(count, len(order))] {
			if !slices.Contains(out, i) {
				out = append(out, i)
			}
		}
	}
	add(func(r tunnel.BenchResult) float64 { return r.Score }, n)
	add(func(r tunnel.BenchResult) float64 { return r.TCPScore }, 2)
	add(func(r tunnel.BenchResult) float64 { return r.UDPScore }, 2)
	slices.Sort(out)
	return out
}

// tuneOrder is the best finalists that have an MTU to tune, best first.
func tuneOrder(final []tunnel.BenchResult, n int) []int {
	var order []int
	for j, r := range final {
		if r.State == tunnel.BenchOK && r.HasInterface() {
			order = append(order, j)
		}
	}
	sort.SliceStable(order, func(a, b int) bool { return final[order[a]].Score > final[order[b]].Score })
	return order[:min(n, len(order))]
}

// endProblem says whether an end that would not start means the candidate cannot run
// here at all (skipped) or that it broke (failed).
func endProblem(err error, where, kind string) (state, msg string) {
	text := err.Error()
	switch {
	case strings.Contains(text, "Unknown device type"):
		return tunnel.BenchSkipped, fmt.Sprintf("the kernel of %s has no %s support", where, kind)
	case strings.Contains(text, "File exists"):
		return tunnel.BenchSkipped, fmt.Sprintf("%s already has a %s link to the other server", where, kind)
	case errors.Is(err, tunnel.ErrNoBackPack) || strings.Contains(text, tunnel.ErrNoBackPack.Error()):
		return tunnel.BenchSkipped, "BackPack is missing on " + where
	}
	return tunnel.BenchFailed, where + ": " + text
}

func (d *benchDriver) measure(ctx context.Context, c tunnel.BenchCandidate, plan tunnel.BenchPlan, stage string) tunnel.BenchResult {
	res := tunnel.BenchResult{BenchCandidate: c, Stage: stage, State: tunnel.BenchOK}
	d.update(func(r *TunnelBenchReport) { r.Current, r.Phase = c.Key, tunnel.BenchPhaseSetup })
	d.push()
	defer d.update(func(r *TunnelBenchReport) { r.Done++ })
	fail := func(state, msg string) tunnel.BenchResult {
		res.State, res.Error = state, msg
		return res
	}
	backpack := c.Kind == tunnel.KindReverse || c.Kind == tunnel.KindL3
	switch {
	case backpack && !d.selfBP:
		return fail(tunnel.BenchSkipped, "BackPack is missing on this server")
	case backpack && !d.peerBP:
		return fail(tunnel.BenchSkipped, "BackPack is missing on the other server")
	}
	n := slices.IndexFunc(tunnel.BenchCatalogue(), func(k tunnel.BenchCandidate) bool { return k.Key == c.Key })
	slot, err := tunnel.BenchSlotAt(n, d.inv.Port, d.net4, d.net6)
	if err != nil {
		return fail(tunnel.BenchFailed, err.Error())
	}
	pairID, token := tunnel.NewPairID(), tunnel.NewToken()
	mine, _, err := tunnel.BuildBenchPair(c, slot, d.self, d.other, pairID, token)
	if errors.Is(err, tunnel.ErrBenchNoIPv6) {
		return fail(tunnel.BenchSkipped, "both servers need a public IPv6 address for this kind")
	}
	if err != nil {
		return fail(tunnel.BenchFailed, err.Error())
	}
	req := benchEndRequest{Op: "start", Slot: n, Key: c.Key, MTU: c.MTU, PairID: pairID, Token: token}
	if err := d.peer.call(ctx, BenchPeerEnd, req, nil); err != nil {
		if errors.Is(err, errBenchPeerGone) {
			d.setFatal(err)
		}
		return fail(endProblem(err, "the other server", c.Kind))
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = d.peer.call(stopCtx, BenchPeerEnd, benchEndRequest{Op: "stop", Slot: n, Key: c.Key}, nil)
	}()
	end, err := d.b.engine.startEnd(mine)
	if err != nil {
		return fail(endProblem(err, "this server", c.Kind))
	}
	defer end.Stop()
	var m tunnel.BenchMetrics
	if d.role == tunnel.RoleServer {
		plan.Dial, plan.Echo = tunnel.BenchTarget(mine, d.inv.Port)
		plan.Key = d.key
		m = d.b.engine.measure(ctx, plan, d.setPhase)
	} else if m, err = d.remoteMeasure(ctx, n, plan); err != nil {
		return fail(tunnel.BenchFailed, "the other server: "+err.Error())
	}
	res.BenchMetrics = m
	if m.Error != "" {
		res.State = tunnel.BenchFailed
		if err := end.Err(); err != nil {
			res.Error += "; on this server " + err.Error()
		}
	}
	d.update(func(r *TunnelBenchReport) { r.Bytes += m.Bytes })
	return res
}

// remoteMeasure has the other server, the relay, measure the candidate and follows it.
func (d *benchDriver) remoteMeasure(ctx context.Context, slot int, plan tunnel.BenchPlan) (tunnel.BenchMetrics, error) {
	if err := d.peer.call(ctx, BenchPeerMeasure, benchMeasureRequest{Op: "start", Slot: slot, Plan: wirePlan(plan)}, nil); err != nil {
		if errors.Is(err, errBenchPeerGone) {
			d.setFatal(err)
		}
		return tunnel.BenchMetrics{}, err
	}
	deadline := time.Now().Add(plan.Setup + 6*plan.Phase + 90*time.Second)
	failures := 0
	for {
		select {
		case <-ctx.Done():
			return tunnel.BenchMetrics{}, ctx.Err()
		case <-time.After(d.b.pollEvery):
		}
		var r benchMeasureReply
		err := d.peer.call(ctx, BenchPeerMeasure, benchMeasureRequest{Op: "poll", Slot: slot}, &r)
		switch {
		case errors.Is(err, errBenchPeerGone):
			d.setFatal(err)
			return tunnel.BenchMetrics{}, err
		case err != nil:
			if failures++; failures >= 10 {
				return tunnel.BenchMetrics{}, err
			}
			continue
		}
		failures = 0
		if r.Phase != "" {
			d.setPhase(r.Phase)
		}
		if r.Done {
			return r.Metrics, nil
		}
		if time.Now().After(deadline) {
			return tunnel.BenchMetrics{}, errors.New("the measurement took too long")
		}
	}
}

// finish keeps the report here and hands it to the other server, which then takes its end down.
func (d *benchDriver) finish(state, msg string) {
	d.b.mu.Lock()
	r := &d.run.report
	r.State, r.Error, r.Current, r.Phase = state, msg, "", ""
	r.FinishedAt = d.b.now().UnixMilli()
	final := copyReport(*r)
	d.b.run, d.b.last = nil, &final
	d.b.mu.Unlock()
	saveBenchReport(final)
	if d.greeted.Load() && !errors.Is(d.fatalErr(), errBenchPeerGone) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = d.peer.call(ctx, BenchPeerClose, benchCloseRequest{State: state, Error: msg, Report: &final}, nil)
	}
}
