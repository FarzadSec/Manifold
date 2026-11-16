// Package speedlimit caps each account's rate in each direction on this host,
// with one nftables table that polices marked proxy sockets and tunnel traffic.
package speedlimit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os/exec"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

const (
	table = "manifold_speed"
	// MarkBase tags the sockets of a limited account; the low 24 bits carry its id.
	MarkBase uint32 = 0x4e000000
	markMask uint32 = 0xff000000
	maxID           = 0x00ffffff
	// MaxMbps keeps a rate inside what nft accepts as bytes per second.
	MaxMbps = 100000
	// A GSO or GRO packet is up to 64 KiB; a smaller bucket would drop every one of them.
	minBurst = 128 * 1024
	// healthEvery is how often the table is checked for having been flushed by someone else.
	healthEvery = time.Minute
)

// ErrNoNftables is returned when the host has no nft command.
var ErrNoNftables = errors.New("speedlimit: the nft command is missing; install the nftables package")

// Mark is the socket mark of the account with this id, false when the id does not fit one.
func Mark(id int) (uint32, bool) {
	if id <= 0 || id > maxID {
		return 0, false
	}
	return MarkBase | uint32(id), true
}

// Account is one limited account and what this host carries for it.
type Account struct {
	ID   int
	Mbps int
	// Addrs are tunnel addresses this host forwards for the account (WireGuard, OpenVPN).
	Addrs []netip.Prefix
	// Ifaces are the interfaces of its live point-to-point sessions (L2TP).
	Ifaces []string
}

// The interface name ends up inside a quoted nft string.
var ifacePattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,15}$`)

type state struct {
	rates  map[int]int
	addr4  map[netip.Prefix]int
	addr6  map[netip.Prefix]int
	ifaces map[string]int
}

// desired normalises accounts: an address or interface claimed twice, or
// overlapping one already taken, stays with the lower account id.
func desired(accounts []Account) state {
	s := state{rates: map[int]int{}, addr4: map[netip.Prefix]int{}, addr6: map[netip.Prefix]int{}, ifaces: map[string]int{}}
	sorted := slices.Clone(accounts)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	for _, a := range sorted {
		if _, ok := Mark(a.ID); !ok || a.Mbps <= 0 {
			continue
		}
		if _, dup := s.rates[a.ID]; dup {
			continue
		}
		s.rates[a.ID] = min(a.Mbps, MaxMbps)
		for _, p := range a.Addrs {
			if !p.IsValid() {
				continue
			}
			p = p.Masked()
			target := s.addr6
			if p.Addr().Is4() {
				target = s.addr4
			}
			if overlapsAny(target, p) {
				logger.Warning("speedlimit: ", p, " of account ", a.ID, " overlaps another limited address; left unlimited")
				continue
			}
			target[p] = a.ID
		}
		for _, name := range a.Ifaces {
			if _, taken := s.ifaces[name]; taken || !ifacePattern.MatchString(name) {
				continue
			}
			s.ifaces[name] = a.ID
		}
	}
	return s
}

func overlapsAny(set map[netip.Prefix]int, p netip.Prefix) bool {
	for q := range set {
		if q.Overlaps(p) {
			return true
		}
	}
	return false
}

func limitName(id int, dir string) string { return fmt.Sprintf("u%d_%s", id, dir) }

// rateLine allows half a second of the rate as burst, so TCP over a long path still fills it.
func rateLine(mbps int) string {
	perSecond := int64(mbps) * 125000
	return fmt.Sprintf("rate over %d bytes/second burst %d bytes", perSecond, max(perSecond/2, minBurst))
}

type element struct {
	set  string
	key  string
	id   int
	quot bool
}

func (e element) keyText() string {
	if e.quot {
		return fmt.Sprintf("%q", e.key)
	}
	return e.key
}

// elements lists every map entry of a state, both directions, in a stable order.
func (s state) elements() []element {
	var out []element
	ids := s.ids()
	for _, id := range ids {
		mark, _ := Mark(id)
		key := fmt.Sprintf("0x%08x", mark)
		out = append(out, element{set: "mark_up", key: key, id: id}, element{set: "mark_down", key: key, id: id})
	}
	add := func(m map[netip.Prefix]int, up, down string) {
		keys := make([]netip.Prefix, 0, len(m))
		for p := range m {
			keys = append(keys, p)
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		for _, p := range keys {
			out = append(out, element{set: up, key: p.String(), id: m[p]}, element{set: down, key: p.String(), id: m[p]})
		}
	}
	add(s.addr4, "addr4_up", "addr4_down")
	add(s.addr6, "addr6_up", "addr6_down")
	names := make([]string, 0, len(s.ifaces))
	for n := range s.ifaces {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		out = append(out, element{set: "iface_up", key: n, id: s.ifaces[n], quot: true}, element{set: "iface_down", key: n, id: s.ifaces[n], quot: true})
	}
	return out
}

func (s state) ids() []int {
	ids := make([]int, 0, len(s.rates))
	for id := range s.rates {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

func direction(set string) string {
	if strings.HasSuffix(set, "_up") {
		return "up"
	}
	return "down"
}

func fullTransaction(s state) string {
	var b strings.Builder
	fmt.Fprintf(&b, "add table inet %[1]s\ndelete table inet %[1]s\ntable inet %[1]s {\n", table)
	for _, id := range s.ids() {
		for _, dir := range []string{"up", "down"} {
			fmt.Fprintf(&b, "\tlimit %s { %s }\n", limitName(id, dir), rateLine(s.rates[id]))
		}
	}
	maps := []struct{ name, typ string }{
		{"mark_up", "mark"}, {"mark_down", "mark"},
		{"addr4_up", "ipv4_addr"}, {"addr4_down", "ipv4_addr"},
		{"addr6_up", "ipv6_addr"}, {"addr6_down", "ipv6_addr"},
		{"iface_up", "ifname"}, {"iface_down", "ifname"},
	}
	bySet := map[string][]string{}
	for _, e := range s.elements() {
		bySet[e.set] = append(bySet[e.set], fmt.Sprintf("%s : %q", e.keyText(), limitName(e.id, direction(e.set))))
	}
	for _, m := range maps {
		flags := ""
		if strings.HasPrefix(m.name, "addr") {
			flags = " flags interval;"
		}
		elems := ""
		if len(bySet[m.name]) > 0 {
			elems = " elements = { " + strings.Join(bySet[m.name], ", ") + " };"
		}
		fmt.Fprintf(&b, "\tmap %s { type %s : limit;%s%s }\n", m.name, m.typ, flags, elems)
	}
	// Each packet is counted once: what a marked socket sends on the way out, and
	// the replies to it on the way in, which on loopback also pass the other hook.
	fmt.Fprintf(&b, "\tchain proxy_upload {\n\t\ttype filter hook output priority mangle; policy accept;\n"+
		"\t\tct direction original meta mark & 0x%08x == 0x%08x ct mark set meta mark\n"+
		"\t\tct direction original limit name meta mark map @mark_up drop\n\t}\n", markMask, MarkBase)
	b.WriteString("\tchain proxy_download {\n\t\ttype filter hook input priority mangle; policy accept;\n" +
		"\t\tct direction reply limit name ct mark map @mark_down drop\n\t}\n")
	b.WriteString("\tchain tunnels {\n\t\ttype filter hook forward priority mangle; policy accept;\n" +
		"\t\tlimit name ip saddr map @addr4_up drop\n\t\tlimit name ip daddr map @addr4_down drop\n" +
		"\t\tlimit name ip6 saddr map @addr6_up drop\n\t\tlimit name ip6 daddr map @addr6_down drop\n" +
		"\t\tlimit name iifname map @iface_up drop\n\t\tlimit name oifname map @iface_down drop\n\t}\n}\n")
	return b.String()
}

// deltaTransaction changes the applied table into next while keeping the buckets
// of every account whose rate stayed: replacing a limit object refills it.
func deltaTransaction(prev, next state) string {
	var del, delLimits, addLimits, add []string
	changed := map[int]bool{}
	for id, rate := range prev.rates {
		if next.rates[id] != rate {
			changed[id] = true
		}
	}
	for id := range next.rates {
		if _, had := prev.rates[id]; !had {
			changed[id] = true
		}
	}
	nextElems := map[element]bool{}
	for _, e := range next.elements() {
		nextElems[e] = true
	}
	prevElems := map[element]bool{}
	for _, e := range prev.elements() {
		prevElems[e] = true
		if !nextElems[e] || changed[e.id] {
			del = append(del, fmt.Sprintf("delete element inet %s %s { %s }", table, e.set, e.keyText()))
		}
	}
	for _, id := range prev.ids() {
		if changed[id] {
			for _, dir := range []string{"up", "down"} {
				delLimits = append(delLimits, fmt.Sprintf("delete limit inet %s %s", table, limitName(id, dir)))
			}
		}
	}
	for _, id := range next.ids() {
		if changed[id] {
			for _, dir := range []string{"up", "down"} {
				addLimits = append(addLimits, fmt.Sprintf("add limit inet %s %s { %s }", table, limitName(id, dir), rateLine(next.rates[id])))
			}
		}
	}
	for _, e := range next.elements() {
		if !prevElems[e] || changed[e.id] {
			add = append(add, fmt.Sprintf("add element inet %s %s { %s : %q }", table, e.set, e.keyText(), limitName(e.id, direction(e.set))))
		}
	}
	lines := slices.Concat(del, delLimits, addLimits, add)
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func removeTransaction() string {
	return fmt.Sprintf("add table inet %[1]s\ndelete table inet %[1]s\n", table)
}

// system is the part of the OS this package drives; tests substitute a fake.
type system interface {
	run(stdin, name string, args ...string) (string, error)
	has(name string) bool
}

// Limiter keeps the host's table in step with the accounts it is given.
type Limiter struct {
	mu      sync.Mutex
	sys     system
	now     func() time.Time
	applied *state
	checked time.Time
}

// New returns a limiter that drives the host's nft.
func New() *Limiter { return &Limiter{sys: osSystem{}, now: time.Now} }

// Apply makes the table police exactly these accounts; with none it removes the table.
func (l *Limiter) Apply(accounts []Account) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	next := desired(accounts)
	if len(next.rates) == 0 {
		if l.applied != nil && len(l.applied.rates) == 0 {
			return nil
		}
		if !l.sys.has("nft") {
			l.applied = &next
			return nil
		}
		if out, err := l.sys.run(removeTransaction(), "nft", "-f", "-"); err != nil {
			return fmt.Errorf("speedlimit: removing the table: %w (%s)", err, out)
		}
		l.applied = &next
		return nil
	}
	if !l.sys.has("nft") {
		return ErrNoNftables
	}
	if l.applied != nil && l.now().Sub(l.checked) >= healthEvery {
		l.checked = l.now()
		if _, err := l.sys.run("", "nft", "-t", "list", "table", "inet", table); err != nil {
			logger.Warning("speedlimit: the table was removed from outside the panel; installing it again")
			l.applied = nil
		}
	}
	if l.applied != nil && len(l.applied.rates) > 0 {
		tx := deltaTransaction(*l.applied, next)
		if tx == "" {
			return nil
		}
		if _, err := l.sys.run(tx, "nft", "-f", "-"); err == nil {
			l.applied = &next
			return nil
		}
	}
	if out, err := l.sys.run(fullTransaction(next), "nft", "-f", "-"); err != nil {
		l.applied = nil
		return fmt.Errorf("speedlimit: installing the table: %w (%s)", err, out)
	}
	l.applied = &next
	l.checked = l.now()
	return nil
}

type osSystem struct{}

func (osSystem) run(stdin, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

func (osSystem) has(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
