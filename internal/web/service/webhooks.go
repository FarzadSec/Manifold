package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/eventbus"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/web/access"
)

const (
	webhookTimeout   = 15 * time.Second
	webhookBatch     = 50
	webhookLogKeep   = 7 * 24 * time.Hour
	defaultBaleAPI   = "https://tapi.bale.ai"
	webhookSigHeader = "X-Manifold-Signature"
)

// The waits before each retry; a delivery still failing after the last is given up.
var webhookBackoff = []time.Duration{
	30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute,
	time.Hour, 2 * time.Hour, 4 * time.Hour, 8 * time.Hour,
}

// webhookClient serves staff webhooks, which may point at a bot on this very server.
var webhookClient = &http.Client{Timeout: webhookTimeout}

// publicClient serves resellers' webhooks: only the public internet, so the delivery log
// cannot read back this server's loopback, its LAN or a cloud metadata address.
var publicClient = &http.Client{Timeout: webhookTimeout, Transport: &http.Transport{
	DialContext:         (&net.Dialer{Timeout: webhookTimeout, Control: publicOnly}).DialContext,
	TLSHandshakeTimeout: webhookTimeout,
}}

var errNotPublic = errors.New("a reseller's webhook may only reach public addresses")

var sharedSpace = netip.MustParsePrefix("100.64.0.0/10")

func publicOnly(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return errNotPublic
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return errNotPublic
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || sharedSpace.Contains(ip) {
		return errNotPublic
	}
	return nil
}

// WebhookService keeps webhooks, turns events into an outbox and delivers it with retries.
type WebhookService struct{}

// WebhookPayload is the JSON body a webhook receives.
type WebhookPayload struct {
	Id     string `json:"id" example:"0f3c2a9b5d7e4f61"`
	Type   string `json:"type" example:"user.limited"`
	Time   string `json:"time" example:"2026-11-17T03:00:00Z"`
	Source string `json:"source" example:"alice"`
	Data   any    `json:"data"`
}

// WebhookView is a webhook without its secret; Secret is filled once, when it is made.
type WebhookView struct {
	model.Webhook
	HasSecret bool   `json:"hasSecret" example:"true"`
	Secret    string `json:"secret,omitempty" example:""`
}

// WebhookInput saves a webhook; a nil Secret keeps the stored one.
type WebhookInput struct {
	Id     int      `json:"id" example:"0"`
	Name   string   `json:"name" example:"mirza"`
	Enable bool     `json:"enable" example:"true"`
	Kind   string   `json:"kind" example:"http"`
	URL    string   `json:"url" example:"https://bot.example.com/manifold"`
	ChatId string   `json:"chatId" example:""`
	Events []string `json:"events" example:"[\"user.limited\"]"`
	Secret *string  `json:"secret,omitempty"`
}

func webhookView(w model.Webhook) WebhookView {
	if w.Events == nil {
		w.Events = []string{}
	}
	return WebhookView{Webhook: w, HasSecret: w.Secret != ""}
}

// visibleTo is the webhooks one account may see and change: all for staff, its own otherwise.
func visibleTo(actor *access.Actor) func(*model.Webhook) bool {
	return func(w *model.Webhook) bool {
		return actor == nil || actor.Tier != access.TierReseller || w.UserId == actor.UserId
	}
}

func (s *WebhookService) List(actor *access.Actor) ([]WebhookView, error) {
	var rows []model.Webhook
	if err := database.GetDB().Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := []WebhookView{}
	can := visibleTo(actor)
	for i := range rows {
		if can(&rows[i]) {
			out = append(out, webhookView(rows[i]))
		}
	}
	return out, nil
}

func (s *WebhookService) owned(id int, actor *access.Actor) (model.Webhook, error) {
	var w model.Webhook
	if err := database.GetDB().First(&w, id).Error; err != nil || !visibleTo(actor)(&w) {
		return w, common.NewError("no such webhook")
	}
	return w, nil
}

// Save validates and stores a webhook; a new HTTP webhook gets a signing secret, shown once.
func (s *WebhookService) Save(in WebhookInput, actor *access.Actor) (WebhookView, error) {
	var w model.Webhook
	if in.Id > 0 {
		stored, err := s.owned(in.Id, actor)
		if err != nil {
			return WebhookView{}, err
		}
		w = stored
	} else if actor != nil {
		w.UserId = actor.UserId
	}
	w.Name, w.Enable, w.Kind = strings.TrimSpace(in.Name), in.Enable, in.Kind
	w.URL, w.ChatId = strings.TrimSpace(in.URL), strings.TrimSpace(in.ChatId)
	if in.Secret != nil {
		w.Secret = strings.TrimSpace(*in.Secret)
	}
	events := []string{}
	for _, e := range in.Events {
		if !slices.Contains(eventbus.AllEventTypes, eventbus.EventType(e)) {
			return WebhookView{}, common.NewError("unknown event:", e)
		}
		if !slices.Contains(events, e) {
			events = append(events, e)
		}
	}
	w.Events = events
	if w.Name == "" {
		return WebhookView{}, common.NewError("a webhook needs a name")
	}
	generated := ""
	switch w.Kind {
	case "http":
		if u, err := url.Parse(w.URL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return WebhookView{}, common.NewError("the webhook URL must be an http(s) address")
		}
		if w.Secret == "" {
			generated = randomToken(32)
			w.Secret = generated
		}
	case "ntfy":
		if u, err := url.Parse(w.URL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return WebhookView{}, common.NewError("the ntfy topic must be an http(s) address, such as https://ntfy.sh/my-panel")
		}
	case "bale":
		if w.Secret == "" || w.ChatId == "" {
			return WebhookView{}, common.NewError("a Bale channel needs the bot token and a chat id")
		}
	default:
		return WebhookView{}, common.NewError("a webhook is http, ntfy or bale")
	}
	if err := database.GetDB().Save(&w).Error; err != nil {
		return WebhookView{}, err
	}
	view := webhookView(w)
	view.Secret = generated
	return view, nil
}

func (s *WebhookService) Delete(id int, actor *access.Actor) error {
	if _, err := s.owned(id, actor); err != nil {
		return err
	}
	db := database.GetDB()
	if err := db.Where("webhook_id = ?", id).Delete(&model.WebhookDelivery{}).Error; err != nil {
		return err
	}
	return db.Delete(&model.Webhook{}, id).Error
}

// Deliveries is a webhook's delivery log, newest first.
func (s *WebhookService) Deliveries(id int, actor *access.Actor) ([]model.WebhookDelivery, error) {
	if _, err := s.owned(id, actor); err != nil {
		return nil, err
	}
	var rows []model.WebhookDelivery
	err := database.GetDB().Where("webhook_id = ?", id).Order("id DESC").Limit(200).Find(&rows).Error
	return rows, err
}

// Replay sends a logged delivery again, from its first attempt.
func (s *WebhookService) Replay(deliveryID int, actor *access.Actor) error {
	var d model.WebhookDelivery
	if err := database.GetDB().First(&d, deliveryID).Error; err != nil {
		return common.NewError("no such delivery")
	}
	if _, err := s.owned(d.WebhookId, actor); err != nil {
		return err
	}
	err := database.GetDB().Model(&d).Updates(map[string]any{
		"status": "pending", "attempts": 0, "next_at": time.Now().UnixMilli(), "last_error": "",
	}).Error
	wakeWebhooks()
	return err
}

// Test queues a webhook.test event for one webhook.
func (s *WebhookService) Test(id int, actor *access.Actor) error {
	w, err := s.owned(id, actor)
	if err != nil {
		return err
	}
	ev := eventbus.Event{
		Type: eventbus.EventWebhookTest, Source: w.Name, Timestamp: time.Now(),
		Data: map[string]string{"message": "Manifold says hello"},
	}
	return s.enqueueTo([]model.Webhook{w}, ev)
}

// hears reports whether a webhook takes an event: its type is chosen, and a reseller's
// webhook only hears about its own users.
func hears(w model.Webhook, ev eventbus.Event, resellers map[int]bool) bool {
	if len(w.Events) > 0 && !slices.Contains(w.Events, string(ev.Type)) {
		return false
	}
	if !resellers[w.UserId] {
		return true
	}
	data, ok := ev.Data.(eventbus.UserEventData)
	return ok && data.OwnerId == w.UserId
}

// Enqueue writes one outbox row per enabled webhook that hears the event.
func (s *WebhookService) Enqueue(ev eventbus.Event) error {
	if ev.Type == eventbus.EventWebhookTest {
		return nil
	}
	var hooks []model.Webhook
	if err := database.GetDB().Where("enable = ?", true).Find(&hooks).Error; err != nil {
		return err
	}
	if len(hooks) == 0 {
		return nil
	}
	resellers := resellerIDs()
	var targets []model.Webhook
	for _, w := range hooks {
		if hears(w, ev, resellers) {
			targets = append(targets, w)
		}
	}
	return s.enqueueTo(targets, ev)
}

func resellerIDs() map[int]bool {
	var ids []int
	database.GetDB().Model(&model.User{}).Where("tier = ?", access.TierReseller).Pluck("id", &ids)
	out := make(map[int]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func (s *WebhookService) enqueueTo(hooks []model.Webhook, ev eventbus.Event) error {
	if len(hooks) == 0 {
		return nil
	}
	at := ev.Timestamp
	if at.IsZero() {
		at = time.Now()
	}
	payload := WebhookPayload{Id: randomToken(16), Type: string(ev.Type), Time: at.UTC().Format(time.RFC3339), Source: ev.Source, Data: ev.Data}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	rows := make([]model.WebhookDelivery, 0, len(hooks))
	for _, w := range hooks {
		rows = append(rows, model.WebhookDelivery{
			WebhookId: w.Id, EventId: payload.Id, Event: payload.Type, Payload: string(body),
			Status: "pending", NextAt: now, CreatedAt: now,
		})
	}
	if err := database.GetDB().Create(&rows).Error; err != nil {
		return err
	}
	wakeWebhooks()
	return nil
}

var webhookWake = make(chan struct{}, 1)

func wakeWebhooks() {
	select {
	case webhookWake <- struct{}{}:
	default:
	}
}

// RunWebhooks delivers the outbox until ctx ends, woken by new rows or every few seconds.
func (s *WebhookService) RunWebhooks(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		s.Dispatch(time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-webhookWake:
		}
	}
}

var dispatching sync.Mutex

// Dispatch sends every delivery that is due and records how each went.
func (s *WebhookService) Dispatch(now time.Time) {
	dispatching.Lock()
	defer dispatching.Unlock()
	var due []model.WebhookDelivery
	if err := database.GetDB().Where("status = ? AND next_at <= ?", "pending", now.UnixMilli()).
		Order("id").Limit(webhookBatch).Find(&due).Error; err != nil || len(due) == 0 {
		return
	}
	resellers := resellerIDs()
	hooks := map[int]*model.Webhook{}
	for _, d := range due {
		w, ok := hooks[d.WebhookId]
		if !ok {
			var row model.Webhook
			if database.GetDB().First(&row, d.WebhookId).Error == nil {
				w = &row
			}
			hooks[d.WebhookId] = w
		}
		code, err := 0, error(nil)
		if w == nil {
			err = fmt.Errorf("the webhook was deleted")
		} else {
			code, err = deliver(*w, d, now, resellers[w.UserId])
		}
		updates := map[string]any{"attempts": d.Attempts + 1, "last_code": code, "last_error": ""}
		switch {
		case err == nil:
			updates["status"], updates["delivered_at"] = "delivered", now.UnixMilli()
		case d.Attempts >= len(webhookBackoff) || w == nil:
			updates["status"], updates["last_error"] = "failed", err.Error()
		default:
			updates["next_at"], updates["last_error"] = now.Add(webhookBackoff[d.Attempts]).UnixMilli(), err.Error()
		}
		if dbErr := database.GetDB().Model(&model.WebhookDelivery{}).Where("id = ?", d.Id).Updates(updates).Error; dbErr != nil {
			logger.Warning("webhook delivery", d.Id, "could not be recorded:", dbErr)
		}
	}
}

// Sign is the signature an HTTP webhook carries: HMAC-SHA256 of "<timestamp>.<body>".
func Sign(secret, timestamp string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(timestamp + "."))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

func deliver(w model.Webhook, d model.WebhookDelivery, now time.Time, reseller bool) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), webhookTimeout)
	defer cancel()
	var req *http.Request
	var err error
	switch w.Kind {
	case "ntfy":
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, w.URL, strings.NewReader(messageText(d)))
		if err == nil {
			req.Header.Set("Title", "Manifold: "+d.Event)
			req.Header.Set("Tags", d.Event)
			if w.Secret != "" {
				req.Header.Set("Authorization", "Bearer "+w.Secret)
			}
		}
	case "bale":
		base := w.URL
		if base == "" {
			base = defaultBaleAPI
		}
		body, _ := json.Marshal(map[string]string{"chat_id": w.ChatId, "text": messageText(d)})
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(base, "/")+"/bot"+w.Secret+"/sendMessage", bytes.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
		}
	default:
		body := []byte(d.Payload)
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
		if err == nil {
			ts := strconv.FormatInt(now.Unix(), 10)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", "Manifold-Webhook/1")
			req.Header.Set("X-Manifold-Event", d.Event)
			req.Header.Set("X-Manifold-Delivery", d.EventId)
			req.Header.Set("X-Manifold-Timestamp", ts)
			req.Header.Set(webhookSigHeader, Sign(w.Secret, ts, body))
		}
	}
	if err != nil {
		return 0, err
	}
	client := webhookClient
	if reseller {
		client = publicClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode/100 != 2 {
		return resp.StatusCode, fmt.Errorf("answered %s: %s", resp.Status, strings.TrimSpace(string(answer)))
	}
	return resp.StatusCode, nil
}

// messageText is the one-line summary ntfy and Bale show.
func messageText(d model.WebhookDelivery) string {
	var p struct {
		Source string          `json:"source"`
		Data   json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal([]byte(d.Payload), &p)
	var user eventbus.UserEventData
	if json.Unmarshal(p.Data, &user) == nil && user.Email != "" {
		used := common.FormatTraffic(user.Up + user.Down)
		total := "∞"
		if user.Total > 0 {
			total = common.FormatTraffic(user.Total)
		}
		return fmt.Sprintf("%s · %s · %s / %s", d.Event, user.Email, used, total)
	}
	if p.Source != "" {
		return d.Event + " · " + p.Source
	}
	return d.Event
}

// PruneDeliveries drops finished deliveries past what the log keeps.
func (s *WebhookService) PruneDeliveries(now time.Time) error {
	return database.GetDB().Where("status <> ? AND created_at < ?", "pending", now.Add(-webhookLogKeep).UnixMilli()).
		Delete(&model.WebhookDelivery{}).Error
}
