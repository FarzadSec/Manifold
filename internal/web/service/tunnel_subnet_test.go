package service

import (
	"fmt"
	"net/netip"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// Two WireGuard inbounds ended up on 172.24.0.1/20 with the same peers: the kernel sent every reply
// out of one interface and the other inbound's users got a handshake and no data.
func TestTunnelNetworksAreKeptApartOnOneHost(t *testing.T) {
	setupConflictDB(t)
	svc := &InboundService{}
	add := func(port int, protocol model.Protocol, settings string) (*model.Inbound, error) {
		ib, _, err := svc.AddInbound(&model.Inbound{
			UserId: 1, Enable: protocol == model.WireGuard, Port: port, Protocol: protocol,
			Tag: fmt.Sprintf("inbound-tunnel-%d", port), Settings: settings,
		})
		return ib, err
	}
	only := func(t *testing.T, ib *model.Inbound) netip.Prefix {
		t.Helper()
		subnets := tunnelSubnets(ib)
		if len(subnets) != 1 {
			t.Fatalf("inbound %d has networks %v, want one", ib.Id, subnets)
		}
		return subnets[0]
	}

	first, err := add(48101, model.WireGuard, wgTestSettings(""))
	if err != nil {
		t.Fatalf("first WireGuard inbound: %v", err)
	}
	second, err := add(48102, model.WireGuard, wgTestSettings(""))
	if err != nil {
		t.Fatalf("second WireGuard inbound with no network named: %v", err)
	}
	if a, b := only(t, first), only(t, second); a.Overlaps(b) {
		t.Fatalf("both inbounds left their network empty and got %s and %s", a, b)
	}

	ovpn := `{"server":{"proto":"udp"},"clients":[]}`
	third, err := add(48103, model.OpenVPN, ovpn)
	if err != nil {
		t.Fatalf("first OpenVPN inbound: %v", err)
	}
	fourth, err := add(48104, model.OpenVPN, ovpn)
	if err != nil {
		t.Fatalf("second OpenVPN inbound with no subnet named: %v", err)
	}
	if a, b := only(t, third), only(t, fourth); a.Overlaps(b) {
		t.Fatalf("both OpenVPN inbounds got %s and %s", a, b)
	}

	taken := only(t, first)
	named := fmt.Sprintf(`,"subnetIp":"%s","subnetCidr":%d`, taken.Addr(), taken.Bits())
	if _, err := add(48105, model.WireGuard, wgTestSettings(named)); err == nil || !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("a WireGuard inbound on %s was stored next to inbound %d: err = %v", taken, first.Id, err)
	}

	second.Settings = wgTestSettings(named)
	if _, _, err := svc.UpdateInbound(second); err == nil || !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("moving inbound %d onto %s was accepted: err = %v", second.Id, taken, err)
	}
}
