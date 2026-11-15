package l2tp

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"slices"
	"strings"
	"testing"
)

// fakeDaemon stands in for a running xl2tpd.
type fakeDaemon struct {
	alive   bool
	stopped bool
}

func (d *fakeDaemon) running() bool     { return d.alive }
func (d *fakeDaemon) stop()             { d.alive, d.stopped = false, true }
func (d *fakeDaemon) lastWords() string { return "" }

// fakeWorld records what the manager asks of the host.
type fakeWorld struct {
	events   []string
	daemons  []*fakeDaemon
	missing  []string
	guardErr error
	loadErr  error
	startErr error
	loaded   string
}

func (w *fakeWorld) note(format string, args ...any) {
	w.events = append(w.events, fmt.Sprintf(format, args...))
}

func (w *fakeWorld) taken() []string {
	out := w.events
	w.events = nil
	return out
}

func (w *fakeWorld) load(config string) error {
	if w.loadErr != nil {
		return w.loadErr
	}
	w.loaded = config
	w.note("ipsec load")
	return nil
}

func (w *fakeWorld) unload() {
	w.loaded = ""
	w.note("ipsec unload")
}

func testManager(t *testing.T) (*Manager, *fakeWorld) {
	t.Helper()
	w := &fakeWorld{}
	m := newManager(t.TempDir(), testExe)
	m.ipsec = w
	m.missingPrograms = func(bool) []string { return w.missing }
	m.startDaemon = func(f files) (daemon, error) {
		if w.startErr != nil {
			return nil, w.startErr
		}
		w.note("xl2tpd start %s", strings.TrimPrefix(f.xl2tpd, m.baseDir))
		d := &fakeDaemon{alive: true}
		w.daemons = append(w.daemons, d)
		return d, nil
	}
	m.applyEgress = func(owner string, subnet netip.Prefix) error {
		w.note("nat %s %s", owner, subnet)
		return nil
	}
	m.requireIPsec = func(owner string, port int) error {
		if w.guardErr != nil {
			return w.guardErr
		}
		w.note("guard %s %d", owner, port)
		return nil
	}
	m.removeEgress = func(owner string) { w.note("remove %s", owner) }
	t.Cleanup(m.StopAll)
	return m, w
}

func equalEvents(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("host was asked:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

func TestEnsureStartsAServerThatOnlyTakesEncryptedSessions(t *testing.T) {
	m, w := testManager(t)
	inst := testInstance(User{Name: "alice", Password: "clientPass"})
	if err := m.Ensure(inst); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	// The guard is in place before anything listens on the port.
	equalEvents(t, w.taken(), []string{"nat l2tp4 10.9.0.0/24", "guard l2tp4g 1701", "ipsec load", "xl2tpd start /4/xl2tpd.conf"})
	if w.loaded != renderSwanctl(inst) {
		t.Fatalf("IPsec was given:\n%s", w.loaded)
	}
	if got := answerTo(t, filesIn(m.baseDir, 4).socket, login("alice")); got != granted {
		t.Fatalf("a login against the started server: %s", got)
	}
	if !m.HasRunning() {
		t.Fatal("HasRunning is false with a server up")
	}
}

func TestAccountsChangeWithoutARestartAndSettingsWithOne(t *testing.T) {
	m, w := testManager(t)
	inst := testInstance(User{Name: "alice", Password: "clientPass"})
	if err := m.Ensure(inst); err != nil {
		t.Fatal(err)
	}
	w.taken()
	socket := filesIn(m.baseDir, 4).socket

	inst.Users = []User{{Name: "bob", Password: "clientPass"}}
	if err := m.Ensure(inst); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	equalEvents(t, w.taken(), nil)
	if answerTo(t, socket, login("bob")) != granted || answerTo(t, socket, login("alice")) != refused {
		t.Fatal("the running server does not decide by the new accounts")
	}

	inst.MTU = 1400
	if err := m.Ensure(inst); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	equalEvents(t, w.taken(), []string{
		"remove l2tp4", "remove l2tp4g", "ipsec unload",
		"nat l2tp4 10.9.0.0/24", "guard l2tp4g 1701", "ipsec load", "xl2tpd start /4/xl2tpd.conf",
	})
	if !w.daemons[0].stopped || !w.daemons[1].alive {
		t.Fatalf("daemons = %+v, %+v: want the first stopped and a second running", w.daemons[0], w.daemons[1])
	}
}

// Without the requirement plain sessions are let in, and IPsec stays on offer
// for the devices that can do it.
func TestServerThatDoesNotRequireIPsecStillOffersIt(t *testing.T) {
	m, w := testManager(t)
	inst := testInstance()
	inst.IPsec = false
	if err := m.Ensure(inst); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	equalEvents(t, w.taken(), []string{"nat l2tp4 10.9.0.0/24", "remove l2tp4g", "ipsec load", "xl2tpd start /4/xl2tpd.conf"})
}

func TestStrongSwanThatDoesNotAnswerStopsOnlyAServerThatRequiresIt(t *testing.T) {
	m, w := testManager(t)
	w.loadErr = errors.New("l2tp: strongSwan did not take the IPsec configuration")

	optional := testInstance()
	optional.IPsec = false
	if err := m.Ensure(optional); err != nil {
		t.Fatalf("a server that takes plain sessions was held back by IPsec: %v", err)
	}
	if len(w.daemons) != 1 || !w.daemons[0].alive {
		t.Fatal("xl2tpd is not running")
	}
	m.StopAll()
	w.taken()

	if err := m.Ensure(testInstance()); err == nil || !strings.Contains(err.Error(), "strongSwan did not take") {
		t.Fatalf("error = %v, want strongSwan's", err)
	}
	if len(w.daemons) != 1 {
		t.Fatal("xl2tpd was started for a server whose clients could not set up IPsec")
	}
}

// A server that must be encrypted and cannot be guarded would take plain sessions.
func TestNothingListensWhenEncryptionCannotBeEnforced(t *testing.T) {
	m, w := testManager(t)
	w.guardErr = errors.New("egress: requiring IPsec for UDP port 1701: exit status 1 (Error: syntax error)")
	err := m.Ensure(testInstance())
	if err == nil || !strings.Contains(err.Error(), "requiring IPsec") {
		t.Fatalf("error = %v, want the guard's", err)
	}
	if len(w.daemons) != 0 || m.HasRunning() {
		t.Fatal("xl2tpd was started without the guard")
	}
	equalEvents(t, w.taken(), []string{"nat l2tp4 10.9.0.0/24", "remove l2tp4", "remove l2tp4g", "ipsec unload"})
	if _, statErr := os.Stat(filesIn(m.baseDir, 4).dir); !os.IsNotExist(statErr) {
		t.Fatal("the files of a server that never started were left behind")
	}
}

func TestEnsureNamesTheProgramsThatAreMissing(t *testing.T) {
	m, w := testManager(t)
	w.missing = []string{"xl2tpd", "swanctl"}
	err := m.Ensure(testInstance())
	if !errors.Is(err, ErrNotInstalled) || !strings.Contains(err.Error(), "xl2tpd, swanctl") {
		t.Fatalf("error = %v, want ErrNotInstalled naming xl2tpd and swanctl", err)
	}
	equalEvents(t, w.taken(), nil)
}

// Every L2TP client dials port 1701, so one server can have only one inbound.
func TestASecondInboundIsRefusedWhileOneRuns(t *testing.T) {
	m, w := testManager(t)
	if err := m.Ensure(testInstance()); err != nil {
		t.Fatal(err)
	}
	w.taken()
	other := testInstance()
	other.Id = 9
	if err := m.Ensure(other); err == nil || err.Error() != "l2tp: inbound 4 already serves L2TP on this server" {
		t.Fatalf("error = %v", err)
	}
	equalEvents(t, w.taken(), nil)
}

func TestReconcileRestartsACrashedDaemonAndStopsWhatIsNoLongerWanted(t *testing.T) {
	m, w := testManager(t)
	inst := testInstance()
	m.Reconcile([]Instance{inst})
	w.taken()

	w.daemons[0].alive = false
	m.Reconcile([]Instance{inst})
	if len(w.daemons) != 2 || !w.daemons[1].alive {
		t.Fatalf("a crashed xl2tpd was not started again: %d daemons", len(w.daemons))
	}
	w.taken()

	m.Reconcile(nil)
	equalEvents(t, w.taken(), []string{"remove l2tp4", "remove l2tp4g", "ipsec unload"})
	if w.daemons[1].alive || m.HasRunning() {
		t.Fatal("the server outlived its inbound")
	}
	if _, err := os.Stat(filesIn(m.baseDir, 4).dir); !os.IsNotExist(err) {
		t.Fatal("the files outlived the server")
	}
}

func TestCollectTrafficReportsUnderTheInboundsTag(t *testing.T) {
	m, _ := testManager(t)
	if err := m.Ensure(testInstance(User{Name: "Alice", Password: "clientPass"})); err != nil {
		t.Fatal(err)
	}
	r := m.servers[4]
	r.ctl.alive = func(int) bool { return true }
	r.ctl.counters = func(string) (int64, int64, bool) { return 700, 9000, true }
	hook(t, filesIn(m.baseDir, 4).socket, "up", "alice", 4101, "ppp0", nil)

	traffic, online := m.CollectTraffic()
	want := []Traffic{{Tag: "inbound-1701", Email: "Alice", Up: 700, Down: 9000}}
	if !slices.Equal(traffic, want) || !slices.Equal(online, []string{"Alice"}) {
		t.Fatalf("traffic = %+v online = %v, want %+v and [Alice]", traffic, online, want)
	}
}
