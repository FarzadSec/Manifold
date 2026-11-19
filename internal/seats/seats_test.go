package seats

import (
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"
)

type clock struct {
	mu sync.Mutex
	at time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

func newTestLedger() (*Ledger, *clock) {
	c := &clock{at: time.Unix(1_800_000_000, 0)}
	return New(c.now), c
}

// kicks records which seats the ledger asked their engine to cut.
type kicks struct {
	mu  sync.Mutex
	ids []string
}

func (k *kicks) hold(id, account, protocol string) Hold {
	return Hold{ID: id, Account: account, Protocol: protocol, Kick: func() {
		k.mu.Lock()
		defer k.mu.Unlock()
		k.ids = append(k.ids, id)
	}}
}

func (k *kicks) cut() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return slices.Clone(k.ids)
}

func ids(seats []Seat) []string {
	out := make([]string, 0, len(seats))
	for _, s := range seats {
		out = append(out, s.ID)
	}
	return out
}

func TestOneUserAccountIsRefusedOnEveryOtherProtocolAndInbound(t *testing.T) {
	l, _ := newTestLedger()
	if !l.Take(Hold{ID: "openvpn/1/7", Account: "alice", Protocol: "openvpn", Inbound: 1}, 1, 0) {
		t.Fatal("the first connection was refused")
	}
	for _, h := range []Hold{
		{ID: "l2tp/9/s4101", Account: "alice", Protocol: "l2tp", Inbound: 9},
		{ID: "wireguard/3/key", Account: "alice", Protocol: "wireguard", Inbound: 3},
		{ID: "openvpn/2/1", Account: "alice", Protocol: "openvpn", Inbound: 2},
	} {
		if l.Take(h, 1, 0) {
			t.Fatalf("%s was let in beside the account's OpenVPN connection", h.ID)
		}
	}
	if !l.Take(Hold{ID: "l2tp/9/s4102", Account: "bob", Protocol: "l2tp"}, 1, 0) {
		t.Fatal("another account was refused")
	}
	l.Release("openvpn/1/7")
	if !l.Take(Hold{ID: "l2tp/9/s4101", Account: "alice", Protocol: "l2tp"}, 1, 0) {
		t.Fatal("refused after the first connection ended")
	}
}

func TestLimitCountsEveryConnectionAndZeroMeansNone(t *testing.T) {
	l, _ := newTestLedger()
	for i, id := range []string{"a", "b"} {
		if !l.Take(Hold{ID: id, Account: "alice"}, 2, 0) {
			t.Fatalf("connection %d of 2 was refused", i+1)
		}
	}
	if l.Take(Hold{ID: "c", Account: "alice"}, 2, 0) {
		t.Fatal("a third connection was let in under a limit of 2")
	}
	for _, id := range []string{"x", "y", "z"} {
		if !l.Take(Hold{ID: id, Account: "carol"}, 0, 0) {
			t.Fatal("an account without a limit was refused")
		}
	}
}

func TestTakingTheSameSeatAgainDoesNotCountAgainstItself(t *testing.T) {
	l, _ := newTestLedger()
	l.Take(Hold{ID: "openvpn/1/7", Account: "alice"}, 1, 0)
	if !l.Take(Hold{ID: "openvpn/1/7", Account: "alice"}, 1, 0) {
		t.Fatal("a seat already held was refused to its own holder")
	}
}

func TestSeatTakenUnderAnotherAccountLeavesTheFirstOne(t *testing.T) {
	l, _ := newTestLedger()
	l.Take(Hold{ID: "openvpn/1/7", Account: "alice"}, 1, 0)
	l.Take(Hold{ID: "openvpn/1/7", Account: "bob"}, 1, 0)
	if !l.Take(Hold{ID: "l2tp/9/s1", Account: "alice"}, 1, 0) {
		t.Fatal("alice still holds a seat that bob's login took over")
	}
}

func TestUnconfirmedSeatLapses(t *testing.T) {
	l, c := newTestLedger()
	l.Take(Hold{ID: "login", Account: "alice"}, 1, 30*time.Second)
	if l.Take(Hold{ID: "other", Account: "alice"}, 1, 0) {
		t.Fatal("a login on its way did not hold its seat")
	}
	c.add(31 * time.Second)
	if !l.Take(Hold{ID: "other", Account: "alice"}, 1, 0) {
		t.Fatal("a login that never finished kept its seat")
	}
	if got := ids(l.Seats("alice")); !reflect.DeepEqual(got, []string{"other"}) {
		t.Fatalf("seats = %v, want only the new one", got)
	}
}

func TestConfirmedSeatStays(t *testing.T) {
	l, c := newTestLedger()
	l.Take(Hold{ID: "s", Account: "alice"}, 1, 30*time.Second)
	l.Confirm("s", "10.8.0.6")
	c.add(time.Hour)
	if l.Take(Hold{ID: "other", Account: "alice"}, 1, 0) {
		t.Fatal("a confirmed seat lapsed")
	}
	got := l.Seats("alice")
	if len(got) != 1 || got[0].Address != "10.8.0.6" || got[0].Pending {
		t.Fatalf("seats = %+v, want the confirmed seat with its address", got)
	}
}

func TestMoveKeepsTheSeatUnderItsNewName(t *testing.T) {
	l, c := newTestLedger()
	l.Take(Hold{ID: "l2tp/1/p1", Account: "alice", Protocol: "l2tp"}, 1, 30*time.Second)
	if !l.Move("l2tp/1/p1", Hold{ID: "l2tp/1/s4101", Account: "alice", Protocol: "l2tp"}, "10.9.0.2") {
		t.Fatal("the login's seat could not become the session's")
	}
	c.add(time.Minute)
	if got := ids(l.Seats("alice")); !reflect.DeepEqual(got, []string{"l2tp/1/s4101"}) {
		t.Fatalf("seats = %v", got)
	}
	if l.Move("l2tp/1/p1", Hold{ID: "l2tp/1/s4102", Account: "alice"}, "") {
		t.Fatal("a seat that is gone was moved")
	}
	c.add(time.Minute)
	l.Take(Hold{ID: "l2tp/1/p2", Account: "alice"}, 0, time.Second)
	c.add(2 * time.Second)
	if l.Move("l2tp/1/p2", Hold{ID: "l2tp/1/s4103", Account: "alice"}, "") {
		t.Fatal("a lapsed login seat was moved")
	}
}

func TestReleaseAllFreesOneServersSeatsOnly(t *testing.T) {
	l, _ := newTestLedger()
	l.Take(Hold{ID: "openvpn/1#4/1", Account: "alice"}, 0, 0)
	l.Take(Hold{ID: "openvpn/1#4/2", Account: "bob"}, 0, 0)
	l.Take(Hold{ID: "openvpn/1#5/1", Account: "alice"}, 0, 0)
	l.ReleaseAll("openvpn/1#4/")
	if got := ids(l.Seats("alice")); !reflect.DeepEqual(got, []string{"openvpn/1#5/1"}) {
		t.Fatalf("alice keeps %v", got)
	}
	if got := l.Seats("bob"); len(got) != 0 {
		t.Fatalf("bob keeps %v", ids(got))
	}
}

func TestLoweredLimitCutsTheNewestConnections(t *testing.T) {
	l, c := newTestLedger()
	k := &kicks{}
	for _, id := range []string{"oldest", "middle", "newest"} {
		l.Take(k.hold(id, "alice", "openvpn"), 3, 0)
		c.add(time.Second)
	}
	l.Take(k.hold("bob's", "bob", "openvpn"), 3, 0)

	l.Limit("alice", 1)
	if got := k.cut(); !reflect.DeepEqual(got, []string{"newest", "middle"}) {
		t.Fatalf("cut %v, want the two newest of alice's", got)
	}
	if got := ids(l.Seats("alice")); !reflect.DeepEqual(got, []string{"oldest"}) {
		t.Fatalf("alice keeps %v", got)
	}
	l.Limit("alice", 0)
	l.Limit("bob", 1)
	if got := k.cut(); len(got) != 2 {
		t.Fatalf("cut %v after limits that leave everyone in", got)
	}
}

func TestSeatsAreListedOldestFirst(t *testing.T) {
	l, c := newTestLedger()
	l.Take(Hold{ID: "b", Account: "alice", Protocol: "l2tp", Inbound: 9}, 0, 0)
	c.add(time.Second)
	l.Take(Hold{ID: "a", Account: "alice", Protocol: "openvpn", Inbound: 1}, 0, 0)
	got := l.Seats("alice")
	want := []Seat{
		{ID: "b", Account: "alice", Protocol: "l2tp", Inbound: 9, Since: time.Unix(1_800_000_000, 0)},
		{ID: "a", Account: "alice", Protocol: "openvpn", Inbound: 1, Since: time.Unix(1_800_000_001, 0)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("seats = %+v\nwant    %+v", got, want)
	}
}

func TestOwnersNeverRepeat(t *testing.T) {
	a, b := NewOwner("openvpn", 1), NewOwner("openvpn", 1)
	if a == b {
		t.Fatalf("two servers of one inbound share the prefix %q", a)
	}
}

func TestOthersCountsTheAccountsSeatsBesidesOne(t *testing.T) {
	l, c := newTestLedger()
	l.Take(Hold{ID: "wireguard/3#1/key", Account: "alice"}, 0, 0)
	l.Take(Hold{ID: "openvpn/1#2/7", Account: "alice"}, 0, 30*time.Second)
	if got := l.Others("alice", "wireguard/3#1/key"); got != 1 {
		t.Fatalf("others = %d, want the OpenVPN login", got)
	}
	if !l.Holds("wireguard/3#1/key") || l.Holds("wireguard/4#3/key") {
		t.Fatal("Holds does not tell held seats from others")
	}
	c.add(time.Minute)
	if got := l.Others("alice", "wireguard/3#1/key"); got != 0 {
		t.Fatalf("others = %d after the login lapsed", got)
	}
	if got := l.Others("nobody", ""); got != 0 {
		t.Fatalf("others of an account without seats = %d", got)
	}
}
