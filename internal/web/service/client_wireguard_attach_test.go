package service

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func wgLocationSettings(subnet string) string {
	return `{"secretKey":"` + wgTestSecretKey() + `","subnetIp":"` + subnet + `","subnetCidr":20,"clients":[]}`
}

func wgPeerAddress(t *testing.T, inboundSvc *InboundService, inboundID int, email string) string {
	t.Helper()
	ib, err := inboundSvc.GetInbound(inboundID)
	if err != nil {
		t.Fatal(err)
	}
	clients, err := inboundSvc.GetClients(ib)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range clients {
		if strings.EqualFold(c.Email, email) {
			if len(c.AllowedIPs) != 1 {
				t.Fatalf("%s on inbound %d has addresses %v, want one", email, inboundID, c.AllowedIPs)
			}
			return c.AllowedIPs[0]
		}
	}
	t.Fatalf("%s is not on inbound %d", email, inboundID)
	return ""
}

func createOn(t *testing.T, svc *ClientService, inboundSvc *InboundService, email string, inboundIDs ...int) {
	t.Helper()
	if _, err := svc.Create(inboundSvc, &ClientCreatePayload{
		Client:     model.Client{Email: email, Enable: true, Password: "pw0123456789abcd"},
		InboundIds: inboundIDs,
	}); err != nil {
		t.Fatalf("create %s: %v", email, err)
	}
}

// A record keeps its last tunnel address after a detach, and another account may have it
// since: carried into a bulk attach, it refused every account of the batch on that inbound.
func TestBulkAttachGivesAStaleRecordAddressBackToThePool(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	inboundSvc := &InboundService{}
	wg := mkInbound(t, 52300, model.WireGuard, wgServerSettings())
	ovpn := mkInbound(t, 52301, model.OpenVPN, `{"clients":[]}`)

	createOn(t, svc, inboundSvc, "former", wg.Id, ovpn.Id)
	formerAddress := wgPeerAddress(t, inboundSvc, wg.Id, "former")
	former := lookupClientRecord(t, "former")
	if _, err := svc.Detach(inboundSvc, former.Id, []int{wg.Id}); err != nil {
		t.Fatal(err)
	}
	createOn(t, svc, inboundSvc, "holder", wg.Id)
	if got := wgPeerAddress(t, inboundSvc, wg.Id, "holder"); got != formerAddress {
		t.Fatalf("holder got %s; the test needs it to reuse the freed %s", got, formerAddress)
	}
	createOn(t, svc, inboundSvc, "newcomer", ovpn.Id)

	res, _, err := svc.BulkAttach(inboundSvc, []string{"former", "newcomer"}, []int{wg.Id})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) != 0 || len(res.Attached) != 2 {
		t.Fatalf("attached %v, errors %v; want both attached", res.Attached, res.Errors)
	}
	seen := map[string]string{"holder": formerAddress}
	for _, email := range []string{"former", "newcomer"} {
		address := wgPeerAddress(t, inboundSvc, wg.Id, email)
		for other, taken := range seen {
			if taken == address {
				t.Fatalf("%s got %s, which %s already holds", email, address, other)
			}
		}
		seen[email] = address
	}
}

// Each location's inbound runs on the same server with a subnet of its own; an address
// carried from another location put one host route on two interfaces.
func TestAttachToAnotherLocationTakesAnAddressFromItsSubnet(t *testing.T) {
	for _, bulk := range []bool{false, true} {
		name := "attach"
		if bulk {
			name = "bulk attach"
		}
		t.Run(name, func(t *testing.T) {
			setupBulkDB(t)
			svc := &ClientService{}
			inboundSvc := &InboundService{}
			germany := mkInbound(t, 52310, model.WireGuard, wgLocationSettings("172.21.0.0"))
			finland := mkInbound(t, 52311, model.WireGuard, wgLocationSettings("172.22.0.0"))

			createOn(t, svc, inboundSvc, "traveller", germany.Id)
			rec := lookupClientRecord(t, "traveller")
			if bulk {
				res, _, err := svc.BulkAttach(inboundSvc, []string{"traveller"}, []int{finland.Id})
				if err != nil || len(res.Errors) != 0 {
					t.Fatalf("bulk attach: %v %v", err, res.Errors)
				}
			} else if _, err := svc.Attach(inboundSvc, rec.Id, []int{finland.Id}); err != nil {
				t.Fatal(err)
			}

			got, err := netip.ParsePrefix(wgPeerAddress(t, inboundSvc, finland.Id, "traveller"))
			if err != nil {
				t.Fatal(err)
			}
			if !netip.MustParsePrefix("172.22.0.0/16").Contains(got.Addr()) {
				t.Fatalf("Finland's peer has %s, want an address of Finland's 172.22.0.0/20", got)
			}
			if wgPeerAddress(t, inboundSvc, germany.Id, "traveller") == got.String() {
				t.Fatal("both locations' peers share one address")
			}
		})
	}
}
