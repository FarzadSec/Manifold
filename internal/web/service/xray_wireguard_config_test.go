package service

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/util/json_util"
)

func wgTestSecretKey() string {
	return base64.StdEncoding.EncodeToString(make([]byte, 32))
}

// #6796: where the core would pick a kernel TUN it cannot set up (Docker, a sandboxed unit),
// one WireGuard outbound aborts the whole start unless it is moved to the userspace TUN.
func TestForceUserspaceWireGuardTun(t *testing.T) {
	cases := []struct {
		name, protocol, settings string
	}{
		{"kernel TUN requested", "wireguard", `{"noKernelTun":false,"mtu":1420}`},
		{"kernel TUN by default, protocol cased like the core accepts", "WireGuard", `{"mtu":1420}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := json_util.RawMessage(`[{"protocol":"freedom","tag":"direct","settings":{}},` +
				`{"protocol":"` + tc.protocol + `","tag":"warp","settings":` + tc.settings + `}]`)
			var outbounds []struct {
				Settings map[string]any `json:"settings"`
			}
			if err := json.Unmarshal(forceUserspaceWireGuardTun(raw), &outbounds); err != nil {
				t.Fatal(err)
			}
			if got := outbounds[1].Settings["noKernelTun"]; got != true {
				t.Fatalf("wireguard noKernelTun = %v, want true", got)
			}
			if got := outbounds[1].Settings["mtu"]; got != float64(1420) {
				t.Fatalf("wireguard mtu = %v, want the template's 1420 kept", got)
			}
			if _, ok := outbounds[0].Settings["noKernelTun"]; ok {
				t.Fatalf("freedom outbound gained noKernelTun: %v", outbounds[0].Settings)
			}
		})
	}
}
