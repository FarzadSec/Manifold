//go:build linux

package tunnel

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/egress"
)

type benchHelperSpec struct {
	End    Tunnel
	Dir    string
	Binary string
	Plan   BenchPlan
	// From and Base open the relay's firewall to the origin for the run, as the panel does.
	From string
	Base int
}

// TestBenchRelayHelper runs the relay's end of a candidate inside the relay namespace and
// measures it, for TestRealBenchMeasuresEachKindOfTunnel, which starts it there.
func TestBenchRelayHelper(t *testing.T) {
	raw := os.Getenv("XUI_BENCH_HELPER")
	if raw == "" {
		t.Skip("started by TestRealBenchMeasuresEachKindOfTunnel")
	}
	var spec benchHelperSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		t.Fatal(err)
	}
	m := BenchMetrics{}
	if err := egress.OpenInput(BenchFirewallOwner, BenchOpening([]netip.Addr{netip.MustParseAddr(spec.From)}, spec.Base)); err != nil {
		t.Fatal(err)
	}
	defer egress.CloseInput(BenchFirewallOwner)
	end, err := StartBenchEnd(spec.End, spec.Dir, spec.Binary)
	if err != nil {
		m.Error = "relay end: " + err.Error()
	} else {
		defer end.Stop()
		m = Measure(context.Background(), spec.Plan, nil)
	}
	_ = json.NewEncoder(os.Stdout).Encode(m)
	_, _ = io.Copy(io.Discard, os.Stdin)
}

// TestRealBenchMeasuresEachKindOfTunnel runs the tunnel test's measurement, over TCP and
// UDP, through real kernel links and BackPack tunnels of each shape between two namespaces.
func TestRealBenchMeasuresEachKindOfTunnel(t *testing.T) {
	if os.Getenv("XUI_TUNNEL_E2E") == "" {
		t.Skip("set XUI_TUNNEL_E2E=1 and run as root to build real tunnels")
	}
	binary := os.Getenv("XUI_BACKPACK_BIN")
	if binary == "" {
		t.Skip("set XUI_BACKPACK_BIN to a BackPack release binary to run the BackPack candidates")
	}
	if os.Geteuid() != 0 {
		t.Fatal("XUI_TUNNEL_E2E is set but the test is not running as root")
	}
	kernelNetwork(t)
	// The relay's firewall turns away what it did not ask for, as ufw's default does.
	for _, tool := range []string{"iptables", "ip6tables"} {
		run(t, "ip", "netns", "exec", relayNS, tool, "-A", "INPUT", "-i", "lo", "-j", "ACCEPT")
		run(t, "ip", "netns", "exec", relayNS, tool, "-A", "INPUT", "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT")
		run(t, "ip", "netns", "exec", relayNS, tool, "-P", "INPUT", "DROP")
	}
	net4, net6 := netip.MustParsePrefix("10.79.9.0/24"), netip.MustParsePrefix("fd4d:5454:9::/64")
	key := []byte("an end-to-end key for the bench test")
	const base = 46100
	srv, err := ListenBench(base, key, func(a netip.Addr) bool { return a.IsLoopback() || net4.Contains(a) || net6.Contains(a) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	origin := BenchSide{Role: RoleClient, Addr4: "192.168.252.1"}
	relay := BenchSide{Role: RoleServer, Addr4: "192.168.252.2"}

	candidates := []string{
		KindGRE, KindIPIP, KindSIT, KindGRETAP,
		KindReverse + ":tcpmux", KindReverse + ":udp",
		KindL3 + ":udp:" + RoleServer, KindL3 + ":quic:" + RoleClient,
	}
	for n, key := range candidates {
		t.Run(key, func(t *testing.T) {
			c, _ := BenchCandidateByKey(key)
			slot, err := BenchSlotAt(n, base, net4, net6)
			if err != nil {
				t.Fatal(err)
			}
			mine, theirs, err := BuildBenchPair(c, slot, origin, relay, NewPairID(), NewToken())
			if err != nil {
				t.Fatal(err)
			}
			end, err := StartBenchEnd(mine, t.TempDir(), binary)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(end.Stop)
			dial, echo := BenchTarget(theirs, slot.BenchPort)
			spec, _ := json.Marshal(benchHelperSpec{End: theirs, Dir: t.TempDir(), Binary: binary, From: origin.Addr4, Base: base, Plan: BenchPlan{
				Dial: dial, Echo: echo, Key: []byte("an end-to-end key for the bench test"), Setup: 30 * time.Second,
				Phase: time.Second, Cap: 16 << 20, Streams: 2,
			}})
			self, _ := os.Executable()
			helper := exec.CommandContext(context.Background(), "ip", "netns", "exec", relayNS, self, "-test.run", "^TestBenchRelayHelper$")
			helper.Env = append(os.Environ(), "XUI_BENCH_HELPER="+string(spec))
			stdin, _ := helper.StdinPipe()
			stdout, _ := helper.StdoutPipe()
			stderr := &strings.Builder{}
			helper.Stderr = stderr
			if err := helper.Start(); err != nil {
				t.Fatal(err)
			}
			var m BenchMetrics
			found := false
			lines := bufio.NewScanner(stdout)
			for lines.Scan() {
				if strings.HasPrefix(lines.Text(), "{") {
					found = json.Unmarshal(lines.Bytes(), &m) == nil
					break
				}
			}
			_ = stdin.Close()
			_ = helper.Wait()
			if !found {
				t.Fatalf("the relay printed no measurement\n%s", stderr.String())
			}
			// BackPack's udp transport forwards a port over UDP only.
			wantTCP := key != KindReverse+":udp"
			if m.Error != "" || m.TCP != wantTCP || !m.UDP || m.StreamErrors != 0 {
				t.Fatalf("measured %+v; origin end: %v\n%s", m, end.Err(), stderr.String())
			}
			if wantTCP && (m.Warning != "" || m.DownMbps <= 0 || m.UpMbps <= 0 || m.LoadDownMbps <= 0 || m.LoadUpMbps <= 0) {
				t.Fatalf("TCP through %s measured %+v", key, m)
			}
			if m.UDPDownMbps <= 0 || m.UDPUpMbps <= 0 || m.UDPLoss > 0 || m.UDPLatencyMs <= 0 {
				t.Fatalf("UDP through %s measured %+v", key, m)
			}
			if echo != "" && m.Loss != 0 {
				t.Fatalf("lost %v%% of the pings across a veth pair", m.Loss)
			}
			if err := end.Err(); err != nil {
				t.Fatalf("the origin's end stopped: %v", err)
			}
			if rules, err := exec.CommandContext(context.Background(), "ip", "netns", "exec", relayNS, "iptables", "-S", "INPUT").Output(); err != nil || strings.Contains(string(rules), "manifold:"+BenchFirewallOwner) {
				t.Fatalf("the relay's firewall still lets the test in after it (%v):\n%s", err, rules)
			}
			t.Logf("%s: setup %.0f ms, %.2f ms, down %.0f / up %.0f Mbps, loaded %.0f / %.0f Mbps; UDP %.2f ms, %.0f / %.0f Mbps",
				key, m.SetupMs, m.LatencyMs, m.DownMbps, m.UpMbps, m.LoadDownMbps, m.LoadUpMbps, m.UDPLatencyMs, m.UDPDownMbps, m.UDPUpMbps)
		})
	}
}
