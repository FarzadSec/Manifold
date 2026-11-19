package sub

import (
	"net/http"
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/seats"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

func TestAppDisconnectedFreesTheSeatItNames(t *testing.T) {
	nodeID := seedAppAccount(t, true)
	db := database.GetDB()
	wg := &model.Inbound{UserId: 1, Tag: "wg-de", Remark: "wg-de", Enable: true, Port: 51820, Protocol: model.WireGuard, NodeID: &nodeID, Settings: "{}", StreamSettings: "{}"}
	if err := db.Create(wg).Error; err != nil {
		t.Fatal(err)
	}
	var ovpn model.Inbound
	if err := db.Where("tag = ?", "ovpn").First(&ovpn).Error; err != nil {
		t.Fatal(err)
	}
	appSvc := &service.AppService{}
	iran, err := appSvc.SaveLocation(model.AppLocation{Name: "Iran", CountryCode: "IR", InboundIds: []int{ovpn.Id}})
	if err != nil {
		t.Fatal(err)
	}
	germany, err := appSvc.SaveLocation(model.AppLocation{Name: "Germany", CountryCode: "DE", InboundIds: []int{wg.Id}})
	if err != nil {
		t.Fatal(err)
	}

	ledger := seats.Local()
	var mu sync.Mutex
	var cut []string
	hold := func(id, address string) {
		ledger.Take(seats.Hold{ID: id, Account: appUser, Protocol: "openvpn", Kick: func() {
			mu.Lock()
			defer mu.Unlock()
			cut = append(cut, id)
		}}, 0, 0)
		ledger.Confirm(id, address)
	}
	hold("openvpn/test#1/6", "10.201.0.6")
	hold("openvpn/test#1/7", "10.201.0.7")
	t.Cleanup(func() {
		ledger.Release("openvpn/test#1/6")
		ledger.Release("openvpn/test#1/7")
		ledger.TakeDepartures()
	})

	router := newAppTestRouter()
	token := loginApp(t, router, "/sub/")
	path := "/sub/" + appSubID + "/app/v1/disconnected"
	if code, _ := callApp(t, router, appCall{method: http.MethodPost, path: path, hwid: appDevice, body: map[string]any{"protocol": "openvpn", "locationId": iran.Id, "address": "10.201.0.6"}}); code != http.StatusUnauthorized {
		t.Fatalf("without the session = %d, want 401", code)
	}

	code, _ := callApp(t, router, appCall{method: http.MethodPost, path: path, token: token, hwid: appDevice, body: map[string]any{"protocol": "openvpn", "locationId": iran.Id, "address": "10.201.0.6"}})
	if code != http.StatusOK {
		t.Fatalf("openvpn = %d", code)
	}
	mu.Lock()
	gotCut := slices.Clone(cut)
	mu.Unlock()
	if !slices.Equal(gotCut, []string{"openvpn/test#1/6"}) {
		t.Fatalf("cut %v, want only the session at the device's address", gotCut)
	}

	code, _ = callApp(t, router, appCall{method: http.MethodPost, path: path, token: token, hwid: appDevice, body: map[string]any{"protocol": "wireguard", "locationId": germany.Id}})
	if code != http.StatusOK {
		t.Fatalf("wireguard = %d", code)
	}
	departures := ledger.TakeDepartures()
	if len(departures) != 1 || departures[0].Account != appUser || !slices.Equal(departures[0].Servers, []string{"node:" + strconv.Itoa(nodeID)}) {
		t.Fatalf("departures = %+v, want alice's WireGuard on the node of Germany", departures)
	}

	// L2TP hangs up by itself and Xray holds no seat: nothing to do, still answered.
	if code, _ := callApp(t, router, appCall{method: http.MethodPost, path: path, token: token, hwid: appDevice, body: map[string]any{"protocol": "l2tp", "locationId": iran.Id}}); code != http.StatusOK {
		t.Fatalf("l2tp = %d", code)
	}
	if got := ledger.TakeDepartures(); len(got) != 0 {
		t.Fatalf("l2tp left departures %+v", got)
	}
}
