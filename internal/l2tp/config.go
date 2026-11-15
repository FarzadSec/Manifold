package l2tp

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// A unix socket address holds at most 107 bytes on Linux.
const maxSocketPath = 107

// IKE and ESP offers of the built-in clients: iOS and macOS, Windows, Android, strongSwan.
const (
	ikeProposals = "aes256-sha256-modp2048,aes256-sha256-modp1024,aes256-sha1-modp2048,aes256-sha1-modp1024,aes128-sha1-modp1024,3des-sha1-modp1024"
	espProposals = "aes256-sha256,aes256-sha1,aes128-sha1,3des-sha1"
	// The name of the connection in strongSwan.
	ipsecName = "manifold-l2tp"
)

// pppd runs the helper through a shell and takes it only if it starts with a slash.
var plainPath = regexp.MustCompile(`^/[A-Za-z0-9_./+=@-]*$`)

// files are the paths the daemons of one inbound read and write.
type files struct {
	dir        string
	xl2tpd     string
	pppOptions string
	ipUp       string
	ipDown     string
	secrets    string
	socket     string
	pid        string
	control    string
	pppLog     string
}

func filesIn(baseDir string, id int) files {
	dir := filepath.Join(baseDir, strconv.Itoa(id))
	return files{
		dir:        dir,
		xl2tpd:     filepath.Join(dir, "xl2tpd.conf"),
		pppOptions: filepath.Join(dir, "ppp-options"),
		ipUp:       filepath.Join(dir, "ip-up"),
		ipDown:     filepath.Join(dir, "ip-down"),
		secrets:    filepath.Join(dir, "l2tp-secrets"),
		socket:     filepath.Join(dir, "control.sock"),
		pid:        filepath.Join(dir, "xl2tpd.pid"),
		control:    filepath.Join(dir, "xl2tpd.control"),
		pppLog:     filepath.Join(dir, "ppp.log"),
	}
}

func renderXl2tpd(inst Instance, f files) string {
	first, last := inst.pool()
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	line("[global]")
	line("port = %d", Port)
	line("access control = no")
	line("auth file = %s", f.secrets)
	line("")
	line("[lns default]")
	line("ip range = %s-%s", first, last)
	line("local ip = %s", inst.serverAddress())
	line("require authentication = yes")
	line("name = manifold")
	// The peer's public address reaches the session scripts as their last argument.
	line("pass peer = yes")
	line("length bit = yes")
	line("pppoptfile = %s", f.pppOptions)
	return b.String()
}

// renderPPPOptions makes pppd ask the panel about every login and report
// every session to it. exe is the panel's own program.
func renderPPPOptions(inst Instance, f files, exe string, debug bool) (string, error) {
	if !plainPath.MatchString(exe) || !plainPath.MatchString(f.socket) {
		return "", fmt.Errorf("l2tp: %q with %q cannot be started by pppd: install the panel in a path of letters, digits and _ . / + = @ -", exe, f.socket)
	}
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	line("auth")
	line("require-mschap-v2")
	// IPsec encrypts the session; PPP's own compression and encryption stay off.
	line("noccp")
	line("nodefaultroute")
	line("ipcp-accept-local")
	line("ipcp-accept-remote")
	for _, server := range inst.DNS {
		line("ms-dns %s", server)
	}
	line("mtu %d", inst.MTU)
	line("mru %d", inst.MTU)
	line("lcp-echo-interval 30")
	line("lcp-echo-failure 4")
	line("plugin winbind.so")
	line("ntlm_auth-helper \"%s l2tp-auth %s\"", exe, f.socket)
	line("ip-up-script %s", f.ipUp)
	line("ip-down-script %s", f.ipDown)
	if debug {
		line("debug")
		line("logfile %s", f.pppLog)
	}
	return b.String(), nil
}

// renderHook is the script pppd runs when a session's link comes up or goes
// down: interface, the peer's tunnel address and its public address go to the panel.
func renderHook(event string, f files, exe string) string {
	return fmt.Sprintf("#!/bin/sh\nexec %s l2tp-hook %s %s \"$1\" \"$5\" \"$6\"\n", exe, event, f.socket)
}

// renderSwanctl is the IPsec side: any peer that knows the key may protect
// UDP port 1701 to this host in transport mode.
func renderSwanctl(inst Instance) string {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	line("# Managed by Manifold: inbound %d. Changes made here are overwritten.", inst.Id)
	line("connections {")
	line("    %s {", ipsecName)
	line("        version = 1")
	line("        proposals = %s", ikeProposals)
	line("        local {")
	line("            auth = psk")
	line("        }")
	line("        remote {")
	line("            auth = psk")
	line("        }")
	line("        children {")
	line("            %s {", ipsecName)
	line("                mode = transport")
	line("                local_ts = dynamic[udp/%d]", Port)
	line("                remote_ts = dynamic[udp]")
	line("                esp_proposals = %s", espProposals)
	line("            }")
	line("        }")
	line("    }")
	line("}")
	line("secrets {")
	line("    ike-%s {", ipsecName)
	// ValidatePSK leaves no character that would end the string.
	line("        secret = \"%s\"", inst.PSK)
	line("    }")
	line("}")
	return b.String()
}

// write stores what xl2tpd and pppd read, for root alone.
func (f files) write(inst Instance, exe string, debug bool) error {
	if len(f.socket) > maxSocketPath {
		return fmt.Errorf("l2tp: %q is too long for a unix socket; use a shorter bin folder", f.socket)
	}
	options, err := renderPPPOptions(inst, f, exe, debug)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(f.dir, 0o700); err != nil {
		return err
	}
	for _, file := range []struct {
		path, content string
		mode          os.FileMode
	}{
		{f.xl2tpd, renderXl2tpd(inst, f), 0o600},
		{f.pppOptions, options, 0o600},
		{f.secrets, "", 0o600},
		{f.ipUp, renderHook("up", f, exe), 0o700},
		{f.ipDown, renderHook("down", f, exe), 0o700},
	} {
		if err := os.WriteFile(file.path, []byte(file.content), file.mode); err != nil {
			return err
		}
		// WriteFile keeps the mode of a file that already exists.
		if err := os.Chmod(file.path, file.mode); err != nil {
			return err
		}
	}
	return nil
}

func (f files) remove() { _ = os.RemoveAll(f.dir) }
