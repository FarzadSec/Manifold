package job

import (
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// ResellerJob keeps every reseller inside its allowance, once a minute.
type ResellerJob struct {
	resellerService service.ResellerService
	inboundService  service.InboundService
}

func NewResellerJob() *ResellerJob {
	return new(ResellerJob)
}

func (j *ResellerJob) Run() {
	if err := j.resellerService.Enforce(&j.inboundService, time.Now()); err != nil {
		logger.Warning("reseller allowance check:", err)
	}
}

// AuditPruneJob deletes audit entries past their retention, once an hour.
type AuditPruneJob struct {
	auditService service.AuditService
}

func NewAuditPruneJob() *AuditPruneJob {
	return new(AuditPruneJob)
}

func (j *AuditPruneJob) Run() {
	if err := j.auditService.Prune(time.Now()); err != nil {
		logger.Warning("audit log prune:", err)
	}
}

// SpeedLimitJob keeps the host's speed table in step with its limited users and sessions.
type SpeedLimitJob struct {
	speedService service.SpeedService
}

func NewSpeedLimitJob() *SpeedLimitJob {
	return new(SpeedLimitJob)
}

func (j *SpeedLimitJob) Run() {
	j.speedService.Sync()
}
