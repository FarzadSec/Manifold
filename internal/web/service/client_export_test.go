package service

import (
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
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

// The list and the CSV carry the user limit beside the IP limit, so neither reads as the other.
func TestTheUserLimitIsListedAndExportedApartFromTheIPLimit(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	ib := mkInbound(t, 47021, model.OpenVPN, openvpnSettings(t, `{"clients":[]}`))
	if _, err := svc.CreateOne(&InboundService{}, ib.Id, model.Client{
		Email: "ali", Password: "pw-ali", Enable: true, LimitIP: 2, UserLimit: 3,
	}); err != nil {
		t.Fatal(err)
	}
	page, err := svc.ListPaged(&InboundService{}, &SettingService{}, ClientPageParams{Page: 1, PageSize: 25})
	if err != nil || len(page.Items) != 1 || page.Items[0].LimitIP != 2 || page.Items[0].UserLimit != 3 {
		t.Fatalf("list = %+v, %v; want ali with IP limit 2 and user limit 3", page, err)
	}
	csv, err := svc.ExportCSV([]string{"ali"}, -1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(csv, "\n")
	if !strings.Contains(lines[0], ",ip_limit,user_limit,device_limit,") || !strings.Contains(lines[1], ",2,3,0,") {
		t.Fatalf("export = %q, want the IP limit then the user limit", csv)
	}
}
