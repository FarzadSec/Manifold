package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/eventbus"
)

type heard struct {
	mu  sync.Mutex
	evs []eventbus.Event
}

func (h *heard) take(t *testing.T, n int) []eventbus.Event {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		if len(h.evs) >= n {
			out := h.evs
			h.evs = nil
			h.mu.Unlock()
			return out
		}
		h.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	out := h.evs
	h.evs = nil
	return out
}

func TestPublishUserChangesNamesWhatSucceeded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	dbtest.InitDB(t, filepath.Join(dbDir, "x-ui.db"))
	if err := database.GetDB().Create(&model.ClientRecord{Email: "bob", SubID: "b", Enable: true, OwnerId: 7}).Error; err != nil {
		t.Fatal(err)
	}
	bus := eventbus.New(16)
	t.Cleanup(bus.Stop)
	got := &heard{}
	bus.Subscribe("t", func(ev eventbus.Event) { got.mu.Lock(); got.evs = append(got.evs, ev); got.mu.Unlock() })
	prev := eventBus
	SetEventBus(bus)
	t.Cleanup(func() { SetEventBus(prev) })

	r := gin.New()
	api := r.Group("/panel/api")
	api.Use(publishUserChanges)
	ok := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"success": true}) }
	api.POST("/clients/add", ok)
	api.POST("/clients/del/:email", func(c *gin.Context) {
		database.GetDB().Where("email = ?", c.Param("email")).Delete(&model.ClientRecord{})
		ok(c)
	})
	api.POST("/clients/bulkDel", ok)
	api.POST("/clients/update/:email", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"success": false, "msg": "no"}) })
	api.POST("/clients/onlines", ok)
	post := func(path string, body any) {
		b, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b)))
	}
	type seen struct {
		Type  eventbus.EventType
		Email string
		Owner int
	}
	view := func(evs []eventbus.Event) []seen {
		out := []seen{}
		for _, e := range evs {
			s := seen{Type: e.Type, Email: e.Source}
			if d, ok := e.Data.(eventbus.UserEventData); ok {
				s.Owner = d.OwnerId
			}
			out = append(out, s)
		}
		return out
	}

	post("/panel/api/clients/add", map[string]any{"client": map[string]any{"email": "bob"}, "inboundIds": []int{1}})
	if want := []seen{{eventbus.EventUserCreated, "bob", 7}}; !slices.Equal(view(got.take(t, 1)), want) {
		t.Fatalf("create: want %v", want)
	}
	post("/panel/api/clients/update/bob", map[string]any{"enable": false})
	post("/panel/api/clients/onlines", map[string]any{})
	post("/panel/api/clients/del/bob", nil)
	if want := []seen{{eventbus.EventUserDeleted, "bob", 7}}; !slices.Equal(view(got.take(t, 1)), want) {
		t.Fatalf("a failed update and a read publish nothing; a delete keeps the owner it had: want %v", want)
	}
	post("/panel/api/clients/bulkDel", map[string]any{"emails": []string{"x", "y"}})
	evs := got.take(t, 1)
	if len(evs) != 1 || evs[0].Type != eventbus.EventUsersBulkChanged || !slices.Equal(evs[0].Data.(eventbus.ChangeEventData).Emails, []string{"x", "y"}) {
		t.Fatalf("bulk delete published %+v", evs)
	}
}
