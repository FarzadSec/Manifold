package tunnel

import (
	"context"
	"errors"
	"io"
	"math"
	"net"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func benchFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func loopbackOnly(a netip.Addr) bool { return a.IsLoopback() }

func TestMeasureMovesDataBothWaysThroughTheBenchServer(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	port := benchFreePort(t)
	srv, err := ListenBench(port, key, loopbackOnly)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	const capBytes = 2 << 20
	var phases []string
	m := Measure(context.Background(), BenchPlan{
		Dial: "127.0.0.1:" + strconv.Itoa(port), Key: key, Setup: 3 * time.Second,
		Phase: 400 * time.Millisecond, Cap: capBytes, Streams: 2,
	}, func(p string) { phases = append(phases, p) })

	if m.Error != "" || m.Warning != "" || m.StreamErrors != 0 {
		t.Fatalf("measurement failed: %+v", m)
	}
	if want := []string{BenchPhaseSetup, BenchPhaseLatency, BenchPhaseDownload, BenchPhaseUpload, BenchPhaseLoad, BenchPhaseUDP}; strings.Join(phases, ",") != strings.Join(want, ",") {
		t.Fatalf("phases %v, want %v", phases, want)
	}
	// Loopback fills every cap inside the phase: one stream each way, then the cap again under load.
	if m.Bytes < 4*capBytes {
		t.Fatalf("moved %d bytes, want at least %d", m.Bytes, 4*capBytes)
	}
	for name, v := range map[string]float64{"down": m.DownMbps, "up": m.UpMbps, "load down": m.LoadDownMbps, "load up": m.LoadUpMbps, "latency": m.LatencyMs} {
		if v <= 0 {
			t.Fatalf("%s is %v: %+v", name, v, m)
		}
	}
	if m.Loss != 0 || m.Streams != 2 {
		t.Fatalf("loss %v and streams %d, want 0 and 2", m.Loss, m.Streams)
	}
}

// wholeCloseProxy carries a connection the way BackPack's muxed transports do: when
// either side finishes sending, both directions close, so no half-close crosses it.
func wholeCloseProxy(t *testing.T, target string) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			in, err := l.Accept()
			if err != nil {
				return
			}
			out, err := net.Dial("tcp", target)
			if err != nil {
				_ = in.Close()
				continue
			}
			done := make(chan struct{}, 2)
			pipe := func(dst, src net.Conn) {
				_, _ = io.Copy(dst, src)
				done <- struct{}{}
			}
			go pipe(out, in)
			go pipe(in, out)
			go func() {
				<-done
				_ = in.Close()
				_ = out.Close()
			}()
		}
	}()
	return l.Addr().String()
}

func TestMeasureUploadsThroughATunnelThatCarriesNoHalfClose(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	port := benchFreePort(t)
	srv, err := ListenBench(port, key, loopbackOnly)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	dial := wholeCloseProxy(t, "127.0.0.1:"+strconv.Itoa(port))

	m := Measure(context.Background(), BenchPlan{
		Dial: dial, Key: key, Setup: 3 * time.Second, Phase: 300 * time.Millisecond, Cap: 1 << 20, Streams: 2,
	}, nil)
	// The proxy carries no UDP, so the only warning may be UDP's.
	if !m.TCP || m.Error != "" || m.StreamErrors != 0 || !strings.HasPrefix(m.Warning, "UDP: ") {
		t.Fatalf("measurement through the proxy failed: %+v", m)
	}
	if m.UpMbps <= 0 || m.LoadUpMbps <= 0 {
		t.Fatalf("no upload measured through the proxy: %+v", m)
	}
}

// udpOnlyForward forwards UDP to target on a port where nothing answers TCP, the way
// BackPack's udp transport forwards a port.
func udpOnlyForward(t *testing.T, target string) string {
	t.Helper()
	in, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close() })
	dst, _ := net.ResolveUDPAddr("udp", target)
	var mu sync.Mutex
	upstream := map[string]*net.UDPConn{}
	go func() {
		buf := make([]byte, 65535)
		for {
			n, from, err := in.ReadFrom(buf)
			if err != nil {
				return
			}
			mu.Lock()
			out := upstream[from.String()]
			if out == nil {
				if out, err = net.DialUDP("udp", nil, dst); err != nil {
					mu.Unlock()
					continue
				}
				upstream[from.String()] = out
				t.Cleanup(func() { _ = out.Close() })
				go func(out *net.UDPConn, back net.Addr) {
					reply := make([]byte, 65535)
					for {
						n, err := out.Read(reply)
						if err != nil {
							return
						}
						_, _ = in.WriteTo(reply[:n], back)
					}
				}(out, from)
			}
			mu.Unlock()
			_, _ = out.Write(buf[:n])
		}
	}()
	return in.LocalAddr().String()
}

func TestMeasureCarriesUDPBesideTCP(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	port := benchFreePort(t)
	srv, err := ListenBench(port, key, loopbackOnly)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	m := Measure(context.Background(), BenchPlan{
		Dial: "127.0.0.1:" + strconv.Itoa(port), Key: key, Setup: 3 * time.Second,
		Phase: 600 * time.Millisecond, Cap: 4 << 20, Streams: 2,
	}, nil)
	if !m.TCP || !m.UDP || m.Error != "" || m.Warning != "" {
		t.Fatalf("got %+v, want both TCP and UDP carried", m)
	}
	if m.UDPLatencyMs <= 0 || m.UDPUpMbps <= 0 || m.UDPDownMbps <= 0 {
		t.Fatalf("UDP not measured: %+v", m)
	}
	if m.UDPLoss != 0 {
		t.Fatalf("lost %v%% of UDP echoes over loopback", m.UDPLoss)
	}
}

func TestMeasureReportsATunnelThatCarriesOnlyUDP(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	port := benchFreePort(t)
	srv, err := ListenBench(port, key, loopbackOnly)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	start := time.Now()
	m := Measure(context.Background(), BenchPlan{
		Dial: udpOnlyForward(t, "127.0.0.1:"+strconv.Itoa(port)), Key: key, Setup: 20 * time.Second,
		Phase: 600 * time.Millisecond, Cap: 4 << 20, Streams: 2,
	}, nil)
	if m.TCP || !m.UDP || m.Error != "" || m.UDPUpMbps <= 0 || m.UDPDownMbps <= 0 {
		t.Fatalf("got %+v, want a UDP-only result", m)
	}
	// TCP is given a short grace once UDP answers, not the whole setup time.
	if took := time.Since(start); took > 15*time.Second {
		t.Fatalf("a UDP-only tunnel took %s to measure", took)
	}
}

func TestBenchServerAnswersOnlyTheTestThroughATestAddress(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	for _, tc := range []struct {
		name  string
		key   []byte
		allow func(netip.Addr) bool
	}{
		{"wrong key", []byte("another key that is not the test"), loopbackOnly},
		{"address outside the test", key, func(netip.Addr) bool { return false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			port := benchFreePort(t)
			srv, err := ListenBench(port, key, tc.allow)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = srv.Close() })
			m := Measure(context.Background(), BenchPlan{
				Dial: "127.0.0.1:" + strconv.Itoa(port), Key: tc.key, Setup: time.Second, Phase: 100 * time.Millisecond,
			}, nil)
			if !strings.Contains(m.Error, "carried no connection") || m.Bytes != 0 || m.TCP || m.UDP {
				t.Fatalf("got %+v, want the tunnel refused", m)
			}
		})
	}
}

func benchSides() (relay, origin BenchSide) {
	return BenchSide{Role: RoleServer, Addr4: "198.51.100.7", Addr6: "2001:db8::7"},
		BenchSide{Role: RoleClient, Addr4: "203.0.113.10", Addr6: "2001:db8::10"}
}

func testSlot(t *testing.T, n int) BenchSlot {
	t.Helper()
	slot, err := BenchSlotAt(n, 40000, netip.MustParsePrefix("10.79.3.0/24"), netip.MustParsePrefix("fd4d:5454::/64"))
	if err != nil {
		t.Fatal(err)
	}
	return slot
}

// TestBuildBenchPairJoinsTheRelayToTheOriginsTestPort builds every candidate from each
// side and follows the relay's target to the origin's end.
func TestBuildBenchPairJoinsTheRelayToTheOriginsTestPort(t *testing.T) {
	relaySide, originSide := benchSides()
	for n, c := range BenchCatalogue() {
		for _, fromRelay := range []bool{true, false} {
			name := c.Key + map[bool]string{true: " from the relay", false: " from the origin"}[fromRelay]
			t.Run(name, func(t *testing.T) {
				slot := testSlot(t, n)
				self, peer := originSide, relaySide
				if fromRelay {
					self, peer = relaySide, originSide
				}
				mine, theirs, err := BuildBenchPair(c, slot, self, peer, NewPairID(), NewToken())
				if err != nil {
					t.Fatal(err)
				}
				relay, origin := theirs, mine
				if fromRelay {
					relay, origin = mine, theirs
				}
				if relay.Role != RoleServer || origin.Role != RoleClient || relay.DevName != BenchDevice(n) || origin.DevName != relay.DevName {
					t.Fatalf("ends %s/%s on %s/%s", relay.Role, origin.Role, relay.DevName, origin.DevName)
				}
				dial, echo := BenchTarget(relay, slot.BenchPort)
				host, port, _ := net.SplitHostPort(dial)
				switch {
				case c.Kind == KindReverse:
					if dial != net.JoinHostPort("127.0.0.1", strconv.Itoa(slot.ForwardPort)) || echo != "" {
						t.Fatalf("relay dials %s and pings %q", dial, echo)
					}
					if f := relay.Settings.Forwards[0]; f.Target != strconv.Itoa(slot.BenchPort) || f.Proto != ProtoBoth {
						t.Fatalf("the relay forwards %s to %s, want both protocols to the test port %d", f.Proto, f.Target, slot.BenchPort)
					}
					if origin.Settings.RemoteAddr != relaySide.Addr4 || relay.Settings.Port != slot.TunnelPort {
						t.Fatalf("the origin dials %s:%d", origin.Settings.RemoteAddr, relay.Settings.Port)
					}
				default:
					originAddr, _ := netip.ParsePrefix(origin.Settings.LocalIP)
					if host != originAddr.Addr().String() || port != strconv.Itoa(slot.BenchPort) || echo != host {
						t.Fatalf("relay dials %s and pings %s, the origin's tunnel address is %s", dial, echo, originAddr)
					}
					if relay.Settings.PeerIP != originAddr.Addr().String() || origin.Settings.PeerIP == relay.Settings.PeerIP {
						t.Fatalf("tunnel addresses %s/%s and %s/%s", relay.Settings.LocalIP, relay.Settings.PeerIP, origin.Settings.LocalIP, origin.Settings.PeerIP)
					}
				}
				switch {
				case IsKernel(c.Kind):
					wantRelay, wantOrigin := originSide.Addr4, relaySide.Addr4
					if c.NeedsIPv6() {
						wantRelay, wantOrigin = originSide.Addr6, relaySide.Addr6
					}
					if relay.Settings.RemoteAddr != wantRelay || origin.Settings.RemoteAddr != wantOrigin {
						t.Fatalf("remote addresses %s and %s", relay.Settings.RemoteAddr, origin.Settings.RemoteAddr)
					}
				case c.Kind == KindL3:
					dialer, listener := relay, origin
					if c.Dialer == RoleClient {
						dialer, listener = origin, relay
					}
					if listener.Settings.RemoteAddr != "" || dialer.Settings.RemoteAddr == "" {
						t.Fatalf("dialer has %q, listener has %q", dialer.Settings.RemoteAddr, listener.Settings.RemoteAddr)
					}
				}
			})
		}
	}
}

// The server that imported the link sends a candidate, a slot and the secrets; the other
// builds its own end from them, which must be the end the first would have built for it.
func TestBenchEndsAreTheSameBuiltFromEitherServer(t *testing.T) {
	relaySide, originSide := benchSides()
	for n, c := range BenchCatalogue() {
		for _, mtu := range []int{0, 1360} {
			if mtu > 0 && !c.HasInterface() {
				continue
			}
			c.MTU = mtu
			pairID, token := NewPairID(), NewToken()
			_, forOrigin, err := BuildBenchPair(c, testSlot(t, n), relaySide, originSide, pairID, token)
			if err != nil {
				t.Fatal(err)
			}
			origin, forRelay, err := BuildBenchPair(c, testSlot(t, n), originSide, relaySide, pairID, token)
			if err != nil {
				t.Fatal(err)
			}
			relay, _, _ := BuildBenchPair(c, testSlot(t, n), relaySide, originSide, pairID, token)
			if !reflect.DeepEqual(forOrigin, origin) || !reflect.DeepEqual(forRelay, relay) {
				t.Fatalf("%s mtu %d: the ends differ by who built them\norigin %+v\n   vs %+v\nrelay  %+v\n   vs %+v",
					c.Key, mtu, forOrigin, origin, forRelay, relay)
			}
		}
	}
}

func TestBuildBenchPairSkipsIPv6KindsWithoutIPv6(t *testing.T) {
	relaySide, originSide := benchSides()
	originSide.Addr6 = ""
	c, _ := BenchCandidateByKey(KindIP6GRE)
	if _, _, err := BuildBenchPair(c, testSlot(t, 2), relaySide, originSide, NewPairID(), NewToken()); !errors.Is(err, ErrBenchNoIPv6) {
		t.Fatalf("got %v, want ErrBenchNoIPv6", err)
	}
	c, _ = BenchCandidateByKey(KindGRE)
	if _, _, err := BuildBenchPair(c, testSlot(t, 0), relaySide, originSide, NewPairID(), NewToken()); err != nil {
		t.Fatalf("an IPv4 kind refused: %v", err)
	}
}

func TestBenchSlotsShareNoPortOrAddress(t *testing.T) {
	ports := map[int]int{}
	var prefixes []netip.Prefix
	for n := range BenchSlots {
		s := testSlot(t, n)
		for _, p := range []int{s.TunnelPort, s.ForwardPort} {
			if prev, taken := ports[p]; taken || p == s.BenchPort || p >= s.BenchPort+BenchPortSpan {
				t.Fatalf("slot %d port %d is taken by slot %d or outside the span", n, p, prev)
			}
			ports[p] = n
		}
		for _, other := range prefixes {
			if other.Overlaps(s.Inner4) || other.Overlaps(s.Inner6) {
				t.Fatalf("slot %d addresses %s %s overlap %s", n, s.Inner4, s.Inner6, other)
			}
		}
		if !netip.MustParsePrefix("10.79.3.0/24").Contains(s.Inner4.Addr()) {
			t.Fatalf("slot %d left the test's /24: %s", n, s.Inner4)
		}
		prefixes = append(prefixes, s.Inner4, s.Inner6)
	}
	if _, err := BenchSlotAt(BenchSlots, 40000, netip.MustParsePrefix("10.79.3.0/24"), netip.MustParsePrefix("fd4d:5454::/64")); err == nil {
		t.Fatal("a slot past the span was given")
	}
	if len(BenchCatalogue()) > BenchSlots {
		t.Fatalf("the catalogue has %d candidates, more than the %d slots", len(BenchCatalogue()), BenchSlots)
	}
}

func TestRankBenchPrefersAStablePathOverAFasterLossyOne(t *testing.T) {
	tcp := func(down, loss float64, broken int) BenchResult {
		return BenchResult{State: BenchOK, BenchMetrics: BenchMetrics{
			TCP: true, DownMbps: down, UpMbps: down, LoadDownMbps: 1.5 * down, LoadUpMbps: 1.5 * down,
			LatencyMs: 40, LoadLatencyMs: 50, Streams: 8, Loss: loss, StreamErrors: broken,
		}}
	}
	failed := BenchResult{State: BenchFailed, Score: 99, BenchMetrics: BenchMetrics{TCP: true, DownMbps: 1000}}
	results := []BenchResult{failed, tcp(100, 30, 0), tcp(100, 0, 5), tcp(80, 0, 0)}
	picks := RankBench(results)
	if picks.Best != 3 || picks.TCP != 3 || picks.UDP != -1 {
		t.Fatalf("picks %+v with scores %v %v %v %v, want the stable path", picks, results[0].TCPScore, results[1].TCPScore, results[2].TCPScore, results[3].TCPScore)
	}
	if results[0].Score != 0 || results[0].TCPScore != 0 {
		t.Fatalf("a failed candidate scored %v", results[0].Score)
	}
	if results[3].TCPScore >= 100 || results[3].TCPScore <= results[1].TCPScore {
		t.Fatalf("scores %v (stable) and %v (lossy)", results[3].TCPScore, results[1].TCPScore)
	}
	if picks := RankBench([]BenchResult{failed}); picks.Best != -1 || picks.TCP != -1 {
		t.Fatalf("a run with nothing working picked %+v", picks)
	}
}

// A tunnel for both kinds of inbound wins overall, while the fastest of each kind is
// still named for a server that runs only that kind.
func TestRankBenchNamesTheBestForEachProtocolAndOverall(t *testing.T) {
	fastTCPOnly := BenchResult{State: BenchOK, BenchMetrics: BenchMetrics{
		TCP: true, DownMbps: 120, UpMbps: 120, LoadDownMbps: 180, LoadUpMbps: 180, LatencyMs: 40, Streams: 8,
	}}
	both := BenchResult{State: BenchOK, BenchMetrics: BenchMetrics{
		TCP: true, DownMbps: 100, UpMbps: 100, LoadDownMbps: 150, LoadUpMbps: 150, LatencyMs: 40, Streams: 8,
		UDP: true, UDPLatencyMs: 40, UDPDownMbps: 60, UDPUpMbps: 60,
	}}
	fastUDPOnly := BenchResult{State: BenchOK, BenchMetrics: BenchMetrics{
		UDP: true, UDPLatencyMs: 38, UDPDownMbps: 200, UDPUpMbps: 200,
	}}
	lossyUDP := BenchResult{State: BenchOK, BenchMetrics: BenchMetrics{
		UDP: true, UDPLatencyMs: 38, UDPDownMbps: 250, UDPUpMbps: 250, UDPLoss: 40,
	}}
	results := []BenchResult{fastTCPOnly, both, fastUDPOnly, lossyUDP}
	picks := RankBench(results)
	if picks.Best != 1 || picks.TCP != 0 || picks.UDP != 2 {
		t.Fatalf("picks %+v; scores %v/%v/%v/%v", picks, results[0].Score, results[1].Score, results[2].Score, results[3].Score)
	}
	if results[0].UDPScore != 0 || results[2].TCPScore != 0 {
		t.Fatalf("a protocol a tunnel did not carry scored: %v %v", results[0].UDPScore, results[2].TCPScore)
	}
}

func TestBenchInviteSurvivesTheLinkAndRefusesABadOne(t *testing.T) {
	now := time.UnixMilli(1_763_500_000_000)
	inv := BenchInvite{
		Version: 1, ID: NewPairID(), URL: "https://relay.example.com:2053/path/", Token: strings.Repeat("ab", 32),
		Role: RoleServer, Addr4: "198.51.100.7", Port: 41000, Expires: now.Add(time.Hour).UnixMilli(),
	}
	code, err := inv.Code()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseBenchInvite(code, now)
	if err != nil || got != inv {
		t.Fatalf("round trip gave %+v, %v", got, err)
	}
	for name, change := range map[string]func(*BenchInvite){
		"expired":          func(i *BenchInvite) { i.Expires = now.Add(-time.Second).UnixMilli() },
		"ports past 65535": func(i *BenchInvite) { i.Port = 65500 },
		"short token":      func(i *BenchInvite) { i.Token = "abc" },
		"ftp panel":        func(i *BenchInvite) { i.URL = "ftp://relay.example.com/" },
		"no address":       func(i *BenchInvite) { i.Addr4 = "" },
		"v6 as v4":         func(i *BenchInvite) { i.Addr4 = "2001:db8::1" },
		"unknown role":     func(i *BenchInvite) { i.Role = "both" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := inv
			change(&bad)
			code, _ := bad.Code()
			if _, err := ParseBenchInvite(code, now); err == nil {
				t.Fatalf("accepted %+v", bad)
			}
		})
	}
	if _, err := ParseBenchInvite(strings.Replace(code, BenchScheme, PairingScheme, 1), now); err == nil {
		t.Fatal("accepted a pairing code as a test link")
	}
}

func TestBenchLinksStayOutOfTheTunnelJobsWay(t *testing.T) {
	f := &fakeHost{files: map[string]string{}}
	useHost(t, f)
	relaySide, originSide := benchSides()
	c, _ := BenchCandidateByKey(KindGRE)
	mine, _, err := BuildBenchPair(c, testSlot(t, 3), relaySide, originSide, NewPairID(), NewToken())
	if err != nil {
		t.Fatal(err)
	}
	end, err := StartBenchEnd(mine, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	add := f.runs[0]
	if !strings.HasPrefix(add, "ip link add mfx3 type gre remote 203.0.113.10") || !strings.Contains(add, " key ") {
		t.Fatalf("first command %q", add)
	}
	alias := "alias " + benchAliasPrefix + mine.PairID
	if !strings.Contains(strings.Join(f.runs, "\n"), alias) {
		t.Fatalf("the link was not marked as the test's: %v", f.runs)
	}

	// The kernel now holds the test link beside one of the panel's own and an interface of another program.
	f.files[sysNet("mfx3", "ifalias")] = benchAliasPrefix + mine.PairID
	f.files[sysNet("mft1", "ifalias")] = aliasPrefix + "abc"
	f.files[sysNet("mfx9", "ifalias")] = "someone else's"
	listing := "4: mfx3@NONE: <POINTOPOINT> mtu 1476\n5: mft1@NONE: <POINTOPOINT> mtu 1476\n6: mfx9: <BROADCAST> mtu 1500"
	host = &listingHost{fakeHost: f, listing: listing}
	if links := panelLinks(); len(links) != 1 || links[1] != "mft1" {
		t.Fatalf("the tunnel job would sweep %v", links)
	}

	f.runs = nil
	end.Stop()
	if strings.Join(f.runs, "\n") != "ip link del dev mfx3" {
		t.Fatalf("stop ran %v", f.runs)
	}
	f.runs = nil
	RemoveBenchLeftovers(t.TempDir(), "/nonexistent/backpack")
	if got := strings.Join(f.runs, "\n"); got != "ip -o link show\nip link del dev mfx3" {
		t.Fatalf("leftover cleanup ran %q, want only the test's link removed", got)
	}
}

// listingHost answers `ip -o link show` with a fixed listing.
type listingHost struct {
	*fakeHost
	listing string
}

func (h *listingHost) run(name string, args ...string) (string, error) {
	out, err := h.fakeHost.run(name, args...)
	if strings.Join(args, " ") == "-o link show" {
		return h.listing, nil
	}
	return out, err
}

// pathOf answers a UDP step the way a path of the given capacity in Mbps does.
func pathOf(capacity float64, budgetUsed *int64, durs *[]time.Duration) func(float64, time.Duration) (udpStep, error) {
	return func(rate float64, dur time.Duration) (udpStep, error) {
		got := min(rate, capacity)
		bytes := int64(rate * 1e6 / 8 * dur.Seconds())
		*budgetUsed += bytes
		*durs = append(*durs, dur)
		return udpStep{mbps: got, loss: math.Round((1-got/rate)*1000) / 10, bytes: bytes}, nil
	}
}

// A tight data budget shortens the steps rather than the rate, so a fast path is still
// told apart from a slow one; the budget itself is never passed.
func TestUDPLadderFindsThePathsRateWithinItsBudget(t *testing.T) {
	for _, tc := range []struct {
		capacity float64
		budget   int64
	}{{80, 16 << 20}, {300, 16 << 20}, {300, 64 << 20}, {900, 64 << 20}} {
		var used int64
		var durs []time.Duration
		best, _, err := udpLadder(context.Background(), 100, tc.budget, 3, 2*time.Second, pathOf(tc.capacity, &used, &durs))
		if err != nil {
			t.Fatal(err)
		}
		// The budget shared by the steps, at the shortest step, is the fastest it can tell.
		if measurable := float64(tc.budget/4) * 8 / 0.25 / 1e6; best.mbps < 0.9*min(tc.capacity, measurable) {
			t.Errorf("a %v Mbps path with %d MiB measured %v Mbps (steps %v)", tc.capacity, tc.budget>>20, best.mbps, durs)
		}
		if used > tc.budget {
			t.Errorf("a %v Mbps path used %d bytes of a %d budget", tc.capacity, used, tc.budget)
		}
	}
}
