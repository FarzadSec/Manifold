package egress

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

// PortForward sends Low..High of one protocol to TargetLow.. on the far end of a tunnel.
type PortForward struct {
	Proto     string
	Low, High int
	TargetLow int
}

func (f PortForward) match() string {
	if f.Low == f.High {
		return fmt.Sprintf("%s dport %d", f.Proto, f.Low)
	}
	return fmt.Sprintf("%s dport %d-%d", f.Proto, f.Low, f.High)
}

func (f PortForward) destination(target netip.Addr) string {
	host := target.String()
	if target.Is6() {
		host = "[" + host + "]"
	}
	switch {
	case f.Low != f.High:
		return host
	case f.TargetLow == 0 || f.TargetLow == f.Low:
		return fmt.Sprintf("%s:%d", host, f.Low)
	default:
		return fmt.Sprintf("%s:%d", host, f.TargetLow)
	}
}

func family(addr netip.Addr) string {
	if addr.Is6() {
		return "ip6"
	}
	return "ip"
}

func validForward(f PortForward) error {
	if f.Proto != "tcp" && f.Proto != "udp" {
		return fmt.Errorf("egress: protocol %q is not tcp or udp", f.Proto)
	}
	if f.Low < 1 || f.High > 65535 || f.High < f.Low {
		return fmt.Errorf("egress: %d-%d is not a port range", f.Low, f.High)
	}
	if f.Low != f.High && f.TargetLow != 0 && f.TargetLow != f.Low {
		return fmt.Errorf("egress: a range is forwarded to the same ports only")
	}
	return nil
}

// forwardTransaction replaces the owner's table: what arrives for the ports on any
// other interface goes to target through device, optionally from this host's address.
func forwardTransaction(owner, device string, target netip.Addr, rules []PortForward, masquerade bool) string {
	table := tableName(owner)
	fam := family(target)
	var b strings.Builder
	fmt.Fprintf(&b, "add table %[1]s %[2]s\ndelete table %[1]s %[2]s\ntable %[1]s %[2]s {\n", fam, table)
	b.WriteString("\tchain prerouting {\n\t\ttype nat hook prerouting priority dstnat; policy accept;\n")
	for _, f := range rules {
		fmt.Fprintf(&b, "\t\tiifname != %q %s dnat to %s\n", device, f.match(), f.destination(target))
	}
	b.WriteString("\t}\n")
	if masquerade {
		fmt.Fprintf(&b, "\tchain postrouting {\n\t\ttype nat hook postrouting priority srcnat; policy accept;\n\t\toifname %q masquerade\n\t}\n", device)
	}
	b.WriteString("}\n")
	return b.String()
}

// ApplyForwards installs the relay end of a tunnel's forwarded ports. Calling it
// again for the same owner replaces them; Remove withdraws them.
func ApplyForwards(owner, device string, target netip.Addr, rules []PortForward, masquerade bool) error {
	if !ownerPattern.MatchString(owner) {
		return fmt.Errorf("egress: invalid owner %q", owner)
	}
	if !devicePattern.MatchString(device) {
		return fmt.Errorf("egress: invalid device %q", device)
	}
	if !target.IsValid() {
		return fmt.Errorf("egress: no address to forward to")
	}
	for _, f := range rules {
		if err := validForward(f); err != nil {
			return err
		}
	}
	if !host.has("nft") {
		return ErrNoNftables
	}
	other := "ip6"
	if target.Is6() {
		other = "ip"
	}
	table := tableName(owner)
	tx := fmt.Sprintf("add table %[1]s %[2]s\ndelete table %[1]s %[2]s\n", other, table) +
		forwardTransaction(owner, device, target, rules, masquerade)
	if out, err := host.run(tx, "nft", "-f", "-"); err != nil {
		return fmt.Errorf("egress: forwarding ports to %s: %w (%s)", target, err, out)
	}
	if err := host.enableIPv4Forwarding(); err != nil {
		return fmt.Errorf("egress: enabling IPv4 forwarding: %w", err)
	}
	removeForwardRules(owner)
	if target.Is4() {
		addForwardRules(owner, [][]string{
			{"-o", device},
			{"-i", device, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED"},
		})
	}
	return nil
}

// replyTransaction marks connections that came in through device, and their
// replies, so a routing rule on mark sends the replies back the same way.
func replyTransaction(owner, device string, mark uint32) string {
	table := tableName(owner)
	return fmt.Sprintf("add table inet %[1]s\ndelete table inet %[1]s\ntable inet %[1]s {\n"+
		"\tchain prerouting {\n\t\ttype filter hook prerouting priority mangle; policy accept;\n"+
		"\t\tiifname %[2]q ct state new ct mark set 0x%[3]x\n\t}\n"+
		"\tchain output {\n\t\ttype route hook output priority mangle; policy accept;\n"+
		"\t\tct mark 0x%[3]x meta mark set 0x%[3]x\n\t}\n}\n", table, device, mark)
}

// ApplyReplyMark marks what arrives through device so its replies can be routed back.
func ApplyReplyMark(owner, device string, mark uint32) error {
	if !ownerPattern.MatchString(owner) || !devicePattern.MatchString(device) || mark == 0 {
		return fmt.Errorf("egress: invalid reply mark for %q on %q", owner, device)
	}
	if !host.has("nft") {
		return ErrNoNftables
	}
	if out, err := host.run(replyTransaction(owner, device, mark), "nft", "-f", "-"); err != nil {
		return fmt.Errorf("egress: marking replies on %s: %w (%s)", device, err, out)
	}
	return nil
}

// RemoveTables deletes the owner's table in every family the forward and reply rules use.
func RemoveTables(owner string) {
	if !ownerPattern.MatchString(owner) || !host.has("nft") {
		return
	}
	table := tableName(owner)
	var tx strings.Builder
	for _, fam := range []string{"ip", "ip6", "inet"} {
		fmt.Fprintf(&tx, "add table %[1]s %[2]s\ndelete table %[1]s %[2]s\n", fam, table)
	}
	if out, err := host.run(tx.String(), "nft", "-f", "-"); err != nil {
		logger.Warning("egress: removing tables of ", owner, ": ", err, " ", out)
	}
	removeForwardRules(owner)
}

// Tables lists the owners of every table this package installed with a prefix.
func Tables(prefix string) []string {
	if !host.has("nft") {
		return nil
	}
	out, err := host.run("", "nft", "list", "tables")
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var owners []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "table" {
			continue
		}
		owner, ok := strings.CutPrefix(fields[2], tableName(prefix))
		if !ok || seen[prefix+owner] {
			continue
		}
		seen[prefix+owner] = true
		owners = append(owners, prefix+owner)
	}
	return owners
}

// Target is one far end that balanced forwards can send to: a tunnel device and the
// address of the other side inside it.
type Target struct {
	Device string
	Addr   netip.Addr
}

func deviceSet(targets []Target) string {
	if len(targets) == 1 {
		return fmt.Sprintf("%q", targets[0].Device)
	}
	names := make([]string, 0, len(targets))
	for _, t := range targets {
		names = append(names, fmt.Sprintf("%q", t.Device))
	}
	return "{ " + strings.Join(names, ", ") + " }"
}

// balancedDestination spreads new connections over the targets in turn; nftables only
// sees a connection's first packet here, so the rest of it stays on the same target.
func balancedDestination(f PortForward, targets []Target) string {
	if len(targets) == 1 {
		return "to " + f.destination(targets[0].Addr)
	}
	port := 0
	if f.Low == f.High && f.TargetLow != 0 && f.TargetLow != f.Low {
		port = f.TargetLow
	}
	entries := make([]string, 0, len(targets))
	for i, t := range targets {
		if port != 0 {
			entries = append(entries, fmt.Sprintf("%d : %s . %d", i, t.Addr, port))
		} else {
			entries = append(entries, fmt.Sprintf("%d : %s", i, t.Addr))
		}
	}
	m := fmt.Sprintf("numgen inc mod %d map { %s }", len(targets), strings.Join(entries, ", "))
	if port != 0 {
		return family(targets[0].Addr) + " to " + m
	}
	return "to " + m
}

func balancedTransaction(owner string, targets []Target, rules []PortForward, masquerade bool) string {
	table := tableName(owner)
	fam := family(targets[0].Addr)
	devices := deviceSet(targets)
	var b strings.Builder
	fmt.Fprintf(&b, "add table %[1]s %[2]s\ndelete table %[1]s %[2]s\ntable %[1]s %[2]s {\n", fam, table)
	b.WriteString("\tchain prerouting {\n\t\ttype nat hook prerouting priority dstnat; policy accept;\n")
	for _, f := range rules {
		fmt.Fprintf(&b, "\t\tiifname != %s %s dnat %s\n", devices, f.match(), balancedDestination(f, targets))
	}
	b.WriteString("\t}\n")
	if masquerade {
		fmt.Fprintf(&b, "\tchain postrouting {\n\t\ttype nat hook postrouting priority srcnat; policy accept;\n\t\toifname %s masquerade\n\t}\n", devices)
	}
	b.WriteString("}\n")
	return b.String()
}

// ApplyBalancedForwards installs ports that go to several tunnels, each new connection
// to the next target in turn; with one target it is ApplyForwards. A later call for
// the same owner replaces the set, which is how a failed target is taken out.
func ApplyBalancedForwards(owner string, targets []Target, rules []PortForward, masquerade bool) error {
	if !ownerPattern.MatchString(owner) {
		return fmt.Errorf("egress: invalid owner %q", owner)
	}
	if len(targets) == 0 {
		return fmt.Errorf("egress: no tunnel to forward to")
	}
	for _, t := range targets {
		if !devicePattern.MatchString(t.Device) || !t.Addr.IsValid() {
			return fmt.Errorf("egress: invalid target %q %s", t.Device, t.Addr)
		}
		if t.Addr.Is4() != targets[0].Addr.Is4() {
			return fmt.Errorf("egress: the targets of one set must be of one address family")
		}
	}
	for _, f := range rules {
		if err := validForward(f); err != nil {
			return err
		}
	}
	if !host.has("nft") {
		return ErrNoNftables
	}
	other := "ip6"
	if targets[0].Addr.Is6() {
		other = "ip"
	}
	table := tableName(owner)
	tx := fmt.Sprintf("add table %[1]s %[2]s\ndelete table %[1]s %[2]s\n", other, table) +
		balancedTransaction(owner, targets, rules, masquerade)
	if out, err := host.run(tx, "nft", "-f", "-"); err != nil {
		return fmt.Errorf("egress: forwarding ports over %d tunnels: %w (%s)", len(targets), err, out)
	}
	if err := host.enableIPv4Forwarding(); err != nil {
		return fmt.Errorf("egress: enabling IPv4 forwarding: %w", err)
	}
	removeForwardRules(owner)
	if targets[0].Addr.Is4() {
		var matches [][]string
		for _, t := range targets {
			matches = append(matches, []string{"-o", t.Device}, []string{"-i", t.Device, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED"})
		}
		addForwardRules(owner, matches)
	}
	return nil
}
