package job

import (
	"github.com/mhsanaei/3x-ui/v3/internal/l2tp"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// L2TPJob keeps the L2TP server in line with its inbound and stores what its
// accounts used.
type L2TPJob struct {
	inboundService service.InboundService
}

func NewL2TPJob() *L2TPJob {
	return new(L2TPJob)
}

func (j *L2TPJob) Run() {
	desired, err := j.inboundService.DesiredL2TPInstances()
	if err != nil {
		logger.Warning("l2tp job: get desired instances failed:", err)
		return
	}
	activeTags := make([]string, 0, len(desired))
	for _, inst := range desired {
		activeTags = append(activeTags, inst.Tag)
	}
	l2tp.GetManager().Reconcile(desired)
	online := j.storeTraffic()
	j.inboundService.RefreshLocalOnlineClients(online, activeTags)
}

// StoreStoppedTraffic saves what was used since the last run; call it before
// stopping the server, whose sessions end with it.
func (j *L2TPJob) StoreStoppedTraffic() {
	j.storeTraffic()
}

func (j *L2TPJob) storeTraffic() []string {
	deltas, online := l2tp.GetManager().CollectTraffic()
	rows := make([]engineTraffic, 0, len(deltas))
	for _, d := range deltas {
		rows = append(rows, engineTraffic(d))
	}
	storeEngineTraffic(&j.inboundService, "l2tp", rows, online)
	return online
}
