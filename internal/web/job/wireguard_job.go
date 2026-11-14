package job

import (
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/wgquick"
)

// WireGuardJob keeps the WireGuard interfaces in line with the inbounds and
// stores what their peers used.
type WireGuardJob struct {
	inboundService service.InboundService
}

func NewWireGuardJob() *WireGuardJob {
	return new(WireGuardJob)
}

func (j *WireGuardJob) Run() {
	desired, err := j.inboundService.DesiredWireGuardInstances()
	if err != nil {
		logger.Warning("wireguard job: get desired instances failed:", err)
		return
	}
	activeTags := make([]string, 0, len(desired))
	for _, inst := range desired {
		activeTags = append(activeTags, inst.Tag)
	}
	wgquick.GetManager().Reconcile(desired)
	online := j.storeTraffic()
	j.inboundService.RefreshLocalOnlineClients(online, activeTags)
}

// StoreStoppedTraffic saves what stopped interfaces still owe their accounts;
// the manager reads an interface's counters before it brings it down.
func (j *WireGuardJob) StoreStoppedTraffic() {
	j.storeTraffic()
}

func (j *WireGuardJob) storeTraffic() []string {
	deltas, online := wgquick.GetManager().CollectTraffic()
	rows := make([]engineTraffic, 0, len(deltas))
	for _, d := range deltas {
		rows = append(rows, engineTraffic(d))
	}
	storeEngineTraffic(&j.inboundService, "wireguard", rows, online)
	return online
}
