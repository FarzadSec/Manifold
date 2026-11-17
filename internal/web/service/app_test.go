package service

import (
	"errors"
	"reflect"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"

	"gorm.io/gorm"
)

func initAppTestDB(t *testing.T, enabled bool) *AppService {
	t.Helper()
	initClientHwidTestDB(t)
	settings := &SettingService{}
	if err := settings.setBool("appEnable", enabled); err != nil {
		t.Fatalf("set appEnable: %v", err)
	}
	return &AppService{}
}

func appDevice(id string) AppDevice {
	return AppDevice{ID: id, Name: "office-pc", OS: "windows", OSVersion: "11", AppVersion: "0.1.0", UserAgent: "DTClient/0.1.0"}
}

func TestAppLoginRefusedWhileAppAccessIsOff(t *testing.T) {
	svc := initAppTestDB(t, false)
	rec := seedHwidClient(t, 0)
	if _, err := svc.Login(rec.SubID, rec.Email, appDevice("device-one")); !errors.Is(err, ErrAppDisabled) {
		t.Fatalf("login with app access off = %v, want ErrAppDisabled", err)
	}
}

func TestAppSessionsStopWhenAppAccessIsTurnedOff(t *testing.T) {
	svc := initAppTestDB(t, true)
	rec := seedHwidClient(t, 0)
	token, err := svc.Login(rec.SubID, rec.Email, appDevice("device-one"))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if err := (&SettingService{}).setBool("appEnable", false); err != nil {
		t.Fatalf("turn app access off: %v", err)
	}
	if _, err := svc.Session(rec.SubID, token, "device-one"); !errors.Is(err, ErrAppDisabled) {
		t.Fatalf("session with app access off = %v, want ErrAppDisabled", err)
	}
}

func TestAppLoginNeedsTheUsernameOfTheLinksAccount(t *testing.T) {
	svc := initAppTestDB(t, true)
	rec := seedHwidClient(t, 0)
	other := &model.ClientRecord{Email: "other@example.com", SubID: "sub-other", UUID: "33333333-2222-4333-8444-555555555555", Enable: true}
	if err := database.GetDB().Create(other).Error; err != nil {
		t.Fatalf("seed other client: %v", err)
	}
	for _, tc := range []struct {
		name, subID, username string
	}{
		{"wrong username", rec.SubID, "guess@example.com"},
		{"another account's username", rec.SubID, other.Email},
		{"username without its link", "sub-unknown", rec.Email},
		{"empty username", rec.SubID, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Login(tc.subID, tc.username, appDevice("device-one")); !errors.Is(err, ErrAppBadLogin) {
				t.Fatalf("login = %v, want ErrAppBadLogin", err)
			}
		})
	}
	if _, err := svc.Login(rec.SubID, "  HWID@Example.com ", appDevice("device-one")); err != nil {
		t.Fatalf("the username differs only in case and spaces: %v", err)
	}
}

// The app and the proxy apps a subscription link feeds share one device limit.
func TestAppLoginTakesASlotOfTheSharedDeviceLimit(t *testing.T) {
	svc := initAppTestDB(t, true)
	rec := seedHwidClient(t, 1)
	clients := &ClientService{}
	if res, err := clients.EnforceHwidForSubID(rec.SubID, HwidRequest{Hwid: "iphone-streisand"}); err != nil || !res.Allowed {
		t.Fatalf("an iOS app takes the only slot: %+v %v", res, err)
	}
	if _, err := svc.Login(rec.SubID, rec.Email, appDevice("device-one")); !errors.Is(err, ErrAppDeviceLimit) {
		t.Fatalf("login past the device limit = %v, want ErrAppDeviceLimit", err)
	}
	var sessions int64
	database.GetDB().Model(&model.AppSession{}).Count(&sessions)
	if sessions != 0 {
		t.Fatalf("a refused login left %d sessions", sessions)
	}

	if err := clients.ClearClientHwids(rec.Email); err != nil {
		t.Fatalf("clear devices: %v", err)
	}
	if _, err := svc.Login(rec.SubID, rec.Email, appDevice("device-one")); err != nil {
		t.Fatalf("login into a free slot: %v", err)
	}
	if res, err := clients.EnforceHwidForSubID(rec.SubID, HwidRequest{Hwid: "iphone-streisand"}); err != nil || res.Allowed {
		t.Fatalf("the app's device must hold the slot against the iOS app: %+v %v", res, err)
	}
}

func TestAppLoginRequiresADeviceID(t *testing.T) {
	svc := initAppTestDB(t, true)
	rec := seedHwidClient(t, 0)
	if _, err := svc.Login(rec.SubID, rec.Email, appDevice("abc")); !errors.Is(err, ErrAppBadDevice) {
		t.Fatalf("login with a short device id = %v, want ErrAppBadDevice", err)
	}
}

func TestAppSessionIsBoundToItsSubscriptionAccountAndDevice(t *testing.T) {
	svc := initAppTestDB(t, true)
	rec := seedHwidClient(t, 0)
	token, err := svc.Login(rec.SubID, rec.Email, appDevice("device-one"))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	session, err := svc.Session(rec.SubID, token, "device-one")
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	if session.Email != rec.Email || session.DeviceName != "office-pc" || session.AppVersion != "0.1.0" {
		t.Fatalf("session = %+v", session)
	}
	for _, tc := range []struct {
		name, subID, token, device string
	}{
		{"another device", rec.SubID, token, "device-two"},
		{"another subscription", "sub-other", token, "device-one"},
		{"unknown token", rec.SubID, token + "x", "device-one"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Session(tc.subID, tc.token, tc.device); !errors.Is(err, ErrAppSession) {
				t.Fatalf("session = %v, want ErrAppSession", err)
			}
		})
	}

	if err := database.GetDB().Model(&model.ClientRecord{}).Where("id = ?", rec.Id).Update("sub_id", "sub-rotated").Error; err != nil {
		t.Fatalf("rotate the link: %v", err)
	}
	if _, err := svc.Session(rec.SubID, token, "device-one"); !errors.Is(err, ErrAppSession) {
		t.Fatalf("a session outlived its rotated link: %v", err)
	}
	if _, err := svc.Session("sub-rotated", token, "device-one"); !errors.Is(err, ErrAppSession) {
		t.Fatalf("a session from before the rotation works with the new link: %v", err)
	}
}

func TestAppSessionEndsWhenTheDeviceLosesItsSlot(t *testing.T) {
	svc := initAppTestDB(t, true)
	rec := seedHwidClient(t, 1)
	clients := &ClientService{}
	token, err := svc.Login(rec.SubID, rec.Email, appDevice("device-one"))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	list, err := clients.ListClientHwids(rec.Email)
	if err != nil || len(list) != 1 || list[0].DeviceModel != "office-pc" || list[0].DeviceOS != "windows" {
		t.Fatalf("the app's device is not listed with the account's devices: %+v %v", list, err)
	}
	if err := clients.DeleteClientHwid(rec.Email, list[0].Id); err != nil {
		t.Fatalf("revoke the device: %v", err)
	}
	if _, err := svc.Session(rec.SubID, token, "device-one"); !errors.Is(err, ErrAppSession) {
		t.Fatalf("a revoked device kept its session: %v", err)
	}
}

func TestAppSessionRefusedOnceTheLimitIsFullWithoutIt(t *testing.T) {
	svc := initAppTestDB(t, true)
	rec := seedHwidClient(t, 2)
	clients := &ClientService{}
	token, err := svc.Login(rec.SubID, rec.Email, appDevice("device-one"))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if res, err := clients.EnforceHwidForSubID(rec.SubID, HwidRequest{Hwid: "android-v2rayng"}); err != nil || !res.Allowed {
		t.Fatalf("second device: %+v %v", res, err)
	}
	if err := runSerializedTx(func(tx *gorm.DB) error { return clients.setClientLimitHwidByEmailTx(tx, rec.Email, 1) }); err != nil {
		t.Fatalf("lower the limit: %v", err)
	}
	if _, err := svc.Session(rec.SubID, token, "device-one"); !errors.Is(err, ErrAppDeviceLimit) {
		t.Fatalf("session of a device trimmed off the limit = %v, want ErrAppDeviceLimit", err)
	}
}

func TestAppLoginReplacesTheDevicesEarlierSession(t *testing.T) {
	svc := initAppTestDB(t, true)
	rec := seedHwidClient(t, 0)
	first, err := svc.Login(rec.SubID, rec.Email, appDevice("device-one"))
	if err != nil {
		t.Fatalf("first login: %v", err)
	}
	second, err := svc.Login(rec.SubID, rec.Email, appDevice("device-one"))
	if err != nil {
		t.Fatalf("second login: %v", err)
	}
	if _, err := svc.Session(rec.SubID, first, "device-one"); !errors.Is(err, ErrAppSession) {
		t.Fatalf("the earlier token still works: %v", err)
	}
	if _, err := svc.Session(rec.SubID, second, "device-one"); err != nil {
		t.Fatalf("the new token: %v", err)
	}
}

func TestAppLogoutEndsTheSession(t *testing.T) {
	svc := initAppTestDB(t, true)
	rec := seedHwidClient(t, 0)
	token, err := svc.Login(rec.SubID, rec.Email, appDevice("device-one"))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if err := svc.Logout(rec.SubID, token); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := svc.Session(rec.SubID, token, "device-one"); !errors.Is(err, ErrAppSession) {
		t.Fatalf("session after logout = %v, want ErrAppSession", err)
	}
}

func TestAppSessionsGoWithTheirSubscription(t *testing.T) {
	svc := initAppTestDB(t, true)
	rec := seedHwidClient(t, 0)
	if _, err := svc.Login(rec.SubID, rec.Email, appDevice("device-one")); err != nil {
		t.Fatalf("login: %v", err)
	}
	if err := runSerializedTx(func(tx *gorm.DB) error { return clearClientHwidsBySubIDTx(tx, rec.SubID) }); err != nil {
		t.Fatalf("clear the subscription's devices: %v", err)
	}
	var left int64
	database.GetDB().Model(&model.AppSession{}).Where("sub_id = ?", rec.SubID).Count(&left)
	if left != 0 {
		t.Fatalf("%d app sessions outlived their subscription's devices", left)
	}
}

func seedLocationInbounds(t *testing.T) (panelVless, panelOvpn, nodeVless model.Inbound) {
	t.Helper()
	db := database.GetDB()
	node := &model.Node{Name: "de-fra-1", Address: "node1.example.com", Port: 2053}
	if err := db.Create(node).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}
	nodeID := node.Id
	inbounds := []*model.Inbound{
		{Tag: "vless-ir", Port: 8443, Protocol: model.VLESS, Enable: true, Settings: `{"clients":[]}`},
		{Tag: "ovpn-ir", Port: 1194, Protocol: model.OpenVPN, Enable: true, Settings: `{"clients":[]}`},
		{Tag: "vless-de", Port: 8443, Protocol: model.VLESS, Enable: true, Settings: `{"clients":[]}`, NodeID: &nodeID},
	}
	for _, ib := range inbounds {
		if err := db.Create(ib).Error; err != nil {
			t.Fatalf("seed inbound: %v", err)
		}
	}
	return *inbounds[0], *inbounds[1], *inbounds[2]
}

// A location gathers the inbounds that serve one place, whatever their protocol or server.
func TestAppLocationsGatherInboundsOfAnyProtocolAndServer(t *testing.T) {
	svc := initAppTestDB(t, true)
	vlessIR, ovpnIR, vlessDE := seedLocationInbounds(t)

	iran, err := svc.SaveLocation(model.AppLocation{Name: " Iran ", CountryCode: "ir", City: "Tehran", Timezone: "Asia/Tehran", InboundIds: []int{ovpnIR.Id, vlessIR.Id}})
	if err != nil {
		t.Fatalf("save Iran: %v", err)
	}
	germany, err := svc.SaveLocation(model.AppLocation{Name: "Germany", CountryCode: "DE", InboundIds: []int{vlessDE.Id}})
	if err != nil {
		t.Fatalf("save Germany: %v", err)
	}
	got, err := svc.Locations()
	if err != nil {
		t.Fatalf("locations: %v", err)
	}
	want := []model.AppLocation{
		{Id: iran.Id, Name: "Iran", CountryCode: "IR", City: "Tehran", Timezone: "Asia/Tehran", InboundIds: []int{vlessIR.Id, ovpnIR.Id}},
		{Id: germany.Id, Name: "Germany", CountryCode: "DE", InboundIds: []int{vlessDE.Id}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("locations = %+v, want %+v", got, want)
	}

	iran.InboundIds = []int{ovpnIR.Id}
	if _, err := svc.SaveLocation(iran); err != nil {
		t.Fatalf("narrow Iran: %v", err)
	}
	byInbound, err := svc.LocationOfInbounds()
	if err != nil {
		t.Fatal(err)
	}
	if want := map[int]int{ovpnIR.Id: iran.Id, vlessDE.Id: germany.Id}; !reflect.DeepEqual(byInbound, want) {
		t.Fatalf("location of each inbound = %v, want %v", byInbound, want)
	}
}

func TestAppSaveLocationRefusesWhatTheAppCannotUse(t *testing.T) {
	svc := initAppTestDB(t, true)
	vlessIR, _, vlessDE := seedLocationInbounds(t)
	if _, err := svc.SaveLocation(model.AppLocation{Name: "Iran", InboundIds: []int{vlessIR.Id}}); err != nil {
		t.Fatalf("save Iran: %v", err)
	}
	for _, tc := range []struct {
		name string
		loc  model.AppLocation
		want error
	}{
		{"no name", model.AppLocation{Name: "  "}, ErrAppLocationName},
		{"three-letter country", model.AppLocation{Name: "x", CountryCode: "IRN"}, ErrAppBadCountry},
		{"country with a digit", model.AppLocation{Name: "x", CountryCode: "I1"}, ErrAppBadCountry},
		{"made-up timezone", model.AppLocation{Name: "x", Timezone: "Mars/Olympus"}, ErrAppBadTimezone},
		{"unknown inbound", model.AppLocation{Name: "x", InboundIds: []int{9999}}, ErrAppUnknownInbound},
		{"inbound of another location", model.AppLocation{Name: "Germany", InboundIds: []int{vlessDE.Id, vlessIR.Id}}, ErrAppInboundTaken},
		{"unknown location", model.AppLocation{Id: 77, Name: "x"}, ErrAppUnknownLocation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.SaveLocation(tc.loc); !errors.Is(err, tc.want) {
				t.Fatalf("save = %v, want %v", err, tc.want)
			}
		})
	}
}

// SQLite hands a deleted inbound's id to the next one, which must not join its location.
func TestDeletingAnInboundTakesItOutOfItsLocation(t *testing.T) {
	svc := initAppTestDB(t, true)
	vlessIR, ovpnIR, _ := seedLocationInbounds(t)
	iran, err := svc.SaveLocation(model.AppLocation{Name: "Iran", InboundIds: []int{vlessIR.Id, ovpnIR.Id}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&InboundService{}).DelInbound(vlessIR.Id); err != nil {
		t.Fatalf("delete inbound: %v", err)
	}
	got, err := svc.Locations()
	if err != nil || len(got) != 1 || !reflect.DeepEqual(got[0].InboundIds, []int{ovpnIR.Id}) {
		t.Fatalf("locations = %+v %v, want Iran with the OpenVPN inbound only", got, err)
	}

	if err := svc.DeleteLocation(iran.Id); err != nil {
		t.Fatalf("delete location: %v", err)
	}
	if byInbound, err := svc.LocationOfInbounds(); err != nil || len(byInbound) != 0 {
		t.Fatalf("a deleted location still holds %v (%v)", byInbound, err)
	}
}
