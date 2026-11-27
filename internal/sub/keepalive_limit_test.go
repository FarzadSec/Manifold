package sub

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// Clash and sing-box carry the same keepalive as the link, the user limit's default included.
func TestScalarKeepAliveGivesALimitedAccountTheDefault(t *testing.T) {
	if ka, ok := keepAliveForScalarExport(model.Client{UserLimit: 1}); !ok || ka != model.LimitedKeepAlive {
		t.Fatalf("limited account without a keepalive: %d %v", ka, ok)
	}
	// A relay or mobile NAT forgets an idle UDP mapping within a minute or two.
	if ka, ok := keepAliveForScalarExport(model.Client{}); !ok || ka != model.LimitedKeepAlive {
		t.Fatalf("account without a limit or keepalive: %d %v", ka, ok)
	}
	off := model.KeepAliveValue("0")
	if ka, ok := keepAliveForScalarExport(model.Client{KeepAlive: &off}); ok || ka != 0 {
		t.Fatalf("account set to no keepalive: %d %v", ka, ok)
	}
}
