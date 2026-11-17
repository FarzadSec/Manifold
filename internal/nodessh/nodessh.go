// Package nodessh runs commands on a server over SSH for the panel's node installer: the
// host key is pinned before any credential is sent, and output is streamed line by line.
package nodessh

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Target is a server to log in to. HostKey is the SHA256 fingerprint the admin accepted.
type Target struct {
	Host       string
	Port       int
	User       string
	Password   string
	PrivateKey string
	HostKey    string
}

var (
	ErrHostKeyMismatch = errors.New("the server's SSH host key is not the one accepted")
	ErrNoCredential    = errors.New("an SSH password or private key is needed")
	errFingerprintOnly = errors.New("host key read")
)

func (t Target) addr() string {
	port := t.Port
	if port <= 0 {
		port = 22
	}
	return net.JoinHostPort(t.Host, strconv.Itoa(port))
}

// HostKey reads the server's host key fingerprint without logging in: the handshake stops
// at the key, so no password or key is ever offered to a server not yet accepted.
func HostKey(ctx context.Context, host string, port int) (string, error) {
	var seen string
	cfg := &ssh.ClientConfig{
		User: "manifold",
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			seen = ssh.FingerprintSHA256(key)
			return errFingerprintOnly
		},
		Timeout: 15 * time.Second,
	}
	conn, err := dial(ctx, Target{Host: host, Port: port}.addr())
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_, _, _, err = ssh.NewClientConn(conn, "", cfg)
	if seen != "" {
		return seen, nil
	}
	if err == nil {
		err = errors.New("the server sent no host key")
	}
	return "", err
}

func dial(ctx context.Context, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: 15 * time.Second}
	return d.DialContext(ctx, "tcp", addr)
}

// Dial logs in once the host key matches t.HostKey.
func Dial(ctx context.Context, t Target) (*ssh.Client, error) {
	var auth []ssh.AuthMethod
	if strings.TrimSpace(t.PrivateKey) != "" {
		signer, err := ssh.ParsePrivateKey([]byte(t.PrivateKey))
		if err != nil {
			return nil, fmt.Errorf("private key: %w", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if t.Password != "" {
		auth = append(auth, ssh.Password(t.Password))
	}
	if len(auth) == 0 {
		return nil, ErrNoCredential
	}
	want := strings.TrimSpace(t.HostKey)
	cfg := &ssh.ClientConfig{
		User: t.User,
		Auth: auth,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if want == "" || ssh.FingerprintSHA256(key) != want {
				return ErrHostKeyMismatch
			}
			return nil
		},
		Timeout: 15 * time.Second,
	}
	conn, err := dial(ctx, t.addr())
	if err != nil {
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, t.addr(), cfg)
	if err != nil {
		conn.Close()
		if errors.Is(err, ErrHostKeyMismatch) {
			return nil, ErrHostKeyMismatch
		}
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}

// RunScript runs script with bash on the server, sending each output line to line as it
// comes; the error carries the exit status when the script fails.
func RunScript(ctx context.Context, client *ssh.Client, script string, line func(string)) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	session.Stdin = strings.NewReader(script)
	stdout, err := session.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return err
	}
	var mu sync.Mutex
	emit := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		line(s)
	}
	var wg sync.WaitGroup
	for _, r := range []io.Reader{stdout, stderr} {
		wg.Add(1)
		go func(r io.Reader) {
			defer wg.Done()
			sc := bufio.NewScanner(r)
			sc.Buffer(make([]byte, 64*1024), 1024*1024)
			for sc.Scan() {
				emit(sc.Text())
			}
		}(r)
	}
	if err := session.Start("bash -s"); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- session.Wait() }()
	select {
	case err = <-done:
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGTERM)
		_ = client.Close()
		err = ctx.Err()
	}
	wg.Wait()
	return err
}

// Quote makes s one shell word.
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
