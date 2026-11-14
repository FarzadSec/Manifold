package job

import (
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/openvpn"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

// OpenVPNJob keeps the OpenVPN servers in line with the inbounds and stores
// what their accounts used.
type OpenVPNJob struct {
	inboundService service.InboundService
}

func NewOpenVPNJob() *OpenVPNJob {
	return new(OpenVPNJob)
}

func (j *OpenVPNJob) Run() {
	desired, err := j.inboundService.DesiredOpenVPNInstances()
	if err != nil {
		logger.Warning("openvpn job: get desired instances failed:", err)
		return
	}
	activeTags := make([]string, 0, len(desired))
	for _, inst := range desired {
		activeTags = append(activeTags, inst.Tag)
	}
	openvpn.GetManager().Reconcile(desired)
	online := j.storeTraffic()
	j.inboundService.RefreshLocalOnlineClients(online, activeTags)
}

// StoreStoppedTraffic saves what was used since the last run; call it before
// stopping the servers, whose counters are lost with them.
func (j *OpenVPNJob) StoreStoppedTraffic() {
	j.storeTraffic()
}

func (j *OpenVPNJob) storeTraffic() []string {
	deltas, online := openvpn.GetManager().CollectTraffic()
	clientTraffics := make([]*xray.ClientTraffic, 0, len(deltas))
	inboundUp := make(map[string]int64)
	inboundDown := make(map[string]int64)
	for _, d := range deltas {
		clientTraffics = append(clientTraffics, &xray.ClientTraffic{Email: d.Email, Up: d.Up, Down: d.Down})
		inboundUp[d.Tag] += d.Up
		inboundDown[d.Tag] += d.Down
	}
	traffics := make([]*xray.Traffic, 0, len(inboundUp))
	for tag, up := range inboundUp {
		traffics = append(traffics, &xray.Traffic{IsInbound: true, Tag: tag, Up: up, Down: inboundDown[tag]})
	}
	if len(traffics) > 0 || len(clientTraffics) > 0 {
		if _, _, err := j.inboundService.AddTraffic(traffics, clientTraffics); err != nil {
			logger.Warning("openvpn job: add traffic failed:", err)
		}
	}
	if len(online) > 0 {
		if err := j.inboundService.BumpClientsLastOnline(online); err != nil {
			logger.Warning("openvpn job: mark online clients failed:", err)
		}
	}
	return online
}
