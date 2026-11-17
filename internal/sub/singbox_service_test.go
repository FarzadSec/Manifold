package sub

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

// Every kind of entry goes through the same path a subscription does: a share link becomes
// the Clash proxy, which becomes the sing-box outbound.
func TestSingboxCarriesEachProtocolsOwnFields(t *testing.T) {
	clash := NewSubClashService(false, "", false, &SubService{})
	convert := func(t *testing.T, link string) (map[string]any, bool) {
		t.Helper()
		proxy := clash.clashProxyFromExternal(link, "entry")
		if proxy == nil {
			t.Fatalf("no Clash proxy from %s", link)
		}
		return singboxFromClash(proxy)
	}
	get := func(m map[string]any, path ...string) any {
		var cur any = m
		for _, k := range path {
			next, _ := cur.(map[string]any)
			cur = next[k]
		}
		return cur
	}

	t.Run("VLESS REALITY with vision", func(t *testing.T) {
		out, _ := convert(t, "vless://11111111-2222-4333-8444-555555555555@a.example:443?type=tcp&security=reality&sni=www.example.com&pbk=PBK&sid=ab12&fp=firefox&flow=xtls-rprx-vision")
		if out["type"] != "vless" || out["flow"] != "xtls-rprx-vision" || out["server_port"] != 443 {
			t.Fatalf("outbound = %v", out)
		}
		if get(out, "tls", "server_name") != "www.example.com" || get(out, "tls", "reality", "public_key") != "PBK" ||
			get(out, "tls", "reality", "short_id") != "ab12" || get(out, "tls", "utls", "fingerprint") != "firefox" {
			t.Fatalf("tls = %v", out["tls"])
		}
	})
	t.Run("VMess over WebSocket and TLS", func(t *testing.T) {
		vmess := `{"v":"2","ps":"w","add":"b.example","port":"8443","id":"11111111-2222-4333-8444-555555555555","aid":"0","scy":"auto","net":"ws","type":"none","host":"cdn.example","path":"/ray","tls":"tls","sni":"cdn.example"}`
		out, _ := convert(t, "vmess://"+b64Std(vmess))
		if out["type"] != "vmess" || get(out, "transport", "type") != "ws" || get(out, "transport", "path") != "/ray" ||
			get(out, "transport", "headers", "Host") != "cdn.example" || get(out, "tls", "server_name") != "cdn.example" {
			t.Fatalf("outbound = %v", out)
		}
	})
	t.Run("Trojan over gRPC", func(t *testing.T) {
		out, _ := convert(t, "trojan://secret@c.example:443?type=grpc&serviceName=svc&security=tls&sni=c.example")
		if out["type"] != "trojan" || out["password"] != "secret" || get(out, "transport", "service_name") != "svc" ||
			get(out, "tls", "enabled") != true {
			t.Fatalf("outbound = %v", out)
		}
	})
	t.Run("Hysteria2 with salamander and port hopping", func(t *testing.T) {
		out, _ := convert(t, "hysteria2://pw@d.example:443?sni=d.example&obfs=salamander&obfs-password=mask&mport=20000-30000")
		if out["type"] != "hysteria2" || get(out, "obfs", "type") != "salamander" || get(out, "obfs", "password") != "mask" {
			t.Fatalf("outbound = %v", out)
		}
		if ports, _ := out["server_ports"].([]string); len(ports) != 1 || ports[0] != "20000:30000" {
			t.Fatalf("server_ports = %v, want 20000:30000", out["server_ports"])
		}
	})
	t.Run("WireGuard becomes an endpoint", func(t *testing.T) {
		out, endpoint := convert(t, "wireguard://cFzzLXZkPN4mG1HMf2eeSsPT1B9%2FSi0VQ6Lkc26ZDEQ%3D@e.example:51820?publickey=bmXOC%2BF1FxEMF9dyiK2H5%2F1SUtzH0JuVo51h2wPfgyo%3D&address=10.0.0.2%2F32")
		peers, _ := out["peers"].([]any)
		if !endpoint || out["type"] != "wireguard" || len(peers) != 1 || out["private_key"] != "cFzzLXZkPN4mG1HMf2eeSsPT1B9/Si0VQ6Lkc26ZDEQ=" {
			t.Fatalf("endpoint=%v %v", endpoint, out)
		}
		if addr, _ := out["address"].([]string); len(addr) != 1 || addr[0] != "10.0.0.2/32" {
			t.Fatalf("address = %v", out["address"])
		}
	})
	t.Run("what sing-box has no transport for is left out", func(t *testing.T) {
		if out, _ := convert(t, "vless://11111111-2222-4333-8444-555555555555@f.example:443?type=xhttp&path=%2Fx&security=tls&sni=f.example"); out != nil {
			t.Fatalf("an XHTTP entry came through: %v", out)
		}
	})
}

func TestSingboxProfileSelectsAmongTheUsersEntries(t *testing.T) {
	setupInfoNodeTestDB(t)
	db := database.GetDB()
	for _, row := range []any{
		&model.Inbound{
			Id: 1, UserId: 1, Remark: "DE", Enable: true, Port: 443, Protocol: model.VLESS,
			Settings:       `{"clients":[{"id":"c1-uuid","email":"u1","subId":"sb","enable":true}]}`,
			StreamSettings: `{"network":"ws","security":"none","wsSettings":{"path":"/ws"}}`,
		},
		&model.ClientRecord{Id: 1, Email: "u1", SubID: "sb", UUID: "c1-uuid", Enable: true},
		&model.ClientInbound{InboundId: 1, ClientId: 1},
		&xray.ClientTraffic{InboundId: 1, Email: "u1", Enable: true},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := NewSubSingboxService(NewSubClashService(false, "", false, NewSubService("")), "")
	body, header, err := svc.GetSingbox("sb", "sub.example.com")
	if err != nil || body == "" || header == "" {
		t.Fatalf("GetSingbox = %q, %q, %v", body, header, err)
	}
	var profile struct {
		Outbounds []map[string]any `json:"outbounds"`
		Route     map[string]any   `json:"route"`
	}
	if err := json.Unmarshal([]byte(body), &profile); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, body)
	}
	byTag := map[string]map[string]any{}
	for _, o := range profile.Outbounds {
		byTag[o["tag"].(string)] = o
	}
	selector := byTag["proxy"]
	members, _ := selector["outbounds"].([]any)
	if selector["type"] != "selector" || len(members) != 2 || members[0] != "auto" {
		t.Fatalf("selector = %v", selector)
	}
	entry := byTag[members[1].(string)]
	if entry["type"] != "vless" || entry["server_port"] != float64(443) || !strings.Contains(body, `"path": "/ws"`) {
		t.Fatalf("entry = %v", entry)
	}
	if byTag["direct"] == nil || profile.Route["final"] != "proxy" {
		t.Fatalf("profile lost its base: %s", body)
	}
	if body, _, _ := svc.GetSingbox("missing", "sub.example.com"); body != "" {
		t.Fatal("an unknown subscription got a profile")
	}
}
