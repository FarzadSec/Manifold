package service

import (
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// The app's contract: it shows info in its notices list only and puts warning
// (amber) and critical (red) at the top of its window as well.
const (
	AppNoticeInfo     = "info"
	AppNoticeWarning  = "warning"
	AppNoticeCritical = "critical"
)

const maxAppNoticeLength = 1000

var (
	ErrAppNoticeEmpty   = errors.New("a notice needs text")
	ErrAppNoticeTooLong = errors.New("a notice is 1000 characters at most")
	ErrAppNoticeLevel   = errors.New("the level must be info, warning or critical")
	ErrAppUnknownNotice = errors.New("no such notice")
)

// Notices returns every published notice, newest first.
func (s *AppService) Notices() ([]model.AppNotice, error) {
	notices := []model.AppNotice{}
	err := database.GetDB().Order("id DESC").Find(&notices).Error
	return notices, err
}

func (s *AppService) PublishNotice(body, level string) (model.AppNotice, error) {
	notice := model.AppNotice{Body: strings.TrimSpace(body), Level: level, CreatedAt: time.Now().UnixMilli()}
	switch {
	case notice.Body == "":
		return notice, ErrAppNoticeEmpty
	case utf8.RuneCountInString(notice.Body) > maxAppNoticeLength:
		return notice, ErrAppNoticeTooLong
	case !slices.Contains([]string{AppNoticeInfo, AppNoticeWarning, AppNoticeCritical}, level):
		return notice, ErrAppNoticeLevel
	}
	err := database.GetDB().Create(&notice).Error
	return notice, err
}

func (s *AppService) DeleteNotice(id int) error {
	res := database.GetDB().Delete(&model.AppNotice{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrAppUnknownNotice
	}
	return nil
}
