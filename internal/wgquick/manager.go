package wgquick

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/egress"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

var (
	// ErrNotInstalled is returned when the host lacks the WireGuard tools.
	ErrNotInstalled = errors.New("wireguard: the wg and wg-quick commands are missing; install the wireguard-tools package")
	// ErrForeignConfig is returned for an interface name whose config this panel did not write.
	ErrForeignConfig = errors.New("wireguard: a config with this interface name already exists and is not managed by this inbound")
)

const (
	configDir      = "/etc/wireguard"
	commandTimeout = 30 * time.Second

	// A connected peer renews its handshake about every two minutes.
	onlineWindow = 3 * time.Minute
)

// Traffic is what one account used on one inbound since the previous collection.
type Traffic struct {
	Tag   string
	Email string
	Up    int64
	Down  int64
}

// system is the part of the host this package drives; tests substitute a fake.
type system interface {
	run(stdin, name string, args ...string) (string, error)
	has(name string) bool
	linkExists(name string) bool
	readFile(path string) (string, bool)
	writeFile(path, content string) error
	removeFile(path string)
	configs(dir string) []string
}

type counters struct{ rx, tx int64 }

type usage struct{ up, down int64 }

type managed struct {
	inst         Instance
	structuralFP string
	peers        map[string]Peer
	last         map[string]counters
	unbilled     map[string]usage
	online       []string
}

// Manager owns the WireGuard interfaces of this panel, keyed by inbound id.
type Manager struct {
	mu           sync.Mutex
	ifaces       map[int]*managed
	dir          string
	sys          system
	now          func() time.Time
	applyEgress  func(owner, device string) error
	removeEgress func(owner string)
}

var (
	managerOnce sync.Once
	manager     *Manager
)

// GetManager returns the process-wide WireGuard manager.
func GetManager() *Manager {
	managerOnce.Do(func() { manager = newManager(configDir, osSystem{}) })
	return manager
}

func newManager(dir string, sys system) *Manager {
	return &Manager{
		ifaces:       map[int]*managed{},
		dir:          dir,
		sys:          sys,
		now:          time.Now,
		applyEgress:  egress.ApplyDevice,
		removeEgress: egress.Remove,
	}
}

// Available reports whether the host has the tools an interface needs.
func (m *Manager) Available() bool {
	return m.sys.has("wg") && m.sys.has("wg-quick")
}

// Ensure makes the inbound's interface match inst: changed peers are applied
// to the live interface, anything else brings it down and up again.
func (m *Manager) Ensure(inst Instance) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ensureLocked(inst)
}

func (m *Manager) ensureLocked(inst Instance) error {
	if !m.Available() {
		return ErrNotInstalled
	}
	path := configPath(m.dir, inst.Interface)
	if content, exists := m.sys.readFile(path); exists {
		if owner, ours := configOwner(content); !ours || owner != inst.Id {
			return fmt.Errorf("%w: %s", ErrForeignConfig, path)
		}
	}
	cur := m.ifaces[inst.Id]
	if cur != nil && cur.inst.Interface != inst.Interface {
		m.stopLocked(inst.Id)
		cur = nil
	}
	structuralFP := inst.structuralFingerprint()
	if cur != nil && cur.structuralFP == structuralFP && m.sys.linkExists(inst.Interface) {
		cur.inst.Tag = inst.Tag
		return m.applyPeersLocked(cur, inst)
	}
	return m.startLocked(cur, inst, structuralFP)
}

func (m *Manager) startLocked(cur *managed, inst Instance, structuralFP string) error {
	path := configPath(m.dir, inst.Interface)
	next := &managed{inst: inst, structuralFP: structuralFP, last: map[string]counters{}, unbilled: map[string]usage{}}
	if cur != nil {
		m.billLocked(cur)
		next.unbilled = cur.unbilled
	}
	// wg-quick reads the config it is given, so the old one must still be there to go down.
	if m.sys.linkExists(inst.Interface) {
		if out, err := m.sys.run("", "wg-quick", "down", path); err != nil {
			return fmt.Errorf("wireguard: stopping %s: %w: %s", inst.Interface, err, out)
		}
	}
	if err := m.sys.writeFile(path, renderConfig(inst)); err != nil {
		return err
	}
	if out, err := m.sys.run("", "wg-quick", "up", path); err != nil {
		m.sys.removeFile(path)
		// The entry stays, stopped, so that what it already billed is still collected.
		if cur != nil {
			cur.peers, cur.online = map[string]Peer{}, nil
		}
		return fmt.Errorf("wireguard: starting %s: %w: %s", inst.Interface, err, lastLine(out))
	}
	next.peers = inst.peersByKey()
	for _, key := range sortedKeys(next.peers) {
		m.routesLocked(inst.Interface, next.peers[key], "replace")
	}
	if err := m.applyEgress(egressOwner(inst.Id), inst.Interface); err != nil {
		logger.Warningf("wireguard: inbound %d has no NAT: %v", inst.Id, err)
	}
	m.ifaces[inst.Id] = next
	logger.Infof("wireguard: inbound %d is up as %s on port %d", inst.Id, inst.Interface, inst.ListenPort)
	return nil
}

func (m *Manager) applyPeersLocked(cur *managed, inst Instance) error {
	want := inst.peersByKey()
	var gone, changed []string
	for _, key := range sortedKeys(cur.peers) {
		if next, kept := want[key]; !kept {
			gone = append(gone, key)
		} else if next.fingerprint() != cur.peers[key].fingerprint() {
			changed = append(changed, key)
		}
	}
	var added []string
	for _, key := range sortedKeys(want) {
		if _, had := cur.peers[key]; !had {
			added = append(added, key)
		}
	}
	renamed := false
	for key, next := range want {
		if old, had := cur.peers[key]; had && old.Name != next.Name {
			renamed = true
		}
	}
	if len(gone)+len(changed)+len(added) == 0 && !renamed {
		return nil
	}
	if len(gone)+len(changed) > 0 {
		m.billLocked(cur)
	}
	for _, key := range append(gone, changed...) {
		m.removePeerLocked(cur, key)
	}
	var firstErr error
	for _, key := range append(changed, added...) {
		if err := m.addPeerLocked(cur, want[key]); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for key, next := range want {
		if _, applied := cur.peers[key]; applied {
			cur.peers[key] = next
		}
	}
	cur.inst.Peers = inst.Peers
	if err := m.sys.writeFile(configPath(m.dir, inst.Interface), renderConfig(inst)); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func (m *Manager) removePeerLocked(cur *managed, key string) {
	iface := cur.inst.Interface
	if out, err := m.sys.run("", "wg", "set", iface, "peer", key, "remove"); err != nil {
		logger.Warningf("wireguard: removing a peer from %s: %v: %s", iface, err, out)
	}
	// A changed peer is added again right after, which puts its routes back.
	m.routesLocked(iface, cur.peers[key], "del")
	delete(cur.peers, key)
	delete(cur.last, key)
}

func (m *Manager) addPeerLocked(cur *managed, p Peer) error {
	iface := cur.inst.Interface
	args := []string{"set", iface, "peer", p.PublicKey}
	stdin := ""
	if p.PresharedKey != "" {
		// Read from stdin so the key never shows up in the process list.
		args = append(args, "preshared-key", "/dev/stdin")
		stdin = p.PresharedKey + "\n"
	}
	args = append(args, "allowed-ips", p.allowedIPs())
	if out, err := m.sys.run(stdin, "wg", args...); err != nil {
		return fmt.Errorf("wireguard: adding %s to %s: %w: %s", p.Name, iface, err, out)
	}
	m.routesLocked(iface, p, "replace")
	cur.peers[p.PublicKey] = p
	return nil
}

func (m *Manager) routesLocked(iface string, p Peer, verb string) {
	for _, prefix := range p.AllowedIPs {
		if out, err := m.sys.run("", "ip", "route", verb, prefix.String(), "dev", iface); err != nil && verb != "del" {
			logger.Warningf("wireguard: route %s for %s: %v: %s", prefix, iface, err, out)
		}
	}
}

// billLocked reads the interface's counters and charges each peer's account
// for their growth since the last reading.
func (m *Manager) billLocked(cur *managed) {
	out, err := m.sys.run("", "wg", "show", cur.inst.Interface, "dump")
	if err != nil {
		return
	}
	cur.online = cur.online[:0]
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, row := range lines[min(1, len(lines)):] {
		fields := strings.Split(row, "\t")
		if len(fields) < 8 {
			continue
		}
		p, known := cur.peers[fields[0]]
		if !known {
			continue
		}
		handshake, errH := strconv.ParseInt(fields[4], 10, 64)
		rx, errRx := strconv.ParseInt(fields[5], 10, 64)
		tx, errTx := strconv.ParseInt(fields[6], 10, 64)
		if errH != nil || errRx != nil || errTx != nil {
			continue
		}
		if handshake > 0 && m.now().Sub(time.Unix(handshake, 0)) <= onlineWindow {
			cur.online = append(cur.online, p.Name)
		}
		last := cur.last[p.PublicKey]
		// Counters that went backwards belong to a peer that was set anew.
		if rx < last.rx || tx < last.tx {
			last = counters{}
		}
		cur.last[p.PublicKey] = counters{rx: rx, tx: tx}
		if up, down := rx-last.rx, tx-last.tx; up > 0 || down > 0 {
			total := cur.unbilled[p.Name]
			total.up += up
			total.down += down
			cur.unbilled[p.Name] = total
		}
	}
}

func (m *Manager) stopLocked(id int) {
	cur, ok := m.ifaces[id]
	if !ok {
		return
	}
	m.billLocked(cur)
	m.downLocked(id, cur.inst.Interface)
	// What was just billed is still owed to the accounts; keep it for the next collection.
	if len(cur.unbilled) > 0 {
		cur.peers = map[string]Peer{}
		cur.online = nil
		return
	}
	delete(m.ifaces, id)
}

func (m *Manager) downLocked(id int, iface string) {
	path := configPath(m.dir, iface)
	if m.sys.linkExists(iface) {
		if out, err := m.sys.run("", "wg-quick", "down", path); err != nil {
			logger.Warningf("wireguard: stopping %s: %v: %s", iface, err, out)
		}
	}
	m.sys.removeFile(path)
	m.removeEgress(egressOwner(id))
}

// Remove stops and forgets the interface of an inbound.
func (m *Manager) Remove(id int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cur, ok := m.ifaces[id]; ok && m.running(cur) {
		m.stopLocked(id)
		logger.Infof("wireguard: stopped the interface of inbound %d", id)
	}
}

func (m *Manager) running(cur *managed) bool {
	_, exists := m.sys.readFile(configPath(m.dir, cur.inst.Interface))
	return exists
}

// Reconcile drives the interfaces toward desired: it stops the ones no longer
// wanted, including those a previous run left behind, and ensures the rest.
func (m *Manager) Reconcile(desired []Instance) {
	m.mu.Lock()
	defer m.mu.Unlock()
	want := make(map[int]struct{}, len(desired))
	for _, inst := range desired {
		want[inst.Id] = struct{}{}
	}
	ids := make([]int, 0, len(m.ifaces))
	for id := range m.ifaces {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if _, ok := want[id]; !ok && m.running(m.ifaces[id]) {
			m.stopLocked(id)
		}
	}
	for _, path := range m.sys.configs(m.dir) {
		content, _ := m.sys.readFile(path)
		id, ours := configOwner(content)
		if _, wanted := want[id]; !ours || wanted {
			continue
		}
		m.downLocked(id, strings.TrimSuffix(filepath.Base(path), ".conf"))
	}
	for _, inst := range desired {
		if err := m.ensureLocked(inst); err != nil {
			logger.Warningf("wireguard: inbound %d: %v", inst.Id, err)
		}
	}
}

// StopAll brings every interface down; a stopped panel enforces no limit, so nothing may keep serving.
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, cur := range m.ifaces {
		if m.running(cur) {
			m.stopLocked(id)
		}
	}
}

// CollectTraffic returns each account's usage since the previous call and the
// accounts whose peers are connected right now.
func (m *Manager) CollectTraffic() ([]Traffic, []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Traffic
	var online []string
	for id, cur := range m.ifaces {
		stopped := !m.running(cur)
		if !stopped {
			m.billLocked(cur)
			online = append(online, cur.online...)
		}
		for email, u := range cur.unbilled {
			out = append(out, Traffic{Tag: cur.inst.Tag, Email: email, Up: u.up, Down: u.down})
		}
		cur.unbilled = map[string]usage{}
		if stopped {
			delete(m.ifaces, id)
		}
	}
	return out, online
}

// HasRunning reports whether any interface is up.
func (m *Manager) HasRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cur := range m.ifaces {
		if m.running(cur) {
			return true
		}
	}
	return false
}

func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return lines[len(lines)-1]
}

type osSystem struct{}

func (osSystem) run(stdin, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
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

func (osSystem) linkExists(name string) bool {
	_, err := os.Stat("/sys/class/net/" + name)
	return err == nil
}

func (osSystem) readFile(path string) (string, bool) {
	content, err := os.ReadFile(path)
	return string(content), err == nil
}

func (osSystem) writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func (osSystem) removeFile(path string) { _ = os.Remove(path) }

func (osSystem) configs(dir string) []string {
	paths, _ := filepath.Glob(filepath.Join(dir, "*.conf"))
	return paths
}
