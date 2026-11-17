package controller

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/eventbus"
	"github.com/mhsanaei/3x-ui/v3/internal/web/access"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// eventBus carries the change events this package publishes; nil until the server sets it.
var eventBus *eventbus.Bus

// SetEventBus gives the API the bus its user change events go out on.
func SetEventBus(b *eventbus.Bus) { eventBus = b }

// successWriter remembers whether a JSON answer said success, the API's own verdict.
type successWriter struct {
	gin.ResponseWriter
	head []byte
}

func (w *successWriter) Write(b []byte) (int, error) {
	if len(w.head) < 512 {
		w.head = append(w.head, b[:min(len(b), 512-len(w.head))]...)
	}
	return w.ResponseWriter.Write(b)
}

func (w *successWriter) succeeded() bool {
	return w.Status() == http.StatusOK && bytes.Contains(w.head, []byte(`"success":true`))
}

// publishUserChanges turns a successful change to users into user.created, user.updated,
// user.deleted, or users.bulk_changed when one request touched several.
func publishUserChanges(c *gin.Context) {
	rel := relAPIPath(c.FullPath())
	if c.Request.Method != http.MethodPost || !strings.HasPrefix(rel, "/clients/") || eventBus == nil {
		c.Next()
		return
	}
	entity, action := auditEntity(rel)
	rule, _ := access.RouteRule(c.Request.Method, rel)
	if entity != "users" || strings.HasSuffix(rule.Perm, ":read") {
		c.Next()
		return
	}
	var raw []byte
	if c.Request.Body != nil {
		raw, _ = io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
		c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	}
	emails := changedEmails(c.Param("email"), raw)
	var gone eventbus.UserEventData
	if action == "delete" && len(emails) == 1 {
		gone, _ = service.UserEventData(emails[0])
	}
	w := &successWriter{ResponseWriter: c.Writer}
	c.Writer = w
	c.Next()
	if !w.succeeded() {
		return
	}
	actor := ""
	if a := access.ActorFrom(c); a != nil {
		actor = a.Username
	}
	now := time.Now()
	if len(emails) != 1 {
		eventBus.Publish(eventbus.Event{
			Type: eventbus.EventUsersBulkChanged, Source: actor, Timestamp: now,
			Data: eventbus.ChangeEventData{Actor: actor, Route: rel, Emails: emails},
		})
		return
	}
	if action == "delete" {
		eventBus.Publish(eventbus.Event{Type: eventbus.EventUserDeleted, Source: emails[0], Data: gone, Timestamp: now})
		return
	}
	typ := eventbus.EventUserUpdated
	if action == "create" {
		typ = eventbus.EventUserCreated
	}
	data, _ := service.UserEventData(emails[0])
	eventBus.Publish(eventbus.Event{Type: typ, Source: emails[0], Data: data, Timestamp: now})
}

// changedEmails is the users a request names: its path, else every "email" in its body.
func changedEmails(fromPath string, body []byte) []string {
	if fromPath != "" {
		return []string{fromPath}
	}
	var v any
	if json.Unmarshal(body, &v) != nil {
		return nil
	}
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			for k, val := range t {
				if s, ok := val.(string); ok && (k == "email" || k == "emails") && s != "" && !slices.Contains(out, s) {
					out = append(out, s)
					continue
				}
				if list, ok := val.([]any); ok && k == "emails" {
					for _, e := range list {
						if s, ok := e.(string); ok && !slices.Contains(out, s) {
							out = append(out, s)
						}
					}
					continue
				}
				walk(val)
			}
		case []any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
	return out
}
