package tunnel

import (
	"context"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Check answers "is this end working" from outside the panel: the link has to be there
// and the far end's tunnel address has to answer.
func TestCheckReportsAnEndFromOutsideThePanel(t *testing.T) {
	gre := Tunnel{ID: 4, Kind: KindGRE, Enabled: true, Settings: TunnelSettings{PeerIP: "10.80.0.1"}}
	var asked []string
	prev := echoOnce
	echoOnce = func(target netip.Addr, _ int) result {
		asked = append(asked, target.String())
		if target.String() == "10.80.0.1" {
			return result{rtt: 20 * time.Millisecond}
		}
		return result{lost: true}
	}
	t.Cleanup(func() { echoOnce = prev })

	useHost(t, &fakeHost{files: map[string]string{}})
	if st := newManager(t.TempDir(), nil).Check(gre, 1); st.State != StateDown || st.Error == "" {
		t.Fatalf("an end whose link is missing reads %+v", st)
	}

	useHost(t, &fakeHost{files: map[string]string{
		sysNet("mft4", "operstate"):           "unknown",
		sysNet("mft4", "statistics/rx_bytes"): "1500",
		sysNet("mft4", "statistics/tx_bytes"): "900",
	}})
	st := newManager(t.TempDir(), nil).Check(gre, 2)
	if st.State != StateUp || st.Loss != 0 || st.LatencyMs != 20 || st.RxBytes != 1500 || st.TxBytes != 900 {
		t.Fatalf("an end whose far side answers reads %+v", st)
	}
	if strings.Join(asked, ",") != "10.80.0.1,10.80.0.1" {
		t.Fatalf("echoes went to %v, want the other end's tunnel address twice", asked)
	}

	silent := gre
	silent.Settings.PeerIP = "10.80.0.5"
	if st := newManager(t.TempDir(), nil).Check(silent, 1); st.State != StateDown || st.Loss != 100 {
		t.Fatalf("an end whose far side is silent reads %+v", st)
	}

	off := gre
	off.Enabled = false
	if st := newManager(t.TempDir(), nil).Check(off, 1); st.State != StateDisabled {
		t.Fatalf("a disabled end reads %+v", st)
	}
}

// A BackPack end is up only while its process runs and BackPack says it is connected.
func TestCheckReadsABackPackEndFromItsProcessAndSnapshot(t *testing.T) {
	m := newManager(t.TempDir(), nil)
	reverse := Tunnel{ID: 7, Kind: KindReverse, Enabled: true}
	if st := m.Check(reverse, 1); st.State != StateDown {
		t.Fatalf("a BackPack end with no process reads %+v", st)
	}
	ctx, cancel := context.WithCancel(context.Background())
	proc := exec.CommandContext(ctx, "sh", "-c", "sleep 30", m.configPath(7))
	if err := proc.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = proc.Wait() })
	if st := m.Check(reverse, 1); st.State != StateConnecting {
		t.Fatalf("a BackPack end that has not connected reads %+v", st)
	}
	if err := os.WriteFile(m.metricsPath(7), []byte(`{"connected":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if st := m.Check(reverse, 1); st.State != StateConnecting {
		t.Fatalf("a BackPack end that lost its connection reads %+v", st)
	}
	snap := `{"taken":"2026-11-17T10:00:00Z","bytes_in":10,"bytes_out":20,"peer":"198.51.100.7:52011","connected":true}`
	if err := os.WriteFile(m.metricsPath(7), []byte(snap), 0o600); err != nil {
		t.Fatal(err)
	}
	if st := m.Check(reverse, 1); st.State != StateUp || st.Peer != "198.51.100.7:52011" || st.RxBytes != 10 {
		t.Fatalf("a connected BackPack end reads %+v", st)
	}
}
