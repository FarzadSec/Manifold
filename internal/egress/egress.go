// Package egress lets the clients of a tunnel reach the internet through this
// host: it masquerades the tunnel subnet and accepts its forwarded traffic.
package egress

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

// ErrNoNftables is returned when the host has no nft command to install NAT with.
var ErrNoNftables = errors.New("egress: the nft command is missing; install the nftables package")

// The owner ends up inside nft syntax and an iptables comment.
var ownerPattern = regexp.MustCompile(`^[a-z0-9]{1,24}$`)

// system is the part of the OS this package drives; tests substitute a fake.
type system interface {
	run(stdin, name string, args ...string) (string, error)
	has(name string) bool
	enableIPv4Forwarding() error
}

var host system = osSystem{}

// The device ends up inside a quoted nft string; this is wg-quick's own rule for a name.
var devicePattern = regexp.MustCompile(`^[a-zA-Z0-9_=+.-]{1,15}$`)

// traffic says which packets belong to a tunnel: by source subnet when its
// interfaces come and go (ppp), by interface when it has one of its own.
type traffic struct {
	nat     string
	forward [][]string
}

func fromSubnet(subnet netip.Prefix) traffic {
	return traffic{
		nat: fmt.Sprintf("ip saddr %s ip daddr != %s masquerade", subnet, subnet),
		forward: [][]string{
			{"-s", subnet.String()},
			{"-d", subnet.String(), "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED"},
		},
	}
}

func fromDevice(device string) traffic {
	return traffic{
		nat: fmt.Sprintf("iifname %q oifname != %q masquerade", device, device),
		forward: [][]string{
			{"-i", device},
			{"-o", device, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED"},
		},
	}
}

// Apply masquerades what leaves subnet and accepts its forwarding. Calling it
// again for the same owner replaces that owner's previous rules.
func Apply(owner string, subnet netip.Prefix) error {
	if !subnet.IsValid() || !subnet.Addr().Is4() {
		return fmt.Errorf("egress: %q is not an IPv4 subnet", subnet)
	}
	return apply(owner, fromSubnet(subnet.Masked()))
}

// ApplyDevice is Apply for a tunnel with an interface of its own: whatever
// enters through device is masqueraded, whichever address it carries.
func ApplyDevice(owner, device string) error {
	if !devicePattern.MatchString(device) {
		return fmt.Errorf("egress: invalid device %q", device)
	}
	return apply(owner, fromDevice(device))
}

func apply(owner string, t traffic) error {
	if !ownerPattern.MatchString(owner) {
		return fmt.Errorf("egress: invalid owner %q", owner)
	}
	if !host.has("nft") {
		return ErrNoNftables
	}
	if out, err := host.run(natTransaction(owner, t.nat), "nft", "-f", "-"); err != nil {
		return fmt.Errorf("egress: applying NAT for %s: %w (%s)", owner, err, out)
	}
	if err := host.enableIPv4Forwarding(); err != nil {
		return fmt.Errorf("egress: enabling IPv4 forwarding: %w", err)
	}
	removeForwardRules(owner)
	addForwardRules(owner, t.forward)
	return nil
}

// RequireIPsec drops packets for a UDP port of this host that did not arrive
// through IPsec. Remove, with the same owner, lifts it.
func RequireIPsec(owner string, port int) error {
	if !ownerPattern.MatchString(owner) {
		return fmt.Errorf("egress: invalid owner %q", owner)
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("egress: %d is not a UDP port", port)
	}
	if !host.has("nft") {
		return ErrNoNftables
	}
	table := tableName(owner)
	guard := fmt.Sprintf("add table ip %[1]s\ndelete table ip %[1]s\n"+
		"table ip %[1]s {\n"+
		"\tchain input {\n"+
		"\t\ttype filter hook input priority 0; policy accept;\n"+
		"\t\tudp dport %[2]d meta ipsec missing drop\n"+
		"\t}\n}\n", table, port)
	if out, err := host.run(guard, "nft", "-f", "-"); err != nil {
		return fmt.Errorf("egress: requiring IPsec for UDP port %d: %w (%s)", port, err, out)
	}
	return nil
}

// Remove withdraws everything Apply or RequireIPsec installed for owner.
func Remove(owner string) {
	if !ownerPattern.MatchString(owner) {
		return
	}
	if host.has("nft") {
		if out, err := host.run(natTransaction(owner, ""), "nft", "-f", "-"); err != nil {
			logger.Warning("egress: removing NAT for ", owner, ": ", err, " ", out)
		}
	}
	removeForwardRules(owner)
}

func tableName(owner string) string { return "manifold_" + owner }

func forwardTag(owner string) string { return "manifold:" + owner }

// natTransaction replaces the owner's table atomically; an empty rule only
// deletes it, and "add" first keeps the delete valid when the table is absent.
func natTransaction(owner, rule string) string {
	table := tableName(owner)
	tx := fmt.Sprintf("add table ip %s\ndelete table ip %s\n", table, table)
	if rule == "" {
		return tx
	}
	return tx + fmt.Sprintf("table ip %s {\n"+
		"\tchain postrouting {\n"+
		"\t\ttype nat hook postrouting priority 100; policy accept;\n"+
		"\t\t%s\n"+
		"\t}\n}\n", table, rule)
}

// addForwardRules is best effort: with a FORWARD policy of ACCEPT the rules
// are redundant, so a host without iptables still forwards.
func addForwardRules(owner string, matches [][]string) {
	if !host.has("iptables") {
		return
	}
	for _, match := range matches {
		args := append([]string{"-w", "5", "-I", "FORWARD", "1"}, match...)
		args = append(args, "-m", "comment", "--comment", forwardTag(owner), "-j", "ACCEPT")
		if out, err := host.run("", "iptables", args...); err != nil {
			logger.Warning("egress: adding FORWARD accept for ", owner, ": ", err, " ", out)
		}
	}
}

// removeForwardRules deletes every FORWARD rule carrying the owner's comment,
// whatever subnet it was written for.
func removeForwardRules(owner string) {
	if !host.has("iptables") {
		return
	}
	listing, err := host.run("", "iptables", "-w", "5", "-S", "FORWARD")
	if err != nil {
		logger.Warning("egress: listing FORWARD rules: ", err, " ", listing)
		return
	}
	tag := forwardTag(owner)
	for _, line := range strings.Split(listing, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "-A" {
			continue
		}
		// -S quotes the comment; passed back verbatim the quotes would be part of it.
		for i := range fields {
			fields[i] = strings.Trim(fields[i], `"`)
		}
		if !hasComment(fields, tag) {
			continue
		}
		fields[0] = "-D"
		if out, err := host.run("", "iptables", append([]string{"-w", "5"}, fields...)...); err != nil {
			logger.Warning("egress: removing FORWARD accept for ", owner, ": ", err, " ", out)
		}
	}
}

func hasComment(fields []string, tag string) bool {
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == "--comment" && fields[i+1] == tag {
			return true
		}
	}
	return false
}

type osSystem struct{}

func (osSystem) run(stdin, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

func (osSystem) has(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func (osSystem) enableIPv4Forwarding() error {
	return os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1\n"), 0o644)
}
