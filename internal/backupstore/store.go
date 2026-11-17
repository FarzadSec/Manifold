package backupstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/mhsanaei/3x-ui/v3/internal/nodessh"
)

// File is one stored backup.
type File struct {
	Name string `json:"name" example:"manifold-backup-panel-20261117-030000.tar.gz.enc"`
	Size int64  `json:"size" example:"1048576"`
}

// Store is somewhere backups are kept.
type Store interface {
	Put(ctx context.Context, name string, data []byte) error
	List(ctx context.Context) ([]File, error)
	Delete(ctx context.Context, name string) error
}

var backupName = regexp.MustCompile(`^manifold-backup-[A-Za-z0-9._-]+-\d{8}-\d{6}\.tar\.gz(\.enc)?$`)

// Name is a backup's file name: sortable by time, and recognised by the retention.
func Name(host string, at time.Time, encrypted bool) string {
	host = regexp.MustCompile(`[^A-Za-z0-9.-]+`).ReplaceAllString(host, "-")
	host = strings.Trim(host, "-.")
	if host == "" {
		host = "panel"
	}
	name := "manifold-backup-" + host + "-" + at.UTC().Format("20060102-150405") + ".tar.gz"
	if encrypted {
		name += ".enc"
	}
	return name
}

func stamp(name string) string {
	m := regexp.MustCompile(`-(\d{8}-\d{6})\.tar\.gz`).FindStringSubmatch(name)
	if m == nil {
		return ""
	}
	return m[1]
}

// Prune deletes the panel's own backups beyond the newest keep; other files are never touched.
func Prune(ctx context.Context, s Store, keep int) ([]string, error) {
	if keep <= 0 {
		return nil, nil
	}
	files, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	var ours []string
	for _, f := range files {
		if backupName.MatchString(f.Name) {
			ours = append(ours, f.Name)
		}
	}
	sort.Slice(ours, func(i, j int) bool { return stamp(ours[i]) > stamp(ours[j]) })
	var deleted []string
	for _, name := range ours[min(keep, len(ours)):] {
		if err := s.Delete(ctx, name); err != nil {
			return deleted, err
		}
		deleted = append(deleted, name)
	}
	return deleted, nil
}

// Local keeps backups in a directory of this server.
type Local struct{ Dir string }

func (l Local) Put(_ context.Context, name string, data []byte) error {
	if err := os.MkdirAll(l.Dir, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(l.Dir, "."+name+".part")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(l.Dir, name))
}

func (l Local) List(_ context.Context) ([]File, error) {
	entries, err := os.ReadDir(l.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []File
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() {
			out = append(out, File{Name: e.Name(), Size: info.Size()})
		}
	}
	return out, nil
}

func (l Local) Delete(_ context.Context, name string) error {
	if !backupName.MatchString(name) {
		return fmt.Errorf("%q is not a panel backup", name)
	}
	return os.Remove(filepath.Join(l.Dir, name))
}

// SSH keeps backups in a directory of another server, written through its shell.
type SSH struct {
	Target nodessh.Target
	Dir    string
}

func (s SSH) run(ctx context.Context, script string, stdin []byte) (string, error) {
	client, err := nodessh.Dial(ctx, s.Target)
	if err != nil {
		return "", err
	}
	defer client.Close()
	return runWith(client, script, stdin)
}

func runWith(client *ssh.Client, cmd string, stdin []byte) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	var out, errOut bytes.Buffer
	session.Stdout, session.Stderr = &out, &errOut
	session.Stdin = bytes.NewReader(stdin)
	if err := session.Run(cmd); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(errOut.String()))
	}
	return out.String(), nil
}

func (s SSH) dir() string {
	if strings.TrimSpace(s.Dir) == "" {
		return "manifold-backups"
	}
	return s.Dir
}

func (s SSH) Put(ctx context.Context, name string, data []byte) error {
	dir, q := s.dir(), nodessh.Quote
	part := dir + "/." + name + ".part"
	_, err := s.run(ctx, "mkdir -p "+q(dir)+" && cat > "+q(part)+" && mv -f "+q(part)+" "+q(dir+"/"+name), data)
	return err
}

func (s SSH) List(ctx context.Context) ([]File, error) {
	out, err := s.run(ctx, "mkdir -p "+nodessh.Quote(s.dir())+" && cd "+nodessh.Quote(s.dir())+" && for f in manifold-backup-*; do [ -f \"$f\" ] && printf '%s %s\\n' \"$(wc -c < \"$f\")\" \"$f\"; done; true", nil)
	if err != nil {
		return nil, err
	}
	var files []File
	for _, line := range strings.Split(out, "\n") {
		size, name, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		var n int64
		_, _ = fmt.Sscan(size, &n)
		files = append(files, File{Name: name, Size: n})
	}
	return files, nil
}

func (s SSH) Delete(ctx context.Context, name string) error {
	if !backupName.MatchString(name) {
		return fmt.Errorf("%q is not a panel backup", name)
	}
	_, err := s.run(ctx, "rm -f "+nodessh.Quote(s.dir()+"/"+name), nil)
	return err
}
