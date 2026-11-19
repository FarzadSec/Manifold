package service

import (
	"context"
	"io"
	"net/netip"
	"os"
	"path/filepath"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/egress"
	"github.com/mhsanaei/3x-ui/v3/internal/tunnel"
)

// benchEnd is one running end of a test candidate.
type benchEnd interface {
	Stop()
	Err() error
}

// benchEngine is what a tunnel test does to this host; tests substitute it.
type benchEngine interface {
	startEnd(t tunnel.Tunnel) (benchEnd, error)
	listen(port int, key []byte, allow func(netip.Addr) bool) (io.Closer, error)
	measure(ctx context.Context, p tunnel.BenchPlan, phase func(string)) tunnel.BenchMetrics
	openFirewall(peer []netip.Addr, base int) error
	closeFirewall()
	portsFree(base int) error
	hasBackPack() bool
	// cleanup takes down what a test this process never finished left on the host.
	cleanup()
}

type osBenchEngine struct{}

func benchDir() string { return filepath.Join(config.GetBinFolderPath(), "tunnel-test") }

func (osBenchEngine) startEnd(t tunnel.Tunnel) (benchEnd, error) {
	return tunnel.StartBenchEnd(t, benchDir(), tunnel.BackPackPath())
}

func (osBenchEngine) listen(port int, key []byte, allow func(netip.Addr) bool) (io.Closer, error) {
	return tunnel.ListenBench(port, key, allow)
}

func (osBenchEngine) measure(ctx context.Context, p tunnel.BenchPlan, phase func(string)) tunnel.BenchMetrics {
	return tunnel.Measure(ctx, p, phase)
}

func (osBenchEngine) openFirewall(peer []netip.Addr, base int) error {
	return egress.OpenInput(tunnel.BenchFirewallOwner, tunnel.BenchOpening(peer, base))
}

func (osBenchEngine) closeFirewall() { egress.CloseInput(tunnel.BenchFirewallOwner) }

func (osBenchEngine) portsFree(base int) error { return tunnel.BenchPortsFree(base) }

func (osBenchEngine) hasBackPack() bool {
	info, err := os.Stat(tunnel.BackPackPath())
	return err == nil && !info.IsDir()
}

func (osBenchEngine) cleanup() {
	tunnel.RemoveBenchLeftovers(benchDir(), tunnel.BackPackPath())
	egress.CloseInput(tunnel.BenchFirewallOwner)
}
