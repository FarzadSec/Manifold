package l2tp

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

// ErrNotInstalled is returned when the host lacks a program the server needs.
var ErrNotInstalled = errors.New("l2tp: programs the server needs are missing")

const (
	gracefulStopTimeout = 5 * time.Second
	forceStopTimeout    = 2 * time.Second
	// xl2tpd gives up within moments when its port is taken or its config is refused.
	earlyExitWindow = 500 * time.Millisecond
)

// findProgram locates a system program even when the service's PATH has no sbin directories.
func findProgram(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	for _, dir := range []string{"/usr/sbin", "/usr/local/sbin", "/sbin"} {
		if info, err := os.Stat(dir + "/" + name); err == nil && !info.IsDir() {
			return dir + "/" + name, nil
		}
	}
	return "", exec.ErrNotFound
}

// MissingPrograms names what an L2TP server cannot run without and the host
// lacks; swanctl is strongSwan's, needed only when sessions must be encrypted.
var MissingPrograms = func(withIPsec bool) []string {
	needed := []string{"xl2tpd", "pppd"}
	if withIPsec {
		needed = append(needed, "swanctl")
	}
	var missing []string
	for _, name := range needed {
		if _, err := findProgram(name); err != nil {
			missing = append(missing, name)
		}
	}
	return missing
}

// daemon is a running xl2tpd.
type daemon interface {
	running() bool
	stop()
	lastWords() string
}

// logLines forwards a child's output to the panel log and keeps its last
// line, which is where xl2tpd says why it gave up.
type logLines struct {
	mu      sync.Mutex
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
		if line := strings.TrimSpace(string(w.pending[:i])); line != "" {
			w.last = line
			logger.Debugf("l2tp: %s", line)
		}
		w.pending = w.pending[i+1:]
	}
	return len(p), nil
}

type process struct {
	cmd     *exec.Cmd
	done    chan struct{}
	log     *logLines
	stopped atomic.Bool
}

// startXl2tpd runs xl2tpd in the foreground on the inbound's own files, so it
// shares nothing with an xl2tpd service the distribution may have installed.
func startXl2tpd(f files) (daemon, error) {
	binary, err := findProgram("xl2tpd")
	if err != nil {
		return nil, err
	}
	p := &process{done: make(chan struct{}), log: &logLines{}}
	p.cmd = exec.CommandContext(context.Background(), binary, "-D", "-c", f.xl2tpd, "-s", f.secrets, "-p", f.pid, "-C", f.control)
	p.cmd.Stdout = p.log
	p.cmd.Stderr = p.log
	if err := p.cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		defer close(p.done)
		if err := p.cmd.Wait(); err != nil && !p.stopped.Load() {
			logger.Errorf("l2tp: xl2tpd exited: %v: %s", err, p.lastWords())
		}
	}()
	if p.waitExit(earlyExitWindow) {
		return nil, errors.New("xl2tpd stopped right after starting: " + p.lastWords())
	}
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

// stop ends xl2tpd, which closes its tunnels and with them every pppd.
func (p *process) stop() {
	if !p.running() {
		return
	}
	p.stopped.Store(true)
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	if p.waitExit(gracefulStopTimeout) {
		return
	}
	logger.Warning("l2tp: xl2tpd did not stop after SIGTERM, killing it")
	_ = p.cmd.Process.Kill()
	p.waitExit(forceStopTimeout)
}

func (p *process) lastWords() string {
	p.log.mu.Lock()
	defer p.log.mu.Unlock()
	return p.log.last
}
