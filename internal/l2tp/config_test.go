package l2tp

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testInstance(users ...User) Instance {
	return Instance{
		Id:     4,
		Tag:    "inbound-1701",
		IPsec:  true,
		PSK:    "shared-key-123",
		Subnet: netip.MustParsePrefix("10.9.0.0/24"),
		DNS:    []string{"1.1.1.1", "8.8.8.8"},
		MTU:    1280,
		Users:  users,
	}
}

const testExe = "/usr/local/manifold/manifold"

func TestServerFilesTellEachDaemonItsPart(t *testing.T) {
	f := filesIn("/usr/local/manifold/bin/l2tp", 4)
	dir := "/usr/local/manifold/bin/l2tp/4"

	wantXl2tpd := "[global]\n" +
		"port = 1701\n" +
		"access control = no\n" +
		"auth file = " + dir + "/l2tp-secrets\n" +
		"\n[lns default]\n" +
		"ip range = 10.9.0.2-10.9.0.254\n" +
		"local ip = 10.9.0.1\n" +
		"require authentication = yes\n" +
		"name = manifold\n" +
		"pass peer = yes\n" +
		"length bit = yes\n" +
		"pppoptfile = " + dir + "/ppp-options\n"
	if got := renderXl2tpd(testInstance(), f); got != wantXl2tpd {
		t.Fatalf("xl2tpd.conf:\n%s\nwant:\n%s", got, wantXl2tpd)
	}

	wantPPP := "auth\n" +
		"require-mschap-v2\n" +
		"noccp\n" +
		"nodefaultroute\n" +
		"ms-dns 1.1.1.1\n" +
		"ms-dns 8.8.8.8\n" +
		"mtu 1280\n" +
		"mru 1280\n" +
		"lcp-echo-interval 10\n" +
		"lcp-echo-failure 3\n" +
		"plugin winbind.so\n" +
		"ntlm_auth-helper \"" + testExe + " l2tp-auth " + dir + "/control.sock\"\n" +
		"ip-up-script " + dir + "/ip-up\n" +
		"ip-down-script " + dir + "/ip-down\n"
	got, err := renderPPPOptions(testInstance(), f, testExe, false)
	if err != nil || got != wantPPP {
		t.Fatalf("ppp options (err %v):\n%s\nwant:\n%s", err, got, wantPPP)
	}
	debug, _ := renderPPPOptions(testInstance(), f, testExe, true)
	if debug != wantPPP+"debug\nlogfile "+dir+"/ppp.log\n" {
		t.Fatalf("debug ppp options:\n%s", debug)
	}

	wantUp := "#!/bin/sh\nexec " + testExe + " l2tp-hook up " + dir + "/control.sock \"$1\" \"$5\" \"$6\"\n"
	if got := renderHook("up", f, testExe); got != wantUp {
		t.Fatalf("ip-up:\n%s\nwant:\n%s", got, wantUp)
	}
}

// pppd refuses a helper that does not begin with a slash, and hands the rest to a shell.
func TestHelperCommandIsRefusedWhenAPathNeedsQuoting(t *testing.T) {
	f := filesIn("/usr/local/manifold/bin/l2tp", 4)
	for _, exe := range []string{"/opt/my panel/manifold", "manifold", "/opt/it's/manifold", "/opt/$(id)/manifold"} {
		if _, err := renderPPPOptions(testInstance(), f, exe, false); err == nil || !strings.Contains(err.Error(), "cannot be started by pppd") {
			t.Fatalf("exe %q: error = %v", exe, err)
		}
	}
	spaced := filesIn("/opt/my panel/bin/l2tp", 4)
	if _, err := renderPPPOptions(testInstance(), spaced, testExe, false); err == nil || !strings.Contains(err.Error(), "cannot be started by pppd") {
		t.Fatalf("socket in a folder with a space: error = %v", err)
	}
}

func TestIPsecConfigAcceptsTheClientsPeopleHave(t *testing.T) {
	want := "# Managed by Manifold: inbound 4. Changes made here are overwritten.\n" +
		"connections {\n" +
		"    manifold-l2tp {\n" +
		"        version = 1\n" +
		"        proposals = aes256-sha256-modp2048,aes256-sha256-modp1024,aes256-sha1-modp2048,aes256-sha1-modp1024,aes128-sha1-modp1024,3des-sha1-modp1024\n" +
		"        local {\n" +
		"            auth = psk\n" +
		"        }\n" +
		"        remote {\n" +
		"            auth = psk\n" +
		"        }\n" +
		"        children {\n" +
		"            manifold-l2tp {\n" +
		"                mode = transport\n" +
		"                local_ts = dynamic[udp/1701]\n" +
		"                remote_ts = dynamic[udp]\n" +
		"                esp_proposals = aes256-sha256,aes256-sha1,aes128-sha1,3des-sha1\n" +
		"            }\n" +
		"        }\n" +
		"    }\n" +
		"}\n" +
		"secrets {\n" +
		"    ike-manifold-l2tp {\n" +
		"        secret = \"shared-key-123\"\n" +
		"    }\n" +
		"}\n"
	if got := renderSwanctl(testInstance()); got != want {
		t.Fatalf("swanctl config:\n%s\nwant:\n%s", got, want)
	}
}

func TestFilesAreWrittenForRootAloneAndRemovedWithTheServer(t *testing.T) {
	base := t.TempDir()
	f := filesIn(base, 4)
	if err := f.write(testInstance(), testExe, false); err != nil {
		t.Fatalf("write: %v", err)
	}
	for path, mode := range map[string]os.FileMode{f.xl2tpd: 0o600, f.pppOptions: 0o600, f.secrets: 0o600, f.ipUp: 0o700, f.ipDown: 0o700} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("%s: %v, mode %v, want %v", filepath.Base(path), err, info.Mode().Perm(), mode)
		}
	}
	f.remove()
	if _, err := os.Stat(f.dir); !os.IsNotExist(err) {
		t.Fatalf("the directory outlived the server: %v", err)
	}
}
