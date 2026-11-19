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
	if ka, ok := keepAliveForScalarExport(model.Client{}); ok || ka != 0 {
		t.Fatalf("account without a limit or keepalive: %d %v", ka, ok)
	}
}
