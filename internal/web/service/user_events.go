package service

import (
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/eventbus"
)

const (
	// quotaWarnAt is the share of a quota at which a user is warned, once per cycle.
	quotaWarnAt = 0.8
	expiringIn  = 24 * time.Hour
)

type userRow struct {
	Email         string
	SubId         string
	OwnerId       int
	Enable        bool
	TrafficEnable *bool
	GroupName     string
	Comment       string
	TgId          int64
	Up            int64
	Down          int64
	Total         int64
	ExpiryTime    int64
	LastOnline    int64
}

func (r userRow) data() eventbus.UserEventData {
	return eventbus.UserEventData{
		Email: r.Email, SubId: r.SubId, OwnerId: r.OwnerId, Enable: r.enabled(), Up: r.Up, Down: r.Down,
		Total: r.Total, ExpiryTime: r.ExpiryTime, Group: r.GroupName, Comment: r.Comment, TgId: r.TgId,
	}
}

func (r userRow) enabled() bool { return r.Enable && (r.TrafficEnable == nil || *r.TrafficEnable) }

// userState is what the watcher last saw of a user, to tell a change from a steady state.
type userState struct {
	enabled, expired, expiring, limited, warned bool
	used, lastOnline                            int64
}

func stateOf(r userRow, now time.Time) userState {
	used := r.Up + r.Down
	ms := now.UnixMilli()
	return userState{
		enabled:    r.enabled(),
		expired:    r.ExpiryTime > 0 && r.ExpiryTime <= ms,
		expiring:   r.ExpiryTime > ms && r.ExpiryTime <= now.Add(expiringIn).UnixMilli(),
		limited:    r.Total > 0 && used >= r.Total,
		warned:     r.Total > 0 && float64(used) >= quotaWarnAt*float64(r.Total),
		used:       used,
		lastOnline: r.LastOnline,
	}
}

var userWatch = struct {
	sync.Mutex
	seen map[string]userState
}{seen: map[string]userState{}}

// WatchUsers compares every user with the last look and publishes what changed: enabled,
// disabled, expired, expiring, limited, the quota warning, the first connection, a reset.
// A user's first look only records it, so a restart announces nothing.
func WatchUsers(now time.Time) []eventbus.Event {
	var rows []userRow
	if err := userRows().Scan(&rows).Error; err != nil {
		return nil
	}
	userWatch.Lock()
	defer userWatch.Unlock()
	var events []eventbus.Event
	next := make(map[string]userState, len(rows))
	for _, r := range rows {
		cur := stateOf(r, now)
		next[r.Email] = cur
		prev, known := userWatch.seen[r.Email]
		if !known {
			continue
		}
		emit := func(t eventbus.EventType) {
			events = append(events, eventbus.Event{Type: t, Source: r.Email, Data: r.data(), Timestamp: now})
		}
		switch {
		case prev.enabled && !cur.enabled:
			emit(eventbus.EventUserDisabled)
		case !prev.enabled && cur.enabled:
			emit(eventbus.EventUserEnabled)
		}
		if cur.expired && !prev.expired {
			emit(eventbus.EventUserExpired)
		}
		if cur.expiring && !prev.expiring {
			emit(eventbus.EventUserExpiring)
		}
		if cur.limited && !prev.limited {
			emit(eventbus.EventUserLimited)
		} else if cur.warned && !prev.warned {
			emit(eventbus.EventUserQuotaWarning)
		}
		if cur.lastOnline > 0 && prev.lastOnline == 0 {
			emit(eventbus.EventUserFirstConnected)
		}
		if cur.used < prev.used {
			emit(eventbus.EventUserTrafficReset)
		}
	}
	userWatch.seen = next
	for _, ev := range events {
		if eventBus != nil {
			eventBus.Publish(ev)
		}
	}
	return events
}

func userRows() *gorm.DB {
	return database.GetDB().Table("clients c").
		Select("c.email, c.sub_id, c.owner_id, c.enable, ct.enable AS traffic_enable, c.group_name, c.comment, c.tg_id, " +
			"COALESCE(ct.up, 0) AS up, COALESCE(ct.down, 0) AS down, COALESCE(ct.total, 0) AS total, " +
			"COALESCE(ct.expiry_time, 0) AS expiry_time, COALESCE(ct.last_online, 0) AS last_online").
		Joins("LEFT JOIN client_traffics ct ON ct.email = c.email")
}

// UserEventData is a user's event payload as the panel holds them now; ok is false once gone.
func UserEventData(email string) (eventbus.UserEventData, bool) {
	var rows []userRow
	userRows().Where("c.email = ?", email).Limit(1).Scan(&rows)
	if len(rows) == 0 {
		return eventbus.UserEventData{Email: email}, false
	}
	return rows[0].data(), true
}
