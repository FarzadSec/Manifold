package service

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/jalali"
)

const (
	usageHourlyKeep = 31 * 24 * time.Hour
	usageDailyKeep  = 400 * 24 * time.Hour
)

type usageDelta struct{ up, down int64 }

var usageZone = struct {
	sync.Mutex
	loc  *time.Location
	read time.Time
}{}

// usageLocation is the panel's time zone, read through db at most once a minute: traffic
// is recorded every few seconds, inside a transaction, and the setting rarely changes.
func usageLocation(db *gorm.DB, now time.Time) *time.Location {
	usageZone.Lock()
	defer usageZone.Unlock()
	if usageZone.loc == nil || now.Sub(usageZone.read) > time.Minute {
		loc, err := timeLocationIn(db)
		if err != nil || loc == nil {
			loc = time.UTC
		}
		usageZone.loc, usageZone.read = loc, now
	}
	return usageZone.loc
}

func dayStart(t time.Time, loc *time.Location) time.Time {
	l := t.In(loc)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc)
}

// recordClientUsage adds traffic to its user's hour and day on that server, in the
// transaction that adds it to the user, so the history never disagrees with the totals.
func recordClientUsage(tx *gorm.DB, nodeID int, deltas map[string]usageDelta, now time.Time) error {
	if len(deltas) == 0 {
		return nil
	}
	hour := now.UTC().Truncate(time.Hour).Unix()
	day := dayStart(now, usageLocation(tx, now)).Unix()
	rows := make([]model.ClientUsage, 0, 2*len(deltas))
	emails := make([]string, 0, len(deltas))
	for email, d := range deltas {
		if d.up > 0 || d.down > 0 {
			emails = append(emails, email)
		}
	}
	// One order for every writer keeps concurrent upserts lock-compatible on PostgreSQL.
	sort.Strings(emails)
	for _, email := range emails {
		d := deltas[email]
		up, down := max(d.up, 0), max(d.down, 0)
		rows = append(rows,
			model.ClientUsage{Email: email, NodeId: nodeID, Start: hour, Up: up, Down: down},
			model.ClientUsage{Email: email, NodeId: nodeID, Daily: true, Start: day, Up: up, Down: down})
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "email"}, {Name: "node_id"}, {Name: "daily"}, {Name: "start"}},
		DoUpdates: clause.Assignments(map[string]any{
			"up":   gorm.Expr("client_usages.up + excluded.up"),
			"down": gorm.Expr("client_usages.down + excluded.down"),
		}),
	}).Create(&rows).Error
}

// renameClientUsage keeps a user's history when its name changes.
func renameClientUsage(tx *gorm.DB, from, to string) error {
	if from == to {
		return nil
	}
	return tx.Model(&model.ClientUsage{}).Where("email = ?", from).Update("email", to).Error
}

// UsageWindow is the stretch of time a usage view covers.
type UsageWindow struct {
	From, To time.Time
	// Daily windows reach past the hours kept and count whole days.
	Daily bool
}

var errUnknownWindow = errors.New("window must be 1h, 24h, 7d, 30d or a month like 2026-11 or 1405-08")

// ParseUsageWindow reads a preset ("1h", "24h", "7d", "30d") or a whole month,
// "YYYY-MM" in the Gregorian or ("persian") the Persian calendar of the panel's time zone.
func ParseUsageWindow(window, month, calendar string, now time.Time) (UsageWindow, error) {
	switch window {
	case "1h":
		return UsageWindow{From: now.Add(-time.Hour), To: now}, nil
	case "24h", "":
		return UsageWindow{From: now.Add(-24 * time.Hour), To: now}, nil
	case "7d":
		return UsageWindow{From: now.Add(-7 * 24 * time.Hour), To: now}, nil
	case "30d":
		return UsageWindow{From: now.Add(-30 * 24 * time.Hour), To: now}, nil
	case "month":
	default:
		return UsageWindow{}, errUnknownWindow
	}
	y, m, ok := parseYearMonth(month)
	if !ok {
		return UsageWindow{}, errUnknownWindow
	}
	loc := usageLocation(database.GetDB(), now)
	var from, to time.Time
	if calendar == "persian" {
		from = jalali.Date(y, m, 1, loc)
		to = from.AddDate(0, 0, jalali.MonthLength(y, m))
	} else {
		from = time.Date(y, time.Month(m), 1, 0, 0, 0, 0, loc)
		to = from.AddDate(0, 1, 0)
	}
	return UsageWindow{From: from, To: to, Daily: now.Sub(from) > usageHourlyKeep}, nil
}

func parseYearMonth(s string) (int, int, bool) {
	ys, ms, ok := strings.Cut(s, "-")
	y, errY := strconv.Atoi(ys)
	m, errM := strconv.Atoi(ms)
	if !ok || errY != nil || errM != nil || m < 1 || m > 12 || y < 1 || y > 3000 {
		return 0, 0, false
	}
	return y, m, true
}

// UsagePoint is the traffic of one slot.
type UsagePoint struct {
	Start int64 `json:"start" example:"1763337600"`
	Up    int64 `json:"up" example:"1048576"`
	Down  int64 `json:"down" example:"4194304"`
}

// NodeUsage is what a user moved through one server in a window; node 0 is this panel.
type NodeUsage struct {
	NodeId int    `json:"nodeId" example:"0"`
	Name   string `json:"name" example:"Frankfurt"`
	Up     int64  `json:"up" example:"1048576"`
	Down   int64  `json:"down" example:"4194304"`
}

// ClientUsageView is a user's traffic over a window: slot by slot and per server.
type ClientUsageView struct {
	From   int64        `json:"from" example:"1763251200"`
	To     int64        `json:"to" example:"1763337600"`
	Daily  bool         `json:"daily" example:"false"`
	Points []UsagePoint `json:"points"`
	Nodes  []NodeUsage  `json:"nodes"`
}

// UsageService reads the recorded traffic history.
type UsageService struct{}

func (s *UsageService) slots(w UsageWindow) *gorm.DB {
	var from int64
	if w.Daily {
		// A day that started before the window still holds traffic inside it.
		from = dayStart(w.From, w.From.Location()).Unix()
	} else {
		from = w.From.UTC().Truncate(time.Hour).Unix()
	}
	return database.GetDB().Model(&model.ClientUsage{}).
		Where("daily = ? AND start >= ? AND start < ?", w.Daily, from, w.To.Unix())
}

// ForClient returns one user's traffic over the window.
func (s *UsageService) ForClient(email string, w UsageWindow) (ClientUsageView, error) {
	view := ClientUsageView{From: w.From.Unix(), To: w.To.Unix(), Daily: w.Daily, Points: []UsagePoint{}, Nodes: []NodeUsage{}}
	if err := s.slots(w).Where("email = ?", email).
		Select("start, SUM(up) AS up, SUM(down) AS down").
		Group("start").Order("start").Scan(&view.Points).Error; err != nil {
		return view, err
	}
	var perNode []NodeUsage
	if err := s.slots(w).Where("email = ?", email).
		Select("node_id, SUM(up) AS up, SUM(down) AS down").
		Group("node_id").Order("node_id").Scan(&perNode).Error; err != nil {
		return view, err
	}
	names, err := nodeNames()
	if err != nil {
		return view, err
	}
	for _, n := range perNode {
		n.Name = names[n.NodeId]
		view.Nodes = append(view.Nodes, n)
	}
	return view, nil
}

// UserUsage is what one user moved through a server in a window.
type UserUsage struct {
	Email string `json:"email" example:"alice"`
	Up    int64  `json:"up" example:"1048576"`
	Down  int64  `json:"down" example:"4194304"`
}

// ForNode returns every user's traffic through one server over the window, most first.
func (s *UsageService) ForNode(nodeID int, w UsageWindow, emails []string) ([]UserUsage, error) {
	out := []UserUsage{}
	q := s.slots(w).Where("node_id = ?", nodeID)
	if emails != nil {
		q = q.Where("email IN ?", emails)
	}
	err := q.Select("email, SUM(up) AS up, SUM(down) AS down").
		Group("email").Order("SUM(up) + SUM(down) DESC").Scan(&out).Error
	return out, err
}

func nodeNames() (map[int]string, error) {
	var nodes []model.Node
	if err := database.GetDB().Select("id", "name").Find(&nodes).Error; err != nil {
		return nil, err
	}
	names := map[int]string{0: ""}
	for _, n := range nodes {
		names[n.Id] = n.Name
	}
	return names, nil
}

// Prune drops hours past a month, days past 400 days, and the history of users that are gone.
func (s *UsageService) Prune(now time.Time) error {
	db := database.GetDB()
	if err := db.Where("daily = ? AND start < ?", false, now.Add(-usageHourlyKeep).Unix()).Delete(&model.ClientUsage{}).Error; err != nil {
		return fmt.Errorf("pruning hourly usage: %w", err)
	}
	if err := db.Where("daily = ? AND start < ?", true, now.Add(-usageDailyKeep).Unix()).Delete(&model.ClientUsage{}).Error; err != nil {
		return fmt.Errorf("pruning daily usage: %w", err)
	}
	return db.Where("email NOT IN (?)", db.Model(&model.ClientRecord{}).Select("email")).Delete(&model.ClientUsage{}).Error
}
