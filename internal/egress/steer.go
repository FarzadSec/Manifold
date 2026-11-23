package egress

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

const (
	// SteerMark routes a steered packet to this host's own sockets; tunnels use 0x4d46xxxx.
	SteerMark  uint32 = 0x4d520000
	steerTable        = 19900
	steerPref         = 9900
	steerOwner        = "steer"
)

// Steer hands the TCP and UDP that a tunnel's clients send past this host to a
// transparent proxy on a loopback port, so the proxy's routing decides where it goes.
type Steer struct {
	// Device matches the tunnel's own interface; without one, Subnet matches its clients.
	Device string
	Subnet netip.Prefix
	Port   int
}

func (s Steer) match() (string, error) {
	if s.Port < 1 || s.Port > 65535 {
		return "", fmt.Errorf("egress: %d is not a proxy port", s.Port)
	}
	if s.Device != "" {
		if !devicePattern.MatchString(s.Device) {
			return "", fmt.Errorf("egress: invalid device %q", s.Device)
		}
		return fmt.Sprintf("iifname %q", s.Device), nil
	}
	if !s.Subnet.IsValid() || !s.Subnet.Addr().Is4() {
		return "", fmt.Errorf("egress: a steer needs a device or an IPv4 subnet, not %q", s.Subnet)
	}
	return "ip saddr " + s.Subnet.Masked().String(), nil
}

func steerTransaction(steers []Steer) (string, error) {
	table := tableName(steerOwner)
	var b strings.Builder
	fmt.Fprintf(&b, "add table inet %[1]s\ndelete table inet %[1]s\n", table)
	if len(steers) == 0 {
		return b.String(), nil
	}
	fmt.Fprintf(&b, "table inet %s {\n\tchain prerouting {\n"+
		"\t\ttype filter hook prerouting priority mangle; policy accept;\n", table)
	for _, s := range steers {
		match, err := s.match()
		if err != nil {
			return "", err
		}
		// What the host serves itself, or broadcasts, stays where it was going.
		for _, proto := range []string{"tcp", "udp"} {
			fmt.Fprintf(&b, "\t\t%s meta l4proto %s fib daddr type != { local, broadcast, multicast } "+
				"tproxy ip to 127.0.0.1:%d meta mark set 0x%x accept\n", match, proto, s.Port, SteerMark)
		}
	}
	b.WriteString("\t}\n}\n")
	return b.String(), nil
}

// ApplySteers replaces every steer with steers; none withdraws the table and its route.
func ApplySteers(steers []Steer) error {
	tx, err := steerTransaction(steers)
	if err != nil {
		return err
	}
	if !host.has("nft") {
		if len(steers) == 0 {
			return nil
		}
		return ErrNoNftables
	}
	pref, routeTable := strconv.Itoa(steerPref), strconv.Itoa(steerTable)
	for range 8 {
		if _, err := host.run("", "ip", "-4", "rule", "del", "pref", pref); err != nil {
			break
		}
	}
	if len(steers) == 0 {
		_, _ = host.run("", "ip", "-4", "route", "flush", "table", routeTable)
		if out, err := host.run(tx, "nft", "-f", "-"); err != nil {
			return fmt.Errorf("egress: removing the steers: %w (%s)", err, out)
		}
		return nil
	}
	if out, err := host.run(tx, "nft", "-f", "-"); err != nil {
		return fmt.Errorf("egress: steering tunnels to their proxy: %w (%s)", err, out)
	}
	for _, args := range [][]string{
		{"-4", "rule", "add", "pref", pref, "fwmark", fmt.Sprintf("0x%x", SteerMark), "lookup", routeTable},
		{"-4", "route", "replace", "local", "0.0.0.0/0", "dev", "lo", "table", routeTable},
	} {
		if out, err := host.run("", "ip", args...); err != nil {
			return fmt.Errorf("egress: ip %s: %w (%s)", strings.Join(args, " "), err, out)
		}
	}
	return nil
}
