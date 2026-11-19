package tunnel

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/egress"
)

// A test end's link carries this alias, so leftovers are found after a crash and the
// tunnel job, which only knows its own mft links, never mistakes them for its own.
const benchAliasPrefix = "manifold-tunnel-test:"

var benchLinkPattern = regexp.MustCompile(`^mfx[0-9]+$`)

// BenchDevice is the interface of the n-th candidate a tunnel test brings up.
func BenchDevice(n int) string { return "mfx" + strconv.Itoa(n) }

// BenchPortsFree reports the first port of a test's span that this server already uses.
func BenchPortsFree(base int) error {
	if base < 1024 || base+BenchPortSpan-1 > 65535 {
		return fmt.Errorf("tunnel: ports %d to %d are out of range", base, base+BenchPortSpan-1)
	}
	lc := net.ListenConfig{}
	for port := base; port < base+BenchPortSpan; port++ {
		addr := ":" + strconv.Itoa(port)
		l, err := lc.Listen(context.Background(), "tcp", addr)
		if err != nil {
			return fmt.Errorf("tunnel: port %d/tcp of the test is in use on this server", port)
		}
		_ = l.Close()
		c, err := lc.ListenPacket(context.Background(), "udp", addr)
		if err != nil {
			return fmt.Errorf("tunnel: port %d/udp of the test is in use on this server", port)
		}
		_ = c.Close()
	}
	return nil
}

// BenchFirewallOwner tags the firewall rules a test opens, so they are lifted after it.
const BenchFirewallOwner = "bench"

// BenchOpening is what a test lets in from the other server while it runs: its ports,
// the protocols of GRE, IPIP and SIT, and whatever arrives through a test interface.
func BenchOpening(peer []netip.Addr, base int) egress.InputOpening {
	return egress.InputOpening{
		From: peer, FirstPort: base, LastPort: base + BenchPortSpan - 1,
		Protocols: []string{"gre", "4", "41"}, Devices: []string{"mfx+"},
	}
}

// BenchEnd is one end of a test candidate, up until Stop.
type BenchEnd struct {
	t      Tunnel
	child  *child
	config string
}

// StartBenchEnd brings up one end of a candidate; BackPack's configuration goes in dir.
func StartBenchEnd(t Tunnel, dir, binary string) (*BenchEnd, error) {
	if !benchLinkPattern.MatchString(t.DevName) {
		return nil, fmt.Errorf("tunnel: a test end needs a test interface name, not %q", t.DevName)
	}
	t, err := Normalize(t)
	if err != nil {
		return nil, err
	}
	e := &BenchEnd{t: t}
	if IsKernel(t.Kind) {
		removeBenchLink(t.DevName)
		for _, args := range linkPlan(t, benchAliasPrefix+t.PairID) {
			if out, err := host.run("ip", args...); err != nil {
				removeBenchLink(t.DevName)
				return nil, fmt.Errorf("tunnel: ip %s: %w (%s)", strings.Join(args, " "), err, out)
			}
		}
		return e, nil
	}
	if info, err := os.Stat(binary); err != nil || info.IsDir() {
		return nil, ErrNoBackPack
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	e.config = filepath.Join(dir, t.DevName+".toml")
	if err := os.WriteFile(e.config, []byte(renderBackPack(t)), 0o600); err != nil {
		return nil, err
	}
	if e.child, err = startChild(binary, e.config, t.DevName); err != nil {
		_ = os.Remove(e.config)
		return nil, err
	}
	return e, nil
}

// Tunnel is the end as it runs.
func (e *BenchEnd) Tunnel() Tunnel { return e.t }

// Err says why a BackPack end stopped on its own; nil while it runs, and for a kernel link.
func (e *BenchEnd) Err() error {
	if e.child == nil || e.child.running() {
		return nil
	}
	if line := e.child.log.lastLine(); line != "" {
		return errors.New("backpack stopped: " + line)
	}
	return errors.New("backpack stopped")
}

// Stop takes the end down and removes what it wrote.
func (e *BenchEnd) Stop() {
	if e.child != nil {
		e.child.stop()
		_ = os.Remove(e.config)
		_ = os.Remove(strings.TrimSuffix(e.config, ".toml") + ".metrics.json")
	}
	if IsKernel(e.t.Kind) {
		removeBenchLink(e.t.DevName)
	}
}

func removeBenchLink(dev string) {
	if alias, err := host.read(sysNet(dev, "ifalias")); err == nil && strings.HasPrefix(alias, benchAliasPrefix) {
		_, _ = host.run("ip", "link", "del", "dev", dev)
	}
}

// RemoveBenchLeftovers takes down what a test that never finished left behind: its
// links, the BackPack processes running on its configurations, and the configurations.
func RemoveBenchLeftovers(dir, binary string) {
	if out, err := host.run("ip", "-o", "link", "show"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			name, _, _ := strings.Cut(strings.TrimSuffix(fields[1], ":"), "@")
			if benchLinkPattern.MatchString(name) {
				removeBenchLink(name)
			}
		}
	}
	if abs, err := filepath.Abs(dir); err == nil {
		reapOrphans(binary, abs)
	}
	_ = os.RemoveAll(dir)
}
