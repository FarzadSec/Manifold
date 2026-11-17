package controller

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/access"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

var auditSvc service.AuditService

// recordAudit stores one entry for the request's actor, whether or not change logging is on.
func recordAudit(c *gin.Context, entity, action, target string, detail any) {
	actor := access.ActorFrom(c)
	e := model.AuditEntry{Entity: entity, Action: action, Target: target, Ip: getRemoteIp(c)}
	if actor != nil {
		e.UserId, e.Username = actor.UserId, actor.Username
	}
	if detail != nil {
		if b, err := json.Marshal(detail); err == nil {
			e.Detail = service.RedactJSON(b)
		}
	}
	auditSvc.Record(e)
}

var auditEntities = map[string]string{
	"clients": "users", "inbounds": "inbounds", "nodes": "nodes", "hosts": "hosts", "tunnels": "tunnels",
	"setting": "settings", "xray": "xray", "sub-balancers": "xray", "server": "server", "ingress": "settings", "backups": "settings",
}

// auditEntity names what a change was about, and the action, from its route.
func auditEntity(rel string) (string, string) {
	parts := strings.Split(strings.Trim(rel, "/"), "/")
	if len(parts) == 0 {
		return "", ""
	}
	entity := auditEntities[parts[0]]
	if parts[0] == "access" && len(parts) > 1 {
		entity = parts[1]
	}
	if parts[0] == "setting" && len(parts) > 1 && strings.HasPrefix(parts[1], "apiTokens") {
		entity = "tokens"
	}
	action := "update"
	for _, p := range parts[1:] {
		switch {
		case strings.HasPrefix(p, ":"):
		case p == "add" || p == "create" || p == "bulkCreate" || p == "import":
			action = "create"
		case p == "del" || p == "delete" || strings.HasPrefix(p, "bulkDel") || strings.HasPrefix(p, "del"):
			action = "delete"
		default:
			if p != "update" && p != "bulk" {
				action = p
			}
		}
	}
	return entity, action
}

// auditChanges records every change a person makes, while change logging is on; machine
// traffic from nodes and tunnels is left out, and reads never are written.
func auditChanges(c *gin.Context) {
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
		c.Next()
		return
	}
	rel := relAPIPath(c.FullPath())
	rule, _ := access.RouteRule(c.Request.Method, rel)
	scope := c.GetString("api_token_scope")
	if strings.HasSuffix(rule.Perm, ":read") || (scope != "" && scope != "admin") || !auditSvc.Settings().Enabled {
		c.Next()
		return
	}
	var raw []byte
	if c.Request.Body != nil {
		raw, _ = io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
		c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	}
	c.Next()
	entity, action := auditEntity(rel)
	if entity == "" {
		return
	}
	target := c.Param("email")
	if target == "" {
		target = c.Param("id")
	}
	detail := map[string]any{"method": c.Request.Method, "path": rel, "status": c.Writer.Status()}
	if redacted := service.RedactJSON(raw); redacted != "" {
		detail["body"] = json.RawMessage(redacted)
	}
	recordAudit(c, entity, action, target, detail)
}
