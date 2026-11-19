package sub

import (
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func keepAliveForScalarExport(client model.Client) (int, bool) {
	if client.KeepAlive != nil && strings.Contains(client.KeepAliveText(), "-") && client.KeepAlive.Validate(true) == nil {
		return client.KeepAlive.MinSeconds(), true
	}
	value := client.ExportKeepAliveSeconds()
	return value, value > 0
}
