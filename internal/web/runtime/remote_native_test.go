package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/wirecodec"
)

// A node built before an engine existed would store the inbound and serve nothing.
func TestRemoteRefusesNativeProtocolOnNodeWithoutItsEngine(t *testing.T) {
	cases := []struct {
		name       string
		caps       string
		protocol   model.Protocol
		wantRefuse bool
	}{
		{"node from before OpenVPN", wirecodec.CapZstd + "," + wirecodec.CapAmneziaWGKernel, model.OpenVPN, true},
		{"node that advertises nothing", "", model.OpenVPN, true},
		{"node with the OpenVPN engine", wirecodec.NodeCaps, model.OpenVPN, false},
		{"Xray protocol on an older node", wirecodec.CapZstd, model.VLESS, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var pushed bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.caps != "" {
					w.Header().Set(wirecodec.CapsHeader, tc.caps)
				}
				if r.Method == http.MethodPost {
					pushed = true
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"success":true,"obj":[]}`))
			}))
			defer srv.Close()

			r := NewRemote(nodeForPlainServer(t, srv, "verify", "tok"), nil)
			ib := &model.Inbound{Id: 1, Tag: "inbound-1194", Port: 1194, Protocol: tc.protocol, Settings: "{}"}
			err := r.AddInbound(context.Background(), ib)
			if refused := err != nil && strings.Contains(err.Error(), "cannot serve openvpn inbounds"); refused != tc.wantRefuse {
				t.Fatalf("refused = %v (err %v), want %v", refused, err, tc.wantRefuse)
			}
			if pushed == tc.wantRefuse {
				t.Fatalf("inbound pushed = %v, want %v", pushed, !tc.wantRefuse)
			}
		})
	}
}
