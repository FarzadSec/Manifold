package service

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/eventbus"
	"github.com/mhsanaei/3x-ui/v3/internal/web/access"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestSignMatchesTheDocumentedRecipe(t *testing.T) {
	// python3 -c 'import hmac,hashlib;print(hmac.new(b"s3cret",b"1700000000.{\"a\":1}",hashlib.sha256).hexdigest())'
	want := "sha256=1698a50bc74d1ff1db85c4e0a5297c2ad9fdba245d5737cdb789e4cc6e098940"
	if got := Sign("s3cret", "1700000000", []byte(`{"a":1}`)); got != want {
		t.Fatalf("Sign = %s, want %s", got, want)
	}
}

type hookReceiver struct {
	mu     sync.Mutex
	status int
	got    []*http.Request
	bodies [][]byte
}

func (h *hookReceiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.got = append(h.got, r)
	h.bodies = append(h.bodies, body)
	w.WriteHeader(h.status)
}

func deliveryRows(t *testing.T) []model.WebhookDelivery {
	t.Helper()
	var rows []model.WebhookDelivery
	if err := database.GetDB().Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestDispatchSignsDeliversAndRetries(t *testing.T) {
	setupBulkDB(t)
	recv := &hookReceiver{status: http.StatusOK}
	srv := httptest.NewServer(recv)
	defer srv.Close()
	s := &WebhookService{}
	secret := "topsecret"
	view, err := s.Save(WebhookInput{Name: "bot", Enable: true, Kind: "http", URL: srv.URL, Secret: &secret}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if view.Secret != "" || !view.HasSecret {
		t.Fatalf("a secret the admin typed is not echoed back: %+v", view)
	}
	ev := eventbus.Event{
		Type: eventbus.EventUserLimited, Source: "alice", Timestamp: time.Unix(1700000000, 0),
		Data: eventbus.UserEventData{Email: "alice", Total: 100, Up: 60, Down: 40},
	}
	if err := s.Enqueue(ev); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.Dispatch(now)
	if len(recv.got) != 1 {
		t.Fatalf("receiver got %d requests, want 1", len(recv.got))
	}
	r, body := recv.got[0], recv.bodies[0]
	ts := r.Header.Get("X-Manifold-Timestamp")
	if r.Header.Get(webhookSigHeader) != Sign(secret, ts, body) {
		t.Fatalf("signature %q does not verify over the body that arrived", r.Header.Get(webhookSigHeader))
	}
	var p WebhookPayload
	if err := json.Unmarshal(body, &p); err != nil || p.Type != "user.limited" || r.Header.Get("X-Manifold-Delivery") != p.Id {
		t.Fatalf("payload %s, delivery header %q", body, r.Header.Get("X-Manifold-Delivery"))
	}
	if rows := deliveryRows(t); rows[0].Status != "delivered" || rows[0].Attempts != 1 || rows[0].LastCode != 200 {
		t.Fatalf("after a 200 the row is %+v", rows[0])
	}

	recv.status = http.StatusBadGateway
	if err := s.Enqueue(ev); err != nil {
		t.Fatal(err)
	}
	now = time.Now()
	s.Dispatch(now)
	failed := deliveryRows(t)[1]
	if failed.Status != "pending" || failed.Attempts != 1 || failed.NextAt != now.Add(webhookBackoff[0]).UnixMilli() {
		t.Fatalf("a 502 is retried after the first backoff, row %+v", failed)
	}
	s.Dispatch(now.Add(webhookBackoff[0] - time.Second))
	if len(recv.got) != 3-1 {
		t.Fatalf("a retry went out before its time: %d requests", len(recv.got))
	}
	at := now
	for i := range webhookBackoff {
		at = at.Add(webhookBackoff[i])
		s.Dispatch(at)
	}
	gaveUp := deliveryRows(t)[1]
	if gaveUp.Status != "failed" || gaveUp.Attempts != len(webhookBackoff)+1 || !strings.Contains(gaveUp.LastError, "502") {
		t.Fatalf("after every retry the row is %+v", gaveUp)
	}

	recv.status = http.StatusOK
	if err := s.Replay(gaveUp.Id, nil); err != nil {
		t.Fatal(err)
	}
	s.Dispatch(time.Now())
	if row := deliveryRows(t)[1]; row.Status != "delivered" {
		t.Fatalf("a replayed delivery went %+v", row)
	}
}

func TestEnqueueHonoursEventsAndResellerScope(t *testing.T) {
	setupBulkDB(t)
	db := database.GetDB()
	reseller := model.User{Username: "res", Tier: access.TierReseller}
	if err := db.Create(&reseller).Error; err != nil {
		t.Fatal(err)
	}
	s := &WebhookService{}
	staff, err := s.Save(WebhookInput{Name: "all", Enable: true, Kind: "ntfy", URL: "https://ntfy.sh/x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	only, err := s.Save(WebhookInput{Name: "expired", Enable: true, Kind: "ntfy", URL: "https://ntfy.sh/y", Events: []string{"user.expired"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	resActor := &access.Actor{UserId: reseller.Id, Tier: access.TierReseller}
	own, err := s.Save(WebhookInput{Name: "mine", Enable: true, Kind: "ntfy", URL: "https://ntfy.sh/z"}, resActor)
	if err != nil {
		t.Fatal(err)
	}
	if list, _ := s.List(resActor); len(list) != 1 || list[0].Id != own.Id {
		t.Fatalf("a reseller sees %+v, want only its own webhook", list)
	}
	if _, err := s.Save(WebhookInput{Id: staff.Id, Name: "taken", Kind: "ntfy", URL: "https://ntfy.sh/x"}, resActor); err == nil {
		t.Fatal("a reseller changed a staff webhook")
	}

	send := func(t eventbus.EventType, owner int) {
		_ = s.Enqueue(eventbus.Event{Type: t, Source: "u", Data: eventbus.UserEventData{Email: "u", OwnerId: owner}})
	}
	send(eventbus.EventUserLimited, 0)
	send(eventbus.EventUserExpired, reseller.Id)
	_ = s.Enqueue(eventbus.Event{Type: eventbus.EventNodeDown, Source: "n1"})
	got := map[int][]string{}
	for _, d := range deliveryRows(t) {
		got[d.WebhookId] = append(got[d.WebhookId], d.Event)
	}
	if want := []string{"user.limited", "user.expired", "node.down"}; !slices.Equal(got[staff.Id], want) {
		t.Fatalf("the staff webhook got %v, want %v", got[staff.Id], want)
	}
	if want := []string{"user.expired"}; !slices.Equal(got[only.Id], want) {
		t.Fatalf("the filtered webhook got %v, want %v", got[only.Id], want)
	}
	if want := []string{"user.expired"}; !slices.Equal(got[own.Id], want) {
		t.Fatalf("the reseller webhook got %v, want only its own user's event", got[own.Id])
	}
}

func TestSaveGeneratesAnHTTPSecretOnce(t *testing.T) {
	setupBulkDB(t)
	s := &WebhookService{}
	made, err := s.Save(WebhookInput{Name: "bot", Kind: "http", URL: "https://bot.example.com/h"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(made.Secret) < 32 {
		t.Fatalf("a new HTTP webhook shows its generated secret once, got %q", made.Secret)
	}
	again, err := s.Save(WebhookInput{Id: made.Id, Name: "bot", Kind: "http", URL: "https://bot.example.com/h2"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var stored model.Webhook
	database.GetDB().First(&stored, made.Id)
	if again.Secret != "" || stored.Secret != made.Secret {
		t.Fatalf("an edit without a secret keeps the first one: shown %q, stored %q", again.Secret, stored.Secret)
	}
	if _, err := s.Save(WebhookInput{Name: "x", Kind: "http", URL: "ftp://bot"}, nil); err == nil {
		t.Fatal("an ftp URL was taken")
	}
	if _, err := s.Save(WebhookInput{Name: "x", Kind: "ntfy", URL: "https://ntfy.sh/a", Events: []string{"user.nope"}}, nil); err == nil {
		t.Fatal("an unknown event was taken")
	}
}

func TestWatchUsersPublishesOnlyChanges(t *testing.T) {
	setupBulkDB(t)
	db := database.GetDB()
	userWatch.Lock()
	userWatch.seen = map[string]userState{}
	userWatch.Unlock()
	now := time.Now()
	day := 24 * time.Hour
	if err := db.Create(&model.ClientRecord{Email: "alice", SubID: "a", Enable: true}).Error; err != nil {
		t.Fatal(err)
	}
	traffic := xray.ClientTraffic{Email: "alice", Enable: true, Total: 1000, Up: 100, ExpiryTime: now.Add(10 * day).UnixMilli()}
	if err := db.Create(&traffic).Error; err != nil {
		t.Fatal(err)
	}
	if evs := WatchUsers(now); len(evs) != 0 {
		t.Fatalf("the first look announced %v", evs)
	}
	types := func(evs []eventbus.Event) []string {
		out := []string{}
		for _, e := range evs {
			out = append(out, string(e.Type))
		}
		return out
	}
	step := func(name string, change map[string]any, want ...string) {
		t.Helper()
		if len(change) > 0 {
			db.Model(&xray.ClientTraffic{}).Where("email = ?", "alice").Updates(change)
		}
		if got := types(WatchUsers(now)); !slices.Equal(got, append([]string{}, want...)) {
			t.Fatalf("%s: got %v, want %v", name, got, want)
		}
	}
	step("nothing changed", nil)
	step("first connection", map[string]any{"last_online": now.UnixMilli()}, "user.first_connected")
	step("80% used", map[string]any{"down": 700}, "user.quota_warning")
	step("still 85%", map[string]any{"down": 750})
	step("quota reached and disabled", map[string]any{"down": 900, "enable": false}, "user.disabled", "user.limited")
	step("reset and enabled", map[string]any{"up": 0, "down": 0, "enable": true}, "user.enabled", "user.traffic_reset")
	step("straight past the limit", map[string]any{"down": 1000}, "user.limited")
	step("a day left", map[string]any{"expiry_time": now.Add(day / 2).UnixMilli()}, "user.expiring")
	step("expired", map[string]any{"expiry_time": now.Add(-time.Minute).UnixMilli()}, "user.expired")
}

func TestResellerWebhookCannotReachThisServer(t *testing.T) {
	setupBulkDB(t)
	recv := &hookReceiver{status: http.StatusOK}
	srv := httptest.NewServer(recv)
	defer srv.Close()
	reseller := model.User{Username: "res", Tier: access.TierReseller}
	if err := database.GetDB().Create(&reseller).Error; err != nil {
		t.Fatal(err)
	}
	s := &WebhookService{}
	staff, _ := s.Save(WebhookInput{Name: "staff", Enable: true, Kind: "http", URL: srv.URL}, nil)
	res, err := s.Save(WebhookInput{Name: "res", Enable: true, Kind: "http", URL: srv.URL},
		&access.Actor{UserId: reseller.Id, Tier: access.TierReseller})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Enqueue(eventbus.Event{Type: eventbus.EventUserLimited, Data: eventbus.UserEventData{Email: "u", OwnerId: reseller.Id}})
	s.Dispatch(time.Now())
	byHook := map[int]model.WebhookDelivery{}
	for _, d := range deliveryRows(t) {
		byHook[d.WebhookId] = d
	}
	if byHook[staff.Id].Status != "delivered" {
		t.Fatalf("a staff webhook to this server went %+v", byHook[staff.Id])
	}
	if got := byHook[res.Id]; got.Status != "pending" || !strings.Contains(got.LastError, errNotPublic.Error()) {
		t.Fatalf("a reseller webhook to 127.0.0.1 went %+v", got)
	}
	if len(recv.got) != 1 {
		t.Fatalf("this server was reached %d times, want once (by staff)", len(recv.got))
	}
}
