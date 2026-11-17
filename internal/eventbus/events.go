package eventbus

import "time"

// EventType identifies the kind of event flowing through the bus.
type EventType string

const (
	// Outbound health (observatory-driven)
	EventOutboundDown EventType = "outbound.down"
	EventOutboundUp   EventType = "outbound.up"

	// Xray core (local)
	EventXrayCrash EventType = "xray.crash"

	// Node health (heartbeat-driven)
	EventNodeDown EventType = "node.down"
	EventNodeUp   EventType = "node.up"

	// System health
	EventCPUHigh    EventType = "cpu.high"
	EventMemoryHigh EventType = "memory.high"

	// Security
	EventLoginAttempt EventType = "login.attempt"

	// Users: changes made through the API, and what their traffic and dates do to them.
	EventUserCreated        EventType = "user.created"
	EventUserUpdated        EventType = "user.updated"
	EventUserDeleted        EventType = "user.deleted"
	EventUsersBulkChanged   EventType = "users.bulk_changed"
	EventUserEnabled        EventType = "user.enabled"
	EventUserDisabled       EventType = "user.disabled"
	EventUserExpired        EventType = "user.expired"
	EventUserExpiring       EventType = "user.expiring"
	EventUserLimited        EventType = "user.limited"
	EventUserQuotaWarning   EventType = "user.quota_warning"
	EventUserFirstConnected EventType = "user.first_connected"
	EventUserTrafficReset   EventType = "user.traffic_reset"

	// Nodes and backups
	EventNodeUsageStopped EventType = "node.usage_stopped"
	EventNodeUsageResumed EventType = "node.usage_resumed"
	EventBackupSucceeded  EventType = "backup.succeeded"
	EventBackupFailed     EventType = "backup.failed"

	// Webhooks: a test a person asked for.
	EventWebhookTest EventType = "webhook.test"
)

// AllEventTypes lists every event a webhook may subscribe to.
var AllEventTypes = []EventType{
	EventUserCreated, EventUserUpdated, EventUserDeleted, EventUsersBulkChanged,
	EventUserEnabled, EventUserDisabled, EventUserExpired, EventUserExpiring, EventUserLimited,
	EventUserQuotaWarning, EventUserFirstConnected, EventUserTrafficReset,
	EventNodeUp, EventNodeDown, EventNodeUsageStopped, EventNodeUsageResumed,
	EventOutboundUp, EventOutboundDown, EventXrayCrash, EventCPUHigh, EventMemoryHigh,
	EventLoginAttempt, EventBackupSucceeded, EventBackupFailed,
}

// UserEventData describes the user an event is about, as the panel holds it then.
type UserEventData struct {
	Email      string `json:"email"`
	SubId      string `json:"subId,omitempty"`
	OwnerId    int    `json:"ownerId"`
	Enable     bool   `json:"enable"`
	Up         int64  `json:"up"`
	Down       int64  `json:"down"`
	Total      int64  `json:"total"`
	ExpiryTime int64  `json:"expiryTime"`
	Group      string `json:"group,omitempty"`
	Comment    string `json:"comment,omitempty"`
	TgId       int64  `json:"tgId,omitempty"`
}

// ChangeEventData is a change made through the API: who made it and on which route.
type ChangeEventData struct {
	Actor  string   `json:"actor"`
	Route  string   `json:"route"`
	Emails []string `json:"emails,omitempty"`
}

// Event is the unit of information flowing through the bus.
type Event struct {
	Type      EventType
	Source    string    // outbound tag, node name, client email, IP, etc.
	Data      any       // event-specific payload, may be nil
	Timestamp time.Time // when the event was detected
}

// OutboundHealthData carries observatory details for outbound events.
type OutboundHealthData struct {
	Delay int64  `json:"delay"` // last measured delay in ms, 0 if unknown
	Error string `json:"error"` // last error if probe failed, empty if up
}

// NodeHealthData carries heartbeat details for node events.
type NodeHealthData struct {
	NodeId    int     `json:"nodeId"`
	LatencyMs int     `json:"latencyMs"`
	CpuPct    float64 `json:"cpuPct"`
	MemPct    float64 `json:"memPct"`
	XrayState string  `json:"xrayState"` // "running", "stopped", etc.
	XrayError string  `json:"xrayError"`
}

// LoginEventData carries login attempt details.
type LoginEventData struct {
	Username string `json:"username"`
	IP       string `json:"ip"`
	Time     string `json:"time"`
	Status   string `json:"status"` // "success" or "fail"
	Reason   string `json:"reason"`
}

// SystemMetricData carries raw system metric values for threshold-based events.
type SystemMetricData struct {
	Percent   float64 `json:"percent"`   // current usage percentage
	Threshold int     `json:"threshold"` // configured threshold
}
