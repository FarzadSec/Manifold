package service

import (
	"net/netip"
	"reflect"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/speedlimit"
)

func TestDesiredAccountsCarryEveryTunnelTheHostRunsForTheUser(t *testing.T) {
	setupConflictDB(t)
	seedInboundConflict(t, "wg-speed", "", 48011, model.WireGuard, "", wgTestSettings("",
		wgTestClient("alice", wgTestAliceKey, "10.0.0.2/32", true),
		wgTestClient("bob", wgTestBobKey, "10.0.0.3/32", true)))
	served := loadInboundByTag(t, "wg-speed")
	seedClientTraffic(t, served.Id, "alice", true)
	seedClientTraffic(t, served.Id, "bob", true)
	records := []model.ClientRecord{
		{Email: "alice", SpeedLimit: 20, Enable: true},
		{Email: "bob", Enable: true},
		{Email: "carol", SpeedLimit: 5, Enable: true},
		{Email: "dave", SpeedLimit: 5, Enable: false},
	}
	for i := range records {
		if err := database.GetDB().Create(&records[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Enable's default turns a created false into true.
	if err := database.GetDB().Model(&model.ClientRecord{}).Where("email = ?", "dave").Update("enable", false).Error; err != nil {
		t.Fatal(err)
	}
	prev := speedSessions
	t.Cleanup(func() { speedSessions = prev })
	speedSessions.openvpn = func() map[string][]netip.Prefix {
		return map[string][]netip.Prefix{"Alice": {netip.MustParsePrefix("10.8.0.6/32")}, "bob": {netip.MustParsePrefix("10.8.0.7/32")}}
	}
	speedSessions.l2tp = func() map[string][]string { return map[string][]string{"carol": {"ppp2"}} }

	got, err := (&SpeedService{}).DesiredAccounts()
	if err != nil {
		t.Fatal(err)
	}
	want := []speedlimit.Account{
		{ID: records[0].Id, Mbps: 20, Addrs: []netip.Prefix{netip.MustParsePrefix("10.0.0.2/32"), netip.MustParsePrefix("10.8.0.6/32")}},
		{ID: records[2].Id, Mbps: 5, Ifaces: []string{"ppp2"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("accounts = %+v\nwant %+v", got, want)
	}
}
