package l2tp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// conf.d is read by the stock swanctl.conf and is all strongSwan's AppArmor profile lets swanctl read.
	swanctlConfig  = "/etc/swanctl/conf.d/manifold-l2tp.conf"
	swanctlTimeout = 20 * time.Second
)

// ipsecBackend is the IPsec side of the server.
type ipsecBackend interface {
	load(config string) error
	unload()
}

// strongSwan is the system's own daemon, told what to accept through swanctl.
type strongSwan struct {
	path string
	run  func(args ...string) (string, error)
}

func newStrongSwan() *strongSwan {
	return &strongSwan{path: swanctlConfig, run: runSwanctl}
}

func runSwanctl(args ...string) (string, error) {
	binary, err := findProgram("swanctl")
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), swanctlTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// load writes the connection and its key where strongSwan reads them and has
// the running daemon take them in.
func (s *strongSwan) load(config string) error {
	if _, err := findProgram("swanctl"); err != nil {
		return errors.New("l2tp: strongSwan is not installed (no swanctl program)")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return fmt.Errorf("l2tp: storing the IPsec configuration: %w", err)
	}
	if err := os.WriteFile(s.path, []byte(config), 0o600); err != nil {
		return fmt.Errorf("l2tp: storing the IPsec configuration: %w", err)
	}
	if err := os.Chmod(s.path, 0o600); err != nil {
		return fmt.Errorf("l2tp: storing the IPsec configuration: %w", err)
	}
	if out, err := s.run("--load-all", "--noprompt"); err != nil {
		return fmt.Errorf("l2tp: strongSwan did not take the IPsec configuration (is the strongswan service running?): %w: %s", err, lastLineOf(out))
	}
	return nil
}

// unload withdraws the connection; a host that never had it is left alone.
func (s *strongSwan) unload() {
	if err := os.Remove(s.path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
	}
	_, _ = s.run("--load-all", "--noprompt")
	_, _ = s.run("--terminate", "--ike", ipsecName, "--timeout", "5")
}

func lastLineOf(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
