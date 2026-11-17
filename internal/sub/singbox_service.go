package sub

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SubSingboxService answers a subscription as a sing-box profile. Its entries come from the
// same proxies the Clash profile is built from, so the two never disagree on what a user gets.
type SubSingboxService struct {
	clash    *SubClashService
	template string
}

// NewSubSingboxService builds on the Clash service; template overrides the built-in base profile.
func NewSubSingboxService(clash *SubClashService, template string) *SubSingboxService {
	return &SubSingboxService{clash: clash, template: template}
}

// defaultSingboxBase is a profile for sing-box 1.12 and later: a TUN for the mobile apps, a
// local mixed port for desktops, and DNS that leaves through the selected server.
const defaultSingboxBase = `{
  "log": {"level": "warn"},
  "dns": {
    "servers": [
      {"type": "https", "tag": "remote", "server": "1.1.1.1", "detour": "proxy"},
      {"type": "local", "tag": "local"}
    ],
    "final": "remote",
    "strategy": "prefer_ipv4"
  },
  "inbounds": [
    {"type": "tun", "tag": "tun-in", "address": ["172.19.0.1/30", "fdfe:dcba:9876::1/126"], "auto_route": true, "strict_route": true, "stack": "mixed"},
    {"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": 2080}
  ],
  "outbounds": [{"type": "direct", "tag": "direct"}],
  "route": {
    "rules": [
      {"action": "sniff"},
      {"protocol": "dns", "action": "hijack-dns"},
      {"ip_is_private": true, "outbound": "direct"}
    ],
    "final": "proxy",
    "auto_detect_interface": true,
    "default_domain_resolver": "local"
  }
}`

// GetSingbox returns the profile and the userinfo header; an empty profile with a header
// means the user exists but nothing it may use can be expressed for sing-box.
func (s *SubSingboxService) GetSingbox(subID, host string) (string, string, error) {
	proxies, header, found, err := s.clash.collectProxies(subID, host, false)
	if err != nil || !found {
		return "", header, err
	}
	var outbounds, endpoints []map[string]any
	var tags []string
	for _, p := range proxies {
		out, isEndpoint := singboxFromClash(p)
		if out == nil {
			continue
		}
		tags = append(tags, out["tag"].(string))
		if isEndpoint {
			endpoints = append(endpoints, out)
		} else {
			outbounds = append(outbounds, out)
		}
	}
	if len(tags) == 0 {
		return "", header, nil
	}
	base := strings.TrimSpace(s.template)
	if base == "" {
		base = defaultSingboxBase
	}
	var profile map[string]any
	if err := json.Unmarshal([]byte(base), &profile); err != nil {
		return "", header, fmt.Errorf("sing-box base profile: %w", err)
	}
	groups := []any{
		map[string]any{"type": "selector", "tag": "proxy", "outbounds": append([]string{"auto"}, tags...), "default": "auto"},
		map[string]any{"type": "urltest", "tag": "auto", "outbounds": tags, "url": "https://www.gstatic.com/generate_204", "interval": "3m"},
	}
	existing, _ := profile["outbounds"].([]any)
	all := groups
	for _, o := range outbounds {
		all = append(all, o)
	}
	profile["outbounds"] = append(all, existing...)
	if len(endpoints) > 0 {
		eps, _ := profile["endpoints"].([]any)
		for _, e := range endpoints {
			eps = append(eps, e)
		}
		profile["endpoints"] = eps
	}
	body, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return "", header, err
	}
	return string(body), header, nil
}

func str(p map[string]any, key string) string {
	v, _ := p[key].(string)
	return v
}

func toStrings(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		if x == "" {
			return nil
		}
		return strings.Split(x, ",")
	}
	return nil
}

// singboxTLS maps a Clash proxy's TLS and REALITY options; nil when it dials in the clear.
func singboxTLS(p map[string]any, always bool) map[string]any {
	if on, _ := p["tls"].(bool); !on && !always {
		return nil
	}
	tls := map[string]any{"enabled": true}
	name := str(p, "servername")
	if name == "" {
		name = str(p, "sni")
	}
	if name != "" {
		tls["server_name"] = name
	}
	if insecure, _ := p["skip-cert-verify"].(bool); insecure {
		tls["insecure"] = true
	}
	if alpn := toStrings(p["alpn"]); len(alpn) > 0 {
		tls["alpn"] = alpn
	}
	if fp := str(p, "client-fingerprint"); fp != "" {
		tls["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
	}
	if reality, ok := p["reality-opts"].(map[string]any); ok {
		r := map[string]any{"enabled": true, "public_key": reality["public-key"]}
		if sid, _ := reality["short-id"].(string); sid != "" {
			r["short_id"] = sid
		}
		tls["reality"] = r
		if tls["utls"] == nil {
			tls["utls"] = map[string]any{"enabled": true, "fingerprint": "chrome"}
		}
	}
	return tls
}

// singboxTransport maps a Clash network; ok is false for one sing-box has no transport for.
func singboxTransport(p map[string]any) (map[string]any, bool) {
	switch str(p, "network") {
	case "", "tcp":
		return nil, true
	case "ws":
		t := map[string]any{"type": "ws"}
		if opts, ok := p["ws-opts"].(map[string]any); ok {
			if path := str(opts, "path"); path != "" {
				t["path"] = path
			}
			if headers, ok := opts["headers"].(map[string]any); ok && len(headers) > 0 {
				t["headers"] = headers
			}
			if n, ok := opts["max-early-data"].(int); ok && n > 0 {
				t["max_early_data"] = n
				t["early_data_header_name"] = str(opts, "early-data-header-name")
			}
		}
		return t, true
	case "grpc":
		t := map[string]any{"type": "grpc"}
		if opts, ok := p["grpc-opts"].(map[string]any); ok {
			t["service_name"] = str(opts, "grpc-service-name")
		}
		return t, true
	case "httpupgrade":
		t := map[string]any{"type": "httpupgrade"}
		if opts, ok := p["http-upgrade-opts"].(map[string]any); ok {
			if path := str(opts, "path"); path != "" {
				t["path"] = path
			}
			if headers, ok := opts["headers"].(map[string]any); ok {
				if host, _ := headers["Host"].(string); host != "" {
					t["host"] = host
				}
			}
		}
		return t, true
	case "h2", "http":
		t := map[string]any{"type": "http"}
		key := "h2-opts"
		if str(p, "network") == "http" {
			key = "http-opts"
		}
		if opts, ok := p[key].(map[string]any); ok {
			if hosts := toStrings(opts["host"]); len(hosts) > 0 {
				t["host"] = hosts
			}
			if paths := toStrings(opts["path"]); len(paths) > 0 {
				t["path"] = paths[0]
			}
		}
		return t, true
	}
	return nil, false
}

// singboxFromClash converts one Clash proxy; nil for what sing-box cannot express (XHTTP,
// AmneziaWG, VLESS encryption), and endpoint true for WireGuard, an endpoint in sing-box.
func singboxFromClash(p map[string]any) (map[string]any, bool) {
	name, server := str(p, "name"), str(p, "server")
	if name == "" || server == "" {
		return nil, false
	}
	port := p["port"]
	out := map[string]any{"tag": name, "server": server, "server_port": port}
	switch str(p, "type") {
	case "vless":
		if str(p, "encryption") != "" {
			return nil, false
		}
		out["type"], out["uuid"] = "vless", p["uuid"]
		if flow := str(p, "flow"); flow != "" {
			out["flow"] = flow
		}
		out["packet_encoding"] = "xudp"
	case "vmess":
		out["type"], out["uuid"], out["alter_id"] = "vmess", p["uuid"], p["alterId"]
		if c := str(p, "cipher"); c != "" {
			out["security"] = c
		}
		out["packet_encoding"] = "xudp"
	case "trojan":
		out["type"], out["password"] = "trojan", p["password"]
		out["tls"] = singboxTLS(p, true)
	case "ss":
		if str(p, "plugin") != "" {
			return nil, false
		}
		out["type"], out["method"], out["password"] = "shadowsocks", p["cipher"], p["password"]
	case "hysteria2":
		out["type"], out["password"] = "hysteria2", p["password"]
		out["tls"] = singboxTLS(p, true)
		if str(p, "obfs") == "salamander" {
			out["obfs"] = map[string]any{"type": "salamander", "password": p["obfs-password"]}
		} else if str(p, "obfs") != "" {
			return nil, false
		}
		if ports := str(p, "ports"); ports != "" {
			out["server_ports"] = strings.Split(strings.ReplaceAll(ports, "-", ":"), ",")
			delete(out, "server_port")
		}
	case "tuic":
		out["type"], out["uuid"], out["password"] = "tuic", p["uuid"], p["password"]
		if cc := str(p, "congestion-controller"); cc != "" {
			out["congestion_control"] = cc
		}
		if mode := str(p, "udp-relay-mode"); mode != "" {
			out["udp_relay_mode"] = mode
		}
		if zero, _ := p["reduce-rtt"].(bool); zero {
			out["zero_rtt_handshake"] = true
		}
		out["tls"] = singboxTLS(p, true)
	case "socks5":
		out["type"] = "socks"
		if u := str(p, "username"); u != "" {
			out["username"], out["password"] = u, p["password"]
		}
	case "http":
		out["type"] = "http"
		if u := str(p, "username"); u != "" {
			out["username"], out["password"] = u, p["password"]
		}
	case "wireguard":
		var addrs []string
		if ip := str(p, "ip"); ip != "" {
			addrs = append(addrs, ip+"/32")
		}
		if ip := str(p, "ipv6"); ip != "" {
			addrs = append(addrs, ip+"/128")
		}
		if len(addrs) == 0 {
			return nil, false
		}
		peer := map[string]any{
			"address": server, "port": port, "public_key": p["public-key"],
			"allowed_ips": []string{"0.0.0.0/0", "::/0"},
		}
		if psk := str(p, "pre-shared-key"); psk != "" {
			peer["pre_shared_key"] = psk
		}
		if ka, ok := p["persistent-keepalive"].(int); ok && ka > 0 {
			peer["persistent_keepalive_interval"] = ka
		}
		ep := map[string]any{"type": "wireguard", "tag": name, "address": addrs, "private_key": p["private-key"], "peers": []any{peer}}
		if mtu, ok := p["mtu"].(int); ok && mtu > 0 {
			ep["mtu"] = mtu
		}
		return ep, true
	default:
		return nil, false
	}
	switch str(p, "type") {
	case "vless", "vmess":
		transport, ok := singboxTransport(p)
		if !ok {
			return nil, false
		}
		if transport != nil {
			out["transport"] = transport
		}
		if tls := singboxTLS(p, false); tls != nil {
			out["tls"] = tls
		}
	case "trojan":
		transport, ok := singboxTransport(p)
		if !ok {
			return nil, false
		}
		if transport != nil {
			out["transport"] = transport
		}
	}
	return out, false
}
