package openvpn

import (
	"bufio"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fakeServer plays the openvpn side of a management connection.
type fakeServer struct {
	t     *testing.T
	conn  net.Conn
	lines chan string
	ctl   *control
	clock time.Time
}

func newFakeServer(t *testing.T, users ...User) *fakeServer {
	t.Helper()
	panelSide, serverSide := net.Pipe()
	f := &fakeServer{t: t, conn: serverSide, lines: make(chan string, 64), clock: time.Unix(1_800_000_000, 0)}
	go func() {
		scanner := bufio.NewScanner(serverSide)
		for scanner.Scan() {
			f.lines <- scanner.Text()
		}
		close(f.lines)
	}()
	f.ctl = newControl(panelSide, users)
	f.ctl.now = func() time.Time { return f.clock }
	go f.ctl.serve()
	t.Cleanup(func() {
		_ = serverSide.Close()
		f.ctl.close()
	})
	f.expect("state on")
	f.expect(fmt.Sprintf("bytecount %d", byteCountInterval))
	return f
}

func (f *fakeServer) emit(lines ...string) {
	f.t.Helper()
	for _, line := range lines {
		if _, err := f.conn.Write([]byte(line + "\r\n")); err != nil {
			f.t.Fatalf("writing %q: %v", line, err)
		}
	}
}

func (f *fakeServer) expect(want string) {
	f.t.Helper()
	select {
	case got, ok := <-f.lines:
		if !ok || got != want {
			f.t.Fatalf("panel sent %q, want %q", got, want)
		}
	case <-time.After(3 * time.Second):
		f.t.Fatalf("panel sent nothing, want %q", want)
	}
}

// settle returns once the panel has processed everything emitted so far.
func (f *fakeServer) settle() {
	f.t.Helper()
	f.emit(">HOLD:Waiting for hold release:0")
	f.expect("hold release")
}

func (f *fakeServer) login(cid int, username, password string) {
	f.t.Helper()
	f.emit(
		fmt.Sprintf(">CLIENT:CONNECT,%d,1", cid),
		">CLIENT:ENV,untrusted_ip=203.0.113.9",
		">CLIENT:ENV,password="+password,
		">CLIENT:ENV,username="+username,
		">CLIENT:ENV,END",
	)
}

func (f *fakeServer) connect(cid int, username, password string) {
	f.t.Helper()
	f.login(cid, username, password)
	f.expect(fmt.Sprintf("client-auth-nt %d 1", cid))
	f.emit(fmt.Sprintf(">CLIENT:ESTABLISHED,%d", cid), ">CLIENT:ENV,common_name="+username, ">CLIENT:ENV,END")
}

func (f *fakeServer) disconnect(cid int, received, sent int64) {
	f.t.Helper()
	f.emit(
		fmt.Sprintf(">CLIENT:DISCONNECT,%d", cid),
		fmt.Sprintf(">CLIENT:ENV,bytes_sent=%d", sent),
		fmt.Sprintf(">CLIENT:ENV,bytes_received=%d", received),
		">CLIENT:ENV,END",
	)
}

func denied(cid int, reason string) string {
	return fmt.Sprintf("client-deny %d 1 %q %q", cid, "refused by the panel", reason)
}

func TestLoginIsDecidedByAccountAndPassword(t *testing.T) {
	tests := []struct {
		name               string
		username, password string
		want               string
	}{
		{name: "right password", username: "alice", password: "s3cret=,x", want: "client-auth-nt 7 1"},
		{name: "wrong password", username: "alice", password: "s3cret", want: denied(7, deniedLogin)},
		{name: "password that only shares a prefix", username: "alice", password: "s3cret=,xx", want: denied(7, deniedLogin)},
		{name: "account the inbound does not have", username: "mallory", password: "s3cret=,x", want: denied(7, deniedLogin)},
		{name: "no credentials at all", want: denied(7, deniedLogin)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeServer(t, User{Name: "alice", Password: "s3cret=,x"})
			f.login(7, tt.username, tt.password)
			f.expect(tt.want)
		})
	}
}

func TestDeviceCapCountsOnlyLiveSessions(t *testing.T) {
	f := newFakeServer(t, User{Name: "alice", Password: "pw", MaxSessions: 2})
	f.connect(1, "alice", "pw")
	f.connect(2, "alice", "pw")

	f.login(3, "alice", "pw")
	f.expect(denied(3, deniedDevice))

	// A renegotiation of a session that already holds a slot must not be counted against itself.
	f.emit(">CLIENT:REAUTH,2,2", ">CLIENT:ENV,username=alice", ">CLIENT:ENV,password=pw", ">CLIENT:ENV,END")
	f.expect("client-auth-nt 2 2")

	f.disconnect(1, 0, 0)
	f.login(4, "alice", "pw")
	f.expect("client-auth-nt 4 1")
}

func TestUnfinishedLoginGivesItsSlotBack(t *testing.T) {
	f := newFakeServer(t, User{Name: "alice", Password: "pw", MaxSessions: 1})
	f.login(1, "alice", "pw")
	f.expect("client-auth-nt 1 1")

	f.login(2, "alice", "pw")
	f.expect(denied(2, deniedDevice))

	f.clock = f.clock.Add(pendingSessionTTL + time.Second)
	f.login(3, "alice", "pw")
	f.expect("client-auth-nt 3 1")
}

func TestTrafficIsBilledOncePerByte(t *testing.T) {
	f := newFakeServer(t, User{Name: "alice", Password: "pw"}, User{Name: "bob", Password: "pw"})
	f.connect(1, "alice", "pw")
	f.connect(2, "bob", "pw")
	f.emit(">BYTECOUNT_CLI:1,1000,5000", ">BYTECOUNT_CLI:2,10,20", ">BYTECOUNT_CLI:9,777,777")
	f.settle()

	used, online := f.ctl.drain()
	wantUsed := map[string]usage{"alice": {up: 1000, down: 5000}, "bob": {up: 10, down: 20}}
	if !reflect.DeepEqual(used, wantUsed) {
		t.Fatalf("first drain = %+v, want %+v", used, wantUsed)
	}
	if want := []string{"alice", "bob"}; !reflect.DeepEqual(online, want) {
		t.Fatalf("online = %v, want %v", online, want)
	}

	// The counters are cumulative: only their growth, and the tail a disconnect reports, is new.
	f.emit(">BYTECOUNT_CLI:1,1500,5000")
	f.disconnect(1, 1800, 5600)
	f.settle()

	used, online = f.ctl.drain()
	wantUsed = map[string]usage{"alice": {up: 800, down: 600}}
	if !reflect.DeepEqual(used, wantUsed) {
		t.Fatalf("second drain = %+v, want %+v", used, wantUsed)
	}
	if want := []string{"bob"}; !reflect.DeepEqual(online, want) {
		t.Fatalf("online after disconnect = %v, want %v", online, want)
	}
	if used, _ = f.ctl.drain(); len(used) != 0 {
		t.Fatalf("third drain = %+v, want nothing", used)
	}
}

func TestChangedAccountsDisconnectWhatTheyNoLongerAllow(t *testing.T) {
	f := newFakeServer(t,
		User{Name: "removed", Password: "pw"},
		User{Name: "rekeyed", Password: "old"},
		User{Name: "capped", Password: "pw"},
		User{Name: "kept", Password: "pw"},
	)
	f.connect(1, "removed", "pw")
	f.connect(2, "rekeyed", "old")
	f.connect(3, "capped", "pw")
	f.connect(4, "capped", "pw")
	f.connect(5, "kept", "pw")
	f.settle()

	f.ctl.setUsers([]User{
		{Name: "rekeyed", Password: "new"},
		{Name: "capped", Password: "pw", MaxSessions: 1},
		{Name: "kept", Password: "pw"},
	})
	f.expect("client-kill 1")
	f.expect("client-kill 2")
	f.expect("client-kill 4")

	// A cut session reconnects at once; the panel must already refuse it.
	f.login(6, "removed", "pw")
	f.expect(denied(6, deniedLogin))
	f.login(7, "rekeyed", "old")
	f.expect(denied(7, deniedLogin))

	_, online := f.ctl.drain()
	if want := []string{"capped", "kept"}; !reflect.DeepEqual(online, want) {
		t.Fatalf("online = %v, want %v", online, want)
	}
}

func TestServerIsReadyOnlyOnceItReportsConnected(t *testing.T) {
	f := newFakeServer(t, User{Name: "alice", Password: "pw"})
	f.emit(">INFO:OpenVPN Management Interface Version 5 -- type 'help' for more info")
	f.settle()
	select {
	case <-f.ctl.ready:
		t.Fatal("ready before the server reported its state")
	default:
	}

	f.emit(">STATE:1794674389,ASSIGN_IP,,10.8.0.1,,,,", ">STATE:1794674389,CONNECTED,SUCCESS,10.8.0.1,,,,")
	select {
	case <-f.ctl.ready:
	case <-time.After(3 * time.Second):
		t.Fatal("not ready after the server reported CONNECTED")
	}
}

func TestMalformedNotificationsAreIgnored(t *testing.T) {
	f := newFakeServer(t, User{Name: "alice", Password: "pw"})
	f.emit(
		">CLIENT:CONNECT,notanumber,1",
		">CLIENT:ENV,username=alice",
		">CLIENT:ENV,password=pw",
		">CLIENT:ENV,END",
		">BYTECOUNT_CLI:1,2",
		">CLIENT:ADDRESS,1,10.8.0.2,1",
		strings.Repeat("x", 1024),
	)
	f.settle()
	if used, online := f.ctl.drain(); len(used) != 0 || len(online) != 0 {
		t.Fatalf("drain = %+v %v, want nothing", used, online)
	}
}
