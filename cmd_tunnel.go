package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/tunnel"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

const tunnelUsage = `Usage:
  manifold tunnel list           every tunnel end on this server and how it is doing
  manifold tunnel import CODE    add the end a pairing code describes, made on the other panel

list checks each end itself (its link or BackPack, and three echoes through it), so it
works while the panel's pages are down. An imported end starts within 15 seconds while
the panel runs.
`

// runTunnelCLI lists and imports tunnel ends, for a relay set up over SSH alone.
func runTunnelCLI(args []string, out io.Writer) int {
	if len(args) == 0 || (args[0] != "list" && args[0] != "import") || (args[0] == "import" && len(args) != 2) {
		_, _ = fmt.Fprint(out, tunnelUsage)
		return 2
	}
	if err := database.InitDB(config.GetDBPath()); err != nil {
		_, _ = fmt.Fprintln(out, "open database:", err)
		return 1
	}
	s := &service.TunnelService{}
	if args[0] == "import" {
		v, err := s.Import(strings.TrimSpace(args[1]))
		if err != nil {
			_, _ = fmt.Fprintln(out, "The code was not taken:", strings.TrimSpace(err.Error()))
			return 1
		}
		_, _ = fmt.Fprintf(out, "Added %q (%s, the %s end). It starts within 15 seconds while the panel runs.\n", v.Name, kindName(v.Kind), v.Role)
		return 0
	}
	views, err := s.CheckAll(3)
	if err != nil {
		_, _ = fmt.Fprintln(out, err)
		return 1
	}
	if len(views) == 0 {
		_, _ = fmt.Fprintln(out, "No tunnels yet. Import a pairing code, or let your Manifold panel manage this relay.")
		return 0
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tNAME\tKIND\tROLE\tSTATE\tLATENCY\tLOSS\tIN/OUT\tFORWARDS\tNOTE")
	for _, v := range views {
		st := v.Status
		latency, loss := "-", "-"
		if st.LatencyMs > 0 {
			latency = fmt.Sprintf("%.0fms", st.LatencyMs)
		}
		if tunnel.IsKernel(v.Kind) || v.Kind == tunnel.KindL3 {
			loss = fmt.Sprintf("%.0f%%", st.Loss)
		}
		_, _ = fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s/%s\t%s\t%s\n", v.Id, v.Name, kindName(v.Kind), v.Role, st.State,
			latency, loss, common.FormatTraffic(int64(st.RxBytes)), common.FormatTraffic(int64(st.TxBytes)), forwardsText(v.Settings.Forwards), st.Error)
	}
	_ = w.Flush()
	return 0
}

func kindName(kind string) string {
	switch kind {
	case tunnel.KindReverse:
		return "BackPack-reverse"
	case tunnel.KindL3:
		return "BackPack-L3"
	}
	return strings.ToUpper(kind)
}

func forwardsText(forwards []tunnel.PortForward) string {
	parts := make([]string, 0, len(forwards))
	for _, f := range forwards {
		if f.Target != "" && f.Target != f.Listen {
			parts = append(parts, f.Listen+"→"+f.Target)
		} else {
			parts = append(parts, f.Listen)
		}
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}
