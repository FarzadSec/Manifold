package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// A sales bot creates an account on every location at once and sends no key; the account
// must still carry one key, with each location's own address, as the panel's form makes it.
func TestAccountCreatedOnSeveralWireGuardInboundsHasOneKey(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	inboundSvc := &InboundService{}
	germany := mkInbound(t, 52230, model.WireGuard, `{"secretKey":"`+wgTestSecretKey()+`","subnetIp":"10.66.0.0","subnetCidr":24,"clients":[]}`)
	netherlands := mkInbound(t, 52231, model.WireGuard, `{"secretKey":"`+wgTestSecretKey()+`","subnetIp":"10.67.0.0","subnetCidr":24,"clients":[]}`)

	if _, err := svc.Create(inboundSvc, &ClientCreatePayload{
		Client:     model.Client{Email: "one-key@wg", Enable: true, SubID: "onekeysub0000001"},
		InboundIds: []int{germany.Id, netherlands.Id},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	peerOf := func(ib *model.Inbound) model.Client {
		t.Helper()
		fresh, err := inboundSvc.GetInbound(ib.Id)
		if err != nil {
			t.Fatal(err)
		}
		clients, err := inboundSvc.GetClients(fresh)
		if err != nil || len(clients) != 1 {
			t.Fatalf("inbound %d clients = %+v (%v)", ib.Id, clients, err)
		}
		return clients[0]
	}
	de, nl := peerOf(germany), peerOf(netherlands)
	if de.PrivateKey == "" || de.PrivateKey != nl.PrivateKey || de.PublicKey != nl.PublicKey {
		t.Fatalf("keys differ between locations: %q/%q and %q/%q", de.PrivateKey, de.PublicKey, nl.PrivateKey, nl.PublicKey)
	}
	if len(de.AllowedIPs) != 1 || len(nl.AllowedIPs) != 1 || de.AllowedIPs[0][:6] != "10.66." || nl.AllowedIPs[0][:6] != "10.67." {
		t.Fatalf("addresses %v and %v, want each from its own inbound's subnet", de.AllowedIPs, nl.AllowedIPs)
	}
}
