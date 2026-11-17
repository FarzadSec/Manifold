package service

import (
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestCustomerFieldsAreKeptSearchedAndExported(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	ib := mkInboundStream(t, 30031, model.VLESS, `{"clients":[],"decryption":"none"}`, wsStream)
	if _, err := svc.Create(&InboundService{}, &ClientCreatePayload{
		Client: model.Client{
			Email: "ali", ID: "aaaaaaaa-1111-2222-3333-444444444444", SubID: "sub-ali", Enable: true,
			Contacts: map[string]string{"phone": " 0912 345 6789 ", "telegram": "@ali", "Instagram": "ali.x", "bale": " "},
		},
		InboundIds: []int{ib.Id},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(&InboundService{}, &ClientCreatePayload{
		Client:     model.Client{Email: "other", ID: "bbbbbbbb-1111-2222-3333-444444444444", SubID: "sub-other", Enable: true},
		InboundIds: []int{ib.Id},
	}); err != nil {
		t.Fatal(err)
	}
	rec, err := svc.GetRecordByEmail(nil, "ali")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"phone": "0912 345 6789", "telegram": "@ali", "Instagram": "ali.x"}
	if len(rec.Contacts) != len(want) || rec.Contacts["phone"] != want["phone"] || rec.Contacts["Instagram"] != "ali.x" {
		t.Fatalf("contacts = %v, want %v", rec.Contacts, want)
	}
	page, err := svc.ListPaged(&InboundService{}, &SettingService{}, ClientPageParams{Page: 1, PageSize: 25, Search: "345 6789"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Email != "ali" {
		t.Fatalf("searching the phone found %+v, want only ali", page.Items)
	}
	if page, _ = svc.ListPaged(&InboundService{}, &SettingService{}, ClientPageParams{Page: 1, PageSize: 25, Search: "ali.x"}); len(page.Items) != 0 {
		t.Fatalf("a custom field was searched: %+v", page.Items)
	}
	csv, err := svc.ExportCSV([]string{"ali"}, -1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(csv, ",@ali,,0912 345 6789,") {
		t.Fatalf("export lacks the contacts in their columns:\n%s", csv)
	}
}
