package service

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/mhsanaei/3x-ui/v3/internal/nodessh"
)

// fakeServer is an SSH server that takes one script on stdin and answers with output.
type fakeServer struct {
	addr     string
	hostKey  string
	mu       sync.Mutex
	scripts  []string
	attempts int
}

func startFakeSSH(t *testing.T, password, output string, exit uint32) *fakeServer {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	fs := &fakeServer{hostKey: ssh.FingerprintSHA256(signer.PublicKey())}
	cfg := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
		fs.mu.Lock()
		fs.attempts++
		fs.mu.Unlock()
		if string(pw) != password {
			return nil, errors.New("denied")
		}
		return nil, nil
	}}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	fs.addr = ln.Addr().String()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go fs.serve(conn, cfg, output, exit)
		}
	}()
	return fs
}

func (fs *fakeServer) serve(conn net.Conn, cfg *ssh.ServerConfig, output string, exit uint32) {
	_, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		ch, requests, err := nc.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer ch.Close()
			for req := range requests {
				if req.Type != "exec" {
					_ = req.Reply(false, nil)
					continue
				}
				_ = req.Reply(true, nil)
				script, _ := io.ReadAll(ch)
				fs.mu.Lock()
				fs.scripts = append(fs.scripts, string(script))
				fs.mu.Unlock()
				_, _ = io.WriteString(ch, output)
				status := make([]byte, 4)
				binary.BigEndian.PutUint32(status, exit)
				_, _ = ch.SendRequest("exit-status", false, status)
				return
			}
		}()
	}
}

func testCertPEM(t *testing.T) (string, string) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "manifold-node"}, NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), base64.StdEncoding.EncodeToString(sum[:])
}

func waitInstall(t *testing.T, id string) NodeInstallJob {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := (&NodeService{}).InstallJob(id)
		if !ok {
			t.Fatalf("job %s vanished", id)
		}
		if job.State != "running" {
			return job
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("install did not finish")
	return NodeInstallJob{}
}

func installRequest(fs *fakeServer, hostKey string) NodeInstallRequest {
	host, port, _ := net.SplitHostPort(fs.addr)
	sshPort, _ := strconv.Atoi(port)
	return NodeInstallRequest{
		Name: "fresh", Address: "203.0.113.40", PanelPort: 2443,
		SSHHost: host, SSHPort: sshPort, SSHUser: "root", Password: "s3cret", HostKey: hostKey,
	}
}

// The install pins the certificate the server printed over the pinned SSH channel and keeps
// the token it minted, while neither the token nor the node's panel password reaches the log.
func TestNodeInstallPinsTheCertificateItMade(t *testing.T) {
	db := initTrafficTestDB(t)
	certPEM, wantPin := testCertPEM(t)
	output := "Server: Linux 6.8 x86_64\nPanel password: will-be-replaced\napiToken: tok-123456\n" + certMarker + "\n" + certPEM
	fs := startFakeSSH(t, "s3cret", output, 0)

	id, err := (&NodeService{}).StartInstall(installRequest(fs, fs.hostKey))
	if err != nil {
		t.Fatal(err)
	}
	job := waitInstall(t, id)
	if job.State != "done" || job.NodeId == 0 {
		t.Fatalf("job = %+v", job)
	}
	n, err := (&NodeService{}).GetById(job.NodeId)
	if err != nil {
		t.Fatal(err)
	}
	if n.ApiToken != "tok-123456" || n.TlsVerifyMode != "pin" || n.PinnedCertSha256 != wantPin || n.Scheme != "https" || n.Port != 2443 {
		t.Fatalf("node = token %q mode %q pin %q scheme %q port %d", n.ApiToken, n.TlsVerifyMode, n.PinnedCertSha256, n.Scheme, n.Port)
	}
	log := strings.Join(job.Log, "\n")
	if strings.Contains(log, "tok-123456") {
		t.Fatalf("the token reached the log:\n%s", log)
	}
	script := fs.scripts[0]
	if !strings.Contains(script, "--port '2443'") || !strings.Contains(script, "-tokenScope node-admin") || !strings.Contains(script, "subjectAltName=IP:203.0.113.40") {
		t.Fatalf("script lacks the port, token scope or address:\n%s", script)
	}
	pass := regexpFind(script, `--password '([0-9a-f]+)'`)
	if pass == "" || strings.Contains(log, pass) {
		t.Fatalf("the node's panel password %q is missing from the script or reached the log", pass)
	}
	var count int64
	db.Table("nodes").Count(&count)
	if count != 1 {
		t.Fatalf("nodes = %d, want 1", count)
	}
}

// A host key other than the one accepted ends the job before any credential is offered.
func TestNodeInstallRefusesAnUnacceptedHostKey(t *testing.T) {
	db := initTrafficTestDB(t)
	fs := startFakeSSH(t, "s3cret", "apiToken: x\n", 0)
	id, err := (&NodeService{}).StartInstall(installRequest(fs, "SHA256:someone-else"))
	if err != nil {
		t.Fatal(err)
	}
	job := waitInstall(t, id)
	if job.State != "failed" || !strings.Contains(job.Error, nodessh.ErrHostKeyMismatch.Error()) {
		t.Fatalf("job = %+v, want a host key failure", job)
	}
	if fs.attempts != 0 || len(fs.scripts) != 0 {
		t.Fatalf("the password was offered %d times and %d scripts ran on an unaccepted server", fs.attempts, len(fs.scripts))
	}
	var count int64
	db.Table("nodes").Count(&count)
	if count != 0 {
		t.Fatal("a node was added")
	}
}

// A script that fails adds no node, whatever it printed before failing.
func TestNodeInstallAddsNothingWhenTheScriptFails(t *testing.T) {
	db := initTrafficTestDB(t)
	certPEM, _ := testCertPEM(t)
	fs := startFakeSSH(t, "s3cret", "apiToken: tok\n"+certMarker+"\n"+certPEM, 1)
	id, _ := (&NodeService{}).StartInstall(installRequest(fs, fs.hostKey))
	if job := waitInstall(t, id); job.State != "failed" {
		t.Fatalf("job = %+v, want failed", job)
	}
	var count int64
	db.Table("nodes").Count(&count)
	if count != 0 {
		t.Fatal("a failed install added a node")
	}
}

// HostKey reads the fingerprint without ever authenticating.
func TestReadingTheHostKeyOffersNoCredential(t *testing.T) {
	fs := startFakeSSH(t, "s3cret", "", 0)
	host, port, _ := net.SplitHostPort(fs.addr)
	p, _ := strconv.Atoi(port)
	got, err := nodessh.HostKey(t.Context(), host, p)
	if err != nil || got != fs.hostKey {
		t.Fatalf("host key = %q, %v; want %q", got, err, fs.hostKey)
	}
	if fs.attempts != 0 {
		t.Fatal("reading the host key tried to log in")
	}
}

func regexpFind(s, pattern string) string {
	m := regexp.MustCompile(pattern).FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return m[1]
}
