package openvpn

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// A unix socket address holds at most 107 bytes on Linux.
const maxSocketPath = 107

// files are the paths one server's process reads and writes.
type files struct {
	dir      string
	config   string
	caCert   string
	cert     string
	key      string
	tlsCrypt string
	socket   string
	pid      string
}

func filesIn(baseDir string, id int) files {
	dir := filepath.Join(baseDir, strconv.Itoa(id))
	return files{
		dir:      dir,
		config:   filepath.Join(dir, "server.conf"),
		caCert:   filepath.Join(dir, "ca.crt"),
		cert:     filepath.Join(dir, "server.crt"),
		key:      filepath.Join(dir, "server.key"),
		tlsCrypt: filepath.Join(dir, "tls-crypt.key"),
		socket:   filepath.Join(dir, "mgmt.sock"),
		pid:      filepath.Join(dir, "server.pid"),
	}
}

func (f files) written() []string {
	return []string{f.config, f.caCert, f.cert, f.key, f.tlsCrypt, f.socket, f.pid}
}

// write stores the server's config and keys, readable by root alone.
func (f files) write(inst Instance) error {
	if len(f.socket) > maxSocketPath {
		return fmt.Errorf("openvpn: %q is too long for a unix socket; use a shorter bin folder", f.socket)
	}
	if err := os.MkdirAll(f.dir, 0o700); err != nil {
		return err
	}
	_ = os.Remove(f.socket)
	contents := map[string]string{
		f.caCert:   inst.CACert,
		f.cert:     inst.ServerCert,
		f.key:      inst.ServerKey,
		f.tlsCrypt: inst.TLSCryptKey,
		f.config:   renderServerConfig(inst, f),
	}
	for path, content := range contents {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func (f files) remove() {
	for _, path := range f.written() {
		_ = os.Remove(path)
	}
	_ = os.Remove(f.dir)
}

// quoted writes a path as one config argument, whatever characters it holds.
func quoted(path string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(path) + `"`
}

// listenAddress returns the address to bind, or the zero Addr for every address.
func listenAddress(listen string) (netip.Addr, bool) {
	if listen == "" {
		return netip.Addr{}, true
	}
	addr, err := netip.ParseAddr(listen)
	if err != nil {
		return netip.Addr{}, false
	}
	if addr.IsUnspecified() && addr.Is4() {
		return netip.Addr{}, true
	}
	return addr, true
}

// serverProto maps the inbound's transport onto OpenVPN's server-side names.
func serverProto(proto string, listen netip.Addr) string {
	v6 := listen.IsValid() && listen.Is6()
	switch {
	case proto == ProtoTCP && v6:
		return "tcp6-server"
	case proto == ProtoTCP:
		return "tcp-server"
	case v6:
		return "udp6"
	default:
		return "udp"
	}
}

func netmask(prefix netip.Prefix) string {
	return net.IP(net.CIDRMask(prefix.Bits(), 32)).String()
}

func renderServerConfig(inst Instance, f files) string {
	listen, _ := listenAddress(inst.Listen)
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	line("# Written by Manifold for inbound %d. Changes made here are overwritten.", inst.Id)
	if listen.IsValid() && !listen.IsUnspecified() {
		line("local %s", listen)
	}
	line("port %d", inst.Port)
	line("proto %s", serverProto(inst.Proto, listen))
	line("dev %s", deviceName(inst.Id))
	line("dev-type tun")
	line("topology subnet")
	line("server %s %s", inst.Subnet.Addr(), netmask(inst.Subnet))
	line("ca %s", quoted(f.caCert))
	line("cert %s", quoted(f.cert))
	line("key %s", quoted(f.key))
	line("dh none")
	switch inst.TLSMode {
	case TLSModeAuth:
		line("tls-auth %s 0", quoted(f.tlsCrypt))
		line("auth SHA256")
	case TLSModeNone:
	default:
		line("tls-crypt %s", quoted(f.tlsCrypt))
	}
	line("tls-version-min 1.2")
	if len(inst.DataCiphers) > 0 {
		line("data-ciphers %s", strings.Join(inst.DataCiphers, ":"))
	}
	if inst.MTU > 0 {
		line("tun-mtu %d", inst.MTU)
	}
	if inst.MSSFix > 0 {
		line("mssfix %d", inst.MSSFix)
	}
	// Logins are decided by the panel, and one account may connect several devices.
	line("verify-client-cert none")
	line("username-as-common-name")
	line("duplicate-cn")
	line("keepalive 10 60")
	line("persist-key")
	line("persist-tun")
	// Kernel offload keeps byte counters out of the management interface.
	line("ignore-unknown-option disable-dco")
	line("disable-dco")
	if inst.Proto == ProtoUDP {
		line("explicit-exit-notify 1")
	}
	line("management %s unix", quoted(f.socket))
	line("management-client-auth")
	// Nobody may connect before the panel is attached, or after it is gone.
	line("management-hold")
	line("management-signal")
	line("writepid %s", quoted(f.pid))
	if !inst.SplitTunnel {
		line(`push "redirect-gateway def1 bypass-dhcp"`)
	}
	for _, dns := range inst.DNS {
		line(`push "dhcp-option DNS %s"`, dns)
	}
	line("verb 3")
	return b.String()
}
