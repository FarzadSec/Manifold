package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/tunnel"
)

// A relay set up over SSH alone: the pairing code goes in on the command line, and the
// list says how the end is doing without the panel's pages.
func TestTunnelImportAndListWorkWithoutThePanelsPages(t *testing.T) {
	newTokenCLIEnv(t)
	t.Setenv("XUI_BIN_FOLDER", t.TempDir())
	origin := tunnel.Tunnel{
		ID: 9, PairID: "0123456789abcdef0123456789abcdef", Name: "to-origin", Kind: tunnel.KindReverse, Role: tunnel.RoleClient, Enabled: true,
		Settings: tunnel.TunnelSettings{
			Transport: "tcpmux", Port: 8443, Token: "secret-token-123456", RemoteAddr: "relay.example.com",
			Forwards: []tunnel.PortForward{{Listen: "443"}, {Listen: "2201", Target: "22"}},
		},
	}
	code, err := tunnel.PairingCode(origin, "203.0.113.10")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if rc := runTunnelCLI([]string{"import", code}, &out); rc != 0 {
		t.Fatalf("import exited %d: %s", rc, out.String())
	}
	out.Reset()
	if rc := runTunnelCLI([]string{"list"}, &out); rc != 0 {
		t.Fatalf("list exited %d: %s", rc, out.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("list printed %q", out.String())
	}
	row := strings.Fields(lines[1])
	want := []string{"to-origin", "BackPack-reverse", "server", "down"}
	for i, w := range want {
		if i+1 >= len(row) || row[i+1] != w {
			t.Fatalf("the imported end is listed as %q, want %v after the id", lines[1], want)
		}
	}
	if !strings.Contains(out.String(), "443, 2201→22") || !strings.Contains(out.String(), "BackPack is not running") {
		t.Fatalf("the list leaves out the forwards or why the end is down:\n%s", out.String())
	}
	if rc := runTunnelCLI([]string{"import", "manifold-tunnel://not-a-code"}, &out); rc != 1 {
		t.Fatalf("a broken code exited %d", rc)
	}
}
