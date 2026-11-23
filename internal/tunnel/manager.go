package tunnel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/egress"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

// States a tunnel reports.
const (
	StateUp         = "up"
	StateDegraded   = "degraded"
	StateDown       = "down"
	StateConnecting = "connecting"
	StateDisabled   = "disabled"
	StateError      = "error"
)

// TunnelStatus is what the panel shows of one tunnel end.
type TunnelStatus struct {
	ID        int     `json:"id" example:"1"`
	State     string  `json:"state" example:"up"`
	Error     string  `json:"error,omitempty" example:""`
	LatencyMs float64 `json:"latencyMs" example:"42.5"`
	Loss      float64 `json:"loss" example:"0"`
	RxBytes   uint64  `json:"rxBytes" example:"1048576"`
	TxBytes   uint64  `json:"txBytes" example:"2097152"`
	Peer      string  `json:"peer,omitempty" example:"198.51.100.7:52011"`
	Since     int64   `json:"since" example:"1763300000000"`
}

type entry struct {
	t        Tunnel
	routeFP  string
	config   string
	child    *child
	prober   *prober
	err      error
	exits    int
	retryAt  time.Time
	state    string
	since    time.Time
	disabled bool
}

// Manager runs the tunnels of this server and watches them.
type Manager struct {
	mu      sync.Mutex
	dir     string
	binary  func() string
	entries map[int]*entry
	reaped  bool
	// groups is what each group's ports were last pointed at.
	groups map[int]string
}

var (
	managerOnce sync.Once
	manager     *Manager
)

// GetManager returns the process-wide tunnel manager.
func GetManager() *Manager {
	managerOnce.Do(func() {
		manager = newManager(filepath.Join(config.GetBinFolderPath(), "tunnels"), BackPackPath)
	})
	return manager
}

func newManager(dir string, binary func() string) *Manager {
	return &Manager{dir: dir, binary: binary, entries: map[int]*entry{}}
}

func (m *Manager) configPath(id int) string { return filepath.Join(m.dir, fmt.Sprintf("t%d.toml", id)) }

func (m *Manager) metricsPath(id int) string {
	return filepath.Join(m.dir, fmt.Sprintf("t%d.metrics.json", id))
}

// Reconcile makes the running tunnels match desired, the panel's whole list.
func (m *Manager) Reconcile(desired []Tunnel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.reaped {
		reapOrphans(m.binary(), m.dir)
		m.reaped = true
	}
	wanted := map[int]bool{}
	for _, t := range desired {
		wanted[t.ID] = true
		e := m.entries[t.ID]
		if e == nil {
			e = &entry{since: time.Now()}
			m.entries[t.ID] = e
		}
		if e.t.Kind != "" && (e.t.Kind != t.Kind || e.t.Role != t.Role) {
			m.teardownLocked(e)
			*e = entry{since: time.Now()}
		}
		e.t = t
		if !t.Enabled {
			m.teardownLocked(e)
			e.disabled = true
			continue
		}
		e.disabled = false
		e.err = m.applyLocked(e)
		if e.err != nil {
			logger.Warning("tunnel ", t.ID, " (", t.Name, "): ", e.err)
		}
	}
	for id, e := range m.entries {
		if !wanted[id] {
			m.teardownLocked(e)
			delete(m.entries, id)
		}
	}
	m.sweepLocked(wanted)
}

func (m *Manager) applyLocked(e *entry) error {
	t := e.t
	if IsKernel(t.Kind) {
		if err := ensureLink(t); err != nil {
			return err
		}
		if fp := routeFingerprint(t); fp != e.routeFP {
			if err := applyRouting(t); err != nil {
				return err
			}
			e.routeFP = fp
		}
	} else if err := m.ensureChild(e); err != nil {
		return err
	}
	if t.HasInterface() {
		peer, err := netip.ParseAddr(t.Settings.PeerIP)
		if err != nil {
			return err
		}
		if e.prober == nil || e.prober.target != peer {
			if e.prober != nil {
				e.prober.stop()
			}
			e.prober = startProber(peer)
		}
	}
	return nil
}

func routeFingerprint(t Tunnel) string {
	raw, _ := json.Marshal([]any{t.Role, t.Settings.PeerIP, t.Settings.NAT, t.Exposed(), t.Device()})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}

// ensureChild keeps a backpack process running on the current configuration,
// waiting longer after each exit so a refused configuration does not spin.
func (m *Manager) ensureChild(e *entry) error {
	binary := m.binary()
	if info, err := os.Stat(binary); err != nil || info.IsDir() {
		return ErrNoBackPack
	}
	rendered := renderBackPack(e.t)
	path := m.configPath(e.t.ID)
	if e.child != nil && e.child.running() && rendered == e.config {
		return nil
	}
	if e.child != nil && e.child.running() {
		e.child.stop()
		e.exits = 0
	}
	if e.child != nil && !e.child.running() && rendered == e.config {
		if time.Since(e.child.started) > time.Minute {
			e.exits = 0
		}
		if time.Now().Before(e.retryAt) {
			return fmt.Errorf("tunnel: backpack stopped: %s", e.child.log.lastLine())
		}
	}
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(rendered), 0o600); err != nil {
		return err
	}
	c, err := startChild(binary, path, "t"+strconv.Itoa(e.t.ID))
	if err != nil {
		return err
	}
	e.child, e.config = c, rendered
	e.exits++
	e.retryAt = time.Now().Add(min(time.Duration(e.exits)*5*time.Second, time.Minute))
	return nil
}

func (m *Manager) teardownLocked(e *entry) {
	if e.prober != nil {
		e.prober.stop()
		e.prober = nil
	}
	if e.child != nil {
		e.child.stop()
		e.child = nil
	}
	e.config, e.routeFP, e.err = "", "", nil
	if e.t.ID == 0 {
		return
	}
	if IsKernel(e.t.Kind) {
		removeLink(e.t.Device())
	}
	egress.RemoveTables(forwardOwner(e.t.ID))
	removeReplyRoute(e.t.ID)
	_ = os.Remove(m.configPath(e.t.ID))
}

// sweepLocked removes links and tables of tunnels the panel no longer has,
// such as those a previous run left behind when its rows were deleted.
func (m *Manager) sweepLocked(wanted map[int]bool) {
	for id, dev := range panelLinks() {
		if e := m.entries[id]; e == nil || e.disabled || !IsKernel(e.t.Kind) {
			_, _ = host.run("ip", "link", "del", "dev", dev)
		}
	}
	for _, prefix := range []string{"tf", "tr"} {
		for _, owner := range egress.Tables(prefix) {
			id, err := strconv.Atoi(strings.TrimPrefix(owner, prefix))
			if err == nil && !wanted[id] {
				egress.RemoveTables(owner)
			}
		}
	}
}

// StopAll stops what dies with the panel and keeps the kernel links, so a
// restart of the panel does not cut the users that cross them.
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.entries {
		if e.prober != nil {
			e.prober.stop()
			e.prober = nil
		}
		if e.child != nil {
			e.child.stop()
			e.child = nil
		}
	}
}

// RemoveAll withdraws every tunnel of this server, for an uninstall.
func (m *Manager) RemoveAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, e := range m.entries {
		m.teardownLocked(e)
		delete(m.entries, id)
	}
	m.sweepLocked(map[int]bool{})
	_ = os.RemoveAll(m.dir)
}

// Statuses reports every tunnel end the manager knows.
func (m *Manager) Statuses() []TunnelStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]TunnelStatus, 0, len(m.entries))
	for id, e := range m.entries {
		st := m.statusLocked(e)
		st.ID = id
		if st.State != e.state {
			e.state, e.since = st.State, time.Now()
		}
		st.Since = e.since.UnixMilli()
		out = append(out, st)
	}
	return out
}

func (m *Manager) statusLocked(e *entry) TunnelStatus {
	t := e.t
	switch {
	case e.disabled:
		return TunnelStatus{State: StateDisabled}
	case e.err != nil:
		return TunnelStatus{State: StateError, Error: e.err.Error()}
	}
	var st TunnelStatus
	if t.HasInterface() {
		st.RxBytes, st.TxBytes = interfaceBytes(t.Device())
	}
	if !IsKernel(t.Kind) {
		if e.child == nil || !e.child.running() {
			msg := ""
			if e.child != nil {
				msg = e.child.log.lastLine()
			}
			return TunnelStatus{State: StateDown, Error: msg}
		}
		if snap, err := readSnapshot(m.metricsPath(t.ID)); err == nil {
			st.Peer = snap.Peer
			if t.Kind == KindReverse {
				st.RxBytes, st.TxBytes = snap.BytesIn, snap.BytesOut
				switch {
				case snap.Connected != nil && *snap.Connected:
					st.State = StateUp
				case time.Since(e.child.started) < 30*time.Second:
					st.State = StateConnecting
				default:
					st.State = StateDown
				}
				return st
			}
		} else if t.Kind == KindReverse {
			st.State = StateConnecting
			return st
		}
	}
	if e.prober == nil {
		st.State = StateConnecting
		return st
	}
	st.Loss, st.LatencyMs, st.State = judge(&e.prober.window)
	return st
}

func interfaceBytes(dev string) (rx, tx uint64) {
	read := func(name string) uint64 {
		text, err := host.read(sysNet(dev, filepath.Join("statistics", name)))
		if err != nil {
			return 0
		}
		v, _ := strconv.ParseUint(text, 10, 64)
		return v
	}
	return read("rx_bytes"), read("tx_bytes")
}
