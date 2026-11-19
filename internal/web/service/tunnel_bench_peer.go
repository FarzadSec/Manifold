package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/tunnel"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
)

// Operations the server that runs a test asks of the other, at /tunnels/bench/peer/<op>.
const (
	BenchPeerHello   = "hello"
	BenchPeerEnd     = "end"
	BenchPeerMeasure = "measure"
	BenchPeerReport  = "report"
	BenchPeerClose   = "close"
)

// benchHello is how the server that imported a link introduces itself.
type benchHello struct {
	ID      string   `json:"id"`
	Version string   `json:"version"`
	Role    string   `json:"role"`
	Addr4   string   `json:"addr4"`
	Addr6   string   `json:"addr6"`
	Key     string   `json:"key"`
	Used    []string `json:"used"`
}

type benchHelloReply struct {
	Version  string   `json:"version"`
	Edition  string   `json:"edition"`
	Addr4    string   `json:"addr4"`
	Addr6    string   `json:"addr6"`
	BackPack bool     `json:"backpack"`
	Net4     string   `json:"net4"`
	Net6     string   `json:"net6"`
	Warnings []string `json:"warnings"`
}

// benchEndRequest names a candidate by its place in the catalogue and gives the secrets
// of its two ends; the receiving server builds its end itself.
type benchEndRequest struct {
	Op     string `json:"op"`
	Slot   int    `json:"slot"`
	Key    string `json:"key"`
	MTU    int    `json:"mtu"`
	PairID string `json:"pairId"`
	Token  string `json:"token"`
}

type benchPlanWire struct {
	SetupMs  int64 `json:"setupMs"`
	PhaseMs  int64 `json:"phaseMs"`
	Cap      int64 `json:"cap"`
	UDPCap   int64 `json:"udpCap"`
	Streams  int   `json:"streams"`
	Pings    int   `json:"pings"`
	UDPPings int   `json:"udpPings"`
	UDPSteps int   `json:"udpSteps"`
}

func wirePlan(p tunnel.BenchPlan) benchPlanWire {
	return benchPlanWire{
		SetupMs: p.Setup.Milliseconds(), PhaseMs: p.Phase.Milliseconds(), Cap: p.Cap, UDPCap: p.UDPCap,
		Streams: p.Streams, Pings: p.Pings, UDPPings: p.UDPPings, UDPSteps: p.UDPSteps,
	}
}

// plan refuses a measurement larger than any profile asks for.
func (w benchPlanWire) plan() (tunnel.BenchPlan, error) {
	if w.SetupMs < 1000 || w.SetupMs > 60_000 || w.PhaseMs < 100 || w.PhaseMs > 30_000 ||
		w.Cap < 0 || w.Cap > 1<<30 || w.UDPCap < 0 || w.UDPCap > 1<<30 || w.Streams < 0 || w.Streams > 32 ||
		w.Pings < 0 || w.Pings > 100 || w.UDPPings < 0 || w.UDPPings > 200 || w.UDPSteps < 0 || w.UDPSteps > 5 {
		return tunnel.BenchPlan{}, errors.New("the measurement asked for is outside what a test does")
	}
	return tunnel.BenchPlan{
		Setup: time.Duration(w.SetupMs) * time.Millisecond, Phase: time.Duration(w.PhaseMs) * time.Millisecond,
		Cap: w.Cap, UDPCap: w.UDPCap, Streams: w.Streams, Pings: w.Pings, UDPPings: w.UDPPings, UDPSteps: w.UDPSteps,
	}, nil
}

type benchMeasureRequest struct {
	Op   string        `json:"op"`
	Slot int           `json:"slot"`
	Plan benchPlanWire `json:"plan"`
}

type benchMeasureReply struct {
	Done    bool                `json:"done"`
	Phase   string              `json:"phase"`
	Metrics tunnel.BenchMetrics `json:"metrics"`
}

type benchCloseRequest struct {
	State  string             `json:"state"`
	Error  string             `json:"error"`
	Report *TunnelBenchReport `json:"report,omitempty"`
}

// ErrTunnelBenchGone answers a peer call for a test this server no longer holds.
var ErrTunnelBenchGone = common.NewError("this server holds no tunnel test for that link")

// Peer answers one operation of the server that runs the test, for its bench token.
func (s TunnelBenchService) Peer(op string, body []byte) (any, error) {
	return s.state().peer(op, body)
}

func (b *benchState) peer(op string, body []byte) (any, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	h := b.host
	if h == nil {
		return nil, ErrTunnelBenchGone
	}
	if op != BenchPeerHello && !h.claimed {
		return nil, common.NewError("the test has not started")
	}
	h.seen = b.now()
	switch op {
	case BenchPeerHello:
		var req benchHello
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, err
		}
		return b.helloLocked(h, req)
	case BenchPeerEnd:
		var req benchEndRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, err
		}
		return struct{}{}, b.endLocked(h, req)
	case BenchPeerMeasure:
		var req benchMeasureRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, err
		}
		return b.measureLocked(h, req)
	case BenchPeerReport:
		var r TunnelBenchReport
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, err
		}
		h.report = mirrorReport(r, h)
		return struct{}{}, nil
	case BenchPeerClose:
		var req benchCloseRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, err
		}
		if req.Report != nil {
			h.report = mirrorReport(*req.Report, h)
		}
		state := req.State
		if state != BenchDone && state != BenchCancelled {
			state = BenchFailed
		}
		b.closeHostLocked(state, req.Error)
		return struct{}{}, nil
	}
	return nil, common.NewErrorf("unknown tunnel test operation %q", op)
}

// mirrorReport is the driver's report as this server tells it: its own role, the other
// server as the peer, and never as the server that ran the test.
func mirrorReport(r TunnelBenchReport, h *benchHost) TunnelBenchReport {
	r.Id, r.Initiator, r.Role = h.inv.ID, false, h.inv.Role
	r.Peer = firstNonEmpty(h.driver.Addr4, h.driver.Addr6)
	if r.Results == nil {
		r.Results = []tunnel.BenchResult{}
	}
	if r.Tuning == nil {
		r.Tuning = []tunnel.BenchResult{}
	}
	if r.Warnings == nil {
		r.Warnings = []string{}
	}
	return r
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func parseSideAddrs(addr4, addr6 string) error {
	if addr4 == "" && addr6 == "" {
		return errors.New("the other server sent no address")
	}
	if a, err := netip.ParseAddr(addr4); addr4 != "" && (err != nil || !a.Is4()) {
		return fmt.Errorf("%q is not an IPv4 address", addr4)
	}
	if a, err := netip.ParseAddr(addr6); addr6 != "" && (err != nil || !a.Is6() || a.Is4In6()) {
		return fmt.Errorf("%q is not an IPv6 address", addr6)
	}
	return nil
}

func sideAddrs(s tunnel.BenchSide) []netip.Addr {
	var out []netip.Addr
	for _, raw := range []string{s.Addr4, s.Addr6} {
		if a, err := netip.ParseAddr(raw); err == nil {
			out = append(out, a)
		}
	}
	return out
}

// benchNets picks the test's tunnel addresses where neither server has a tunnel.
func benchNets(used []netip.Prefix) (netip.Prefix, netip.Prefix, error) {
	for x := range 256 {
		n4 := netip.PrefixFrom(netip.AddrFrom4([4]byte{10, 79, byte(x), 0}), 24)
		n6 := netip.PrefixFrom(netip.AddrFrom16([16]byte{0xfd, 0x4d, 0x54, 0x54, 0, byte(x)}), 64)
		if !slices.ContainsFunc(used, func(p netip.Prefix) bool { return p.Overlaps(n4) || p.Overlaps(n6) }) {
			return n4, n6, nil
		}
	}
	return netip.Prefix{}, netip.Prefix{}, errors.New("no free tunnel addresses are left in 10.79.0.0/16 for the test")
}

func (b *benchState) helloLocked(h *benchHost, req benchHello) (*benchHelloReply, error) {
	if req.ID != h.inv.ID {
		return nil, ErrTunnelBenchGone
	}
	key, err := hex.DecodeString(req.Key)
	if err != nil || len(key) < 16 {
		return nil, errors.New("the test's key is missing")
	}
	if h.claimed {
		if subtle.ConstantTimeCompare(key, h.key) != 1 {
			return nil, common.NewError("this link was used by another server already")
		}
	} else {
		if req.Role != tunnel.Opposite(h.inv.Role) {
			return nil, common.NewError("both servers took the same side; make the link again with the other side")
		}
		if err := parseSideAddrs(req.Addr4, req.Addr6); err != nil {
			return nil, err
		}
		self := b.self()
		used := slices.Clone(self.Used)
		for _, raw := range req.Used {
			if p, err := netip.ParsePrefix(raw); err == nil {
				used = append(used, p)
			}
		}
		if h.net4, h.net6, err = benchNets(used); err != nil {
			return nil, err
		}
		if err := b.engine.portsFree(h.inv.Port); err != nil {
			return nil, err
		}
		h.key, h.claimed = key, true
		h.driver = tunnel.BenchSide{Role: req.Role, Addr4: req.Addr4, Addr6: req.Addr6}
		if err := b.engine.openFirewall(sideAddrs(h.driver), h.inv.Port); err != nil {
			h.report.Warnings = append(h.report.Warnings, "the firewall of "+describeSide(h.inv.Role)+" could not be opened for the test: "+err.Error())
		}
		if h.inv.Role == tunnel.RoleClient {
			n4, n6 := h.net4, h.net6
			srv, err := b.engine.listen(h.inv.Port, key, func(a netip.Addr) bool { return a.IsLoopback() || n4.Contains(a) || n6.Contains(a) })
			if err != nil {
				h.claimed = false
				b.engine.closeFirewall()
				return nil, err
			}
			h.server = srv
		}
		h.report.State, h.report.PeerVersion = BenchRunning, req.Version
		h.report.Peer = firstNonEmpty(req.Addr4, req.Addr6)
	}
	self := b.self()
	return &benchHelloReply{
		Version: self.Version, Edition: self.Edition, Addr4: h.self.Addr4, Addr6: h.self.Addr6,
		BackPack: b.engine.hasBackPack(), Net4: h.net4.String(), Net6: h.net6.String(), Warnings: slices.Clone(h.report.Warnings),
	}, nil
}

func describeSide(role string) string {
	if role == tunnel.RoleClient {
		return "the panel that serves the accounts"
	}
	return "the relay"
}

// benchCandidateAt checks that a slot is the candidate's place in the catalogue, the one
// place its ports and addresses may come from.
func benchCandidateAt(slot int, key string, mtu int) (tunnel.BenchCandidate, error) {
	cat := tunnel.BenchCatalogue()
	if slot < 0 || slot >= len(cat) || cat[slot].Key != key {
		return tunnel.BenchCandidate{}, fmt.Errorf("candidate %q is not at slot %d of the test", key, slot)
	}
	c := cat[slot]
	if mtu != 0 && !slices.Contains(c.MTUVariants(), mtu) {
		return tunnel.BenchCandidate{}, fmt.Errorf("MTU %d is not one the test tries for %s", mtu, key)
	}
	c.MTU = mtu
	return c, nil
}

func validBenchSecret(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil && len(s) >= 32 && len(s) <= 128
}

func (b *benchState) endLocked(h *benchHost, req benchEndRequest) error {
	if h.job != nil && h.job.slot == req.Slot && !h.job.done {
		h.job.cancel()
	}
	if prev := h.ends[req.Slot]; prev != nil {
		prev.Stop()
		delete(h.ends, req.Slot)
		delete(h.built, req.Slot)
	}
	if req.Op == "stop" {
		return nil
	}
	if req.Op != "start" {
		return fmt.Errorf("unknown end operation %q", req.Op)
	}
	c, err := benchCandidateAt(req.Slot, req.Key, req.MTU)
	if err != nil {
		return err
	}
	if !validBenchSecret(req.Token) {
		return errors.New("the candidate's secret is missing")
	}
	slot, err := tunnel.BenchSlotAt(req.Slot, h.inv.Port, h.net4, h.net6)
	if err != nil {
		return err
	}
	mine, _, err := tunnel.BuildBenchPair(c, slot, h.self, h.driver, req.PairID, req.Token)
	if err != nil {
		return err
	}
	end, err := b.engine.startEnd(mine)
	if err != nil {
		return err
	}
	h.ends[req.Slot], h.built[req.Slot] = end, mine
	return nil
}

func (b *benchState) measureLocked(h *benchHost, req benchMeasureRequest) (*benchMeasureReply, error) {
	if h.inv.Role != tunnel.RoleServer {
		return nil, errors.New("this server is the origin; the relay measures")
	}
	switch req.Op {
	case "start":
		end, built := h.ends[req.Slot], h.built[req.Slot]
		if end == nil {
			return nil, fmt.Errorf("slot %d has no end running", req.Slot)
		}
		plan, err := req.Plan.plan()
		if err != nil {
			return nil, err
		}
		if h.job != nil && !h.job.done {
			h.job.cancel()
		}
		plan.Dial, plan.Echo = tunnel.BenchTarget(built, h.inv.Port)
		plan.Key = h.key
		ctx, cancel := context.WithCancel(context.Background())
		job := &benchJob{slot: req.Slot, cancel: cancel}
		h.job = job
		go func() {
			defer cancel()
			m := b.engine.measure(ctx, plan, func(phase string) {
				b.mu.Lock()
				job.phase = phase
				b.mu.Unlock()
			})
			b.mu.Lock()
			job.metrics, job.done = m, true
			b.mu.Unlock()
		}()
		return &benchMeasureReply{}, nil
	case "poll":
		job := h.job
		if job == nil || job.slot != req.Slot {
			return nil, fmt.Errorf("no measurement of slot %d is under way", req.Slot)
		}
		return &benchMeasureReply{Done: job.done, Phase: job.phase, Metrics: job.metrics}, nil
	}
	return nil, fmt.Errorf("unknown measurement operation %q", req.Op)
}

// benchPeer is the other server's panel as the server that runs the test reaches it.
type benchPeer struct {
	base   string
	token  string
	client *http.Client
}

var errBenchPeerGone = errors.New("the other server no longer holds this test: it was cancelled there or its link expired")

func (p benchPeer) call(ctx context.Context, op string, in, out any) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+"panel/api/tunnels/bench/peer/"+op, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("the other panel cannot be reached at %s: %w", p.base, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	switch {
	case resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusUnauthorized ||
		resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound:
		return errBenchPeerGone
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("the other panel answered %s", resp.Status)
	}
	var msg struct {
		Success bool            `json:"success"`
		Msg     string          `json:"msg"`
		Obj     json.RawMessage `json:"obj"`
	}
	if err := json.Unmarshal(body, &msg); err != nil {
		return errors.New("the other server did not answer as a Manifold panel")
	}
	if !msg.Success {
		return errors.New(msg.Msg)
	}
	if out != nil && len(msg.Obj) > 0 && string(msg.Obj) != "null" {
		return json.Unmarshal(msg.Obj, out)
	}
	return nil
}

// benchHTTPClient trusts the certificate the link pinned, which may be self-signed, or
// one the system trusts for the address, as behind a proxy with a certificate of its own.
func benchHTTPClient(pin string) *http.Client {
	want, _ := runtime.DecodeCertPin(pin)
	tlsCfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, // lgtm[go/disabled-certificate-check] VerifyConnection checks the pin or the chain
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("the other panel presented no certificate")
			}
			if len(want) == sha256.Size {
				sum := sha256.Sum256(cs.PeerCertificates[0].Raw)
				if subtle.ConstantTimeCompare(sum[:], want) == 1 {
					return nil
				}
			}
			inter := x509.NewCertPool()
			for _, c := range cs.PeerCertificates[1:] {
				inter.AddCert(c)
			}
			if _, err := cs.PeerCertificates[0].Verify(x509.VerifyOptions{DNSName: cs.ServerName, Intermediates: inter}); err != nil {
				return fmt.Errorf("the other panel's certificate is neither the one in the link nor one this server trusts: %w", err)
			}
			return nil
		},
	}
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig:     tlsCfg,
			DialContext:         (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
			MaxIdleConnsPerHost: 2,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
