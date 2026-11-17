package tunnel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

// ErrNoBackPack is returned when the package that brings the backpack program is not installed.
var ErrNoBackPack = errors.New("tunnel: the backpack program is missing from the bin folder; reinstall Manifold")

// BackPackPath is where the panel keeps the unmodified BackPack release binary.
var BackPackPath = func() string {
	return filepath.Join(config.GetBinFolderPath(), "backpack-linux-"+runtime.GOARCH)
}

// tomlString writes a TOML basic string; JSON's escapes are all valid there.
func tomlString(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

func tomlList(items []string) string {
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = tomlString(item)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// mappings renders forwards in BackPack's port syntax; a reverse tunnel names the
// loopback of the far end, a layer-3 one leaves the host to mean the peer.
func mappings(t Tunnel) []string {
	out := make([]string, 0, len(t.Settings.Forwards))
	for _, f := range t.Settings.Forwards {
		target := f.Listen
		if f.Target != "" {
			target = f.Target
		}
		switch {
		case t.Kind == KindReverse && strings.Contains(target, "-"):
			out = append(out, f.Listen+"="+target)
		case t.Kind == KindReverse:
			out = append(out, f.Listen+"=127.0.0.1:"+target)
		case f.Target != "":
			out = append(out, f.Listen+"="+target)
		default:
			out = append(out, f.Listen)
		}
	}
	return out
}

func acceptsUDP(t Tunnel) bool {
	for _, f := range t.Settings.Forwards {
		if f.Proto == ProtoBoth || f.Proto == ProtoUDP {
			return true
		}
	}
	return false
}

// renderBackPack writes the configuration of one end, as `backpack -c` reads it.
func renderBackPack(t Tunnel) string {
	s := t.Settings
	var b strings.Builder
	line := func(key, value string) { fmt.Fprintf(&b, "%s = %s\n", key, value) }
	fmt.Fprintf(&b, "# Written by Manifold for tunnel %d. Changes made here are overwritten.\n", t.ID)
	listenAll := "0.0.0.0:" + strconv.Itoa(s.Port)
	dial := net.JoinHostPort(s.RemoteAddr, strconv.Itoa(s.Port))
	switch {
	case t.Kind == KindReverse && t.Role == RoleServer:
		b.WriteString("[server]\n")
		line("bind_addr", tomlString(listenAll))
		line("transport", tomlString(s.Transport))
		line("token", tomlString(s.Token))
		line("ports", tomlList(mappings(t)))
		line("accept_udp", strconv.FormatBool(acceptsUDP(t)))
		line("proxy_protocol", strconv.FormatBool(s.ProxyProtocol))
	case t.Kind == KindReverse:
		b.WriteString("[client]\n")
		line("remote_addr", tomlString(dial))
		line("transport", tomlString(s.Transport))
		line("token", tomlString(s.Token))
	default:
		b.WriteString("[l3]\n")
		if s.Dialer == t.Role {
			line("mode", tomlString("dial"))
			line("addr", tomlString(dial))
		} else {
			line("mode", tomlString("listen"))
			line("addr", tomlString(listenAll))
		}
		line("token", tomlString(s.Token))
		line("carrier", tomlString(s.Carrier))
		line("iface", tomlString(t.Device()))
		line("local_ip", tomlString(s.LocalIP))
		line("peer_ip", tomlString(s.PeerIP))
		if s.MTU > 0 {
			line("mtu", strconv.Itoa(s.MTU))
		}
		if t.Role == RoleServer && len(s.Forwards) > 0 {
			line("ports", tomlList(mappings(t)))
			line("accept_udp", strconv.FormatBool(acceptsUDP(t)))
		}
		return b.String()
	}
	// The panel owns the host's tuning, and the unit forbids most of it anyway.
	line("skip_optz", "true")
	line("nodelay", "true")
	line("web_port", "0")
	line("log_level", tomlString("info"))
	return b.String()
}

// snapshot is the part of BackPack's metrics file the panel reads.
type snapshot struct {
	Taken     time.Time `json:"taken"`
	BytesIn   uint64    `json:"bytes_in"`
	BytesOut  uint64    `json:"bytes_out"`
	Peer      string    `json:"peer"`
	Connected *bool     `json:"connected"`
}

func readSnapshot(path string) (snapshot, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return snapshot{}, err
	}
	var s snapshot
	err = json.Unmarshal(raw, &s)
	return s, err
}

// logTail keeps the last lines a child wrote, which is where it says why it failed.
type logTail struct {
	mu      sync.Mutex
	label   string
	pending []byte
	last    string
}

func (w *logTail) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending = append(w.pending, p...)
	for {
		i := bytes.IndexByte(w.pending, '\n')
		if i < 0 {
			break
		}
		if line := strings.TrimSpace(string(w.pending[:i])); line != "" {
			w.last = line
			logger.Debugf("backpack[%s]: %s", w.label, line)
		}
		w.pending = w.pending[i+1:]
	}
	return len(p), nil
}

func (w *logTail) lastLine() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.last
}

// child is one running backpack process.
type child struct {
	cmd     *exec.Cmd
	done    chan struct{}
	log     *logTail
	stopped atomic.Bool
	started time.Time
}

func startChild(binary, configPath, label string) (*child, error) {
	// BackPack runs in its config's folder, and the service's bin folder is relative to
	// its own: given as they are, both paths would point inside the config's folder.
	binary, err := filepath.Abs(binary)
	if err != nil {
		return nil, err
	}
	if configPath, err = filepath.Abs(configPath); err != nil {
		return nil, err
	}
	c := &child{done: make(chan struct{}), log: &logTail{label: label}, started: time.Now()}
	c.cmd = exec.CommandContext(context.Background(), binary, "-c", configPath)
	c.cmd.Dir = filepath.Dir(configPath)
	c.cmd.Stdout = c.log
	c.cmd.Stderr = c.log
	c.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := c.cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		defer close(c.done)
		if err := c.cmd.Wait(); err != nil && !c.stopped.Load() {
			logger.Warningf("backpack[%s]: exited: %v: %s", label, err, c.log.lastLine())
		}
	}()
	return c, nil
}

func (c *child) running() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

func (c *child) stop() {
	if !c.running() {
		return
	}
	c.stopped.Store(true)
	_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGTERM)
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
		<-c.done
	}
}

// reapOrphans stops backpack processes a killed panel left running on its configs.
func reapOrphans(binary, dir string) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		exe, err := os.Readlink(filepath.Join("/proc", entry.Name(), "exe"))
		if err != nil || strings.TrimSuffix(exe, " (deleted)") != binary {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil || !strings.Contains(string(cmdline), dir+string(filepath.Separator)) {
			continue
		}
		logger.Info("tunnel: stopping backpack process ", pid, " left by a previous run")
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
}
