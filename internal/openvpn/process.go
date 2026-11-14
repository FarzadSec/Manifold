package openvpn

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

// ErrNotInstalled is returned when the host has no openvpn program to run.
var ErrNotInstalled = errors.New("openvpn: the openvpn program is missing; install the openvpn package")

const (
	gracefulStopTimeout = 5 * time.Second
	forceStopTimeout    = 2 * time.Second
)

// FindBinary locates the openvpn program; tests of other packages replace it.
var FindBinary = func() (string, error) {
	if path, err := exec.LookPath("openvpn"); err == nil {
		return path, nil
	}
	// A service started with a trimmed PATH may not include the sbin directories.
	for _, path := range []string{"/usr/sbin/openvpn", "/usr/local/sbin/openvpn"} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", ErrNotInstalled
}

// logLines forwards a child's output to the panel log and keeps its last line,
// which is where OpenVPN says why it gave up.
type logLines struct {
	mu      sync.Mutex
	label   string
	pending []byte
	last    string
}

func (w *logLines) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending = append(w.pending, p...)
	for {
		i := bytes.IndexByte(w.pending, '\n')
		if i < 0 {
			break
		}
		w.emitLocked(string(w.pending[:i]))
		w.pending = w.pending[i+1:]
	}
	return len(p), nil
}

func (w *logLines) emitLocked(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	w.last = line
	logger.Debugf("openvpn[%s]: %s", w.label, line)
}

func (w *logLines) lastLine() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.last
}

// process is one running openvpn server.
type process struct {
	cmd     *exec.Cmd
	done    chan struct{}
	log     *logLines
	stopped atomic.Bool
}

func startProcess(binary, configPath, label string) (*process, error) {
	p := &process{done: make(chan struct{}), log: &logLines{label: label}}
	p.cmd = exec.CommandContext(context.Background(), binary, "--config", configPath)
	p.cmd.Stdout = p.log
	p.cmd.Stderr = p.log
	if err := p.cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		defer close(p.done)
		if err := p.cmd.Wait(); err != nil && !p.stopped.Load() {
			logger.Errorf("openvpn[%s]: exited: %v: %s", label, err, p.log.lastLine())
		}
	}()
	return p, nil
}

func (p *process) running() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *process) waitExit(timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-p.done:
		return true
	case <-timer.C:
		return false
	}
}

func (p *process) stop() {
	if !p.running() {
		return
	}
	p.stopped.Store(true)
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	if p.waitExit(gracefulStopTimeout) {
		return
	}
	logger.Warningf("openvpn[%s]: did not stop after SIGTERM, killing it", p.log.label)
	_ = p.cmd.Process.Kill()
	p.waitExit(forceStopTimeout)
}

// failure describes why a start did not complete, with OpenVPN's own last words.
func (p *process) failure(what string) error {
	if last := p.log.lastLine(); last != "" {
		return fmt.Errorf("openvpn: %s: %s", what, last)
	}
	return fmt.Errorf("openvpn: %s", what)
}
