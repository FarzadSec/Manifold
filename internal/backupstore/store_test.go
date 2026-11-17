package backupstore

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/mhsanaei/3x-ui/v3/internal/nodessh"
)

// AWS's own worked example ("GET Object", Signature Version 4 header auth).
func TestS3SignatureMatchesAWSExample(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	req.Header.Set("Range", "bytes=0-9")
	signV4(req, emptySHA256, "AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1", "s3",
		time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request," +
		"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date," +
		"Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("Authorization =\n%s\nwant\n%s", got, want)
	}
}

// fakeBucket is a path-style S3 bucket that refuses unsigned requests.
func fakeBucket(t *testing.T, bucket string) (*httptest.Server, map[string][]byte) {
	t.Helper()
	objects := map[string][]byte{}
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=AK/") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		path := strings.TrimPrefix(r.URL.Path, "/"+bucket)
		switch {
		case r.Method == http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			objects[strings.TrimPrefix(path, "/")] = body
		case r.Method == http.MethodDelete:
			delete(objects, strings.TrimPrefix(path, "/"))
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
			prefix := r.URL.Query().Get("prefix")
			var keys []string
			for k := range objects {
				if strings.HasPrefix(k, prefix) {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			io.WriteString(w, "<ListBucketResult>")
			for _, k := range keys {
				io.WriteString(w, "<Contents><Key>"+k+"</Key><Size>"+strconv.Itoa(len(objects[k]))+"</Size></Contents>")
			}
			io.WriteString(w, "<IsTruncated>false</IsTruncated></ListBucketResult>")
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, objects
}

func storeBackups(t *testing.T, s Store, host string, n int) []string {
	t.Helper()
	var names []string
	base := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)
	for i := range n {
		name := Name(host, base.Add(time.Duration(i)*24*time.Hour), true)
		if err := s.Put(context.Background(), name, []byte("backup "+strconv.Itoa(i))); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	return names
}

// Retention keeps the newest backups and never deletes a file the panel did not write.
func TestRetentionKeepsTheNewestAndLeavesOtherFiles(t *testing.T) {
	srv, objects := fakeBucket(t, "bk")
	stores := map[string]Store{
		"local": Local{Dir: t.TempDir()},
		"s3":    &S3{Endpoint: srv.URL, Bucket: "bk", Prefix: "panels/fra", AccessKey: "AK", SecretKey: "SK", Client: srv.Client()},
	}
	for kind, s := range stores {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			names := storeBackups(t, s, "panel.example.com", 5)
			if err := s.Put(ctx, "notes.txt", []byte("mine")); err != nil {
				t.Fatal(err)
			}
			deleted, err := Prune(ctx, s, 2)
			if err != nil {
				t.Fatal(err)
			}
			if len(deleted) != 3 {
				t.Fatalf("deleted %v, want the three oldest", deleted)
			}
			files, _ := s.List(ctx)
			var left []string
			for _, f := range files {
				left = append(left, f.Name)
			}
			sort.Strings(left)
			want := []string{names[3], names[4], "notes.txt"}
			sort.Strings(want)
			if strings.Join(left, ",") != strings.Join(want, ",") {
				t.Fatalf("left %v, want %v", left, want)
			}
		})
	}
	if _, ok := objects["panels/fra/"+Name("panel.example.com", time.Date(2026, 11, 5, 3, 0, 0, 0, time.UTC), true)]; !ok {
		t.Fatalf("objects are not under the prefix: %v", objects)
	}
}

// A shell-backed SSH server, so the store's commands really run.
func shellSSH(t *testing.T) nodessh.Target {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is needed")
	}
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	cfg := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
		return nil, nil
	}}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	home := t.TempDir()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(conn, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for nc := range chans {
					ch, requests, _ := nc.Accept()
					go func() {
						defer ch.Close()
						for req := range requests {
							if req.Type != "exec" {
								_ = req.Reply(false, nil)
								continue
							}
							_ = req.Reply(true, nil)
							cmdLen := binary.BigEndian.Uint32(req.Payload[:4])
							cmd := exec.Command("bash", "-c", string(req.Payload[4:4+cmdLen]))
							cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = home, ch, ch, ch.Stderr()
							code := 0
							if err := cmd.Run(); err != nil {
								code = 1
							}
							status := make([]byte, 4)
							binary.BigEndian.PutUint32(status, uint32(code))
							_, _ = ch.SendRequest("exit-status", false, status)
							return
						}
					}()
				}
			}()
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	p, _ := strconv.Atoi(port)
	t.Setenv("MANIFOLD_TEST_SSH_HOME", home)
	return nodessh.Target{Host: host, Port: p, User: "u", Password: "p", HostKey: ssh.FingerprintSHA256(signer.PublicKey())}
}

// Backups over SSH land whole in the directory, and retention prunes them there.
func TestSSHStoreWritesListsAndPrunes(t *testing.T) {
	target := shellSSH(t)
	s := SSH{Target: target, Dir: "backups dir"}
	ctx := context.Background()
	names := storeBackups(t, s, "panel", 3)
	data, err := os.ReadFile(filepath.Join(os.Getenv("MANIFOLD_TEST_SSH_HOME"), "backups dir", names[2]))
	if err != nil || string(data) != "backup 2" {
		t.Fatalf("stored file = %q, %v", data, err)
	}
	deleted, err := Prune(ctx, s, 1)
	if err != nil || len(deleted) != 2 {
		t.Fatalf("prune = %v, %v", deleted, err)
	}
	files, err := s.List(ctx)
	if err != nil || len(files) != 1 || files[0].Name != names[2] || files[0].Size != int64(len("backup 2")) {
		t.Fatalf("left %+v, %v", files, err)
	}
}
