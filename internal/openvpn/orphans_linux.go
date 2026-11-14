//go:build linux

package openvpn

import (
	"bytes"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/egress"
)

// reapOrphans stops the servers a previous panel run left behind and clears
// their files and NAT. A server is ours only if it runs one of our configs, so
// an openvpn the admin started for another purpose is never touched.
func reapOrphans(baseDir string) int {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return 0
	}
	stopped := 0
	for _, entry := range entries {
		id, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() {
			continue
		}
		f := filesIn(baseDir, id)
		if pid := orphanPID(f); pid > 0 {
			terminate(pid)
			stopped++
		}
		egress.Remove(egressOwner(id))
		f.remove()
	}
	return stopped
}

func orphanPID(f files) int {
	raw, err := os.ReadFile(f.pid)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 1 {
		return 0
	}
	cmdline, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return 0
	}
	for _, arg := range bytes.Split(cmdline, []byte{0}) {
		if string(arg) == f.config {
			return pid
		}
	}
	return 0
}

func terminate(pid int) {
	_ = syscall.Kill(pid, syscall.SIGTERM)
	deadline := time.Now().Add(gracefulStopTimeout)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
}
