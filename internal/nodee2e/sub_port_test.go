package nodee2e

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"testing"
)

// A subscription port held by another program must not take the panel down:
// the panel is where that port is changed. Installing next to another panel
// used to leave a service that died on every start.
func TestPanelStartsWhenTheSubscriptionPortIsTaken(t *testing.T) {
	bin := panelBinary(t)
	squatter, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = squatter.Close() })
	taken := squatter.Addr().(*net.TCPAddr).Port

	p := newPanel(t, bin, "busy-sub-port")
	p.cli("setting", "-subPort", strconv.Itoa(taken))
	p.start()

	var settings struct {
		SubPort int `json:"subPort"`
	}
	if err := json.Unmarshal(p.call(http.MethodPost, "/panel/api/setting/all", nil), &settings); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if settings.SubPort != taken {
		t.Fatalf("subPort = %d, want the taken port %d: the panel was not started against it", settings.SubPort, taken)
	}
}
