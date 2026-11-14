package openvpn

import (
	"bufio"
	"crypto/subtle"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

const (
	byteCountInterval = 5

	// A login that never completes its handshake sends no disconnect, so its
	// slot is given back after OpenVPN's own handshake window has passed.
	pendingSessionTTL = 90 * time.Second

	// The texts an app shows its user; they must not tell a wrong password from a disabled account.
	deniedLogin  = "Access denied. Check the account and its password."
	deniedDevice = "Access denied. This account is connected on its maximum number of devices."
)

type session struct {
	user        string
	established bool
	closing     bool
	authorized  time.Time
	in, out     int64
}

type usage struct{ up, down int64 }

// control is the panel's side of one openvpn management connection: it answers
// every login, tracks the live sessions and gathers their traffic.
type control struct {
	conn io.ReadWriteCloser
	now  func() time.Time

	writeMu sync.Mutex

	mu       sync.Mutex
	users    map[string]User
	sessions map[uint64]*session
	unbilled map[string]usage

	ready     chan struct{}
	readyOnce sync.Once
	done      chan struct{}
}

func newControl(conn io.ReadWriteCloser, users []User) *control {
	c := &control{
		conn:     conn,
		now:      time.Now,
		users:    map[string]User{},
		sessions: map[uint64]*session{},
		unbilled: map[string]usage{},
		ready:    make(chan struct{}),
		done:     make(chan struct{}),
	}
	for _, u := range users {
		c.users[u.Name] = u
	}
	return c
}

func (c *control) send(command string) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, _ = io.WriteString(c.conn, command+"\n")
}

func (c *control) close() { _ = c.conn.Close() }

type clientEvent struct {
	kind     string
	cid, kid uint64
	env      map[string]string
}

// The only values the panel acts on; the rest of a client's environment is dropped.
var keptEnv = map[string]bool{"username": true, "password": true, "bytes_received": true, "bytes_sent": true}

// parseClientEvent reads the first line of a multi-line client notification.
func parseClientEvent(line string) *clientEvent {
	kind, rest, _ := strings.Cut(strings.TrimPrefix(line, ">CLIENT:"), ",")
	ids := strings.Split(rest, ",")
	ev := &clientEvent{kind: kind, env: map[string]string{}}
	var err error
	switch kind {
	case "CONNECT", "REAUTH":
		if len(ids) != 2 {
			return nil
		}
		if ev.kid, err = strconv.ParseUint(ids[1], 10, 64); err != nil {
			return nil
		}
	case "ESTABLISHED", "DISCONNECT":
		if len(ids) != 1 {
			return nil
		}
	default:
		return nil
	}
	if ev.cid, err = strconv.ParseUint(ids[0], 10, 64); err != nil {
		return nil
	}
	return ev
}

// serve reads the management connection until it closes.
func (c *control) serve() {
	defer close(c.done)
	c.send("state on")
	c.send(fmt.Sprintf("bytecount %d", byteCountInterval))

	reader := bufio.NewReaderSize(c.conn, 64*1024)
	var ev *clientEvent
	for {
		raw, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line := strings.TrimRight(raw, "\r\n")
		switch {
		case strings.HasPrefix(line, ">CLIENT:ENV,"):
			if ev == nil {
				continue
			}
			pair := strings.TrimPrefix(line, ">CLIENT:ENV,")
			if pair == "END" {
				c.handle(ev)
				ev = nil
				continue
			}
			if key, value, _ := strings.Cut(pair, "="); keptEnv[key] {
				ev.env[key] = value
			}
		case strings.HasPrefix(line, ">CLIENT:"):
			ev = parseClientEvent(line)
		case strings.HasPrefix(line, ">BYTECOUNT_CLI:"):
			c.recordByteCount(strings.TrimPrefix(line, ">BYTECOUNT_CLI:"))
		case strings.HasPrefix(line, ">HOLD:"):
			c.send("hold release")
		case strings.HasPrefix(line, ">STATE:"):
			if strings.Contains(line, ",CONNECTED,") {
				c.readyOnce.Do(func() { close(c.ready) })
			}
		case strings.HasPrefix(line, ">FATAL:"):
			logger.Warning("openvpn: ", strings.TrimPrefix(line, ">FATAL:"))
		}
	}
}

func (c *control) handle(ev *clientEvent) {
	switch ev.kind {
	case "CONNECT", "REAUTH":
		if reason := c.authorize(ev.cid, ev.env["username"], ev.env["password"]); reason != "" {
			c.send(fmt.Sprintf("client-deny %d %d %q %q", ev.cid, ev.kid, "refused by the panel", reason))
			return
		}
		c.send(fmt.Sprintf("client-auth-nt %d %d", ev.cid, ev.kid))
	case "ESTABLISHED":
		c.mu.Lock()
		if s := c.sessions[ev.cid]; s != nil {
			s.established = true
		}
		c.mu.Unlock()
	case "DISCONNECT":
		in, errIn := strconv.ParseInt(ev.env["bytes_received"], 10, 64)
		out, errOut := strconv.ParseInt(ev.env["bytes_sent"], 10, 64)
		c.mu.Lock()
		if errIn == nil && errOut == nil {
			c.billLocked(ev.cid, in, out)
		}
		delete(c.sessions, ev.cid)
		c.mu.Unlock()
	}
}

// authorize records the session and returns "", or the reason to refuse it.
func (c *control) authorize(cid uint64, username, password string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	user, known := c.users[username]
	if !known || subtle.ConstantTimeCompare([]byte(user.Password), []byte(password)) != 1 {
		delete(c.sessions, cid)
		return deniedLogin
	}
	if existing := c.sessions[cid]; existing != nil && existing.user == username {
		return ""
	}
	if user.MaxSessions > 0 && c.liveSessionsLocked(username, cid) >= user.MaxSessions {
		return deniedDevice
	}
	c.sessions[cid] = &session{user: username, authorized: c.now()}
	return ""
}

// liveSessionsLocked counts the user's sessions other than cid, dropping
// logins that never finished.
func (c *control) liveSessionsLocked(username string, except uint64) int {
	count := 0
	for cid, s := range c.sessions {
		if !s.established && c.now().Sub(s.authorized) > pendingSessionTTL {
			delete(c.sessions, cid)
			continue
		}
		if cid != except && s.user == username && !s.closing {
			count++
		}
	}
	return count
}

func (c *control) recordByteCount(fields string) {
	parts := strings.Split(fields, ",")
	if len(parts) != 3 {
		return
	}
	cid, errCid := strconv.ParseUint(parts[0], 10, 64)
	in, errIn := strconv.ParseInt(parts[1], 10, 64)
	out, errOut := strconv.ParseInt(parts[2], 10, 64)
	if errCid != nil || errIn != nil || errOut != nil {
		return
	}
	c.mu.Lock()
	c.billLocked(cid, in, out)
	c.mu.Unlock()
}

// billLocked charges a session's owner for what its counters grew by. What the
// server received is the account's upload.
func (c *control) billLocked(cid uint64, in, out int64) {
	s := c.sessions[cid]
	if s == nil {
		return
	}
	up, down := max(in-s.in, 0), max(out-s.out, 0)
	s.in, s.out = max(in, s.in), max(out, s.out)
	if up == 0 && down == 0 {
		return
	}
	total := c.unbilled[s.user]
	total.up += up
	total.down += down
	c.unbilled[s.user] = total
}

// setUsers replaces the accounts and disconnects every session the new set no
// longer allows: a removed account, a changed password, a lowered device cap.
func (c *control) setUsers(users []User) {
	next := make(map[string]User, len(users))
	for _, u := range users {
		next[u.Name] = u
	}
	c.mu.Lock()
	var kill []uint64
	byUser := map[string][]uint64{}
	for cid, s := range c.sessions {
		if s.closing {
			continue
		}
		user, kept := next[s.user]
		if !kept || user.Password != c.users[s.user].Password {
			kill = append(kill, cid)
			s.closing = true
			continue
		}
		byUser[s.user] = append(byUser[s.user], cid)
	}
	for name, cids := range byUser {
		limit := next[name].MaxSessions
		if limit <= 0 || len(cids) <= limit {
			continue
		}
		// Client ids grow with every connection, so the newest sessions go first.
		slices.Sort(cids)
		for _, cid := range cids[limit:] {
			kill = append(kill, cid)
			c.sessions[cid].closing = true
		}
	}
	c.users = next
	c.mu.Unlock()

	slices.Sort(kill)
	for _, cid := range kill {
		c.send(fmt.Sprintf("client-kill %d", cid))
	}
}

// drain returns the traffic gathered since the last call and the accounts
// that are connected right now.
func (c *control) drain() (map[string]usage, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	traffic := c.unbilled
	c.unbilled = map[string]usage{}
	seen := map[string]bool{}
	var online []string
	for _, s := range c.sessions {
		if s.established && !s.closing && !seen[s.user] {
			seen[s.user] = true
			online = append(online, s.user)
		}
	}
	slices.Sort(online)
	return traffic, online
}
