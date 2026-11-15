//go:build linux

package l2tp

import (
	"bytes"
	"os"
	"strconv"
	"syscall"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

// reapOrphans ends the xl2tpd servers a panel that died without stopping them
// left behind; they hold the port and would answer without anyone billing.
func reapOrphans(baseDir string) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return
	}
	marker := []byte("\x00-c\x00" + baseDir + "/")
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		cmdline, err := os.ReadFile("/proc/" + entry.Name() + "/cmdline")
		if err != nil || !bytes.Contains(cmdline, marker) || !bytes.Contains(cmdline, []byte("xl2tpd")) {
			continue
		}
		logger.Warningf("l2tp: stopping the xl2tpd (pid %d) a previous run left behind", pid)
		_ = syscall.Kill(pid, syscall.SIGTERM)
		for waited := time.Duration(0); waited < gracefulStopTimeout; waited += 100 * time.Millisecond {
			if _, err := os.Stat("/proc/" + entry.Name()); err != nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}
