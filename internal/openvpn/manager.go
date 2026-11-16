package openvpn

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/egress"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

const (
	attachTimeout = 5 * time.Second
	readyTimeout  = 15 * time.Second
)

// Traffic is what one account used on one inbound since the previous collection.
type Traffic struct {
	Tag   string
	Email string
	Up    int64
	Down  int64
}

type managed struct {
	proc         *process
	ctl          *control
	files        files
	tag          string
	structuralFP string
	usersFP      string
}

func (m *managed) alive() bool {
	if !m.proc.running() {
		return false
	}
	select {
	case <-m.ctl.done:
		return false
	default:
		return true
	}
}

// Manager owns the running OpenVPN servers, keyed by inbound id.
type Manager struct {
	mu      sync.Mutex
	servers map[int]*managed
	baseDir string
	// swept records that servers left behind by a previous panel run were cleaned up.
	swept bool
}

var (
	managerOnce sync.Once
	manager     *Manager
)

// GetManager returns the process-wide OpenVPN manager.
func GetManager() *Manager {
	managerOnce.Do(func() {
		manager = newManager(config.GetBinFolderPath() + "/openvpn")
	})
	return manager
}

func newManager(baseDir string) *Manager {
	return &Manager{servers: map[int]*managed{}, baseDir: baseDir}
}

// Ensure makes the inbound's server run with this configuration: a changed
// account set is applied to the live server, anything else restarts it.
func (m *Manager) Ensure(inst Instance) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepLocked()
	return m.ensureLocked(inst)
}

func (m *Manager) sweepLocked() {
	if m.swept {
		return
	}
	m.swept = true
	if n := reapOrphans(m.baseDir); n > 0 {
		logger.Warningf("openvpn: stopped %d server(s) left by a previous run", n)
	}
}

func (m *Manager) ensureLocked(inst Instance) error {
	structuralFP, usersFP := inst.structuralFingerprint(), inst.usersFingerprint()
	if cur, ok := m.servers[inst.Id]; ok {
		if cur.alive() && cur.structuralFP == structuralFP {
			cur.tag = inst.Tag
			if cur.usersFP != usersFP {
				cur.ctl.setUsers(inst.Users)
				cur.usersFP = usersFP
			}
			return nil
		}
		m.stopLocked(inst.Id)
	}
	started, err := m.start(inst)
	if err != nil {
		return err
	}
	started.structuralFP, started.usersFP = structuralFP, usersFP
	m.servers[inst.Id] = started
	logger.Infof("openvpn: inbound %d is serving %s port %d", inst.Id, inst.Proto, inst.Port)
	return nil
}

func (m *Manager) start(inst Instance) (*managed, error) {
	binary, err := FindBinary()
	if err != nil {
		return nil, err
	}
	f := filesIn(m.baseDir, inst.Id)
	if err := f.write(inst); err != nil {
		return nil, err
	}
	if err := egress.Apply(egressOwner(inst.Id), inst.Subnet); err != nil {
		f.remove()
		return nil, err
	}
	proc, err := startProcess(binary, f.config, deviceName(inst.Id))
	if err != nil {
		egress.Remove(egressOwner(inst.Id))
		f.remove()
		return nil, err
	}
	abort := func(err error) (*managed, error) {
		proc.stop()
		egress.Remove(egressOwner(inst.Id))
		f.remove()
		return nil, err
	}
	conn, ok := attach(f.socket, proc)
	if !ok {
		return abort(proc.failure("the server did not open its management socket"))
	}
	ctl := newControl(conn, inst.Users)
	go ctl.serve()
	select {
	case <-ctl.ready:
	case <-proc.done:
		ctl.close()
		return abort(proc.failure("the server exited while starting"))
	case <-time.After(readyTimeout):
		ctl.close()
		return abort(proc.failure("the server did not come up in time"))
	}
	return &managed{proc: proc, ctl: ctl, files: f, tag: inst.Tag}, nil
}

// attach connects to a starting server's management socket.
func attach(socket string, proc *process) (net.Conn, bool) {
	deadline := time.Now().Add(attachTimeout)
	for time.Now().Before(deadline) && proc.running() {
		conn, err := (&net.Dialer{}).DialContext(context.Background(), "unix", socket)
		if err == nil {
			return conn, true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, false
}

func (m *Manager) stopLocked(id int) {
	cur, ok := m.servers[id]
	if !ok {
		return
	}
	cur.ctl.close()
	cur.proc.stop()
	egress.Remove(egressOwner(id))
	cur.files.remove()
	delete(m.servers, id)
}

// Remove stops and forgets the server of an inbound.
func (m *Manager) Remove(id int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.servers[id]; ok {
		m.stopLocked(id)
		logger.Infof("openvpn: stopped the server of inbound %d", id)
	}
}

// Reconcile drives the running set toward desired: it stops servers no longer
// wanted and starts or updates the rest, which also revives a crashed one.
func (m *Manager) Reconcile(desired []Instance) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepLocked()
	want := make(map[int]struct{}, len(desired))
	for _, inst := range desired {
		want[inst.Id] = struct{}{}
	}
	for id := range m.servers {
		if _, ok := want[id]; !ok {
			m.stopLocked(id)
		}
	}
	for _, inst := range desired {
		if err := m.ensureLocked(inst); err != nil {
			logger.Warningf("openvpn: inbound %d: %v", inst.Id, err)
		}
	}
}

// StopAll stops every server; a stopped panel enforces no limit, so nothing may keep serving.
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := range m.servers {
		m.stopLocked(id)
	}
}

// CollectTraffic returns each account's usage since the previous call and the
// accounts connected right now.
func (m *Manager) CollectTraffic() ([]Traffic, []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Traffic
	var online []string
	for _, cur := range m.servers {
		used, connected := cur.ctl.drain()
		for email, u := range used {
			out = append(out, Traffic{Tag: cur.tag, Email: email, Up: u.up, Down: u.down})
		}
		online = append(online, connected...)
	}
	return out, online
}

// SessionAddresses returns the tunnel addresses of every live session, by account.
func (m *Manager) SessionAddresses() map[string][]netip.Prefix {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string][]netip.Prefix{}
	for _, cur := range m.servers {
		for user, addrs := range cur.ctl.addresses() {
			out[user] = append(out[user], addrs...)
		}
	}
	return out
}

var sessionHook atomic.Pointer[func()]

// SetSessionHook runs f, outside any lock, whenever a session comes up or goes away.
func SetSessionHook(f func()) { sessionHook.Store(&f) }

func notifySessionsChanged() {
	if f := sessionHook.Load(); f != nil && *f != nil {
		go (*f)()
	}
}

// HasRunning reports whether any server is up.
func (m *Manager) HasRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cur := range m.servers {
		if cur.alive() {
			return true
		}
	}
	return false
}
