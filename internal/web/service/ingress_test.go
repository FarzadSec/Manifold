package service

import (
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// A shared port and an inbound, the panel or another shared port can never both hold one
// TCP port; a UDP-only inbound on it is fine.
func TestSharedPortsAndInboundsNeverCollide(t *testing.T) {
	setupBulkDB(t)
	t.Cleanup(func() { ingressManager.Close() })
	svc := &IngressService{}
	mkInbound(t, 41443, model.VLESS, `{"clients":[]}`)
	err := svc.Save(&model.IngressListener{Enable: true, Port: 41443, DefaultTarget: "127.0.0.1:9"})
	if err == nil || !strings.Contains(err.Error(), "already listens on TCP port 41443") {
		t.Fatalf("save over a TCP inbound = %v", err)
	}
	mkInbound(t, 41444, model.Hysteria, `{"clients":[]}`)
	if err := svc.Save(&model.IngressListener{Enable: false, Port: 41444, DefaultTarget: "127.0.0.1:9"}); err != nil {
		t.Fatalf("a UDP-only inbound on the port refused the shared port: %v", err)
	}
	panelPort, _ := (&SettingService{}).GetPort()
	if err := svc.Save(&model.IngressListener{Port: panelPort, DefaultTarget: "127.0.0.1:9"}); err == nil || !strings.Contains(err.Error(), "the panel") {
		t.Fatalf("save on the panel's port = %v", err)
	}
	if err := svc.Save(&model.IngressListener{Port: 41444, DefaultTarget: "127.0.0.1:9"}); err == nil {
		t.Fatal("two shared ports on one port were saved")
	}

	ib := &model.Inbound{Tag: "late", Enable: true, Port: 41444, Protocol: model.VLESS, Settings: `{"clients":[]}`}
	conflict, err := checkPortConflictTx(database.GetDB(), ib, 0)
	if err != nil || conflict == nil || !strings.Contains(conflict.String(), "shared port") {
		t.Fatalf("an inbound on the shared port = %v, %v; want a conflict naming the shared port", conflict, err)
	}
}

// Saving binds the port at once and routes by name; deleting it frees the port.
func TestSavingASharedPortServesItAtOnce(t *testing.T) {
	setupBulkDB(t)
	t.Cleanup(func() { ingressManager.Close() })
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	got := make(chan string, 1)
	go func() {
		c, err := target.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 256)
		n, _ := io.ReadAtLeast(c, buf, 1)
		got <- string(buf[:n])
	}()
	port := freeTCPPort(t)
	row := &model.IngressListener{Enable: true, Listen: "127.0.0.1", Port: port, Routes: []model.IngressRoute{
		{Hosts: []string{"Site.Example.com."}, Target: target.Addr().String()},
	}}
	svc := &IngressService{}
	if err := svc.Save(row); err != nil {
		t.Fatal(err)
	}
	if row.Routes[0].Hosts[0] != "site.example.com" {
		t.Fatalf("host stored as %q", row.Routes[0].Hosts[0])
	}
	conn, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = conn.Write([]byte("GET / HTTP/1.1\r\nHost: site.example.com\r\n\r\n"))
	select {
	case req := <-got:
		if !strings.HasPrefix(req, "GET / HTTP/1.1") {
			t.Fatalf("target got %q", req)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the request never reached its target")
	}
	conn.Close()
	if err := svc.Delete(row.Id); err != nil {
		t.Fatal(err)
	}
	if ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port)); err != nil {
		t.Fatalf("the port is still held after delete: %v", err)
	} else {
		ln.Close()
	}
}
