package l2tp

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

const (
	deniedLogin  = "Access denied"
	deniedDevice = "This account is connected on its maximum number of devices"

	// A granted login holds a device seat this long while its session comes up.
	pendingLogin = 30 * time.Second
	// Nothing pppd or its scripts send takes longer than this to read.
	requestTimeout = 5 * time.Second
)

// request is what a helper started by pppd asks or tells the panel.
type request struct {
	Op        string `json:"op"`
	User      string `json:"user,omitempty"`
	Challenge string `json:"challenge,omitempty"`
	Response  string `json:"response,omitempty"`
	Interface string `json:"interface,omitempty"`
	PID       int    `json:"pid,omitempty"`
	Address   string `json:"address,omitempty"`
	Peer      string `json:"peer,omitempty"`
	// Sent and Received are pppd's totals for a finished session, from the server's side.
	Sent     int64 `json:"sent,omitempty"`
	Received int64 `json:"received,omitempty"`
}

type reply struct {
	OK    bool   `json:"ok"`
	Key   string `json:"key,omitempty"`
	Error string `json:"error,omitempty"`
}

type usage struct{ up, down int64 }

type session struct {
	user   string
	ifname string
	// rx and tx are the interface counters already billed.
	rx, tx int64
}

// control answers pppd's helpers for one inbound: it decides logins against
// the account table and follows every session from link up to link down.
type control struct {
	mu       sync.Mutex
	listener net.Listener
	users    map[string]User
	sessions map[int]*session
	pending  map[string][]time.Time
	unbilled map[string]usage
	done     chan struct{}

	counters func(ifname string) (rx, tx int64, ok bool)
	alive    func(pid int) bool
	kill     func(pid int)
	now      func() time.Time
}

func newControl(socket string, users []User) (*control, error) {
	_ = os.Remove(socket)
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", socket)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		_ = listener.Close()
		return nil, err
	}
	c := &control{
		listener: listener,
		sessions: map[int]*session{},
		pending:  map[string][]time.Time{},
		unbilled: map[string]usage{},
		done:     make(chan struct{}),
		counters: interfaceCounters,
		alive:    isPppd,
		kill:     terminatePppd,
		now:      time.Now,
	}
	c.users = accountTable(users)
	return c, nil
}

// accountTable keys accounts the way people type them: without regard to case.
func accountTable(users []User) map[string]User {
	table := make(map[string]User, len(users))
	for _, u := range users {
		table[strings.ToLower(u.Name)] = u
	}
	return table
}

// accountOf strips the Windows domain pppd may pass along with a name.
func accountOf(name string) string {
	if cut := strings.LastIndexByte(name, '\\'); cut >= 0 {
		name = name[cut+1:]
	}
	return strings.ToLower(strings.TrimSpace(name))
}

func (c *control) serve() {
	defer close(c.done)
	for {
		conn, err := c.listener.Accept()
		if err != nil {
			return
		}
		go c.handle(conn)
	}
}

func (c *control) close() {
	_ = c.listener.Close()
	<-c.done
}

func (c *control) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(requestTimeout))
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return
	}
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return
	}
	var out reply
	switch req.Op {
	case "auth":
		out = c.authorize(req)
	case "up":
		c.sessionUp(req)
		out.OK = true
	case "down":
		c.sessionDown(req)
		out.OK = true
	default:
		out.Error = "unknown request"
	}
	answer, _ := json.Marshal(out)
	_, _ = conn.Write(append(answer, '\n'))
}

func (c *control) authorize(req request) reply {
	challenge, errC := hex.DecodeString(req.Challenge)
	response, errR := hex.DecodeString(req.Response)
	name := accountOf(req.User)
	c.mu.Lock()
	defer c.mu.Unlock()
	user, known := c.users[name]
	if !known || errC != nil || errR != nil {
		return reply{Error: deniedLogin}
	}
	key, ok := verifyMSCHAPv2(user.Password, challenge, response)
	if !ok {
		return reply{Error: deniedLogin}
	}
	if user.MaxSessions > 0 && c.seatsTakenLocked(name) >= user.MaxSessions {
		logger.Infof("l2tp: %s was refused another device: its limit is %d", user.Name, user.MaxSessions)
		return reply{Error: deniedDevice}
	}
	c.pending[name] = append(c.pending[name], c.now())
	return reply{OK: true, Key: strings.ToUpper(hex.EncodeToString(key))}
}

// seatsTakenLocked counts an account's sessions and its logins still on the way to one.
func (c *control) seatsTakenLocked(name string) int {
	taken := 0
	for _, s := range c.sessions {
		if s.user == name {
			taken++
		}
	}
	fresh := c.pending[name][:0]
	for _, at := range c.pending[name] {
		if c.now().Sub(at) < pendingLogin {
			fresh = append(fresh, at)
		}
	}
	c.pending[name] = fresh
	return taken + len(fresh)
}

func (c *control) sessionUp(req request) {
	name := accountOf(req.User)
	c.mu.Lock()
	defer c.mu.Unlock()
	if waiting := c.pending[name]; len(waiting) > 0 {
		c.pending[name] = waiting[1:]
	}
	if req.PID <= 1 {
		return
	}
	user, known := c.users[name]
	if !known {
		// The account was switched off between its login and its link coming up.
		c.kill(req.PID)
		return
	}
	c.sessions[req.PID] = &session{user: name, ifname: req.Interface}
	logger.Infof("l2tp: %s connected from %s on %s as %s", user.Name, req.Peer, req.Interface, req.Address)
}

func (c *control) sessionDown(req request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.sessions[req.PID]
	if !ok {
		return
	}
	c.chargeLocked(s, req.Received, req.Sent)
	delete(c.sessions, req.PID)
}

// chargeLocked bills what the counters grew by since the session was last read.
func (c *control) chargeLocked(s *session, rx, tx int64) {
	up, down := max(rx-s.rx, 0), max(tx-s.tx, 0)
	s.rx, s.tx = max(rx, s.rx), max(tx, s.tx)
	if up == 0 && down == 0 {
		return
	}
	total := c.unbilled[s.user]
	total.up += up
	total.down += down
	c.unbilled[s.user] = total
}

// setUsers replaces the account table and cuts the sessions of accounts it no longer holds.
func (c *control) setUsers(users []User) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.users = accountTable(users)
	for pid, s := range c.sessions {
		if _, served := c.users[s.user]; !served {
			logger.Infof("l2tp: cutting the session of %s on %s", s.user, s.ifname)
			c.kill(pid)
		}
	}
}

// drain returns each account's usage since the previous call, under the name
// the panel knows it by, and the accounts connected right now.
func (c *control) drain() (map[string]usage, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	connected := map[string]bool{}
	for pid, s := range c.sessions {
		// Without this an interface name reused by a later session would be billed to this one.
		if !c.alive(pid) {
			delete(c.sessions, pid)
			continue
		}
		if rx, tx, ok := c.counters(s.ifname); ok {
			c.chargeLocked(s, rx, tx)
		}
		connected[s.user] = true
	}
	used := make(map[string]usage, len(c.unbilled))
	for name, total := range c.unbilled {
		used[c.displayNameLocked(name)] = total
	}
	c.unbilled = map[string]usage{}
	online := make([]string, 0, len(connected))
	for name := range connected {
		online = append(online, c.displayNameLocked(name))
	}
	return used, online
}

func (c *control) displayNameLocked(name string) string {
	if user, ok := c.users[name]; ok {
		return user.Name
	}
	return name
}

// interfaceCounters reads what the peer sent (rx) and was sent (tx) on a ppp interface.
func interfaceCounters(ifname string) (rx, tx int64, ok bool) {
	read := func(counter string) (int64, bool) {
		raw, err := os.ReadFile("/sys/class/net/" + ifname + "/statistics/" + counter)
		if err != nil {
			return 0, false
		}
		n, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
		return n, err == nil
	}
	rx, okRx := read("rx_bytes")
	tx, okTx := read("tx_bytes")
	return rx, tx, okRx && okTx
}

func isPppd(pid int) bool {
	comm, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
	return err == nil && strings.TrimSpace(string(comm)) == "pppd"
}

// terminatePppd ends a session; a pid that no longer names a pppd is left alone.
func terminatePppd(pid int) {
	if isPppd(pid) {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
}
