package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	core "github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"
	"golang.org/x/net/proxy"

	"github.com/mhsanaei/3x-ui/v3/internal/speedlimit"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

// speedTestConfig is a panel-shaped config: a socks inbound serving two users,
// a copyable direct outbound first, a WireGuard outbound and a balancer.
func speedTestConfig(t *testing.T, socksPort int, strategy string) *xray.Config {
	t.Helper()
	outbounds := []any{
		map[string]any{
			"tag": "direct", "protocol": "freedom", "settings": map[string]any{"redirect": ""},
			"streamSettings": map[string]any{"sockopt": map[string]any{"domainStrategy": "UseIP"}},
		},
		map[string]any{"tag": "warp", "protocol": "wireguard", "settings": map[string]any{
			"secretKey": "cFzzLXZkPN4mG1HMf2eeSsPT1B9/Si0VQ6Lkc26ZDEQ=", "noKernelTun": true,
			"peers": []any{map[string]any{"publicKey": "bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo=", "endpoint": "127.0.0.1:1"}},
		}},
		map[string]any{"tag": "blocked", "protocol": "blackhole", "settings": map[string]any{}},
	}
	rules := []any{
		map[string]any{"type": "field", "ip": []any{"10.255.0.0/16"}, "outboundTag": "blocked"},
		map[string]any{"type": "field", "domain": []any{"full:warp.test"}, "outboundTag": "warp", "ruleTag": "to-warp"},
		map[string]any{"type": "field", "port": "7777", "balancerTag": "pool"},
		map[string]any{"type": "field", "user": []any{"fast@x"}, "port": "8888", "outboundTag": "direct"},
		map[string]any{"type": "field", "inboundTag": []any{"other"}, "outboundTag": "direct"},
	}
	routing := map[string]any{
		"domainStrategy": strategy,
		"rules":          rules,
		"balancers":      []any{map[string]any{"tag": "pool", "selector": []any{"dir"}}},
	}
	return &xray.Config{
		LogConfig:       mustJSON(t, map[string]any{"loglevel": "warning"}),
		RouterConfig:    mustJSON(t, routing),
		OutboundConfigs: mustJSON(t, outbounds),
		InboundConfigs: []xray.InboundConfig{{
			Listen:   mustJSON(t, "127.0.0.1"),
			Port:     socksPort,
			Protocol: "socks",
			Settings: mustJSON(t, map[string]any{"auth": "password", "udp": true, "accounts": []any{
				map[string]any{"user": "slow@x", "pass": "p"},
				map[string]any{"user": "fast@x", "pass": "p"},
			}}),
			Tag: "in-socks",
		}},
	}
}

type parsedSpeedConfig struct {
	rules     []map[string]any
	outbounds map[string]map[string]any
	inbounds  map[string]xray.InboundConfig
}

func parseSpeedConfig(t *testing.T, cfg *xray.Config) parsedSpeedConfig {
	t.Helper()
	var routing struct {
		Rules []map[string]any `json:"rules"`
	}
	if err := json.Unmarshal(cfg.RouterConfig, &routing); err != nil {
		t.Fatal(err)
	}
	var outbounds []map[string]any
	if err := json.Unmarshal(cfg.OutboundConfigs, &outbounds); err != nil {
		t.Fatal(err)
	}
	p := parsedSpeedConfig{rules: routing.Rules, outbounds: map[string]map[string]any{}, inbounds: map[string]xray.InboundConfig{}}
	for _, ob := range outbounds {
		p.outbounds[ob["tag"].(string)] = ob
	}
	for _, ib := range cfg.InboundConfigs {
		p.inbounds[ib.Tag] = ib
	}
	return p
}

func markOf(ob map[string]any) string {
	stream, _ := ob["streamSettings"].(map[string]any)
	sockopt, _ := stream["sockopt"].(map[string]any)
	if f, ok := sockopt["mark"].(float64); ok {
		return strconv.FormatInt(int64(f), 10)
	}
	return fmt.Sprint(sockopt["mark"])
}

func TestInjectSpeedLimitsKeepsTheAdminsRoutingForTheLimitedUser(t *testing.T) {
	cfg := speedTestConfig(t, 1080, "AsIs")
	injectSpeedLimits(cfg, map[string]int{"slow@x": 5, "nobody@x": 6})
	p := parseSpeedConfig(t, cfg)

	wantMark := strconv.FormatUint(uint64(speedlimit.MarkBase|5), 10)
	direct := p.outbounds["~speed~5~direct"]
	if direct == nil || direct["protocol"] != "freedom" || markOf(direct) != wantMark {
		t.Fatalf("copy of direct = %v, want a freedom outbound with mark %s", direct, wantMark)
	}
	if sockopt := direct["streamSettings"].(map[string]any)["sockopt"].(map[string]any); sockopt["domainStrategy"] != "UseIP" {
		t.Fatalf("copy dropped the outbound's own socket options: %v", sockopt)
	}
	for _, target := range []string{"warp", "pool"} {
		via := p.outbounds["~speed~5~via~"+target]
		if via == nil || via["protocol"] != "socks" || markOf(via) != wantMark {
			t.Fatalf("hop to %s = %v, want a marked socks outbound", target, via)
		}
		hop, ok := p.inbounds["~speed~in~"+target]
		port := via["settings"].(map[string]any)["port"]
		if !ok || fmt.Sprint(hop.Port) != fmt.Sprint(port) {
			t.Fatalf("hop inbound for %s = %+v, want it listening on %v", target, hop, port)
		}
	}
	if _, ok := p.outbounds["~speed~5~warp"]; ok {
		t.Fatal("a WireGuard outbound was copied; two copies would fight over one peer session")
	}
	if _, ok := p.outbounds["~speed~6~direct"]; ok {
		t.Fatal("a user no inbound serves got outbounds")
	}

	type route struct{ user, target, extra string }
	var got []route
	for _, r := range p.rules {
		target, _ := r["outboundTag"].(string)
		if b, _ := r["balancerTag"].(string); b != "" {
			target = "balancer:" + b
		}
		user := fmt.Sprint(r["user"])
		extra := fmt.Sprintf("%v %v", r["inboundTag"], r["ruleTag"])
		got = append(got, route{user, target, extra})
	}
	want := []route{
		{"<nil>", "balancer:pool", "[~speed~in~pool] <nil>"},
		{"<nil>", "warp", "[~speed~in~warp] <nil>"},
		{"<nil>", "blocked", "<nil> <nil>"},
		{"[slow@x]", "~speed~5~via~warp", "<nil> ~speed~5~to-warp"},
		{"<nil>", "warp", "<nil> to-warp"},
		{"[slow@x]", "~speed~5~via~pool", "<nil> <nil>"},
		{"<nil>", "balancer:pool", "<nil> <nil>"},
		{"[fast@x]", "direct", "<nil> <nil>"},
		{"<nil>", "direct", "[other] <nil>"},
		{"[slow@x]", "~speed~5~direct", "<nil> <nil>"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("rules:\n%v\nwant:\n%v", got, want)
	}
}

// Under IPIfNonMatch the catch-all must not answer the first pass, or a domain the
// admin routes by its address would skip the second pass for the limited user.
func TestInjectSpeedLimitsCatchAllWaitsForTheAddressPass(t *testing.T) {
	cfg := speedTestConfig(t, 1080, "IPIfNonMatch")
	injectSpeedLimits(cfg, map[string]int{"slow@x": 5})
	p := parseSpeedConfig(t, cfg)
	last := p.rules[len(p.rules)-1]
	if last["outboundTag"] != "~speed~5~direct" || fmt.Sprint(last["ip"]) != "[0.0.0.0/0 ::/0]" {
		t.Fatalf("last rule = %v, want the catch-all limited to resolved addresses", last)
	}
}

func TestInjectSpeedLimitsLeavesAScriptedRouterAlone(t *testing.T) {
	cfg := speedTestConfig(t, 1080, "AsIs")
	var routing map[string]any
	_ = json.Unmarshal(cfg.RouterConfig, &routing)
	routing["script"] = "return 'direct'"
	cfg.RouterConfig = mustJSON(t, routing)
	before := string(cfg.OutboundConfigs)
	injectSpeedLimits(cfg, map[string]int{"slow@x": 5})
	if string(cfg.OutboundConfigs) != before {
		t.Fatal("outbounds changed although the script ignores every rule they would need")
	}
}

func TestInjectedSpeedConfigIsAcceptedByTheCore(t *testing.T) {
	cfg := speedTestConfig(t, freeLoopbackPort(t), "AsIs")
	injectSpeedLimits(cfg, map[string]int{"slow@x": 5})
	var config conf.Config
	if err := json.Unmarshal(mustJSON(t, cfg), &config); err != nil {
		t.Fatal(err)
	}
	built, err := config.Build()
	if err != nil {
		t.Fatalf("the core refuses the injected config: %v", err)
	}
	instance, err := core.New(built)
	if err != nil {
		t.Fatalf("the core cannot load the injected config: %v", err)
	}
	_ = instance.Close()
}

func TestFoldSpeedTrafficBooksCopiesUnderTheirOutbound(t *testing.T) {
	got := foldSpeedTraffic([]*xray.Traffic{
		{IsOutbound: true, Tag: "direct", Up: 10, Down: 20},
		{IsOutbound: true, Tag: "~speed~5~direct", Up: 1, Down: 2},
		{IsOutbound: true, Tag: "~speed~5~via~warp", Up: 100, Down: 100},
		{IsOutbound: true, Tag: "~speed~7~warp-2", Up: 3, Down: 4},
		{IsInbound: true, Tag: "~speed~in~warp", Up: 100, Down: 100},
		{IsInbound: true, Tag: "in-socks", Up: 5, Down: 6},
	})
	want := "[{false true direct 11 22} {true false in-socks 5 6} {false true warp-2 3 4}]"
	var flat []xray.Traffic
	for _, t := range got {
		flat = append(flat, *t)
	}
	if fmt.Sprint(flat) != want {
		t.Fatalf("folded = %v, want %v", flat, want)
	}
}

// The whole path on a real kernel: the same core serves a limited and an unlimited
// user, through the copied outbound and through the hop.
func TestSpeedLimitedXrayUserIsHeldToItsRate(t *testing.T) {
	if os.Getenv("XUI_SPEED_E2E") != "1" {
		t.Skip("set XUI_SPEED_E2E=1 (as root, with nft) to police a real core")
	}
	socksPort := freeLoopbackPort(t)
	cfg := speedTestConfig(t, socksPort, "AsIs")
	injectSpeedLimits(cfg, map[string]int{"slow@x": 5})
	var config conf.Config
	if err := json.Unmarshal(mustJSON(t, cfg), &config); err != nil {
		t.Fatal(err)
	}
	built, err := config.Build()
	if err != nil {
		t.Fatal(err)
	}
	instance, err := core.New(built)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	if err := instance.Start(); err != nil {
		t.Fatal(err)
	}
	limiter := speedlimit.New()
	t.Cleanup(func() { _ = limiter.Apply(nil) })
	if err := limiter.Apply([]speedlimit.Account{{ID: 5, Mbps: 8}}); err != nil {
		t.Fatal(err)
	}
	server := startSpeedSource(t, 7777)
	defaultServer := startSpeedSource(t, 0)
	for _, tc := range []struct {
		user, addr string
		limited    bool
	}{
		{"slow@x", defaultServer, true},
		{"slow@x", server, true},
		{"fast@x", defaultServer, false},
	} {
		mbps := downloadThroughSocks(t, socksPort, tc.user, tc.addr)
		if tc.limited && (mbps < 4 || mbps > 13) {
			t.Errorf("%s via %s ran at %.1f Mbps, want about 8", tc.user, tc.addr, mbps)
		}
		if !tc.limited && mbps < 50 {
			t.Errorf("%s via %s ran at %.1f Mbps, want it unlimited", tc.user, tc.addr, mbps)
		}
	}
}

// startSpeedSource writes without end to whoever connects; port 0 picks any.
func startSpeedSource(t *testing.T, port int) string {
	t.Helper()
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Skipf("port %d is taken: %v", port, err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	buf := make([]byte, 64*1024)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				for {
					if _, err := c.Write(buf); err != nil {
						return
					}
				}
			}()
		}
	}()
	return ln.Addr().String()
}

func downloadThroughSocks(t *testing.T, socksPort int, user, addr string) float64 {
	t.Helper()
	dialer, err := proxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", socksPort), &proxy.Auth{User: user, Password: "p"}, proxy.Direct)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := dialer.(proxy.ContextDialer).DialContext(ctx, "tcp", addr)
	if err != nil {
		t.Fatalf("%s could not reach %s: %v", user, addr, err)
	}
	defer conn.Close()
	start := time.Now()
	var counted int64
	buf := make([]byte, 64*1024)
	for time.Since(start) < 4*time.Second {
		_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		n, err := conn.Read(buf)
		if err != nil && !errors.Is(err, io.EOF) && !isTimeout(err) {
			t.Fatalf("%s reading from %s: %v", user, addr, err)
		}
		if time.Since(start) > time.Second {
			counted += int64(n)
		}
	}
	return float64(counted) * 8 / (time.Since(start) - time.Second).Seconds() / 1e6
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
