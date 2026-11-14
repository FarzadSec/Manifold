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

// Apply masquerades what leaves subnet and accepts its forwarding. Calling it
// again for the same owner replaces that owner's previous rules.
func Apply(owner string, subnet netip.Prefix) error {
	if !ownerPattern.MatchString(owner) {
		return fmt.Errorf("egress: invalid owner %q", owner)
	}
	if !subnet.IsValid() || !subnet.Addr().Is4() {
		return fmt.Errorf("egress: %q is not an IPv4 subnet", subnet)
	}
	if !host.has("nft") {
		return ErrNoNftables
	}
	subnet = subnet.Masked()
	if out, err := host.run(natTransaction(owner, &subnet), "nft", "-f", "-"); err != nil {
		return fmt.Errorf("egress: applying NAT for %s: %w (%s)", owner, err, out)
	}
	if err := host.enableIPv4Forwarding(); err != nil {
		return fmt.Errorf("egress: enabling IPv4 forwarding: %w", err)
	}
	removeForwardRules(owner)
	addForwardRules(owner, subnet)
	return nil
}

// Remove withdraws everything Apply installed for owner.
func Remove(owner string) {
	if !ownerPattern.MatchString(owner) {
		return
	}
	if host.has("nft") {
		if out, err := host.run(natTransaction(owner, nil), "nft", "-f", "-"); err != nil {
			logger.Warning("egress: removing NAT for ", owner, ": ", err, " ", out)
		}
	}
	removeForwardRules(owner)
}

func tableName(owner string) string { return "manifold_" + owner }

func forwardTag(owner string) string { return "manifold:" + owner }

// natTransaction replaces the owner's table atomically; a nil subnet only
// deletes it, and "add" first keeps the delete valid when the table is absent.
func natTransaction(owner string, subnet *netip.Prefix) string {
	table := tableName(owner)
	tx := fmt.Sprintf("add table ip %s\ndelete table ip %s\n", table, table)
	if subnet == nil {
		return tx
	}
	return tx + fmt.Sprintf("table ip %s {\n"+
		"\tchain postrouting {\n"+
		"\t\ttype nat hook postrouting priority 100; policy accept;\n"+
		"\t\tip saddr %s ip daddr != %s masquerade\n"+
		"\t}\n}\n", table, subnet, subnet)
}

func forwardRules(owner string, subnet netip.Prefix) [][]string {
	tag := []string{"-m", "comment", "--comment", forwardTag(owner), "-j", "ACCEPT"}
	return [][]string{
		append([]string{"-s", subnet.String()}, tag...),
		append([]string{"-d", subnet.String(), "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED"}, tag...),
	}
}

// addForwardRules is best effort: with a FORWARD policy of ACCEPT the rules
// are redundant, so a host without iptables still forwards.
func addForwardRules(owner string, subnet netip.Prefix) {
	if !host.has("iptables") {
		return
	}
	for _, rule := range forwardRules(owner, subnet) {
		args := append([]string{"-w", "5", "-I", "FORWARD", "1"}, rule...)
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
