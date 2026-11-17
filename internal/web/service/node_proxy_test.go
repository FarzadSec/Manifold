package service

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
)

// nodeProxy is an http proxy that serves every request for a node whose name does not
// resolve, so a request reaching the node at all went through it.
type nodeProxy struct {
	mu    sync.Mutex
	hosts []string
	tls   *httptest.Server
}

func (p *nodeProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.hosts = append(p.hosts, r.Host)
	p.mu.Unlock()
	if r.Method == http.MethodConnect {
		upstream, err := net.Dial("tcp", p.tls.Listener.Addr().String())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		client, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		go func() { _, _ = io.Copy(upstream, client) }()
		_, _ = io.Copy(client, upstream)
		_ = client.Close()
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if strings.HasSuffix(r.URL.Path, "/inbounds/list") {
		_, _ = w.Write([]byte(`{"success":true,"obj":[{"id":7,"tag":"in-7","protocol":"vless","port":443}]}`))
		return
	}
	_, _ = w.Write([]byte(`{"success":true,"obj":{"panelGuid":"via-proxy","xray":{"state":"running","version":"26.1.1"}}}`))
}

func (p *nodeProxy) asked() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.hosts...)
}

// Every way the panel reaches a node goes through the node's proxy when it has one.
func TestNodeProxyCarriesEveryCallToTheNode(t *testing.T) {
	setupBulkDB(t)
	node := httptest.NewTLSServer(http.NotFoundHandler())
	defer node.Close()
	p := &nodeProxy{tls: node}
	proxy := httptest.NewServer(p)
	defer proxy.Close()

	n := &model.Node{
		Id: 41, Name: "far", Scheme: "http", Address: "node.invalid", Port: 2053, BasePath: "/",
		Enable: true, TlsVerifyMode: "skip", ApiToken: "", ProxyURL: proxy.URL,
	}
	svc := &NodeService{}
	patch, err := svc.Probe(context.Background(), n)
	if err != nil {
		t.Fatalf("probe through the proxy: %v (%s)", err, patch.LastError)
	}
	if patch.Guid != "via-proxy" {
		t.Errorf("probe guid = %q, want the answer that came through the proxy", patch.Guid)
	}
	remote := *n
	remote.ApiToken = "node-token"
	options, err := runtime.NewRemote(&remote, nil).ListInboundOptions(context.Background())
	if err != nil || len(options) != 1 || options[0].Tag != "in-7" {
		t.Fatalf("inbound list through the proxy = %v, %v", options, err)
	}
	n.Scheme = "https"
	if _, err := svc.FetchCertFingerprint(context.Background(), n); err != nil {
		t.Fatalf("certificate fetch through the proxy: %v", err)
	}
	if got := p.asked(); len(got) != 3 || got[0] != "node.invalid:2053" || got[2] != "node.invalid:2053" {
		t.Errorf("the proxy was asked for %v, want the node three times", got)
	}
}

func TestNodeProxyIsOneWayOfReachingANode(t *testing.T) {
	base := func() *model.Node {
		return &model.Node{Name: "n", Address: "node.example.com", Port: 2053}
	}
	for name, edit := range map[string]func(*model.Node){
		"both a proxy and an outbound": func(n *model.Node) { n.ProxyURL, n.OutboundTag = "socks5://127.0.0.1:1080", "warp" },
		"an unknown scheme":            func(n *model.Node) { n.ProxyURL = "ftp://127.0.0.1:21" },
		"no port":                      func(n *model.Node) { n.ProxyURL = "socks5://127.0.0.1" },
	} {
		n := base()
		edit(n)
		if err := (&NodeService{}).normalize(n); err == nil {
			t.Errorf("a node with %s was accepted", name)
		}
	}
	n := base()
	n.ProxyURL = " socks5://user:pass@127.0.0.1:1080 "
	if err := (&NodeService{}).normalize(n); err != nil || n.ProxyURL != "socks5://user:pass@127.0.0.1:1080" {
		t.Errorf("a socks5 proxy was refused or kept untrimmed: %q, %v", n.ProxyURL, err)
	}
}
