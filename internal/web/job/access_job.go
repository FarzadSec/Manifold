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

// UsagePruneJob drops traffic history past what is kept, once a day.
type UsagePruneJob struct {
	usageService service.UsageService
}

func NewUsagePruneJob() *UsagePruneJob {
	return new(UsagePruneJob)
}

func (j *UsagePruneJob) Run() {
	if err := j.usageService.Prune(time.Now()); err != nil {
		logger.Warning("usage history prune:", err)
	}
	if err := (&service.NodeService{}).PruneHealth(time.Now()); err != nil {
		logger.Warning("node health prune:", err)
	}
}

// NodeUsageLimitJob stops nodes over their period's traffic limit and starts them when it turns.
type NodeUsageLimitJob struct {
	nodeService service.NodeService
}

func NewNodeUsageLimitJob() *NodeUsageLimitJob {
	return new(NodeUsageLimitJob)
}

func (j *NodeUsageLimitJob) Run() {
	if err := j.nodeService.EnforceUsageLimits(time.Now()); err != nil {
		logger.Warning("node usage limits:", err)
	}
}

// NodeHealthJob records whether each node answered, and its load, every five minutes.
type NodeHealthJob struct {
	nodeService service.NodeService
}

func NewNodeHealthJob() *NodeHealthJob {
	return new(NodeHealthJob)
}

func (j *NodeHealthJob) Run() {
	if err := j.nodeService.RecordHealth(time.Now()); err != nil {
		logger.Warning("node health:", err)
	}
}
