package tunnel

import (
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/egress"
)

func greClientForwardingBack() Tunnel {
	tun := greClient()
	tun.ID = 7
	tun.Settings.ReverseForwards = []PortForward{{Listen: "9090", Target: "19090"}, {Listen: "5353", Proto: ProtoUDP}}
	return tun
}

// Each end listens on the ports it sends to the other: the relay on Forwards, the
// client end on ReverseForwards, and the pairing code carries both to the other end.
func TestEachEndListensOnThePortsItForwards(t *testing.T) {
	client := normalized(t, greClientForwardingBack())
	if got, want := client.ListenPorts(), map[int]string{9090: ProtoTCP, 5353: ProtoUDP}; !reflect.DeepEqual(got, want) {
		t.Fatalf("client end listens on %v, want %v", got, want)
	}
	relay := Mirror(client, "203.0.113.10")
	if !reflect.DeepEqual(relay.Settings.ReverseForwards, client.Settings.ReverseForwards) {
		t.Fatalf("the relay's end lost the client's forwards: %+v", relay.Settings.ReverseForwards)
	}
	if got, want := relay.ListenPorts(), map[int]string{443: ProtoTCP, 2201: ProtoBoth}; !reflect.DeepEqual(got, want) {
		t.Fatalf("relay listens on %v, want %v", got, want)
	}
}

func TestReverseForwardsNeedATunnelWithAnInterface(t *testing.T) {
	_, err := Normalize(Tunnel{Name: "r", Kind: KindReverse, Role: RoleClient, Settings: TunnelSettings{
		Transport: "tcpmux", Port: 8443, RemoteAddr: "relay.example.com", ReverseForwards: []PortForward{{Listen: "9090"}},
	}})
	if err == nil || !strings.Contains(err.Error(), "BackPack reverse tunnel carries ports from the relay only") {
		t.Fatalf("a reverse tunnel took client-side forwards: %v", err)
	}
	tun := greClientForwardingBack()
	tun.Settings.ReverseForwards = append(tun.Settings.ReverseForwards, PortForward{Listen: "9090"})
	if _, err := Normalize(tun); err == nil || !strings.Contains(err.Error(), "port 9090 is forwarded twice") {
		t.Fatalf("a doubled client-side port: %v", err)
	}
}

func TestClientEndForwardsItsPortsToTheRelayThroughTheLink(t *testing.T) {
	useHost(t, &fakeHost{files: map[string]string{}})
	type call struct {
		owner, device string
		target        netip.Addr
		rules         []egress.PortForward
		masquerade    bool
	}
	var calls []call
	prev := applyPortForwards
	applyPortForwards = func(owner, device string, target netip.Addr, rules []egress.PortForward, masquerade bool) error {
		calls = append(calls, call{owner, device, target, rules, masquerade})
		return nil
	}
	t.Cleanup(func() { applyPortForwards = prev })

	client := normalized(t, greClientForwardingBack())
	client.Settings.NAT = NATPreserve
	if err := applyRouting(client); err != nil {
		t.Fatal(err)
	}
	want := call{"tf7", "mft7", netip.MustParseAddr("10.80.0.1"), []egress.PortForward{
		{Proto: "tcp", Low: 9090, High: 9090, TargetLow: 19090},
		{Proto: "udp", Low: 5353, High: 5353, TargetLow: 5353},
	}, true}
	if len(calls) != 1 || !reflect.DeepEqual(calls[0], want) {
		t.Fatalf("forwards applied = %+v, want %+v", calls, want)
	}
}

func TestLayer3ClientEndCarriesItsOwnPorts(t *testing.T) {
	client := normalized(t, Tunnel{ID: 6, Name: "l", Kind: KindL3, Role: RoleClient, Settings: TunnelSettings{
		Carrier: "udp", Port: 9000, Token: "0123456789abcdef0123", LocalIP: "10.9.0.2/30", PeerIP: "10.9.0.1",
		Forwards: []PortForward{{Listen: "443"}}, ReverseForwards: []PortForward{{Listen: "9090", Target: "19090", Proto: ProtoBoth}},
	}})
	text := renderBackPack(client)
	if !strings.Contains(text, `ports = ["9090=19090"]`) || !strings.Contains(text, "accept_udp = true") || strings.Contains(text, `"443"`) {
		t.Fatalf("the client end's layer-3 config does not carry its own ports:\n%s", text)
	}
}
