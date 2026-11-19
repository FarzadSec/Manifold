package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func seedReportingClient(t *testing.T, tgID int64) *model.ClientRecord {
	t.Helper()
	rec := seedHwidClient(t, 0)
	if err := database.GetDB().Model(&model.ClientRecord{}).Where("id = ?", rec.Id).Update("tg_id", tgID).Error; err != nil {
		t.Fatalf("set tg id: %v", err)
	}
	return rec
}

func signedInSession(t *testing.T, svc *AppService, rec *model.ClientRecord, device string) model.AppSession {
	t.Helper()
	token, err := svc.Login(rec.SubID, rec.Email, appDevice(device))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	session, err := svc.Session(rec.SubID, token, device)
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	return session
}

func TestAppReportIsFiledUnderTheAccountAndItsTelegramID(t *testing.T) {
	svc := initAppTestDB(t, true)
	session := signedInSession(t, svc, seedReportingClient(t, 987654321), "device-one")
	filed, err := svc.FileReport(session, AppReportInput{
		Message: "  به هیچ لوکیشنی وصل نمی\u200cشود  ", Log: "=== app ===\nline 1\n", DeviceName: "Pixel 8", OS: "android", OSVersion: "15", AppVersion: "1.1.0",
	})
	if err != nil {
		t.Fatalf("FileReport: %v", err)
	}
	got, err := svc.Report(filed.Id)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	want := model.AppReport{
		Id: filed.Id, CreatedAt: got.CreatedAt, SubID: session.SubID, Email: session.Email, TgID: 987654321,
		DeviceName: "Pixel 8", DeviceOS: "android", OsVersion: "15", AppVersion: "1.1.0",
		Message: "به هیچ لوکیشنی وصل نمی\u200cشود", Log: "=== app ===\nline 1\n", LogSize: 19,
	}
	got.HwidHash = ""
	if got != want || got.CreatedAt == 0 {
		t.Fatalf("report = %+v\nwant     %+v", got, want)
	}
}

func TestAppReportListLeavesTheLogsOutAndCountsUnseen(t *testing.T) {
	svc := initAppTestDB(t, true)
	session := signedInSession(t, svc, seedReportingClient(t, 0), "device-one")
	first, err := svc.FileReport(session, AppReportInput{Log: strings.Repeat("x", 5000)})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := svc.FileReport(session, AppReportInput{Message: "again", Log: "short"})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	list, err := svc.Reports()
	if err != nil || len(list) != 2 || list[0].Id != second.Id || list[1].Id != first.Id {
		t.Fatalf("reports = %+v %v, want the newest first", list, err)
	}
	if list[0].Log != "" || list[1].Log != "" || list[1].LogSize != 5000 {
		t.Fatalf("the list carries logs or lost their size: %+v", list)
	}

	if n, err := svc.UnseenReports(); err != nil || n != 2 {
		t.Fatalf("unseen = %d %v, want 2", n, err)
	}
	if err := svc.MarkReportSeen(first.Id); err != nil {
		t.Fatalf("mark seen: %v", err)
	}
	if n, _ := svc.UnseenReports(); n != 1 {
		t.Fatalf("unseen after marking one = %d, want 1", n)
	}
	if err := svc.DeleteReport(second.Id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	for name, err := range map[string]error{"delete": svc.DeleteReport(second.Id), "seen": svc.MarkReportSeen(second.Id)} {
		if !errors.Is(err, ErrAppUnknownReport) {
			t.Fatalf("%s of a deleted report = %v, want ErrAppUnknownReport", name, err)
		}
	}
	if _, err := svc.Report(second.Id); !errors.Is(err, ErrAppUnknownReport) {
		t.Fatalf("reading a deleted report = %v, want ErrAppUnknownReport", err)
	}
}

func TestAppReportRefusesFloodsAndOversizedLogs(t *testing.T) {
	svc := initAppTestDB(t, true)
	rec := seedReportingClient(t, 0)
	session := signedInSession(t, svc, rec, "device-one")
	if _, err := svc.FileReport(session, AppReportInput{Log: strings.Repeat("x", maxAppReportLog+1)}); !errors.Is(err, ErrAppReportTooLarge) {
		t.Fatalf("an oversized log = %v, want ErrAppReportTooLarge", err)
	}
	for i := range appReportsPerDeviceHour {
		if _, err := svc.FileReport(session, AppReportInput{Message: "try"}); err != nil {
			t.Fatalf("report %d: %v", i+1, err)
		}
	}
	if _, err := svc.FileReport(session, AppReportInput{Message: "one more"}); !errors.Is(err, ErrAppTooManyReports) {
		t.Fatalf("report over the hourly limit = %v, want ErrAppTooManyReports", err)
	}
	other := signedInSession(t, svc, rec, "device-two")
	if _, err := svc.FileReport(other, AppReportInput{Message: "another phone"}); err != nil {
		t.Fatalf("another device is held to its own limit: %v", err)
	}
	if long, _ := svc.FileReport(signedInSession(t, svc, rec, "device-three"), AppReportInput{Message: strings.Repeat("ب", maxAppReportMessage+10)}); len([]rune(long.Message)) != maxAppReportMessage {
		t.Fatalf("message kept %d runes, want it cut to %d", len([]rune(long.Message)), maxAppReportMessage)
	}
}

func TestPruneAppReportsKeepsTheNewest(t *testing.T) {
	initAppTestDB(t, true)
	db := database.GetDB()
	for i := range 4 {
		if err := db.Create(&model.AppReport{CreatedAt: int64(i + 1), SubID: "s", Email: "e", HwidHash: "h"}).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	if err := pruneAppReports(db, 2); err != nil {
		t.Fatalf("prune: %v", err)
	}
	var left []model.AppReport
	if err := db.Order("id").Find(&left).Error; err != nil || len(left) != 2 || left[0].CreatedAt != 3 || left[1].CreatedAt != 4 {
		t.Fatalf("after prune = %+v %v, want the two newest", left, err)
	}
}
