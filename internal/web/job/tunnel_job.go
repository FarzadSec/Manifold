package job

import (
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// TunnelJob keeps the tunnels of this server running as stored, and puts back
// a link or a process that went away.
type TunnelJob struct {
	tunnelService service.TunnelService
}

func NewTunnelJob() *TunnelJob {
	return new(TunnelJob)
}

func (j *TunnelJob) Run() {
	j.tunnelService.Reconcile()
}
