package egress

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

// InputOpening is what a short job lets in ahead of the firewall's own INPUT rules, such
// as ufw's deny: from addresses to TCP and UDP ports and IP protocols, and from devices.
type InputOpening struct {
	From      []netip.Addr
	FirstPort int
	LastPort  int
	Protocols []string
	Devices   []string
}

var (
	protocolPattern = regexp.MustCompile(`^(gre|[0-9]{1,3})$`)
	// An iptables interface match: a name, or a prefix ending in +.
	deviceMatchPattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,14}\+?$`)
)

func (o InputOpening) check() error {
	if o.FirstPort < 1 || o.LastPort > 65535 || o.FirstPort > o.LastPort {
		return fmt.Errorf("egress: %d to %d is not a port range", o.FirstPort, o.LastPort)
	}
	for _, p := range o.Protocols {
		if !protocolPattern.MatchString(p) {
			return fmt.Errorf("egress: invalid IP protocol %q", p)
		}
	}
	for _, d := range o.Devices {
		if !deviceMatchPattern.MatchString(d) {
			return fmt.Errorf("egress: invalid device %q", d)
		}
	}
	for _, a := range o.From {
		if !a.IsValid() {
			return errors.New("egress: an opening needs valid source addresses")
		}
	}
	return nil
}

// OpenInput accepts what o describes, replacing what owner opened before; CloseInput
// withdraws it. A host without iptables has no firewall of that kind to open.
func OpenInput(owner string, o InputOpening) error {
	if !ownerPattern.MatchString(owner) {
		return fmt.Errorf("egress: invalid owner %q", owner)
	}
	if err := o.check(); err != nil {
		return err
	}
	CloseInput(owner)
	tag := forwardTag(owner)
	ports := strconv.Itoa(o.FirstPort) + ":" + strconv.Itoa(o.LastPort)
	var errs []error
	for _, tool := range []string{"iptables", "ip6tables"} {
		if !host.has(tool) {
			continue
		}
		var rules [][]string
		for _, a := range o.From {
			if a.Unmap().Is4() != (tool == "iptables") {
				continue
			}
			src := a.Unmap().String()
			rules = append(rules, []string{"-s", src, "-p", "tcp", "--dport", ports}, []string{"-s", src, "-p", "udp", "--dport", ports})
			for _, p := range o.Protocols {
				rules = append(rules, []string{"-s", src, "-p", p})
			}
		}
		for _, d := range o.Devices {
			rules = append(rules, []string{"-i", d})
		}
		for _, match := range rules {
			args := append([]string{"-w", "5", "-I", "INPUT", "1"}, match...)
			args = append(args, "-m", "comment", "--comment", tag, "-j", "ACCEPT")
			if out, err := host.run("", tool, args...); err != nil {
				errs = append(errs, fmt.Errorf("egress: %s %s: %w (%s)", tool, strings.Join(args, " "), err, out))
			}
		}
	}
	return errors.Join(errs...)
}

// CloseInput withdraws every INPUT rule OpenInput wrote for owner.
func CloseInput(owner string) {
	if !ownerPattern.MatchString(owner) {
		return
	}
	for _, tool := range []string{"iptables", "ip6tables"} {
		removeTagged(tool, "INPUT", owner)
	}
}
