package job

import "github.com/mhsanaei/3x-ui/v3/internal/web/service"

// NativeRoutingJob keeps the tunnel traffic that routing rules name steered into the running core.
type NativeRoutingJob struct {
	xrayService service.XrayService
}

func NewNativeRoutingJob() *NativeRoutingJob {
	return new(NativeRoutingJob)
}

func (j *NativeRoutingJob) Run() {
	j.xrayService.SyncNativeRouting()
}
