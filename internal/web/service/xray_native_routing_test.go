package service

import (
	"encoding/json"
	"net/netip"
	"slices"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/egress"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
	"github.com/xtls/xray-core/infra/conf"
)

func saveRoutingTemplate(t *testing.T, rules ...map[string]any) {
	t.Helper()
	template := map[string]any{
		"outbounds": []any{
			map[string]any{"protocol": "freedom", "tag": "direct"},
			map[string]any{"protocol": "socks", "tag": "warp", "settings": map[string]any{"servers": []any{map[string]any{"address": "127.0.0.1", "port": 40000}}}},
		},
		"routing": map[string]any{"rules": rules},
	}
	if err := (&SettingService{}).saveSetting("xrayTemplateConfig", string(mustJSON(t, template))); err != nil {
		t.Fatal(err)
	}
}

func routingRules(t *testing.T, cfg *xray.Config) []map[string]any {
	t.Helper()
	var routing struct {
		Rules []map[string]any `json:"rules"`
	}
	if err := json.Unmarshal(cfg.RouterConfig, &routing); err != nil {
		t.Fatal(err)
	}
	return routing.Rules
}

func inboundTagged(cfg *xray.Config, tag string) *xray.InboundConfig {
	for i := range cfg.InboundConfigs {
		if cfg.InboundConfigs[i].Tag == tag {
			return &cfg.InboundConfigs[i]
		}
	}
	return nil
}

// OpenVPN and WireGuard carry their users' traffic in the kernel, so a rule naming them
// never matched: their TCP and UDP must reach Xray for the admin's rules to decide.
func TestARoutingRuleOnANativeInboundBringsItsTrafficIntoXray(t *testing.T) {
	setupConflictDB(t)
	seedInboundConflict(t, "wg-germany", "", 48101, model.WireGuard, "",
		wgTestSettings("", wgTestClient("alice", wgTestAliceKey, "10.0.0.2/32", true)))
	seedInboundConflict(t, "ovpn-germany", "", 48102, model.OpenVPN, "", openvpnSettings(t, `{"server":{"subnet":"10.21.0.0/22"},"clients":[]}`))
	seedInboundConflict(t, "wg-finland", "", 48103, model.WireGuard, "",
		wgTestSettings("", wgTestClient("bob", wgTestBobKey, "10.0.1.2/32", true)))
	saveRoutingTemplate(t,
		map[string]any{"type": "field", "inboundTag": []any{"wg-germany", "ovpn-germany"}, "outboundTag": "warp"},
		map[string]any{"type": "field", "domain": []any{"geosite:google"}, "outboundTag": "direct"})

	cfg, err := (&XrayService{}).GetXrayConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"wg-germany", "ovpn-germany"} {
		in := inboundTagged(cfg, nativeRouteTag(tag))
		if in == nil {
			t.Fatalf("no Xray inbound takes %s's traffic", tag)
		}
		var settings struct {
			Network        string `json:"network"`
			FollowRedirect bool   `json:"followRedirect"`
		}
		var stream struct {
			Sockopt struct {
				Tproxy string `json:"tproxy"`
			} `json:"sockopt"`
		}
		_ = json.Unmarshal(in.Settings, &settings)
		_ = json.Unmarshal(in.StreamSettings, &stream)
		if in.Protocol != "dokodemo-door" || string(in.Listen) != `"127.0.0.1"` || !settings.FollowRedirect ||
			settings.Network != "tcp,udp" || stream.Sockopt.Tproxy != "tproxy" {
			t.Fatalf("%s's inbound is not a loopback transparent proxy: %+v", tag, in)
		}
		raw, _ := json.Marshal(in)
		detour := new(conf.InboundDetourConfig)
		if err := json.Unmarshal(raw, detour); err != nil {
			t.Fatal(err)
		}
		if _, err := detour.Build(); err != nil {
			t.Fatalf("Xray refuses %s's inbound: %v", tag, err)
		}
		if in.Port == 48101 || in.Port == 48102 || in.Port == 48103 {
			t.Fatalf("%s's proxy takes port %d of an inbound", tag, in.Port)
		}
	}
	if inboundTagged(cfg, nativeRouteTag("wg-finland")) != nil {
		t.Fatal("an inbound no rule names is sent through Xray")
	}
	rules := routingRules(t, cfg)
	tags, _ := rules[0]["inboundTag"].([]any)
	for _, want := range []string{"wg-germany", "ovpn-germany", nativeRouteTag("wg-germany"), nativeRouteTag("ovpn-germany")} {
		if !slices.Contains(tags, any(want)) {
			t.Fatalf("the rule's inboundTag %v lacks %s", tags, want)
		}
	}
}

// The host steers a tunnel's traffic only to a proxy port the running core really listens on.
func TestNativeRouteSteersFollowTheRunningConfig(t *testing.T) {
	setupConflictDB(t)
	seedInboundConflict(t, "wg-germany", "", 48111, model.WireGuard, "",
		wgTestSettings(`,"interface":"DE"`, wgTestClient("alice", wgTestAliceKey, "10.0.0.2/32", true)))
	seedInboundConflict(t, "ovpn-germany", "", 48112, model.OpenVPN, "", openvpnSettings(t, `{"server":{"subnet":"10.21.0.0/22"},"clients":[]}`))
	seedInboundConflict(t, "wg-off", "", 48113, model.WireGuard, "",
		wgTestSettings("", wgTestClient("bob", wgTestBobKey, "10.0.1.2/32", true)))
	off := loadInboundByTag(t, "wg-off")
	off.Enable = false
	if err := database.GetDB().Save(off).Error; err != nil {
		t.Fatal(err)
	}
	cfg := &xray.Config{InboundConfigs: []xray.InboundConfig{
		{Tag: nativeRouteTag("wg-germany"), Port: 62301},
		{Tag: nativeRouteTag("ovpn-germany"), Port: 62302},
		{Tag: nativeRouteTag("wg-off"), Port: 62303},
		{Tag: "vless-in", Port: 443},
	}}

	steers, err := (&XrayService{}).nativeRouteSteers(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := []egress.Steer{
		{Device: "DE", Port: 62301},
		{Subnet: netip.MustParsePrefix("10.21.0.0/22"), Port: 62302},
	}
	if !slices.Equal(steers, want) {
		t.Fatalf("steers = %+v, want %+v", steers, want)
	}
}
