// Package seats counts each account's connections on this server across OpenVPN, L2TP and
// WireGuard, so one user limit holds over every protocol and inbound at once.
package seats

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Seat is one connection an account holds, as the panel lists it.
type Seat struct {
	ID       string    `json:"id"`
	Account  string    `json:"account"`
	Protocol string    `json:"protocol"`
	Inbound  int       `json:"inbound"`
	Address  string    `json:"address,omitempty"`
	Since    time.Time `json:"since"`
	// Pending is a login still on its way to a session; it lapses unless confirmed.
	Pending bool `json:"pending,omitempty"`
}

// Hold is what an engine asks a seat for. Kick cuts the connection; nil when it cannot be cut.
type Hold struct {
	ID       string
	Account  string
	Protocol string
	Inbound  int
	Kick     func()
}

type entry struct {
	seat  Seat
	lapse time.Time
	kick  func()
}

// Ledger is first come, first served: a connection is refused while the account's
// others fill its limit, and a lowered limit cuts the newest.
type Ledger struct {
	mu        sync.Mutex
	now       func() time.Time
	byID      map[string]*entry
	byAccount map[string]map[string]*entry
}

func New(now func() time.Time) *Ledger {
	return &Ledger{now: now, byID: map[string]*entry{}, byAccount: map[string]map[string]*entry{}}
}

func (l *Ledger) putLocked(e *entry) {
	l.byID[e.seat.ID] = e
	if l.byAccount[e.seat.Account] == nil {
		l.byAccount[e.seat.Account] = map[string]*entry{}
	}
	l.byAccount[e.seat.Account][e.seat.ID] = e
}

func (l *Ledger) dropLocked(id string) *entry {
	e := l.byID[id]
	if e == nil {
		return nil
	}
	delete(l.byID, id)
	if held := l.byAccount[e.seat.Account]; held != nil {
		delete(held, id)
		if len(held) == 0 {
			delete(l.byAccount, e.seat.Account)
		}
	}
	return e
}

func (e *entry) lapsed(now time.Time) bool { return !e.lapse.IsZero() && !now.Before(e.lapse) }

var local = New(time.Now)

// Local is the ledger every engine of this panel shares.
func Local() *Ledger { return local }

var owners atomic.Uint64

// NewOwner returns a seat id prefix that no other server, also of the same inbound, ever had.
func NewOwner(protocol string, inbound int) string {
	return fmt.Sprintf("%s/%d#%d/", protocol, inbound, owners.Add(1))
}

// liveLocked returns the account's seats, dropping logins that never finished.
func (l *Ledger) liveLocked(account string) []*entry {
	now := l.now()
	var out []*entry
	for id, e := range l.byAccount[account] {
		if e.lapsed(now) {
			l.dropLocked(id)
			continue
		}
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b *entry) int {
		if c := a.seat.Since.Compare(b.seat.Since); c != 0 {
			return c
		}
		return strings.Compare(a.seat.ID, b.seat.ID)
	})
	return out
}

// Take gives h a seat unless the account's other seats already fill limit; 0 is no limit.
// With pending above zero the seat lapses unless Confirm or Move keeps it within that time.
func (l *Ledger) Take(h Hold, limit int, pending time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e := l.byID[h.ID]; e != nil && e.seat.Account != h.Account {
		l.dropLocked(h.ID)
	}
	others := 0
	for _, e := range l.liveLocked(h.Account) {
		if e.seat.ID == h.ID {
			return true
		}
		others++
	}
	if limit > 0 && others >= limit {
		return false
	}
	e := &entry{seat: Seat{ID: h.ID, Account: h.Account, Protocol: h.Protocol, Inbound: h.Inbound, Since: l.now()}, kick: h.Kick}
	if pending > 0 {
		e.lapse = e.seat.Since.Add(pending)
		e.seat.Pending = true
	}
	l.putLocked(e)
	return true
}

// Confirm keeps a pending seat for good, with the tunnel address its session was given.
func (l *Ledger) Confirm(id, address string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e := l.byID[id]; e != nil {
		e.lapse, e.seat.Pending = time.Time{}, false
		if address != "" {
			e.seat.Address = address
		}
	}
}

// Move hands a live seat to h under its new id, confirmed; false when the seat is gone.
func (l *Ledger) Move(from string, h Hold, address string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.dropLocked(from)
	if e == nil || e.lapsed(l.now()) {
		return false
	}
	e.seat.ID, e.seat.Protocol, e.seat.Inbound, e.seat.Address = h.ID, h.Protocol, h.Inbound, address
	e.lapse, e.seat.Pending, e.kick = time.Time{}, false, h.Kick
	l.putLocked(e)
	return true
}

func (l *Ledger) Release(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.dropLocked(id)
}

// ReleaseAll frees every seat under prefix, for a server whose connections all ended with it.
func (l *Ledger) ReleaseAll(prefix string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for id := range l.byID {
		if strings.HasPrefix(id, prefix) {
			l.dropLocked(id)
		}
	}
}

// Limit applies an account's limit to the seats it holds now: the newest beyond it are cut.
func (l *Ledger) Limit(account string, limit int) {
	if limit <= 0 {
		return
	}
	l.mu.Lock()
	live := l.liveLocked(account)
	var cut []func()
	for i := len(live) - 1; i >= limit; i-- {
		l.dropLocked(live[i].seat.ID)
		if live[i].kick != nil {
			cut = append(cut, live[i].kick)
		}
	}
	l.mu.Unlock()
	for _, kick := range cut {
		kick()
	}
}

// Seats returns the account's seats, oldest first.
func (l *Ledger) Seats(account string) []Seat {
	l.mu.Lock()
	defer l.mu.Unlock()
	live := l.liveLocked(account)
	out := make([]Seat, 0, len(live))
	for _, e := range live {
		out = append(out, e.seat)
	}
	return out
}

// Others counts the account's live seats other than except.
func (l *Ledger) Others(account, except string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, e := range l.liveLocked(account) {
		if e.seat.ID != except {
			n++
		}
	}
	return n
}

// Holds reports whether the seat is held and has not lapsed.
func (l *Ledger) Holds(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.byID[id]
	return e != nil && !e.lapsed(l.now())
}
