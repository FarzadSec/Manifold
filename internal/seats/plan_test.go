package seats

import (
	"reflect"
	"testing"
	"time"
)

func seatAt(id, account string, limit, second int) Seat {
	return Seat{ID: id, Account: account, Limit: limit, Since: time.Unix(1_800_000_000+int64(second), 0)}
}

func TestCoordinateKeepsTheOldestSeatsAcrossServers(t *testing.T) {
	servers := map[string][]Seat{
		"":       {seatAt("openvpn/1#1/7", "alice", 1, 0), seatAt("openvpn/1#1/8", "bob", 2, 0), seatAt("x", "carol", 0, 0)},
		"node:1": {seatAt("l2tp/9#2/s1", "alice", 1, 1), seatAt("y", "carol", 0, 1)},
		"node:2": {seatAt("wireguard/3#4/k", "bob", 2, 2)},
	}
	got := Coordinate(servers)
	want := map[string]Plan{
		"":       {Elsewhere: map[string]int{"bob": 1}},
		"node:1": {Elsewhere: map[string]int{"alice": 1, "bob": 2}, Cut: []string{"l2tp/9#2/s1"}},
		"node:2": {Elsewhere: map[string]int{"alice": 1, "bob": 1}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("plans = %+v\nwant    %+v", got, want)
	}
}

func TestCoordinateHoldsAnAccountToItsLowestLimit(t *testing.T) {
	// A server that has not yet applied a lowered limit still reports the old one.
	got := Coordinate(map[string][]Seat{
		"":       {seatAt("a", "alice", 2, 0)},
		"node:1": {seatAt("b", "alice", 1, 1)},
	})
	if cut := got["node:1"].Cut; !reflect.DeepEqual(cut, []string{"b"}) {
		t.Fatalf("cut %v, want the newer seat under the lower limit", cut)
	}
}
