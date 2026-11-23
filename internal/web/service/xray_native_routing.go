package service

import (
	"encoding/json"
	"slices"
	"strings"
	"sync"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/egress"
	"github.com/mhsanaei/3x-ui/v3/internal/l2tp"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/openvpn"
	json_util "github.com/mhsanaei/3x-ui/v3/internal/util/json_util"
	"github.com/mhsanaei/3x-ui/v3/internal/wgquick"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

// A native inbound's proxy has a tag of its own: the engine already books the inbound's
// traffic under the inbound's tag, and Xray's counters for the same tag would double it.
const nativeRouteTagPrefix = "~route~"

// nativeRouteBasePort is the first loopback port tried for a native inbound's proxy.
const nativeRouteBasePort = 62300

// Clients resolve names on their own, so domain rules match only through sniffing.
const nativeRouteSniffing = `{"enabled":true,"destOverride":["http","tls","quic"],"routeOnly":true}`

func nativeRouteTag(tag string) string { return nativeRouteTagPrefix + tag }

// injectNativeRouting gives every local OpenVPN, WireGuard and L2TP inbound that a routing
// rule names a loopback transparent proxy, and lets those rules match what arrives there.
func injectNativeRouting(cfg *xray.Config, inbounds []*model.Inbound) {
	if len(cfg.RouterConfig) == 0 {
		return
	}
	native := map[string]bool{}
	for _, ib := range inbounds {
		if ib.Enable && ib.NodeID == nil && ib.Tag != "" && isNativeProtocol(ib.Protocol) {
			native[ib.Tag] = true
		}
	}
	if len(native) == 0 {
		return
	}
	routing := map[string]any{}
	if err := json.Unmarshal(cfg.RouterConfig, &routing); err != nil {
		logger.Warning("native routing: routing section is unparsable, tunnel traffic stays out of Xray:", err)
		return
	}
	rules, _ := routing["rules"].([]any)
	routed := map[string]bool{}
	for _, r := range rules {
		rule, ok := r.(map[string]any)
		if !ok {
			continue
		}
		tags, _ := rule["inboundTag"].([]any)
		var proxies []any
		for _, t := range tags {
			if tag, _ := t.(string); native[tag] {
				routed[tag] = true
				proxies = append(proxies, nativeRouteTag(tag))
			}
		}
		if len(proxies) > 0 {
			rule["inboundTag"] = append(tags, proxies...)
		}
	}
	if len(routed) == 0 {
		return
	}
	newRouting, err := json.Marshal(routing)
	if err != nil {
		logger.Warning("native routing: rebuilding the routing section failed:", err)
		return
	}
	cfg.RouterConfig = json_util.RawMessage(newRouting)

	used := map[int]bool{}
	for _, in := range cfg.InboundConfigs {
		used[in.Port] = true
	}
	for _, ib := range inbounds {
		used[ib.Port] = true
	}
	port := nativeRouteBasePort
	for _, tag := range sortedKeys(routed) {
		for used[port] {
			port++
		}
		used[port] = true
		cfg.InboundConfigs = append(cfg.InboundConfigs, xray.InboundConfig{
			Listen:         json_util.RawMessage(`"127.0.0.1"`),
			Port:           port,
			Protocol:       "dokodemo-door",
			Settings:       json_util.RawMessage(`{"network":"tcp,udp","followRedirect":true}`),
			StreamSettings: json_util.RawMessage(`{"sockopt":{"tproxy":"tproxy"}}`),
			Sniffing:       json_util.RawMessage(nativeRouteSniffing),
			Tag:            nativeRouteTag(tag),
		})
	}
}

// nativeRouteSteers lists, for the proxies in cfg, the local engine traffic to hand them.
func (s *XrayService) nativeRouteSteers(cfg *xray.Config) ([]egress.Steer, error) {
	ports := map[string]int{}
	for _, in := range cfg.InboundConfigs {
		if tag, ok := strings.CutPrefix(in.Tag, nativeRouteTagPrefix); ok {
			ports[tag] = in.Port
		}
	}
	if len(ports) == 0 {
		return nil, nil
	}
	inbounds, err := s.inboundService.GetAllInbounds()
	if err != nil {
		return nil, err
	}
	var steers []egress.Steer
	for _, ib := range inbounds {
		port, routed := ports[ib.Tag]
		if !routed || !ib.Enable || ib.NodeID != nil {
			continue
		}
		switch ib.Protocol {
		case model.WireGuard:
			if inst, ok := wgquick.InstanceFromInbound(ib); ok {
				steers = append(steers, egress.Steer{Device: inst.Interface, Port: port})
			}
		case model.OpenVPN:
			if inst, ok := openvpn.InstanceFromInbound(ib); ok {
				steers = append(steers, egress.Steer{Subnet: inst.Subnet, Port: port})
			}
		case model.L2TP:
			if inst, ok := l2tp.InstanceFromInbound(ib); ok {
				steers = append(steers, egress.Steer{Subnet: inst.Subnet, Port: port})
			}
		}
	}
	slices.SortFunc(steers, func(a, b egress.Steer) int { return a.Port - b.Port })
	return steers, nil
}

var nativeRouting struct {
	sync.Mutex
	applied []egress.Steer
	failed  string
}

// SyncNativeRouting steers tunnel traffic to the proxies the running core listens on, and
// to none while the core is down, so that traffic falls back to plain NAT instead of a dead port.
func (s *XrayService) SyncNativeRouting() {
	var steers []egress.Steer
	if process := currentXrayProcess(); process != nil && process.IsRunning() {
		var err error
		if steers, err = s.nativeRouteSteers(process.GetConfig()); err != nil {
			logger.Warning("native routing:", err)
			return
		}
	}
	nativeRouting.Lock()
	defer nativeRouting.Unlock()
	if slices.Equal(steers, nativeRouting.applied) && nativeRouting.failed == "" {
		return
	}
	if err := egress.ApplySteers(steers); err != nil {
		if msg := err.Error(); msg != nativeRouting.failed {
			logger.Warning("native routing:", msg)
			nativeRouting.failed = msg
		}
		return
	}
	nativeRouting.applied, nativeRouting.failed = steers, ""
}
