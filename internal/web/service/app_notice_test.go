package service

import (
	"errors"
	"strings"
	"testing"
)

func TestAppNoticePublishRefusesWhatTheAppCannotShow(t *testing.T) {
	svc := initAppTestDB(t, true)
	for _, tc := range []struct {
		name, body, level string
		want              error
	}{
		{"blank text", "  \n ", AppNoticeInfo, ErrAppNoticeEmpty},
		{"unknown level", "سرور آلمان امشب به\u200cروزرسانی می\u200cشود", "loud", ErrAppNoticeLevel},
		{"over the length cap in runes", strings.Repeat("ب", maxAppNoticeLength+1), AppNoticeInfo, ErrAppNoticeTooLong},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.PublishNotice(tc.body, tc.level); !errors.Is(err, tc.want) {
				t.Fatalf("PublishNotice = %v, want %v", err, tc.want)
			}
		})
	}
	if list, err := svc.Notices(); err != nil || len(list) != 0 {
		t.Fatalf("a refused notice was stored: %v %v", list, err)
	}
	if _, err := svc.PublishNotice(strings.Repeat("ب", maxAppNoticeLength), AppNoticeInfo); err != nil {
		t.Fatalf("a notice of exactly the cap in Persian letters (twice the bytes) = %v", err)
	}
}

func TestAppNoticesListNewestFirstAndDelete(t *testing.T) {
	svc := initAppTestDB(t, true)
	older, err := svc.PublishNotice("  سرور آلمان امشب ساعت ۲ به\u200cروزرسانی می\u200cشود  ", AppNoticeInfo)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	newer, err := svc.PublishNotice("لینک اشتراک عوض شد؛ اپ را یک بار Refresh کنید", AppNoticeCritical)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if older.Body != "سرور آلمان امشب ساعت ۲ به\u200cروزرسانی می\u200cشود" || older.CreatedAt == 0 {
		t.Fatalf("stored notice = %+v, want the trimmed text and its time", older)
	}

	list, err := svc.Notices()
	if err != nil || len(list) != 2 || list[0].Id != newer.Id || list[1].Id != older.Id || list[0].Level != AppNoticeCritical {
		t.Fatalf("notices = %+v %v, want the critical one first", list, err)
	}

	if err := svc.DeleteNotice(newer.Id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := svc.DeleteNotice(newer.Id); !errors.Is(err, ErrAppUnknownNotice) {
		t.Fatalf("deleting it twice = %v, want ErrAppUnknownNotice", err)
	}
	if list, _ := svc.Notices(); len(list) != 1 || list[0].Id != older.Id {
		t.Fatalf("after delete = %+v", list)
	}
}
