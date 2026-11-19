package tunnel

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/egress"
)

// The link alias marks a link as the panel's and records what it was made from;
// it lives in the kernel with the link, so a restarted panel finds both.
const aliasPrefix = "manifold-tunnel:"

// system is the part of the OS the kernel links need; tests substitute a fake.
type system interface {
	run(name string, args ...string) (string, error)
	read(path string) (string, error)
	write(path, content string) error
}

var host system = osSystem{}

type osSystem struct{}

func (osSystem) run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

func (osSystem) read(path string) (string, error) {
	raw, err := os.ReadFile(path)
	return strings.TrimSpace(string(raw)), err
}

func (osSystem) write(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// defaultMTU leaves room for each link's outer headers on a 1500-byte path.
func defaultMTU(kind string) int {
	switch kind {
	case KindGRE:
		return 1476
	case KindGRETAP:
		return 1462
	case KindIP6GRE:
		return 1448
	case KindIP6GRETAP:
		return 1434
	default:
		return 1480
	}
}

func (t Tunnel) mtu() int {
	if t.Settings.MTU > 0 {
		return t.Settings.MTU
	}
	return defaultMTU(t.Kind)
}

// linkCommands is the plan for one kernel link, in the order it is run.
func linkCommands(t Tunnel) [][]string { return linkPlan(t, aliasPrefix+linkFingerprint(t)) }

func linkPlan(t Tunnel, alias string) [][]string {
	s := t.Settings
	dev := t.Device()
	add := []string{"link", "add", dev, "type", t.Kind, "remote", s.RemoteAddr}
	if s.LocalAddr != "" {
		add = append(add, "local", s.LocalAddr)
	}
	add = append(add, "ttl", strconv.Itoa(s.TTL))
	if s.Key != 0 {
		add = append(add, "key", strconv.FormatUint(uint64(s.Key), 10))
	}
	return [][]string{
		add,
		{"link", "set", "dev", dev, "mtu", strconv.Itoa(t.mtu())},
		{"addr", "add", s.LocalIP, "dev", dev},
		{"link", "set", "dev", dev, "alias", alias},
		{"link", "set", "dev", dev, "up"},
	}
}

// linkFingerprint moves with everything that needs the link made again.
func linkFingerprint(t Tunnel) string {
	s := t.Settings
	sum := sha256.Sum256([]byte(strings.Join([]string{
		t.Kind, s.RemoteAddr, s.LocalAddr, s.LocalIP, strconv.Itoa(s.TTL), strconv.Itoa(t.mtu()),
		strconv.FormatUint(uint64(s.Key), 10),
	}, "\x00")))
	return hex.EncodeToString(sum[:8])
}

func sysNet(dev, file string) string { return filepath.Join("/sys/class/net", dev, file) }

// ensureLink makes the link unless one with the same fingerprint is already up.
func ensureLink(t Tunnel) error {
	dev := t.Device()
	want := aliasPrefix + linkFingerprint(t)
	if alias, err := host.read(sysNet(dev, "ifalias")); err == nil && alias == want {
		if state, _ := host.read(sysNet(dev, "flags")); linkUp(state) {
			return nil
		}
	}
	_, _ = host.run("ip", "link", "del", "dev", dev)
	for _, args := range linkCommands(t) {
		if out, err := host.run("ip", args...); err != nil {
			_, _ = host.run("ip", "link", "del", "dev", dev)
			return fmt.Errorf("tunnel: ip %s: %w (%s)", strings.Join(args, " "), err, out)
		}
	}
	return nil
}

// linkUp reads IFF_UP from /sys/class/net/<dev>/flags.
func linkUp(flags string) bool {
	v, err := strconv.ParseUint(strings.TrimPrefix(flags, "0x"), 16, 32)
	return err == nil && v&1 == 1
}

func removeLink(dev string) {
	if alias, err := host.read(sysNet(dev, "ifalias")); err != nil || !strings.HasPrefix(alias, aliasPrefix) {
		return
	}
	_, _ = host.run("ip", "link", "del", "dev", dev)
}

var linkNamePattern = regexp.MustCompile(`^mft([0-9]+)$`)

// panelLinks lists the kernel links this panel made, by tunnel id.
func panelLinks() map[int]string {
	out, err := host.run("ip", "-o", "link", "show")
	if err != nil {
		return nil
	}
	links := map[int]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimSuffix(fields[1], ":"), "@")
		m := linkNamePattern.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		if alias, err := host.read(sysNet(name, "ifalias")); err != nil || !strings.HasPrefix(alias, aliasPrefix) {
			continue
		}
		id, _ := strconv.Atoi(m[1])
		links[id] = name
	}
	return links
}

func forwardOwner(id int) string { return fmt.Sprintf("tf%d", id) }
func replyOwner(id int) string   { return fmt.Sprintf("tr%d", id) }

// Replies to what came in through a tunnel leave by a routing table of its own.
func replyMark(id int) uint32 { return 0x4d460000 | uint32(id&0xffff) }
func replyTable(id int) int   { return 20000 + id }
func replyPref(id int) int    { return 10000 + id%20000 }

// portForwards turns a server end's forwards into the relay's DNAT rules.
func portForwards(t Tunnel) []egress.PortForward {
	var rules []egress.PortForward
	for _, f := range t.Settings.Forwards {
		if f.Proto == ProtoESP {
			rules = append(rules, egress.PortForward{Proto: egress.ProtoESP})
			continue
		}
		listen, _ := parsePorts(f.Listen)
		target := listen.low
		if f.Target != "" {
			r, _ := parsePorts(f.Target)
			target = r.low
		}
		for _, proto := range []string{ProtoTCP, ProtoUDP} {
			if f.Proto == proto || f.Proto == ProtoBoth {
				rules = append(rules, egress.PortForward{Proto: proto, Low: listen.low, High: listen.high, TargetLow: target})
			}
		}
	}
	return rules
}

// applyRouting gives a kernel link what its role needs beyond the link itself:
// the relay forwards its ports, the client routes replies back when sources are kept.
func applyRouting(t Tunnel) error {
	dev := t.Device()
	peer, err := netip.ParseAddr(t.Settings.PeerIP)
	if err != nil {
		return fmt.Errorf("tunnel: peer address: %w", err)
	}
	if t.Role == RoleServer && len(t.Settings.Forwards) > 0 {
		if err := egress.ApplyForwards(forwardOwner(t.ID), dev, peer, portForwards(t), t.Settings.NAT != NATPreserve); err != nil {
			return err
		}
	} else {
		egress.RemoveTables(forwardOwner(t.ID))
	}
	if t.Role == RoleClient && t.Settings.NAT == NATPreserve {
		return applyReplyRoute(t, peer)
	}
	removeReplyRoute(t.ID)
	return nil
}

func applyReplyRoute(t Tunnel, peer netip.Addr) error {
	dev, id := t.Device(), t.ID
	if err := egress.ApplyReplyMark(replyOwner(id), dev, replyMark(id)); err != nil {
		return err
	}
	fam := "-4"
	if peer.Is6() {
		fam = "-6"
	}
	pref, table, mark := strconv.Itoa(replyPref(id)), strconv.Itoa(replyTable(id)), fmt.Sprintf("0x%x", replyMark(id))
	for range 8 {
		if _, err := host.run("ip", fam, "rule", "del", "pref", pref); err != nil {
			break
		}
	}
	steps := [][]string{
		{fam, "route", "replace", "default", "via", peer.String(), "dev", dev, "table", table},
		{fam, "rule", "add", "pref", pref, "fwmark", mark, "table", table},
	}
	for _, args := range steps {
		if out, err := host.run("ip", args...); err != nil {
			return fmt.Errorf("tunnel: ip %s: %w (%s)", strings.Join(args, " "), err, out)
		}
	}
	// Strict reverse path filtering would drop sources that are not routed out this link.
	if peer.Is4() {
		_ = host.write(filepath.Join("/proc/sys/net/ipv4/conf", dev, "rp_filter"), "2\n")
	}
	return nil
}

func removeReplyRoute(id int) {
	egress.RemoveTables(replyOwner(id))
	pref, table := strconv.Itoa(replyPref(id)), strconv.Itoa(replyTable(id))
	for _, fam := range []string{"-4", "-6"} {
		for range 8 {
			if _, err := host.run("ip", fam, "rule", "del", "pref", pref); err != nil {
				break
			}
		}
		_, _ = host.run("ip", fam, "route", "flush", "table", table)
	}
}
