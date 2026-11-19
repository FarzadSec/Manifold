package wgquick

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/seats"
)

type peerState struct {
	key       string
	handshake time.Time
	rx        int64
}

// dumpOf is what `wg show <iface> dump` prints for these peers.
func dumpOf(peers ...peerState) string {
	lines := []string{"private\tpublic\t51820\toff"}
	for _, p := range peers {
		hs := int64(0)
		if !p.handshake.IsZero() {
			hs = p.handshake.Unix()
		}
		lines = append(lines, fmt.Sprintf("%s\t(none)\t203.0.113.5:4242\t10.0.0.2/32\t%d\t%d\t0\t25", p.key, hs, p.rx))
	}
	return strings.Join(lines, "\n")
}

type seatClock struct{ at time.Time }

func (c *seatClock) now() time.Time { return c.at }

func seatManager(t *testing.T) (*Manager, *fakeSystem, *seatClock) {
	t.Helper()
	m, sys, _ := testManager()
	clock := &seatClock{at: time.Unix(1_800_000_000, 0)}
	m.now = clock.now
	m.seats = seats.New(clock.now)
	return m, sys, clock
}

func limited(p Peer, limit int) Peer {
	p.Limit = limit
	return p
}

func onInterface(iface string, inst Instance) Instance {
	inst.Interface = iface
	return inst
}

func removed(iface, key string) string { return "wg set " + iface + " peer " + key + " remove" }

func added(iface, key, ips string) string {
	return "wg set " + iface + " peer " + key + " allowed-ips " + ips
}

func has(commands []string, want string) bool { return slices.Contains(commands, want) }

func TestPeersOfAnAccountConnectedElsewhereAreHeldOff(t *testing.T) {
	m, sys, _ := seatManager(t)
	if err := m.Ensure(testInstance(limited(peer("alice", aliceKey, "10.0.0.2/32"), 1), peer("bob", bobKey, "10.0.0.3/32"))); err != nil {
		t.Fatal(err)
	}
	openvpn := seats.Hold{ID: "openvpn/1#1/7", Account: "alice", Protocol: "openvpn"}
	m.seats.Take(openvpn, 1, 0)
	sys.taken()

	m.WatchSeats()
	got := sys.taken()
	if !has(got, removed("mfwg3", aliceKey)) || has(got, removed("mfwg3", bobKey)) {
		t.Fatalf("commands %q: want alice's peer, and only hers, taken off", got)
	}
	// Reapplying the inbound must not bring her back while the seat is taken.
	if err := m.Ensure(testInstance(limited(peer("alice", aliceKey, "10.0.0.2/32"), 1), peer("bob", bobKey, "10.0.0.3/32"))); err != nil {
		t.Fatal(err)
	}
	if got := sys.taken(); has(got, added("mfwg3", aliceKey, "10.0.0.2/32")) {
		t.Fatalf("reapplying put the held-off peer back: %q", got)
	}

	m.seats.Release(openvpn.ID)
	m.WatchSeats()
	if got := sys.taken(); !has(got, added("mfwg3", aliceKey, "10.0.0.2/32")) {
		t.Fatalf("commands %q: want alice's peer back once the seat is free", got)
	}
}

func TestConnectedPeerHoldsTheSeatUntilItFallsQuiet(t *testing.T) {
	m, sys, clock := seatManager(t)
	if err := m.Ensure(testInstance(limited(peer("alice", aliceKey, "10.0.0.2/32"), 1))); err != nil {
		t.Fatal(err)
	}
	sys.dumps["mfwg3"] = dumpOf(peerState{key: aliceKey, handshake: clock.at, rx: 1200})
	m.WatchSeats()
	held := m.seats.Seats("alice")
	if len(held) != 1 || held[0].Protocol != "wireguard" || held[0].Inbound != 3 || held[0].Address != "10.0.0.2" {
		t.Fatalf("seats = %+v, want alice's WireGuard peer", held)
	}
	if m.seats.Take(seats.Hold{ID: "l2tp/9#1/s1", Account: "alice"}, 1, 0) {
		t.Fatal("L2TP was let in beside the connected WireGuard peer")
	}

	// Keepalives keep the counter moving; then the device goes away.
	clock.at = clock.at.Add(25 * time.Second)
	sys.dumps["mfwg3"] = dumpOf(peerState{key: aliceKey, handshake: clock.at.Add(-25 * time.Second), rx: 1232})
	m.WatchSeats()
	clock.at = clock.at.Add(seatIdle - time.Second)
	m.WatchSeats()
	if len(m.seats.Seats("alice")) != 1 {
		t.Fatal("the seat was given back while the peer was still within its idle window")
	}
	clock.at = clock.at.Add(2 * time.Second)
	m.WatchSeats()
	if held := m.seats.Seats("alice"); len(held) != 0 {
		t.Fatalf("seats = %+v after the peer fell quiet", held)
	}
}

func TestOneLocationInUseHoldsTheAccountsOtherLocationsOff(t *testing.T) {
	m, sys, clock := seatManager(t)
	alice := limited(peer("alice", aliceKey, "10.0.0.2/32"), 1)
	germany := testInstance(alice)
	netherlands := onInterface("mfwg4", testInstance(limited(peer("alice", aliceKey, "10.0.1.2/32"), 1)))
	netherlands.Id, netherlands.ListenPort = 4, 51821
	for _, inst := range []Instance{germany, netherlands} {
		if err := m.Ensure(inst); err != nil {
			t.Fatal(err)
		}
	}
	sys.taken()
	sys.dumps["mfwg3"] = dumpOf(peerState{key: aliceKey, handshake: clock.at, rx: 500})

	m.WatchSeats()
	got := sys.taken()
	if !has(got, removed("mfwg4", aliceKey)) || has(got, removed("mfwg3", aliceKey)) {
		t.Fatalf("commands %q: want the account's other location held off, not the one in use", got)
	}
}

func TestPeerThatGotInBesideAnotherProtocolIsCut(t *testing.T) {
	m, sys, clock := seatManager(t)
	if err := m.Ensure(testInstance(limited(peer("alice", aliceKey, "10.0.0.2/32"), 1))); err != nil {
		t.Fatal(err)
	}
	// OpenVPN logged in on another server between two readings, so the peer was still on.
	m.seats.Take(seats.Hold{ID: "openvpn/1#1/7", Account: "alice", Protocol: "openvpn"}, 1, 0)
	sys.dumps["mfwg3"] = dumpOf(peerState{key: aliceKey, handshake: clock.at, rx: 900})
	sys.taken()

	m.WatchSeats()
	if got := sys.taken(); !has(got, removed("mfwg3", aliceKey)) {
		t.Fatalf("commands %q: want the late WireGuard peer cut", got)
	}
	if held := m.seats.Seats("alice"); len(held) != 1 || held[0].Protocol != "openvpn" {
		t.Fatalf("seats = %+v, want the OpenVPN session to keep the seat", held)
	}
}

func TestLoweredLimitCutsTheNewerWireGuardLocation(t *testing.T) {
	m, sys, clock := seatManager(t)
	germany := testInstance(limited(peer("alice", aliceKey, "10.0.0.2/32"), 2))
	netherlands := onInterface("mfwg4", testInstance(limited(peer("alice", aliceKey, "10.0.1.2/32"), 2)))
	netherlands.Id, netherlands.ListenPort = 4, 51821
	for _, inst := range []Instance{germany, netherlands} {
		if err := m.Ensure(inst); err != nil {
			t.Fatal(err)
		}
	}
	sys.dumps["mfwg3"] = dumpOf(peerState{key: aliceKey, handshake: clock.at, rx: 500})
	m.WatchSeats()
	clock.at = clock.at.Add(time.Second)
	sys.dumps["mfwg3"] = dumpOf(peerState{key: aliceKey, handshake: clock.at, rx: 600})
	sys.dumps["mfwg4"] = dumpOf(peerState{key: aliceKey, handshake: clock.at, rx: 500})
	m.WatchSeats()
	if got := len(m.seats.Seats("alice")); got != 2 {
		t.Fatalf("%d seats, want both locations under a limit of 2", got)
	}

	germany.Peers[0].Limit, netherlands.Peers[0].Limit = 1, 1
	for _, inst := range []Instance{germany, netherlands} {
		if err := m.Ensure(inst); err != nil {
			t.Fatal(err)
		}
	}
	sys.taken()
	clock.at = clock.at.Add(time.Second)
	sys.dumps["mfwg3"] = dumpOf(peerState{key: aliceKey, handshake: clock.at, rx: 700})
	sys.dumps["mfwg4"] = dumpOf(peerState{key: aliceKey, handshake: clock.at, rx: 600})
	m.WatchSeats()
	got := sys.taken()
	if !has(got, removed("mfwg4", aliceKey)) || has(got, removed("mfwg3", aliceKey)) {
		t.Fatalf("commands %q: want the location connected second cut", got)
	}
}

func TestStoppedInterfaceGivesItsSeatsBack(t *testing.T) {
	m, sys, clock := seatManager(t)
	if err := m.Ensure(testInstance(limited(peer("alice", aliceKey, "10.0.0.2/32"), 1))); err != nil {
		t.Fatal(err)
	}
	sys.dumps["mfwg3"] = dumpOf(peerState{key: aliceKey, handshake: clock.at, rx: 500})
	m.WatchSeats()
	if held := m.seats.Seats("alice"); len(held) != 1 {
		t.Fatalf("seats = %+v, want the connected peer's before the interface goes", held)
	}
	m.Remove(3)
	if held := m.seats.Seats("alice"); len(held) != 0 {
		t.Fatalf("seats = %+v after the interface went down", held)
	}
}

func TestPeerThatSaidItLeftDoesNotTakeItsSeatBackUntilItIsHeardAgain(t *testing.T) {
	m, sys, clock := seatManager(t)
	if err := m.Ensure(testInstance(limited(peer("alice", aliceKey, "10.0.0.2/32"), 1))); err != nil {
		t.Fatal(err)
	}
	sys.dumps["mfwg3"] = dumpOf(peerState{key: aliceKey, handshake: clock.at, rx: 1200})
	m.WatchSeats()
	held := m.seats.Seats("alice")
	if len(held) != 1 {
		t.Fatalf("seats = %+v", held)
	}

	// DT Connect said it left; the counter has not moved since.
	m.seats.Cut([]string{held[0].ID})
	clock.at = clock.at.Add(2 * time.Second)
	m.WatchSeats()
	if got := m.seats.Seats("alice"); len(got) != 0 {
		t.Fatalf("seats = %+v: the departed peer took its seat back on old traffic", got)
	}
	if !m.seats.Take(seats.Hold{ID: "openvpn/1#1/7", Account: "alice"}, 1, 0) {
		t.Fatal("OpenVPN was refused right after the WireGuard device left")
	}
	m.seats.Release("openvpn/1#1/7")

	sys.dumps["mfwg3"] = dumpOf(peerState{key: aliceKey, handshake: clock.at, rx: 1300})
	m.WatchSeats()
	if got := m.seats.Seats("alice"); len(got) != 1 {
		t.Fatalf("seats = %+v: the peer is back but holds no seat", got)
	}
}
