package l2tp

import (
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/egress"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

// Traffic is the usage one account accumulated since the previous collection.
type Traffic struct {
	Tag   string
	Email string
	Up    int64
	Down  int64
}

type running struct {
	inst       Instance
	files      files
	daemon     daemon
	ctl        *control
	structural string
	users      string
}

// Manager runs the L2TP server of this host: xl2tpd, the control socket pppd's
// helpers talk to, the NAT of the tunnel subnet and the IPsec side.
type Manager struct {
	mu      sync.Mutex
	baseDir string
	// exe is the panel's own program, which pppd starts as its helper.
	exe     string
	servers map[int]*running

	ipsec           ipsecBackend
	missingPrograms func(withIPsec bool) []string
	startDaemon     func(f files) (daemon, error)
	applyEgress     func(owner string, subnet netip.Prefix) error
	requireIPsec    func(owner string, port int) error
	removeEgress    func(owner string)
}

var (
	manager     *Manager
	managerOnce sync.Once
)

// GetManager returns the process-wide L2TP manager.
func GetManager() *Manager {
	managerOnce.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			logger.Warning("l2tp: cannot tell the panel's own path: ", err)
		}
		manager = newManager(filepath.Join(config.GetBinFolderPath(), "l2tp"), exe)
		reapOrphans(manager.baseDir)
	})
	return manager
}

func newManager(baseDir, exe string) *Manager {
	// pppd starts the login helper and the hooks from "/": the paths it is given
	// must not depend on the panel's working directory, as the default "bin" does.
	if abs, err := filepath.Abs(baseDir); err == nil {
		baseDir = abs
	}
	return &Manager{
		baseDir:         baseDir,
		exe:             exe,
		servers:         map[int]*running{},
		ipsec:           newStrongSwan(),
		missingPrograms: func(withIPsec bool) []string { return MissingPrograms(withIPsec) },
		startDaemon:     startXl2tpd,
		applyEgress:     egress.Apply,
		requireIPsec:    egress.RequireIPsec,
		removeEgress:    egress.Remove,
	}
}

// Ensure makes the server match inst. A change of accounts reaches the
// running server; anything else restarts it, which drops its sessions.
func (m *Manager) Ensure(inst Instance) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ensureLocked(inst)
}

func (m *Manager) ensureLocked(inst Instance) error {
	for id := range m.servers {
		if id != inst.Id {
			return fmt.Errorf("l2tp: inbound %d already serves L2TP on this server", id)
		}
	}
	if r, ok := m.servers[inst.Id]; ok {
		if r.daemon.running() && r.structural == inst.structuralFingerprint() {
			if users := inst.usersFingerprint(); users != r.users {
				r.ctl.setUsers(inst.Users)
				r.users = users
			}
			r.inst = inst
			return nil
		}
		m.stopLocked(inst.Id)
	}
	return m.startLocked(inst)
}

func (m *Manager) startLocked(inst Instance) (err error) {
	if missing := m.missingPrograms(inst.IPsec); len(missing) > 0 {
		return fmt.Errorf("%w: %s", ErrNotInstalled, strings.Join(missing, ", "))
	}
	f := filesIn(m.baseDir, inst.Id)
	if err := f.write(inst, m.exe, config.IsDebug()); err != nil {
		return err
	}
	ctl, err := newControl(f.socket, inst.Id, inst.Users)
	if err != nil {
		f.remove()
		return fmt.Errorf("l2tp: opening the control socket: %w", err)
	}
	go ctl.serve()
	defer func() {
		if err != nil {
			ctl.close()
			m.withdrawLocked(inst.Id, f)
		}
	}()
	if err := m.applyEgress(egressOwner(inst.Id), inst.Subnet); err != nil {
		return err
	}
	// The guard goes in before anything listens, so no session is ever taken in plain text.
	if inst.IPsec {
		if err := m.requireIPsec(guardOwner(inst.Id), Port); err != nil {
			return err
		}
	} else {
		m.removeEgress(guardOwner(inst.Id))
	}
	// Without the requirement IPsec stays on offer, and a host that cannot offer it still serves.
	if err := m.ipsec.load(renderSwanctl(inst)); err != nil {
		if inst.IPsec {
			return err
		}
		logger.Warningf("l2tp: inbound %d takes sessions without IPsec only: %v", inst.Id, err)
	}
	d, err := m.startDaemon(f)
	if err != nil {
		return fmt.Errorf("l2tp: starting xl2tpd: %w", err)
	}
	m.servers[inst.Id] = &running{
		inst:       inst,
		files:      f,
		daemon:     d,
		ctl:        ctl,
		structural: inst.structuralFingerprint(),
		users:      inst.usersFingerprint(),
	}
	logger.Infof("l2tp: inbound %d serves %s with %d accounts (IPsec required: %v)", inst.Id, inst.Subnet, len(inst.Users), inst.IPsec)
	return nil
}

// withdrawLocked takes back everything a server put on the host besides its daemon.
func (m *Manager) withdrawLocked(id int, f files) {
	m.removeEgress(egressOwner(id))
	m.removeEgress(guardOwner(id))
	m.ipsec.unload()
	f.remove()
}

func (m *Manager) stopLocked(id int) {
	r, ok := m.servers[id]
	if !ok {
		return
	}
	delete(m.servers, id)
	r.daemon.stop()
	r.ctl.close()
	m.withdrawLocked(id, r.files)
}

// Remove stops the server of an inbound.
func (m *Manager) Remove(id int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked(id)
}

// Reconcile makes the host match desired: it starts what is missing or has
// crashed, applies changes and stops what is no longer wanted.
func (m *Manager) Reconcile(desired []Instance) {
	m.mu.Lock()
	defer m.mu.Unlock()
	wanted := make(map[int]bool, len(desired))
	for _, inst := range desired {
		wanted[inst.Id] = true
	}
	for id := range m.servers {
		if !wanted[id] {
			m.stopLocked(id)
		}
	}
	for _, inst := range desired {
		if err := m.ensureLocked(inst); err != nil {
			logger.Warningf("l2tp: inbound %d: %v", inst.Id, err)
		}
	}
}

// StopAll stops every server; the panel calls it on shutdown.
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := range m.servers {
		m.stopLocked(id)
	}
}

// HasRunning reports whether a server is up.
func (m *Manager) HasRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.servers) > 0
}

// SessionInterfaces returns the ppp interface of every live session, by account.
func (m *Manager) SessionInterfaces() map[string][]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string][]string{}
	for _, r := range m.servers {
		for name, ifaces := range r.ctl.interfaces() {
			out[name] = append(out[name], ifaces...)
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

// CollectTraffic returns each account's usage since the previous call and the
// accounts connected right now.
func (m *Manager) CollectTraffic() ([]Traffic, []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var traffic []Traffic
	var online []string
	for _, r := range m.servers {
		used, connected := r.ctl.drain()
		for email, u := range used {
			traffic = append(traffic, Traffic{Tag: r.inst.Tag, Email: email, Up: u.up, Down: u.down})
		}
		online = append(online, connected...)
	}
	return traffic, online
}
