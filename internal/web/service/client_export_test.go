package service

import (
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/openvpn"
)

// A config pack holds what a dedicated app opens, not only links it cannot read.
func TestClientAppFilesOpenInTheirApps(t *testing.T) {
	files := clientAppFiles([]string{
		openvpn.ShareLink("client\nremote vpn.example 1194\n"),
		"wireguard://cFzzLXZkPN4mG1HMf2eeSsPT1B9%2FSi0VQ6Lkc26ZDEQ%3D@vpn.example:51820?publickey=bmXOC%2BF1FxEMF9dyiK2H5%2F1SUtzH0JuVo51h2wPfgyo%3D&address=10.0.0.2%2F32&dns=1.1.1.1&keepalive=25#home%20wg",
		"vless://uuid@vpn.example:443#ignored",
	})
	if got := files["openvpn.ovpn"]; got != "client\nremote vpn.example 1194\n" {
		t.Fatalf("ovpn = %q", got)
	}
	conf := files["home_wg.conf"]
	for _, want := range []string{
		"PrivateKey = cFzzLXZkPN4mG1HMf2eeSsPT1B9/Si0VQ6Lkc26ZDEQ=\n",
		"Address = 10.0.0.2/32\n",
		"DNS = 1.1.1.1\n",
		"PublicKey = bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo=\n",
		"Endpoint = vpn.example:51820\n",
		"PersistentKeepalive = 25\n",
	} {
		if !strings.Contains(conf, want) {
			t.Errorf("conf lacks %q:\n%s", want, conf)
		}
	}
	if len(files) != 2 {
		t.Fatalf("files = %v, want only the two app files", files)
	}
}
