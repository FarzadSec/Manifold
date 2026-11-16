package service

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/speedlimit"
	json_util "github.com/mhsanaei/3x-ui/v3/internal/util/json_util"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

// speedTagPrefix starts every tag the speed limit adds, so no balancer or
// observatory selector written for the admin's own outbounds picks one up.
const speedTagPrefix = "~speed~"

// speedHopBasePort is the first loopback port tried for a hop; nothing else of the panel lives here.
const speedHopBasePort = 63500

// Outbounds that never dial anything a user's traffic could be limited on.
var speedUnlimitedProtocols = map[string]bool{"blackhole": true, "dns": true, "loopback": true}

// A stateless dialer can be copied per user; one that keeps a session (WireGuard,
// MASQUE), dials through another outbound or carries its own mark is reached through a hop.
var speedCopyableProtocols = map[string]bool{
	"freedom": true, "vless": true, "vmess": true, "trojan": true, "shadowsocks": true,
	"socks": true, "http": true, "hysteria": true,
}

func speedCopyTag(id int, target string) string {
	return fmt.Sprintf("%s%d~%s", speedTagPrefix, id, target)
}

func speedViaTag(id int, target string) string {
	return fmt.Sprintf("%s%d~via~%s", speedTagPrefix, id, target)
}

func speedHopInboundTag(target string) string { return speedTagPrefix + "in~" + target }

// limitedClientMarks returns the socket mark of every enabled user with a speed limit, by email.
func limitedClientMarks() map[string]int {
	var rows []model.ClientRecord
	if err := database.GetDB().Select("id", "email").
		Where("speed_limit > 0 AND enable = ?", true).Find(&rows).Error; err != nil {
		logger.Warning("speed limit: reading limited users:", err)
		return nil
	}
	out := make(map[string]int, len(rows))
	for _, r := range rows {
		if _, ok := speedlimit.Mark(r.Id); ok {
			out[r.Email] = r.Id
		}
	}
	return out
}

type speedTarget struct {
	balancer bool
	copyable bool
}

// injectSpeedLimits sends each limited user's traffic, after the admin's own rules
// decided where it goes, out of a socket carrying the user's mark for nft to police.
func injectSpeedLimits(cfg *xray.Config, limited map[string]int) {
	if len(limited) == 0 || len(cfg.RouterConfig) == 0 && len(cfg.OutboundConfigs) == 0 {
		return
	}
	routing := map[string]any{}
	if len(cfg.RouterConfig) > 0 {
		if err := json.Unmarshal(cfg.RouterConfig, &routing); err != nil {
			logger.Warning("speed limit: routing section is unparsable, Xray users stay unlimited:", err)
			return
		}
	}
	if script, _ := routing["script"].(string); script != "" {
		logger.Warning("speed limit: a routing script decides every route, so Xray users stay unlimited")
		return
	}
	var outbounds []map[string]any
	if len(cfg.OutboundConfigs) > 0 {
		if err := json.Unmarshal(cfg.OutboundConfigs, &outbounds); err != nil {
			logger.Warning("speed limit: outbounds are unparsable, Xray users stay unlimited:", err)
			return
		}
	}
	users := servedLimitedUsers(cfg.InboundConfigs, limited)
	if len(users) == 0 {
		return
	}
	targets := classifySpeedTargets(routing, outbounds)

	rules, _ := routing["rules"].([]any)
	var newRules []any
	copies := map[string]bool{}
	hops := map[string]bool{}
	route := func(rule map[string]any, target string, u servedUser) map[string]any {
		t := targets[target]
		next := maps.Clone(rule)
		delete(next, "outboundTag")
		delete(next, "balancerTag")
		next["user"] = []any{u.email}
		if tag, _ := rule["ruleTag"].(string); tag != "" {
			next["ruleTag"] = fmt.Sprintf("%s%d~%s", speedTagPrefix, u.id, tag)
		}
		if t.copyable {
			next["outboundTag"] = speedCopyTag(u.id, target)
			copies[target] = true
		} else {
			next["outboundTag"] = speedViaTag(u.id, target)
			hops[target] = true
		}
		return next
	}
	for _, raw := range rules {
		rule, ok := raw.(map[string]any)
		if !ok {
			newRules = append(newRules, raw)
			continue
		}
		target := ruleTarget(rule)
		if _, limitedTarget := targets[target]; limitedTarget {
			for _, u := range users {
				if ruleCanMatch(rule, u) {
					newRules = append(newRules, route(rule, target, u))
				}
			}
		}
		newRules = append(newRules, rule)
	}
	if len(outbounds) > 0 {
		first, _ := outbounds[0]["tag"].(string)
		if _, limitedTarget := targets[first]; limitedTarget && first != "" {
			catchAll := map[string]any{"type": "field"}
			// IPIfNonMatch tries every rule again with resolved addresses; a rule matching
			// any address waits for that second pass instead of cutting it short.
			if strategy, _ := routing["domainStrategy"].(string); strings.EqualFold(strategy, "IPIfNonMatch") {
				catchAll["ip"] = []any{"0.0.0.0/0", "::/0"}
			}
			for _, u := range users {
				newRules = append(newRules, route(catchAll, first, u))
			}
		}
	}

	byTag := map[string]map[string]any{}
	for _, ob := range outbounds {
		if tag, _ := ob["tag"].(string); tag != "" {
			byTag[tag] = ob
		}
	}
	for _, u := range users {
		mark, _ := speedlimit.Mark(u.id)
		for _, target := range sortedKeys(copies) {
			outbounds = append(outbounds, markedCopy(byTag[target], speedCopyTag(u.id, target), mark))
		}
	}
	hopRules, hopPorts := addSpeedHops(cfg, sortedKeys(hops), targets)
	for _, u := range users {
		mark, _ := speedlimit.Mark(u.id)
		for _, target := range sortedKeys(hops) {
			outbounds = append(outbounds, map[string]any{
				"tag":            speedViaTag(u.id, target),
				"protocol":       "socks",
				"settings":       map[string]any{"address": "127.0.0.1", "port": hopPorts[target]},
				"streamSettings": map[string]any{"sockopt": map[string]any{"mark": mark}},
			})
		}
	}
	routing["rules"] = append(hopRules, newRules...)
	newRouting, err := json.Marshal(routing)
	if err != nil {
		logger.Warning("speed limit: rebuilding routing:", err)
		return
	}
	newOutbounds, err := json.Marshal(outbounds)
	if err != nil {
		logger.Warning("speed limit: rebuilding outbounds:", err)
		return
	}
	cfg.RouterConfig = json_util.RawMessage(newRouting)
	cfg.OutboundConfigs = json_util.RawMessage(newOutbounds)
}

type servedUser struct {
	email string
	id    int
	tags  map[string]bool
}

// servedLimitedUsers lists the limited users some inbound serves, with the inbounds that do.
func servedLimitedUsers(inbounds []xray.InboundConfig, limited map[string]int) []servedUser {
	byEmail := map[string]*servedUser{}
	for _, ib := range inbounds {
		var settings struct {
			Clients []struct {
				Email string `json:"email"`
			} `json:"clients"`
			Accounts []struct {
				User string `json:"user"`
			} `json:"accounts"`
		}
		if len(ib.Settings) == 0 || json.Unmarshal(ib.Settings, &settings) != nil {
			continue
		}
		names := make([]string, 0, len(settings.Clients)+len(settings.Accounts))
		for _, c := range settings.Clients {
			names = append(names, c.Email)
		}
		for _, a := range settings.Accounts {
			names = append(names, a.User)
		}
		for _, name := range names {
			id, ok := limited[name]
			if !ok {
				continue
			}
			u := byEmail[name]
			if u == nil {
				u = &servedUser{email: name, id: id, tags: map[string]bool{}}
				byEmail[name] = u
			}
			u.tags[ib.Tag] = true
		}
	}
	out := make([]servedUser, 0, len(byEmail))
	for _, u := range byEmail {
		out = append(out, *u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

func classifySpeedTargets(routing map[string]any, outbounds []map[string]any) map[string]speedTarget {
	targets := map[string]speedTarget{}
	balancers, _ := routing["balancers"].([]any)
	for _, b := range balancers {
		if bm, ok := b.(map[string]any); ok {
			if tag, _ := bm["tag"].(string); tag != "" {
				targets[tag] = speedTarget{balancer: true}
			}
		}
	}
	for _, ob := range outbounds {
		tag, _ := ob["tag"].(string)
		protocol, _ := ob["protocol"].(string)
		if tag == "" || speedUnlimitedProtocols[protocol] || strings.HasPrefix(tag, speedTagPrefix) {
			continue
		}
		_, chained := ob["proxySettings"]
		sockopt := map[string]any{}
		if stream, ok := ob["streamSettings"].(map[string]any); ok {
			sockopt, _ = stream["sockopt"].(map[string]any)
		}
		dialer, _ := sockopt["dialerProxy"].(string)
		ownMark := sockopt["mark"] != nil && fmt.Sprint(sockopt["mark"]) != "0"
		targets[tag] = speedTarget{copyable: speedCopyableProtocols[protocol] && !chained && dialer == "" && !ownMark}
	}
	return targets
}

func ruleTarget(rule map[string]any) string {
	if tag, _ := rule["balancerTag"].(string); tag != "" {
		return tag
	}
	tag, _ := rule["outboundTag"].(string)
	return tag
}

// ruleCanMatch drops the copies that could never match the user: a rule for
// other users, or for inbounds that do not serve this one.
func ruleCanMatch(rule map[string]any, u servedUser) bool {
	if list := stringList(rule["user"]); list != nil && !slices.Contains(list, u.email) {
		return false
	}
	if list := stringList(rule["inboundTag"]); list != nil {
		return slices.ContainsFunc(list, func(tag string) bool { return u.tags[tag] })
	}
	return true
}

func stringList(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func markedCopy(ob map[string]any, tag string, mark uint32) map[string]any {
	raw, _ := json.Marshal(ob)
	var copied map[string]any
	_ = json.Unmarshal(raw, &copied)
	copied["tag"] = tag
	stream, _ := copied["streamSettings"].(map[string]any)
	if stream == nil {
		stream = map[string]any{}
		copied["streamSettings"] = stream
	}
	sockopt, _ := stream["sockopt"].(map[string]any)
	if sockopt == nil {
		sockopt = map[string]any{}
		stream["sockopt"] = sockopt
	}
	sockopt["mark"] = mark
	return copied
}

// addSpeedHops opens one loopback inbound per target that cannot be copied, and
// the rules that hand what arrives there to that target as the admin defined it.
func addSpeedHops(cfg *xray.Config, targets []string, kinds map[string]speedTarget) ([]any, map[string]int) {
	used := map[int]bool{}
	for _, ib := range cfg.InboundConfigs {
		used[ib.Port] = true
	}
	port := speedHopBasePort
	rules := make([]any, 0, len(targets))
	ports := map[string]int{}
	for _, target := range targets {
		for used[port] {
			port++
		}
		used[port] = true
		ports[target] = port
		tag := speedHopInboundTag(target)
		cfg.InboundConfigs = append(cfg.InboundConfigs, xray.InboundConfig{
			Listen:   json_util.RawMessage(`"127.0.0.1"`),
			Port:     port,
			Protocol: "socks",
			Settings: json_util.RawMessage(`{"auth":"noauth","udp":true,"ip":"127.0.0.1"}`),
			Tag:      tag,
		})
		rule := map[string]any{"type": "field", "inboundTag": []any{tag}}
		if kinds[target].balancer {
			rule["balancerTag"] = target
		} else {
			rule["outboundTag"] = target
		}
		rules = append(rules, rule)
	}
	return rules, ports
}

func sortedKeys(m map[string]bool) []string {
	keys := slices.Collect(maps.Keys(m))
	sort.Strings(keys)
	return keys
}

// foldSpeedTraffic books a user's copied outbound under the outbound it copies,
// and drops the hops, whose traffic the real target counts once already.
func foldSpeedTraffic(traffic []*xray.Traffic) []*xray.Traffic {
	out := make([]*xray.Traffic, 0, len(traffic))
	merged := map[string]*xray.Traffic{}
	for _, t := range traffic {
		if t == nil || !strings.HasPrefix(t.Tag, speedTagPrefix) {
			if t != nil && t.IsOutbound {
				merged[t.Tag] = t
			}
			out = append(out, t)
		}
	}
	for _, t := range traffic {
		if t == nil || !t.IsOutbound || !strings.HasPrefix(t.Tag, speedTagPrefix) {
			continue
		}
		idAndTarget := strings.TrimPrefix(t.Tag, speedTagPrefix)
		id, target, ok := strings.Cut(idAndTarget, "~")
		if _, err := strconv.Atoi(id); !ok || err != nil || strings.HasPrefix(target, "via~") {
			continue
		}
		if into := merged[target]; into != nil {
			into.Up += t.Up
			into.Down += t.Down
			continue
		}
		folded := &xray.Traffic{IsOutbound: true, Tag: target, Up: t.Up, Down: t.Down}
		merged[target] = folded
		out = append(out, folded)
	}
	return out
}
