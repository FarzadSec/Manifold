package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AppService signs devices in to the app API. A subscription link and the
// username of one of its accounts open a session bound to one device.
type AppService struct {
	settingService SettingService
	clientService  ClientService
}

var (
	ErrAppDisabled    = errors.New("app access is off")
	ErrAppBadLogin    = errors.New("the username does not belong to this subscription link")
	ErrAppBadDevice   = errors.New("the device id is missing or malformed")
	ErrAppDeviceLimit = errors.New("the subscription's device limit is reached")
	ErrAppSession     = errors.New("the session is not valid")
	ErrAppUnknownNode = errors.New("no such node")
	ErrAppBadCountry  = errors.New("the country must be a two-letter ISO 3166 code")
	ErrAppBadTimezone = errors.New("the timezone must be an IANA name such as Europe/Berlin")
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

// Locations returns how the app names each server of nodeIDs, or of this panel
// and every node when none is given. An unnamed node goes by its node name.
func (s *AppService) Locations(nodeIDs ...int) ([]model.AppLocation, error) {
	db := database.GetDB()
	if len(nodeIDs) == 0 {
		nodeIDs = []int{0}
		var ids []int
		if err := db.Model(&model.Node{}).Order("id ASC").Pluck("id", &ids).Error; err != nil {
			return nil, err
		}
		nodeIDs = append(nodeIDs, ids...)
	}
	var saved []model.AppLocation
	if err := db.Where("node_id IN ?", nodeIDs).Find(&saved).Error; err != nil {
		return nil, err
	}
	var nodes []model.Node
	if err := db.Select("id", "name").Where("id IN ?", nodeIDs).Find(&nodes).Error; err != nil {
		return nil, err
	}
	byID := make(map[int]model.AppLocation, len(saved))
	for _, loc := range saved {
		byID[loc.NodeID] = loc
	}
	nodeNames := make(map[int]string, len(nodes))
	for _, n := range nodes {
		nodeNames[n.Id] = n.Name
	}
	out := make([]model.AppLocation, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		loc, ok := byID[id]
		if !ok {
			loc = model.AppLocation{NodeID: id}
		}
		if loc.Name == "" {
			loc.Name = nodeNames[id]
		}
		out = append(out, loc)
	}
	return out, nil
}

// SaveLocation stores how the app names this panel (node 0) or a node.
func (s *AppService) SaveLocation(loc model.AppLocation) error {
	loc.Name = strings.TrimSpace(loc.Name)
	loc.City = strings.TrimSpace(loc.City)
	loc.CountryCode = strings.ToUpper(strings.TrimSpace(loc.CountryCode))
	loc.Timezone = strings.TrimSpace(loc.Timezone)
	if loc.CountryCode != "" && !isCountryCode(loc.CountryCode) {
		return ErrAppBadCountry
	}
	if loc.Timezone != "" {
		if _, err := time.LoadLocation(loc.Timezone); err != nil {
			return ErrAppBadTimezone
		}
	}
	db := database.GetDB()
	if loc.NodeID != 0 {
		var count int64
		if err := db.Model(&model.Node{}).Where("id = ?", loc.NodeID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return ErrAppUnknownNode
		}
	}
	// Save would insert node 0 again: GORM reads a zero primary key as a new row.
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "node_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "country_code", "city", "timezone"}),
	}).Create(&loc).Error
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
