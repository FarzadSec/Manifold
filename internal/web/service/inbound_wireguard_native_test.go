package service

import (
	"slices"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/wgquick"
)

const (
	wgTestServerKey = "OEyW2GYBFVHTgB0a4gYZ4pXW0vlwtWRmKEB5H3xPz1Y="
	wgTestAliceKey  = "j+YN0164vTj8VigZXhwG7W55DcuT//8qERucl32kzVM="
	wgTestBobKey    = "qraEGD5B2ve022rwYQM+vw1sQgGeGJEDbzystC+MbDw="
	wgTestCarolKey  = "UNIsSne3GWwPunxmglRqjTPNZo6Zw0BQIIRw+p0a8HM="
)

func wgTestSettings(extra string, clients ...string) string {
	return `{"secretKey":"` + wgTestServerKey + `"` + extra + `,"clients":[` + strings.Join(clients, ",") + `]}`
}

func wgTestClient(email, key, address string, enable bool) string {
	state := "false"
	if enable {
		state = "true"
	}
	return `{"email":"` + email + `","publicKey":"` + key + `","allowedIPs":["` + address + `"],"enable":` + state + `}`
}

func peerNames(peers []wgquick.Peer) []string {
	names := make([]string, 0, len(peers))
	for _, p := range peers {
		names = append(names, p.Name)
	}
	return names
}

func TestDesiredWireGuardInstancesServeOnlyAccountsWithinTheirLimits(t *testing.T) {
	setupConflictDB(t)
	svc := &InboundService{}

	seedInboundConflict(t, "wg-served", "", 48001, model.WireGuard, "", wgTestSettings("",
		wgTestClient("alice", wgTestAliceKey, "10.0.0.2/32", true),
		wgTestClient("bob", wgTestBobKey, "10.0.0.3/32", true),
		wgTestClient("carol", wgTestCarolKey, "10.0.0.4/32", false)))
	served := loadInboundByTag(t, "wg-served")
	seedClientTraffic(t, served.Id, "alice", true)
	seedClientTraffic(t, served.Id, "bob", false)
	seedClientTraffic(t, served.Id, "carol", true)

	nodeID := 5
	seedInboundConflictNode(t, "wg-node-owned", "", 48002, model.WireGuard, "",
		wgTestSettings("", wgTestClient("erin", wgTestBobKey, "10.0.0.9/32", true)), &nodeID)

	instances, err := svc.DesiredWireGuardInstances()
	if err != nil {
		t.Fatalf("DesiredWireGuardInstances: %v", err)
	}
	if len(instances) != 1 || instances[0].Id != served.Id {
		t.Fatalf("instances = %+v, want only inbound %d", instances, served.Id)
	}
	if got := peerNames(instances[0].Peers); !slices.Equal(got, []string{"alice"}) {
		t.Fatalf("peers = %v, want only alice: bob is out of quota and carol is disabled", got)
	}

	// The save path and the job must agree, or the two would keep removing each other's peers.
	built, err := svc.buildInboundForLocalRuntime(database.GetDB(), served)
	if err != nil {
		t.Fatalf("buildInboundForLocalRuntime: %v", err)
	}
	pushed, ok := wgquick.InstanceFromInbound(built)
	if !ok || !slices.Equal(peerNames(pushed.Peers), peerNames(instances[0].Peers)) {
		t.Fatalf("save path serves %v, job serves %v", peerNames(pushed.Peers), peerNames(instances[0].Peers))
	}
}

// WireGuard runs on the kernel interface; a second, userspace copy inside Xray
// would fight it for the port.
func TestWireGuardInboundIsLeftOutOfTheXrayConfig(t *testing.T) {
	setupConflictDB(t)
	seedInboundConflict(t, "wg-kernel", "", 48010, model.WireGuard, "",
		wgTestSettings("", wgTestClient("alice", wgTestAliceKey, "10.0.0.2/32", true)))

	cfg, err := (&XrayService{}).GetXrayConfig()
	if err != nil {
		t.Fatalf("GetXrayConfig: %v", err)
	}
	for i := range cfg.InboundConfigs {
		if cfg.InboundConfigs[i].Tag == "wg-kernel" || cfg.InboundConfigs[i].Port == 48010 {
			t.Fatalf("the Xray config carries the WireGuard inbound: %+v", cfg.InboundConfigs[i])
		}
	}
}

func TestWireGuardInterfaceNamesAreCheckedOnSave(t *testing.T) {
	setupConflictDB(t)
	svc := &InboundService{}
	add := func(port int, extra string) error {
		_, _, err := svc.AddInbound(&model.Inbound{
			UserId: 1, Enable: true, Port: port, Protocol: model.WireGuard, Tag: "inbound-" + strings.Repeat("x", port%7+1),
			Settings: wgTestSettings(extra),
		})
		return err
	}
	if err := add(48020, `,"interface":"DE"`); err != nil {
		t.Fatalf("a valid interface name was refused: %v", err)
	}
	if err := add(48021, `,"interface":"has space"`); err == nil || !strings.Contains(err.Error(), "cannot be an interface name") {
		t.Fatalf("error = %v, want the interface name refusal", err)
	}
	if err := add(48022, `,"interface":"DE"`); err == nil || !strings.Contains(err.Error(), "already uses the interface DE") {
		t.Fatalf("error = %v, want the duplicate interface refusal", err)
	}
	if err := add(48023, `,"address":"fd42::1/64"`); err == nil || !strings.Contains(err.Error(), "the interface needs an IPv4 address") {
		t.Fatalf("error = %v, want the refusal of a server without an IPv4 address", err)
	}
}
