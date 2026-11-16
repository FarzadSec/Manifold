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
}

var errUserNotFound = errors.New("user not found")

func refuse(c *gin.Context, err error) {
	c.AbortWithStatusJSON(http.StatusOK, gin.H{"success": false, "msg": err.Error()})
}

// guardClientAccess lets a reseller reach only its own users, and only in the ways its
// account allows; every other actor passes untouched.
func guardClientAccess(c *gin.Context) {
	actor := access.ActorFrom(c)
	if !actor.OwnsOnly() {
		c.Next()
		return
	}
	route := c.Request.Method + " " + relAPIPath(c.FullPath())
	kind, ok := resellerClientRoutes[route]
	if !ok {
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
	case targetBodyWrite:
		raw, err := io.ReadAll(c.Request.Body)
		if err != nil {
			refuse(c, err)
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(raw))
		var body struct {
			Emails []string `json:"emails"`
		}
		_ = json.Unmarshal(raw, &body)
		emails = body.Emails
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
	if kind != targetNone {
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
	switch route {
	case "POST /clients/bulkEnable":
		if err := ca.CheckMayEnable(); err != nil {
			refuse(c, err)
			return
		}
	case "POST /clients/bulkAdjust":
		if err := ca.CheckMayChangeLimits(); err != nil {
			refuse(c, err)
			return
		}
	case "POST /clients/update", "POST /clients/update/:email":
		if err := checkResellerEdit(c, ca, emails[0]); err != nil {
			refuse(c, err)
			return
		}
	}
	c.Next()
}

// limitKeys are the fields a plans-only reseller may not change on its users.
var limitKeys = []string{"totalGB", "expiryTime", "limitIp", "speedLimit", "limitHwid", "reset", "resetDay", "resetWeekday", "resetMax", "trafficReset", "trafficResetDay"}

// checkResellerEdit applies a reseller's allowance to an edit before the body reaches the handler.
func checkResellerEdit(c *gin.Context, ca service.ClientAccess, email string) error {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return err
	}
	var rec model.ClientRecord
	if err := database.GetDB().Where("LOWER(email) = ?", strings.ToLower(email)).First(&rec).Error; err != nil {
		return err
	}
	stored := map[string]any{
		"totalGB": rec.TotalGB, "expiryTime": rec.ExpiryTime, "limitIp": rec.LimitIP, "speedLimit": rec.SpeedLimit, "limitHwid": rec.LimitHwid,
		"reset": rec.Reset, "resetDay": rec.ResetDay, "resetWeekday": rec.ResetWeekday, "resetMax": rec.ResetMax,
		"trafficReset": rec.TrafficReset, "trafficResetDay": rec.TrafficResetDay,
	}
	changed := false
	for _, k := range limitKeys {
		v, ok := body[k]
		if !ok {
			continue
		}
		want, _ := json.Marshal(stored[k])
		if !bytes.Equal(bytes.TrimSpace(v), want) {
			changed = true
		}
	}
	if changed {
		if err := ca.CheckMayChangeLimits(); err != nil {
			return err
		}
	}
	if v, ok := body["enable"]; ok && string(bytes.TrimSpace(v)) == "true" && !rec.Enable {
		if err := ca.CheckMayEnable(); err != nil {
			return err
		}
	}
	expiry, hwid := rec.ExpiryTime, rec.LimitHwid
	if v, ok := body["expiryTime"]; ok {
		_ = json.Unmarshal(v, &expiry)
	}
	if v, ok := body["limitHwid"]; ok {
		_ = json.Unmarshal(v, &hwid)
	}
	if changed {
		return ca.CheckUpdate(expiry, hwid, time.Now())
	}
	return nil
}

// ownedOnly keeps the items whose email the reseller owns; others pass everything through.
func ownedOnly[T any](c *gin.Context, items []T, email func(T) string) ([]T, error) {
	actor := access.ActorFrom(c)
	if !actor.OwnsOnly() {
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
	if !actor.OwnsOnly() {
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
