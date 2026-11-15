package l2tp

import (
	"bytes"
	"encoding/base64"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeHost stands in for the kernel and the process table behind the sessions.
type fakeHost struct {
	mu       sync.Mutex
	counters map[string][2]int64
	dead     map[int]bool
	killed   []int
	clock    time.Time
}

func (h *fakeHost) set(ifname string, rx, tx int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.counters[ifname] = [2]int64{rx, tx}
}

func (h *fakeHost) killedPIDs() []int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.killed)
}

func testControl(t *testing.T, users ...User) (*control, *fakeHost, string) {
	t.Helper()
	host := &fakeHost{counters: map[string][2]int64{}, dead: map[int]bool{}, clock: time.Unix(1_800_000_000, 0)}
	socket := filepath.Join(t.TempDir(), "control.sock")
	ctl, err := newControl(socket, users)
	if err != nil {
		t.Fatalf("newControl: %v", err)
	}
	ctl.counters = func(ifname string) (int64, int64, bool) {
		host.mu.Lock()
		defer host.mu.Unlock()
		c, ok := host.counters[ifname]
		return c[0], c[1], ok
	}
	ctl.alive = func(pid int) bool {
		host.mu.Lock()
		defer host.mu.Unlock()
		return !host.dead[pid]
	}
	ctl.kill = func(pid int) {
		host.mu.Lock()
		defer host.mu.Unlock()
		host.killed = append(host.killed, pid)
	}
	ctl.now = func() time.Time {
		host.mu.Lock()
		defer host.mu.Unlock()
		return host.clock
	}
	go ctl.serve()
	t.Cleanup(ctl.close)
	return ctl, host, socket
}

// login is what pppd's winbind plugin writes for an MS-CHAPv2 login, with the
// RFC 2759 vectors of the password "clientPass".
func login(user string, extra ...string) string {
	lines := []string{"Username:: " + base64.StdEncoding.EncodeToString([]byte(user))}
	lines = append(lines, extra...)
	lines = append(lines, "Request-User-Session-Key: yes", "LANMAN-Challenge: "+rfcChallengeHash, "NT-response: "+rfcNTResponse, ".")
	return strings.Join(lines, "\n") + "\n"
}

func answerTo(t *testing.T, socket, request string) string {
	t.Helper()
	var out bytes.Buffer
	RunAuthHelper(socket, strings.NewReader(request), &out)
	return out.String()
}

func hook(t *testing.T, socket, event, user string, pid int, ifname string, extraEnv map[string]string) {
	t.Helper()
	env := map[string]string{"PEERNAME": user, "PPPD_PID": "0"}
	if pid > 0 {
		env["PPPD_PID"] = strconv.Itoa(pid)
	}
	for k, v := range extraEnv {
		env[k] = v
	}
	if code := RunHook(event, socket, []string{ifname, "10.9.0.2", "203.0.113.9"}, func(key string) string { return env[key] }); code != 0 {
		t.Fatalf("hook %s exited %d", event, code)
	}
}

const (
	granted = "Authenticated: Yes\nUser-session-key: " + rfcPasswordHashHash + "\n.\n"
	refused = "Authenticated: No\nAuthentication-Error: Access denied\n.\n"
)

func TestLoginIsDecidedByThePanelsAccounts(t *testing.T) {
	_, _, socket := testControl(t, User{Name: "alice", Password: "clientPass"}, User{Name: "bob", Password: "another"})
	tests := map[string]struct {
		request, want string
	}{
		"the right password":             {login("alice"), granted},
		"name typed in another case":     {login("ALICE"), granted},
		"a Windows domain is left out":   {login("alice", "NT-Domain:: "+base64.StdEncoding.EncodeToString([]byte("WORKGROUP"))), granted},
		"the wrong password":             {login("bob"), refused},
		"an account that does not exist": {login("mallory"), refused},
		"a plain-text login":             {"Full-Username:: " + base64.StdEncoding.EncodeToString([]byte("alice")) + "\nPassword:: " + base64.StdEncoding.EncodeToString([]byte("clientPass")) + "\n.\n", refused},
		"a request cut short":            {"Username:: YWxpY2U=\n", refused},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := answerTo(t, socket, tt.request); got != tt.want {
				t.Fatalf("answer:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestHelperRefusesWhenThePanelIsNotThere(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nobody.sock")
	want := "Authenticated: No\nAuthentication-Error: The panel is not answering\n.\n"
	if got := answerTo(t, missing, login("alice")); got != want {
		t.Fatalf("answer:\n%s\nwant:\n%s", got, want)
	}
}

func TestDeviceLimitCountsSessionsAndLoginsOnTheirWay(t *testing.T) {
	_, host, socket := testControl(t, User{Name: "alice", Password: "clientPass", MaxSessions: 1})
	tooMany := "Authenticated: No\nAuthentication-Error: This account is connected on its maximum number of devices\n.\n"

	if got := answerTo(t, socket, login("alice")); got != granted {
		t.Fatalf("first device: %s", got)
	}
	// The first device is still negotiating: its login already holds the seat.
	if got := answerTo(t, socket, login("alice")); got != tooMany {
		t.Fatalf("second device while the first is connecting: %s", got)
	}
	hook(t, socket, "up", "alice", 4101, "ppp0", nil)
	if got := answerTo(t, socket, login("alice")); got != tooMany {
		t.Fatalf("second device while the first is connected: %s", got)
	}
	hook(t, socket, "down", "alice", 4101, "ppp0", nil)
	if got := answerTo(t, socket, login("alice")); got != granted {
		t.Fatalf("after the first device left: %s", got)
	}
	// A login that never became a session gives its seat back.
	host.mu.Lock()
	host.clock = host.clock.Add(2 * pendingLogin)
	host.mu.Unlock()
	if got := answerTo(t, socket, login("alice")); got != granted {
		t.Fatalf("after an abandoned login: %s", got)
	}
}

func TestTrafficIsBilledFromTheInterfaceAndFromTheLastWordOfPppd(t *testing.T) {
	ctl, host, socket := testControl(t, User{Name: "alice", Password: "clientPass"}, User{Name: "bob", Password: "another"})
	hook(t, socket, "up", `WORKGROUP\Alice`, 4101, "ppp0", nil)
	hook(t, socket, "up", "bob", 4102, "ppp1", nil)
	host.set("ppp0", 1000, 5000)
	host.set("ppp1", 10, 20)

	used, online := ctl.drain()
	if used["alice"] != (usage{up: 1000, down: 5000}) || used["bob"] != (usage{up: 10, down: 20}) {
		t.Fatalf("first reading = %+v", used)
	}
	slices.Sort(online)
	if !slices.Equal(online, []string{"alice", "bob"}) {
		t.Fatalf("online = %v", online)
	}

	host.set("ppp0", 1500, 5000)
	if used, _ := ctl.drain(); len(used) != 1 || used["alice"] != (usage{up: 500}) {
		t.Fatalf("second reading = %+v, want only alice's 500 new bytes up", used)
	}

	// pppd counts what it sent to the peer, which is the account's download.
	hook(t, socket, "down", "alice", 4101, "ppp0", map[string]string{"BYTES_RCVD": "2000", "BYTES_SENT": "6000"})
	used, online = ctl.drain()
	if used["alice"] != (usage{up: 500, down: 1000}) || !slices.Equal(online, []string{"bob"}) {
		t.Fatalf("after alice left: used %+v online %v", used, online)
	}
	if used, _ := ctl.drain(); len(used) != 0 {
		t.Fatalf("bytes billed twice: %+v", used)
	}
}

func TestSessionsOfAccountsThePanelNoLongerServesAreCut(t *testing.T) {
	ctl, host, socket := testControl(t, User{Name: "alice", Password: "clientPass"}, User{Name: "bob", Password: "another"})
	hook(t, socket, "up", "alice", 4101, "ppp0", nil)
	hook(t, socket, "up", "bob", 4102, "ppp1", nil)

	ctl.setUsers([]User{{Name: "bob", Password: "another"}})
	if got := host.killedPIDs(); !slices.Equal(got, []int{4101}) {
		t.Fatalf("killed %v, want only alice's pppd 4101", got)
	}
	if got := answerTo(t, socket, login("alice")); got != refused {
		t.Fatalf("a removed account still logs in: %s", got)
	}

	// A session that slips in for an account that is gone is cut as it comes up.
	hook(t, socket, "up", "alice", 4103, "ppp2", nil)
	if got := host.killedPIDs(); !slices.Equal(got, []int{4101, 4103}) {
		t.Fatalf("killed %v, want 4101 and 4103", got)
	}
}

func TestSessionWhoseProcessDiedSilentlyIsForgotten(t *testing.T) {
	ctl, host, socket := testControl(t, User{Name: "alice", Password: "clientPass", MaxSessions: 1})
	hook(t, socket, "up", "alice", 4101, "ppp0", nil)
	host.set("ppp0", 100, 100)
	ctl.drain()

	// pppd was killed hard: no ip-down, and ppp0 now belongs to somebody else's session.
	host.mu.Lock()
	host.dead[4101] = true
	host.mu.Unlock()
	host.set("ppp0", 900_000, 900_000)
	used, online := ctl.drain()
	if len(used) != 0 || len(online) != 0 {
		t.Fatalf("billed %+v, online %v for a session that is gone", used, online)
	}
	if got := answerTo(t, socket, login("alice")); got != granted {
		t.Fatalf("the dead session still holds the account's only seat: %s", got)
	}
}
