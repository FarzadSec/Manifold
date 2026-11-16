package job

import (
	"context"

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

// ServeRequests applies a saved change at once rather than at the next run, until ctx ends.
func (j *TunnelJob) ServeRequests(ctx context.Context) {
	j.tunnelService.ServeReconcileRequests(ctx)
}
