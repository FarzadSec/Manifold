package service

import (
	"slices"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func seedNode(t *testing.T, name string) int {
	t.Helper()
	n := model.Node{Name: name, Address: name + ".example.com", Port: 2053, Scheme: "https", Enable: true, Status: "online", TlsVerifyMode: "verify"}
	if err := database.GetDB().Create(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n.Id
}

func inboundsOfEmail(t *testing.T, svc *ClientService, email string) []int {
	t.Helper()
	rec, err := svc.GetRecordByEmail(nil, email)
	if err != nil {
		t.Fatalf("GetRecordByEmail(%q): %v", email, err)
	}
	ids, err := svc.GetInboundIdsForRecord(rec.Id)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(ids)
	return ids
}

// A deployed inbound lands on each new node once, with its users, and stays in step:
// an edit reaches every copy, and a user taken off one copy is taken off all.
func TestLinkedInboundCopiesStayInStep(t *testing.T) {
	setupBulkDB(t)
	svc, inboundSvc := &ClientService{}, &InboundService{}
	kept := model.Client{Email: "kept@x", ID: "11111111-1111-1111-1111-111111111111", SubID: "keptsub", Enable: true}
	src := mkInbound(t, 54001, model.VLESS, clientsSettings(t, []model.Client{kept}))
	if err := svc.SyncInbound(nil, src.Id, []model.Client{kept}); err != nil {
		t.Fatalf("seed linkage: %v", err)
	}
	if err := database.GetDB().Model(&model.Inbound{}).Where("id = ?", src.Id).
		Updates(map[string]any{"share_addr_strategy": "custom", "share_addr": "a.example.com"}).Error; err != nil {
		t.Fatal(err)
	}
	fra, ams := seedNode(t, "fra"), seedNode(t, "ams")

	res, _, err := inboundSvc.DeployToNodes(src.Id, []int{fra, ams, 0})
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if len(res.Created) != 2 {
		t.Fatalf("deploy created %v, want one copy on each of the two nodes and none on this panel", res.Created)
	}
	again, _, err := inboundSvc.DeployToNodes(src.Id, []int{fra})
	if err != nil || len(again.Created) != 0 {
		t.Fatalf("a second deploy to a node holding a copy created %v, %v", again.Created, err)
	}
	all := append([]int{src.Id}, res.Created...)
	slices.Sort(all)
	if got := inboundsOfEmail(t, svc, "kept@x"); !slices.Equal(got, all) {
		t.Fatalf("the inbound's user is on %v, want every copy %v", got, all)
	}
	stored, _ := inboundSvc.GetInbound(src.Id)
	for _, id := range res.Created {
		cp, _ := inboundSvc.GetInbound(id)
		if cp.LinkGroup == "" || cp.LinkGroup != stored.LinkGroup || cp.Port != src.Port || cp.NodeID == nil || cp.Tag == src.Tag {
			t.Errorf("copy %+v is not a linked copy of %d on its own node with its own tag", cp, src.Id)
		}
		if cp.ShareAddrStrategy != "node" {
			t.Errorf("copy share address strategy = %q, want its own node's, not src's custom address", cp.ShareAddrStrategy)
		}
	}

	edited := *stored
	edited.Remark, edited.Port = "moved", 54002
	updated, _, err := inboundSvc.UpdateInbound(&edited)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := inboundSvc.UpdateLinkedCopies(updated); err != nil {
		t.Fatalf("carry the edit: %v", err)
	}
	for _, id := range res.Created {
		cp, _ := inboundSvc.GetInbound(id)
		if cp.Remark != "moved" || cp.Port != 54002 || cp.NodeID == nil {
			t.Errorf("copy %d after the edit: remark %q port %d node %v, want the edit on its own node", id, cp.Remark, cp.Port, cp.NodeID)
		}
		if cp.ShareAddrStrategy != "node" {
			t.Errorf("the edit carried src's share address to copy %d", id)
		}
	}
	if got := inboundsOfEmail(t, svc, "kept@x"); !slices.Equal(got, all) {
		t.Fatalf("after the edit the user is on %v, want %v", got, all)
	}

	ids, err := ClientAccess{}.NarrowInbounds([]int{res.Created[0]})
	if slices.Sort(ids); err != nil || !slices.Equal(ids, all) {
		t.Errorf("a user put on one copy lands on %v, %v, want every copy %v", ids, err, all)
	}
	if _, err := svc.DetachByEmailMany(inboundSvc, "kept@x", []int{res.Created[1]}); err != nil {
		t.Fatalf("detach: %v", err)
	}
	if got := inboundsOfEmail(t, svc, "kept@x"); len(got) != 0 {
		t.Errorf("taken off one copy, the user is still on %v", got)
	}
	if _, err := svc.AttachByEmail(inboundSvc, "kept@x", all); err != nil {
		t.Fatalf("attach back: %v", err)
	}
	if _, _, err := svc.BulkDetach(inboundSvc, []string{"kept@x"}, []int{res.Created[0]}); err != nil {
		t.Fatalf("bulk detach: %v", err)
	}
	if got := inboundsOfEmail(t, svc, "kept@x"); len(got) != 0 {
		t.Errorf("bulk-detached from one copy, the user is still on %v", got)
	}

	if err := inboundSvc.Unlink(res.Created[0]); err != nil {
		t.Fatal(err)
	}
	edited = *updated
	edited.Remark = "alone"
	updated, _, err = inboundSvc.UpdateInbound(&edited)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inboundSvc.UpdateLinkedCopies(updated); err != nil {
		t.Fatal(err)
	}
	if cp, _ := inboundSvc.GetInbound(res.Created[0]); cp.Remark != "moved" {
		t.Errorf("an unlinked copy took the edit: remark %q", cp.Remark)
	}
	if cp, _ := inboundSvc.GetInbound(res.Created[1]); cp.Remark != "alone" {
		t.Errorf("a still-linked copy missed the edit: remark %q", cp.Remark)
	}
}
