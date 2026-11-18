package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/web/access"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// What a reseller route acts on, so the guard knows which users to check before the handler runs.
type resellerTarget int

const (
	targetNone resellerTarget = iota
	targetEmailRead
	targetEmailWrite
	targetBodyWrite
	// targetBodyRead checks the users a body names, or none when it asks for all of its own.
	targetBodyRead
	targetSubID
	targetRecordID
)

var resellerClientRoutes = map[string]resellerTarget{
	"GET /clients/list":                  targetNone,
	"GET /clients/list/paged":            targetNone,
	"POST /clients/onlines":              targetNone,
	"POST /clients/lastOnline":           targetNone,
	"POST /clients/renewalPreview":       targetNone,
	"POST /clients/add":                  targetNone,
	"POST /clients/bulkCreate":           targetNone,
	"GET /clients/get/tgId/:tgId":        targetNone,
	"GET /clients/get":                   targetEmailRead,
	"GET /clients/get/:email":            targetEmailRead,
	"GET /clients/traffic/:email":        targetEmailRead,
	"GET /clients/usage/:email":          targetEmailRead,
	"GET /clients/links/:email":          targetEmailRead,
	"POST /clients/ips/:email":           targetEmailRead,
	"POST /clients/hwids/:email":         targetEmailRead,
	"GET /clients/get/subId/:subId":      targetSubID,
	"GET /clients/subLinks/:subId":       targetSubID,
	"GET /clients/subEntries/:subId":     targetSubID,
	"POST /clients/happLink/:id":         targetRecordID,
	"POST /clients/update":               targetEmailWrite,
	"POST /clients/update/:email":        targetEmailWrite,
	"POST /clients/del":                  targetEmailWrite,
	"POST /clients/del/:email":           targetEmailWrite,
	"POST /clients/:email/attach":        targetEmailWrite,
	"POST /clients/:email/detach":        targetEmailWrite,
	"POST /clients/:email/externalLinks": targetEmailWrite,
	"POST /clients/resetTraffic/:email":  targetEmailWrite,
	"POST /clients/clearIps/:email":      targetEmailWrite,
	"DELETE /clients/hwids/:email":       targetEmailWrite,
	"DELETE /clients/hwids/:email/:id":   targetEmailWrite,
	"POST /clients/bulkAdjust":           targetBodyWrite,
	"POST /clients/bulkAttach":           targetBodyWrite,
	"POST /clients/bulkDetach":           targetBodyWrite,
	"POST /clients/bulkDel":              targetBodyWrite,
	"POST /clients/bulkEnable":           targetBodyWrite,
	"POST /clients/bulkDisable":          targetBodyWrite,
	"POST /clients/bulkResetTraffic":     targetBodyWrite,
	"POST /clients/exportCsv":            targetBodyRead,
	"POST /clients/configPack":           targetBodyRead,
}

var errUserNotFound = errors.New("user not found")

func refuse(c *gin.Context, err error) {
	c.AbortWithStatusJSON(http.StatusOK, gin.H{"success": false, "msg": err.Error()})
}

// guardClientAccess keeps an actor scoped to its own users to those users, and holds every
// actor's changes to what its account and role allow.
func guardClientAccess(c *gin.Context) {
	actor := access.ActorFrom(c)
	route := c.Request.Method + " " + relAPIPath(c.FullPath())
	if !actor.ReadsOwnOnly() && !actor.WritesOwnOnly() {
		if err := checkUserPolicy(c, service.ClientAccess{Actor: actor}, route); err != nil {
			refuse(c, err)
			return
		}
		c.Next()
		return
	}
	kind, ok := resellerClientRoutes[route]
	if !ok {
		if !actor.IsReseller() {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "msg": "your account is not permitted to do this"})
		return
	}
	ca := service.ClientAccess{Actor: actor}
	var emails []string
	switch kind {
	case targetEmailRead, targetEmailWrite:
		email := c.Param("email")
		if email == "" {
			email = c.Query("email")
		}
		emails = []string{email}
	case targetBodyWrite, targetBodyRead:
		raw, err := io.ReadAll(c.Request.Body)
		if err != nil {
			refuse(c, err)
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(raw))
		var body struct {
			Emails []string `json:"emails"`
			All    bool     `json:"all"`
		}
		_ = json.Unmarshal(raw, &body)
		emails = body.Emails
		if kind == targetBodyRead && body.All && len(emails) == 0 {
			kind = targetNone
		}
	case targetSubID:
		if err := database.GetDB().Model(&model.ClientRecord{}).Where("sub_id = ?", c.Param("subId")).
			Pluck("email", &emails).Error; err != nil {
			refuse(c, err)
			return
		}
	case targetRecordID:
		id, _ := strconv.Atoi(c.Param("id"))
		var rec model.ClientRecord
		if err := database.GetDB().Select("email").First(&rec, id).Error; err == nil {
			emails = []string{rec.Email}
		}
	}
	if kind != targetNone && len(emails) == 0 {
		refuse(c, errUserNotFound)
		return
	}
	write := kind == targetEmailWrite || kind == targetBodyWrite
	if kind != targetNone && (write && actor.WritesOwnOnly() || !write && actor.ReadsOwnOnly()) {
		if err := ca.CheckOwned(emails); err != nil {
			refuse(c, err)
			return
		}
	}
	if kind == targetEmailWrite || kind == targetBodyWrite {
		if err := ca.CheckMayEdit(); err != nil {
			refuse(c, err)
			return
		}
	}
	if err := checkUserPolicy(c, ca, route); err != nil {
		refuse(c, err)
		return
	}
	c.Next()
}

// checkUserPolicy holds a change inside what the actor's account and role let it set.
func checkUserPolicy(c *gin.Context, ca service.ClientAccess, route string) error {
	if ca.Actor.Bounds() == nil && !ca.Actor.IsReseller() {
		return nil
	}
	switch route {
	case "POST /clients/bulkEnable":
		return ca.CheckMayEnable()
	case "POST /clients/bulkAdjust":
		if err := ca.CheckMayChangeLimits(); err != nil {
			return err
		}
		return checkBulkBounds(c, ca)
	case "POST /clients/update", "POST /clients/update/:email":
		email := c.Param("email")
		if email == "" {
			email = c.Query("email")
		}
		return checkEdit(c, ca, email)
	}
	return nil
}

// limitKeys are the fields a plans-only account may not change on its users.
var limitKeys = []string{"totalGB", "expiryTime", "limitIp", "userLimit", "speedLimit", "limitHwid", "reset", "resetDay", "resetWeekday", "resetMax", "trafficReset", "trafficResetDay"}

// readBody returns a JSON body and puts it back for the handler.
func readBody(c *gin.Context) (map[string]json.RawMessage, error) {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	return body, nil
}

func recordByEmail(email string) (model.ClientRecord, error) {
	var rec model.ClientRecord
	err := database.GetDB().Where("LOWER(email) = ?", strings.ToLower(email)).First(&rec).Error
	return rec, err
}

// checkEdit applies the actor's allowance, plans-only rule and role bounds to an edit before
// the body reaches the handler; an unknown user is left to the handler to report.
func checkEdit(c *gin.Context, ca service.ClientAccess, email string) error {
	body, err := readBody(c)
	if err != nil {
		return err
	}
	rec, err := recordByEmail(email)
	if err != nil {
		return nil
	}
	stored := map[string]any{
		"totalGB": rec.TotalGB, "expiryTime": rec.ExpiryTime, "limitIp": rec.LimitIP, "userLimit": rec.UserLimit, "speedLimit": rec.SpeedLimit, "limitHwid": rec.LimitHwid,
		"reset": rec.Reset, "resetDay": rec.ResetDay, "resetWeekday": rec.ResetWeekday, "resetMax": rec.ResetMax,
		"trafficReset": rec.TrafficReset, "trafficResetDay": rec.TrafficResetDay,
	}
	for _, k := range limitKeys {
		v, ok := body[k]
		if !ok {
			continue
		}
		want, _ := json.Marshal(stored[k])
		if !bytes.Equal(bytes.TrimSpace(v), want) {
			if err := ca.CheckMayChangeLimits(); err != nil {
				return err
			}
			break
		}
	}
	if v, ok := body["enable"]; ok && string(bytes.TrimSpace(v)) == "true" && !rec.Enable {
		if err := ca.CheckMayEnable(); err != nil {
			return err
		}
	}
	return ca.CheckChange(editChange(body, rec), time.Now())
}

// editChange is the bounded limits an edit body changes; a value it repeats is not checked.
func editChange(body map[string]json.RawMessage, rec model.ClientRecord) service.UserChange {
	var ch service.UserChange
	if v, ok := body["totalGB"]; ok {
		var total int64
		if json.Unmarshal(v, &total) == nil && total != rec.TotalGB {
			ch.Total = &total
		}
	}
	if v, ok := body["expiryTime"]; ok {
		var expiry int64
		if json.Unmarshal(v, &expiry) == nil && expiry != rec.ExpiryTime {
			ch.Expiry = &expiry
		}
	}
	if v, ok := body["limitHwid"]; ok {
		var devices int
		if json.Unmarshal(v, &devices) == nil && devices != rec.LimitHwid {
			ch.Devices = &devices
		}
	}
	return ch
}

// checkBulkBounds checks each selected user as the bulk change would leave it.
func checkBulkBounds(c *gin.Context, ca service.ClientAccess) error {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	var req struct {
		Emails    []string `json:"emails"`
		AddDays   int      `json:"addDays"`
		AddBytes  int64    `json:"addBytes"`
		LimitHwid *int     `json:"limitHwid"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return err
	}
	now := time.Now()
	for _, email := range req.Emails {
		rec, err := recordByEmail(email)
		if err != nil {
			continue
		}
		var ch service.UserChange
		if req.AddDays != 0 && rec.ExpiryTime > 0 {
			expiry := rec.ExpiryTime + int64(req.AddDays)*24*int64(time.Hour/time.Millisecond)
			ch.Expiry = &expiry
		}
		if req.AddBytes != 0 && rec.TotalGB > 0 {
			total := rec.TotalGB + req.AddBytes
			ch.Total = &total
		}
		ch.Devices = req.LimitHwid
		if err := ca.CheckChange(ch, now); err != nil {
			return common.NewError(email + ": " + err.Error())
		}
	}
	return nil
}

// ownedOnly keeps the items whose email the reseller owns; others pass everything through.
func ownedOnly[T any](c *gin.Context, items []T, email func(T) string) ([]T, error) {
	actor := access.ActorFrom(c)
	if !actor.ReadsOwnOnly() {
		return items, nil
	}
	owned, err := service.OwnedEmailSet(actor.UserId)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(items, func(it T) bool { return !owned[strings.ToLower(email(it))] }), nil
}

// ownedMap keeps the map entries whose email the reseller owns.
func ownedMap[V any](c *gin.Context, m map[string]V) (map[string]V, error) {
	actor := access.ActorFrom(c)
	if !actor.ReadsOwnOnly() {
		return m, nil
	}
	owned, err := service.OwnedEmailSet(actor.UserId)
	if err != nil {
		return nil, err
	}
	out := make(map[string]V, len(owned))
	for k, v := range m {
		if owned[strings.ToLower(k)] {
			out[k] = v
		}
	}
	return out, nil
}
