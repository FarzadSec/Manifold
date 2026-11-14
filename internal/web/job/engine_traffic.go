package job

import (
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

// engineTraffic is what one account used on one inbound of an engine that
// runs outside Xray, since that engine was last asked.
type engineTraffic struct {
	Tag   string
	Email string
	Up    int64
	Down  int64
}

// storeEngineTraffic adds an engine's traffic to the accounts and inbounds it
// belongs to, and marks the connected accounts as seen.
func storeEngineTraffic(inboundService *service.InboundService, engine string, rows []engineTraffic, online []string) {
	clientTraffics := make([]*xray.ClientTraffic, 0, len(rows))
	inboundUp := make(map[string]int64)
	inboundDown := make(map[string]int64)
	for _, row := range rows {
		clientTraffics = append(clientTraffics, &xray.ClientTraffic{Email: row.Email, Up: row.Up, Down: row.Down})
		inboundUp[row.Tag] += row.Up
		inboundDown[row.Tag] += row.Down
	}
	traffics := make([]*xray.Traffic, 0, len(inboundUp))
	for tag, up := range inboundUp {
		traffics = append(traffics, &xray.Traffic{IsInbound: true, Tag: tag, Up: up, Down: inboundDown[tag]})
	}
	if len(traffics) > 0 || len(clientTraffics) > 0 {
		if _, _, err := inboundService.AddTraffic(traffics, clientTraffics); err != nil {
			logger.Warningf("%s job: add traffic failed: %v", engine, err)
		}
	}
	if len(online) > 0 {
		if err := inboundService.BumpClientsLastOnline(online); err != nil {
			logger.Warningf("%s job: mark online clients failed: %v", engine, err)
		}
	}
}
