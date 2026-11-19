package service

import (
	"errors"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"

	"gorm.io/gorm"
)

// The app cuts its log to 2 MB and its message to 4000 characters; these leave it room.
const (
	maxAppReportMessage     = 4000
	maxAppReportLog         = 8 << 20
	maxAppReportDeviceField = 128
	appReportsPerDeviceHour = 5
	keptAppReports          = 500
)

var (
	ErrAppReportTooLarge = errors.New("the report's log is larger than the panel takes")
	ErrAppTooManyReports = errors.New("this device sent too many reports; it can send again within the hour")
	ErrAppUnknownReport  = errors.New("no such report")
)

// AppReportInput is what an app sends with a report; the session says whose it is.
type AppReportInput struct {
	Message    string
	Log        string
	DeviceName string
	OS         string
	OSVersion  string
	AppVersion string
}

func cutRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	if runes := []rune(s); len(runes) > n {
		return string(runes[:n])
	}
	return s
}

// orSession prefers what the app says now (it may have been updated since it signed in).
func orSession(now, atLogin string) string {
	if v := cutRunes(now, maxAppReportDeviceField); v != "" {
		return v
	}
	return atLogin
}

// FileReport stores a report under the session's account with the account's Telegram id,
// at most appReportsPerDeviceHour an hour per device, keeping the newest keptAppReports.
func (s *AppService) FileReport(session model.AppSession, in AppReportInput) (model.AppReport, error) {
	report := model.AppReport{
		CreatedAt:  time.Now().UnixMilli(),
		SubID:      session.SubID,
		Email:      session.Email,
		HwidHash:   session.HwidHash,
		DeviceName: orSession(in.DeviceName, session.DeviceName),
		DeviceOS:   orSession(in.OS, session.DeviceOS),
		OsVersion:  orSession(in.OSVersion, session.OsVersion),
		AppVersion: orSession(in.AppVersion, session.AppVersion),
		Message:    cutRunes(in.Message, maxAppReportMessage),
		Log:        in.Log,
		LogSize:    len(in.Log),
	}
	if report.LogSize > maxAppReportLog {
		return report, ErrAppReportTooLarge
	}
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		var recent int64
		since := report.CreatedAt - time.Hour.Milliseconds()
		if err := tx.Model(&model.AppReport{}).Where("hwid_hash = ? AND created_at > ?", report.HwidHash, since).Count(&recent).Error; err != nil {
			return err
		}
		if recent >= appReportsPerDeviceHour {
			return ErrAppTooManyReports
		}
		var owner model.ClientRecord
		err := tx.Select("tg_id").Where("sub_id = ? AND email = ?", session.SubID, session.Email).First(&owner).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		report.TgID = owner.TgID
		if err := tx.Create(&report).Error; err != nil {
			return err
		}
		return pruneAppReports(tx, keptAppReports)
	})
	return report, err
}

func pruneAppReports(tx *gorm.DB, keep int) error {
	var oldestKept model.AppReport
	err := tx.Select("id").Order("id DESC").Offset(keep - 1).Limit(1).Take(&oldestKept).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return tx.Where("id < ?", oldestKept.Id).Delete(&model.AppReport{}).Error
}

// Reports lists every report, newest first, without its log.
func (s *AppService) Reports() ([]model.AppReport, error) {
	reports := []model.AppReport{}
	err := database.GetDB().Omit("log").Order("id DESC").Find(&reports).Error
	return reports, err
}

func (s *AppService) Report(id int) (model.AppReport, error) {
	var report model.AppReport
	err := database.GetDB().First(&report, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return report, ErrAppUnknownReport
	}
	return report, err
}

func (s *AppService) UnseenReports() (int64, error) {
	var n int64
	err := database.GetDB().Model(&model.AppReport{}).Where("seen = ?", false).Count(&n).Error
	return n, err
}

func (s *AppService) MarkReportSeen(id int) error {
	return reportRowsAffected(database.GetDB().Model(&model.AppReport{}).Where("id = ?", id).Update("seen", true))
}

func (s *AppService) DeleteReport(id int) error {
	return reportRowsAffected(database.GetDB().Delete(&model.AppReport{}, id))
}

func reportRowsAffected(res *gorm.DB) error {
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrAppUnknownReport
	}
	return nil
}
