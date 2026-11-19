package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"

	"gorm.io/gorm"
)

// AppService signs devices in to the app API. A subscription link and the
// username of one of its accounts open a session bound to one device.
type AppService struct {
	settingService SettingService
	clientService  ClientService
}

var (
	ErrAppDisabled        = errors.New("app access is off")
	ErrAppBadLogin        = errors.New("the username does not belong to this subscription link")
	ErrAppBadDevice       = errors.New("the device id is missing or malformed")
	ErrAppDeviceLimit     = errors.New("the subscription's device limit is reached")
	ErrAppSession         = errors.New("the session is not valid")
	ErrAppLocationName    = errors.New("a location needs a name")
	ErrAppUnknownLocation = errors.New("no such location")
	ErrAppUnknownInbound  = errors.New("no such inbound")
	ErrAppInboundTaken    = errors.New("an inbound serves one location only, and this one is in another")
	ErrAppBadCountry      = errors.New("the country must be a two-letter ISO 3166 code")
	ErrAppBadTimezone     = errors.New("the timezone must be an IANA name such as Europe/Berlin")
)

const maxAppDeviceIDLength = 128

// AppDevice is what an app says about the device it runs on. ID must stay the
// same across logins: it is the device's slot in the subscription's HWID limit.
type AppDevice struct {
	ID         string
	Name       string
	OS         string
	OSVersion  string
	AppVersion string
	UserAgent  string
}

func (d AppDevice) hwidRequest() HwidRequest {
	return HwidRequest{Hwid: d.ID, UserAgent: d.UserAgent, DeviceOS: d.OS, OsVersion: d.OSVersion, DeviceModel: d.Name}
}

func hashAppToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Login checks the username against the accounts of subID, takes the device's
// HWID slot and returns the session token, which is stored only as its hash.
func (s *AppService) Login(subID, username string, device AppDevice) (string, error) {
	enabled, err := s.settingService.GetAppEnable()
	if err != nil {
		return "", err
	}
	if !enabled {
		return "", ErrAppDisabled
	}
	device.ID = strings.TrimSpace(device.ID)
	if len(device.ID) < minHwidLength || len(device.ID) > maxAppDeviceIDLength {
		return "", ErrAppBadDevice
	}
	subID = strings.TrimSpace(subID)
	username = strings.TrimSpace(username)
	if subID == "" || username == "" {
		return "", ErrAppBadLogin
	}
	var rec model.ClientRecord
	err = database.GetDB().Where("sub_id = ? AND LOWER(email) = LOWER(?)", subID, username).First(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrAppBadLogin
	}
	if err != nil {
		return "", err
	}

	gate, err := s.clientService.EnforceHwidForSubID(subID, device.hwidRequest())
	if err != nil {
		return "", err
	}
	if !gate.Allowed {
		return "", ErrAppDeviceLimit
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	now := time.Now().UnixMilli()
	n := normalizeHwidRequest(device.hwidRequest())
	session := model.AppSession{
		SubID:      subID,
		Email:      rec.Email,
		HwidHash:   hashHwid(device.ID),
		TokenHash:  hashAppToken(token),
		DeviceName: n.DeviceModel,
		DeviceOS:   n.DeviceOS,
		OsVersion:  n.OsVersion,
		AppVersion: trimHwidMeta(device.AppVersion),
		UserAgent:  n.UserAgent,
		CreatedAt:  now,
		LastSeen:   now,
	}
	err = database.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("sub_id = ? AND hwid_hash = ?", subID, session.HwidHash).Delete(&model.AppSession{}).Error; err != nil {
			return err
		}
		return tx.Create(&session).Error
	})
	if err != nil {
		return "", err
	}
	return token, nil
}

// Session returns the session of token, checking that its link still names its
// account and that its device still holds, or can take back, its HWID slot.
func (s *AppService) Session(subID, token, deviceID string) (model.AppSession, error) {
	var session model.AppSession
	enabled, err := s.settingService.GetAppEnable()
	if err != nil {
		return session, err
	}
	if !enabled {
		return session, ErrAppDisabled
	}
	subID = strings.TrimSpace(subID)
	token = strings.TrimSpace(token)
	deviceID = strings.TrimSpace(deviceID)
	if subID == "" || token == "" || deviceID == "" {
		return session, ErrAppSession
	}
	db := database.GetDB()
	err = db.Where("token_hash = ?", hashAppToken(token)).First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return session, ErrAppSession
	}
	if err != nil {
		return session, err
	}
	if session.SubID != subID || session.HwidHash != hashHwid(deviceID) {
		return session, ErrAppSession
	}
	var owners int64
	if err := db.Model(&model.ClientRecord{}).Where("sub_id = ? AND email = ?", subID, session.Email).Count(&owners).Error; err != nil {
		return session, err
	}
	if owners == 0 {
		return session, ErrAppSession
	}

	gate, err := s.clientService.EnforceHwidForSubID(subID, HwidRequest{
		Hwid: deviceID, UserAgent: session.UserAgent, DeviceOS: session.DeviceOS, OsVersion: session.OsVersion, DeviceModel: session.DeviceName,
	})
	if err != nil {
		return session, err
	}
	if !gate.Allowed {
		return session, ErrAppDeviceLimit
	}
	session.LastSeen = time.Now().UnixMilli()
	if err := db.Model(&model.AppSession{}).Where("id = ?", session.Id).UpdateColumn("last_seen", session.LastSeen).Error; err != nil {
		return session, err
	}
	return session, nil
}

// Logout ends the session of token.
func (s *AppService) Logout(subID, token string) error {
	return database.GetDB().
		Where("sub_id = ? AND token_hash = ?", strings.TrimSpace(subID), hashAppToken(strings.TrimSpace(token))).
		Delete(&model.AppSession{}).Error
}

// endAppSessionsTx signs out the devices of subIDs, or with hwidHash only that one.
func endAppSessionsTx(tx *gorm.DB, hwidHash string, subIDs ...string) error {
	query := tx.Where("sub_id IN ?", subIDs)
	if hwidHash != "" {
		query = query.Where("hwid_hash = ?", hwidHash)
	}
	return query.Delete(&model.AppSession{}).Error
}

// Locations lists the places the app offers, each with the inbounds that serve it.
func (s *AppService) Locations() ([]model.AppLocation, error) {
	db := database.GetDB()
	var locations []model.AppLocation
	if err := db.Order("sort_order ASC, id ASC").Find(&locations).Error; err != nil {
		return nil, err
	}
	var links []model.AppLocationInbound
	if err := db.Order("inbound_id ASC").Find(&links).Error; err != nil {
		return nil, err
	}
	byLocation := make(map[int][]int, len(locations))
	for _, l := range links {
		byLocation[l.LocationID] = append(byLocation[l.LocationID], l.InboundID)
	}
	for i := range locations {
		locations[i].InboundIds = byLocation[locations[i].Id]
		if locations[i].InboundIds == nil {
			locations[i].InboundIds = []int{}
		}
	}
	return locations, nil
}

// LocationOfInbounds maps each inbound that serves a location to that location's id.
func (s *AppService) LocationOfInbounds() (map[int]int, error) {
	var links []model.AppLocationInbound
	if err := database.GetDB().Find(&links).Error; err != nil {
		return nil, err
	}
	out := make(map[int]int, len(links))
	for _, l := range links {
		out[l.InboundID] = l.LocationID
	}
	return out, nil
}

// ServersOfLocation names the servers holding a location's inbounds of protocol: "" for this
// panel, "node:N" for a node. An unknown location names this panel and every enabled node.
func (s *AppService) ServersOfLocation(locationID int, protocol model.Protocol) ([]string, error) {
	db := database.GetDB()
	var inbounds []model.Inbound
	if locationID > 0 {
		err := db.Model(&model.Inbound{}).
			Joins("JOIN location_inbounds ON location_inbounds.inbound_id = inbounds.id").
			Where("location_inbounds.location_id = ? AND inbounds.protocol = ?", locationID, protocol).
			Find(&inbounds).Error
		if err != nil {
			return nil, err
		}
	}
	servers := map[string]bool{}
	for _, ib := range inbounds {
		if ib.NodeID == nil {
			servers[""] = true
		} else {
			servers["node:"+strconv.Itoa(*ib.NodeID)] = true
		}
	}
	if len(servers) == 0 {
		servers[""] = true
		var nodes []model.Node
		if err := db.Where("enable = ?", true).Find(&nodes).Error; err != nil {
			return nil, err
		}
		for _, n := range nodes {
			servers["node:"+strconv.Itoa(n.Id)] = true
		}
	}
	out := make([]string, 0, len(servers))
	for server := range servers {
		out = append(out, server)
	}
	slices.Sort(out)
	return out, nil
}

// SaveLocation creates a location (Id 0) or replaces one, inbounds included.
func (s *AppService) SaveLocation(loc model.AppLocation) (model.AppLocation, error) {
	loc.Name = strings.TrimSpace(loc.Name)
	loc.City = strings.TrimSpace(loc.City)
	loc.CountryCode = strings.ToUpper(strings.TrimSpace(loc.CountryCode))
	loc.Timezone = strings.TrimSpace(loc.Timezone)
	if loc.Name == "" {
		return loc, ErrAppLocationName
	}
	if loc.CountryCode != "" && !isCountryCode(loc.CountryCode) {
		return loc, ErrAppBadCountry
	}
	if loc.Timezone != "" {
		if _, err := time.LoadLocation(loc.Timezone); err != nil {
			return loc, ErrAppBadTimezone
		}
	}
	ids := slices.Compact(slices.Sorted(slices.Values(loc.InboundIds)))
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		if loc.Id != 0 {
			var count int64
			if err := tx.Model(&model.AppLocation{}).Where("id = ?", loc.Id).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				return ErrAppUnknownLocation
			}
		}
		if len(ids) > 0 {
			var found int64
			if err := tx.Model(&model.Inbound{}).Where("id IN ?", ids).Count(&found).Error; err != nil {
				return err
			}
			if int(found) != len(ids) {
				return ErrAppUnknownInbound
			}
			var taken int64
			if err := tx.Model(&model.AppLocationInbound{}).Where("inbound_id IN ? AND location_id <> ?", ids, loc.Id).
				Count(&taken).Error; err != nil {
				return err
			}
			if taken > 0 {
				return ErrAppInboundTaken
			}
		}
		if err := tx.Save(&loc).Error; err != nil {
			return err
		}
		if err := tx.Where("location_id = ?", loc.Id).Delete(&model.AppLocationInbound{}).Error; err != nil {
			return err
		}
		for _, id := range ids {
			if err := tx.Create(&model.AppLocationInbound{InboundID: id, LocationID: loc.Id}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if ids == nil {
		ids = []int{}
	}
	loc.InboundIds = ids
	return loc, err
}

// DeleteLocation removes a location; its inbounds stay, serving no location.
func (s *AppService) DeleteLocation(id int) error {
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("location_id = ?", id).Delete(&model.AppLocationInbound{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.AppLocation{}, id).Error
	})
}

func isCountryCode(code string) bool {
	if len(code) != 2 {
		return false
	}
	for _, r := range code {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}
