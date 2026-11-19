package job

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/seats"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

const (
	seatSyncTimeout = 4 * time.Second
	// A node's seats, and the counts it is given, count this long after it last answered.
	seatTrust = 30 * time.Second
)

type heldAt struct {
	held []seats.Seat
	at   time.Time
}

// SeatSyncJob holds each account to its user limit across this panel and its nodes: it gathers
// every server's seats, keeps an account's oldest, and tells each server what the others hold.
type SeatSyncJob struct {
	running sync.Mutex
	ledger  *seats.Ledger
	now     func() time.Time
	nodes   func() ([]*model.Node, error)
	sync    func(ctx context.Context, n *model.Node, plan seats.Plan) ([]seats.Seat, error)
	// plans go to each node on the next run; last is what each node held when it last answered.
	plans map[int]seats.Plan
	last  map[int]heldAt
}

func NewSeatSyncJob() *SeatSyncJob {
	return &SeatSyncJob{
		ledger: seats.Local(),
		now:    time.Now,
		nodes:  (&service.NodeService{}).GetAll,
		sync: func(ctx context.Context, n *model.Node, plan seats.Plan) ([]seats.Seat, error) {
			remote, err := runtime.GetManager().RemoteFor(n)
			if err != nil {
				return nil, err
			}
			return remote.SyncSeats(ctx, plan)
		},
	}
}

func (j *SeatSyncJob) Run() {
	if !j.running.TryLock() {
		return
	}
	defer j.running.Unlock()
	if j.plans == nil {
		j.plans, j.last = map[int]seats.Plan{}, map[int]heldAt{}
	}
	all, err := j.nodes()
	if err != nil {
		logger.Warning("seat sync: load nodes failed:", err)
		return
	}
	var enabled []*model.Node
	for _, n := range all {
		if n.Enable {
			enabled = append(enabled, n)
		}
	}
	if len(enabled) == 0 {
		return
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, n := range enabled {
		wg.Add(1)
		go func(n *model.Node, plan seats.Plan) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), seatSyncTimeout)
			defer cancel()
			held, err := j.sync(ctx, n, plan)
			if err != nil {
				logger.Debugf("seat sync: node %d: %v", n.Id, err)
				return
			}
			mu.Lock()
			j.last[n.Id] = heldAt{held: held, at: j.now()}
			mu.Unlock()
		}(n, j.plans[n.Id])
	}
	wg.Wait()

	now := j.now()
	servers := map[string][]seats.Seat{"": j.ledger.All()}
	for _, n := range enabled {
		// A node whose seats are not known still hears what the other servers hold.
		servers[nodeServer(n.Id)] = nil
		if seen, ok := j.last[n.Id]; ok && now.Sub(seen.at) <= seatTrust {
			servers[nodeServer(n.Id)] = seen.held
		}
	}
	plans := seats.Coordinate(servers)
	own := plans[""]
	j.ledger.SetElsewhere(own.Elsewhere, now.Add(seatTrust))
	j.ledger.Cut(own.Cut)
	next := make(map[int]seats.Plan, len(enabled))
	for _, n := range enabled {
		next[n.Id] = plans[nodeServer(n.Id)]
	}
	j.plans = next
}

func nodeServer(id int) string { return "node:" + strconv.Itoa(id) }
