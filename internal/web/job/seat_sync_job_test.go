package job

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/seats"
)

// fakeSeatNodes stands in for the nodes' /seats/sync: each answers its seats and records the plans it got.
type fakeSeatNodes struct {
	mu    sync.Mutex
	held  map[int][]seats.Seat
	down  map[int]bool
	plans map[int][]seats.Plan
}

func (f *fakeSeatNodes) sync(_ context.Context, n *model.Node, plan seats.Plan) ([]seats.Seat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down[n.Id] {
		return nil, errors.New("unreachable")
	}
	f.plans[n.Id] = append(f.plans[n.Id], plan)
	return f.held[n.Id], nil
}

func (f *fakeSeatNodes) lastPlan(id int) seats.Plan {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.plans[id]
	if len(p) == 0 {
		return seats.Plan{}
	}
	return p[len(p)-1]
}

func TestSeatSyncHoldsAnAccountToItsLimitAcrossServers(t *testing.T) {
	clock := time.Unix(1_800_000_000, 0)
	now := func() time.Time { return clock }
	ledger := seats.New(now)
	ledger.Take(seats.Hold{ID: "openvpn/1#1/7", Account: "alice", Protocol: "openvpn"}, 1, 0)

	nodes := &fakeSeatNodes{held: map[int][]seats.Seat{}, down: map[int]bool{}, plans: map[int][]seats.Plan{}}
	// The same moment on the node: alice got in there too, a second later.
	nodes.held[2] = []seats.Seat{{ID: "l2tp/9#2/s1", Account: "alice", Protocol: "l2tp", Limit: 1, Since: clock.Add(time.Second)}}
	j := &SeatSyncJob{
		ledger: ledger,
		now:    now,
		nodes: func() ([]*model.Node, error) {
			return []*model.Node{{Id: 2, Enable: true}, {Id: 3, Enable: false}}, nil
		},
		sync: nodes.sync,
	}

	j.Run()
	if got := ledger.Others("alice", "openvpn/1#1/7"); got != 0 {
		t.Fatalf("master counts %d of alice's seats elsewhere; the node's is the one to go", got)
	}
	if _, asked := nodes.plans[3]; asked {
		t.Fatal("a disabled node was asked for its seats")
	}

	j.Run()
	if got := nodes.lastPlan(2); !reflect.DeepEqual(got.Cut, []string{"l2tp/9#2/s1"}) || got.Elsewhere["alice"] != 1 {
		t.Fatalf("node's plan = %+v, want its newer seat cut and the master's counted", got)
	}

	// The node's seat went; bob now holds one there.
	nodes.held[2] = []seats.Seat{{ID: "wireguard/3#5/k", Account: "bob", Protocol: "wireguard", Limit: 2, Since: clock}}
	j.Run()
	if got := ledger.Others("bob", ""); got != 1 {
		t.Fatalf("master counts %d of bob's seats, want the node's", got)
	}

	// The node stops answering: past 30 s its last word no longer counts.
	nodes.down[2] = true
	clock = clock.Add(31 * time.Second)
	j.Run()
	if got := ledger.Others("bob", ""); got != 0 {
		t.Fatalf("master still counts %d seats from a node silent for 31 s", got)
	}
}
