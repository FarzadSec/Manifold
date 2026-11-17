package tunnel

import (
	"bytes"
	"net/netip"
	"os"
	"path/filepath"
)

// Check looks at one end from outside the panel, for the command line: whether its link
// or BackPack is there, and how the far end's tunnel address answers a few echoes.
func (m *Manager) Check(t Tunnel, echoes int) TunnelStatus {
	st := TunnelStatus{ID: t.ID}
	if !t.Enabled {
		st.State = StateDisabled
		return st
	}
	if isBackPack(t.Kind) && !childRunningFor(m.configPath(t.ID)) {
		st.State, st.Error = StateDown, "BackPack is not running; the panel starts it, so check that the panel runs"
		return st
	}
	if t.Kind == KindReverse {
		snap, err := readSnapshot(m.metricsPath(t.ID))
		st.RxBytes, st.TxBytes, st.Peer = snap.BytesIn, snap.BytesOut, snap.Peer
		st.State = StateConnecting
		if err == nil && snap.Connected != nil && *snap.Connected {
			st.State = StateUp
		}
		return st
	}
	if _, err := host.read(sysNet(t.Device(), "operstate")); err != nil {
		st.State, st.Error = StateDown, t.Device()+" does not exist; the panel makes it, so check that the panel runs"
		return st
	}
	st.RxBytes, st.TxBytes = interfaceBytes(t.Device())
	peer, err := netip.ParseAddr(t.Settings.PeerIP)
	if err != nil {
		st.State, st.Error = StateError, "the other end's tunnel address is not set"
		return st
	}
	var w window
	for seq := 1; seq <= echoes; seq++ {
		w.add(echoOnce(peer, seq))
	}
	st.Loss, st.LatencyMs, st.State = judge(&w)
	return st
}

// echoOnce sends one echo through the tunnel; tests replace it, since ICMP sockets
// need privileges a test runner may not have.
var echoOnce = func(target netip.Addr, seq int) result {
	return (&prober{target: target}).echo(seq)
}

// judge turns a window of echoes into the loss, latency and state the panel shows.
func judge(w *window) (loss, latencyMs float64, state string) {
	loss, rtt, recentLost, total := w.summary()
	latencyMs = float64(rtt.Microseconds()) / 1000
	switch {
	case total == 0:
		state = StateConnecting
	case recentLost >= 3 || loss >= 99.9:
		state = StateDown
	case loss >= 20 || rtt.Milliseconds() > 800:
		state = StateDegraded
	default:
		state = StateUp
	}
	return loss, latencyMs, state
}

// childRunningFor reports whether a process runs on this BackPack config.
func childRunningFor(config string) bool {
	procs, _ := filepath.Glob("/proc/[0-9]*/cmdline")
	for _, p := range procs {
		raw, err := os.ReadFile(p)
		if err == nil && bytes.Contains(raw, []byte(config)) {
			return true
		}
	}
	return false
}
