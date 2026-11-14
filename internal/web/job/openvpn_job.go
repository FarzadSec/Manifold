package job

import (
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/openvpn"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
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
	rows := make([]engineTraffic, 0, len(deltas))
	for _, d := range deltas {
		rows = append(rows, engineTraffic(d))
	}
	storeEngineTraffic(&j.inboundService, "openvpn", rows, online)
	return online
}
