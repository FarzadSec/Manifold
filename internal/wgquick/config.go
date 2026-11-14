package wgquick

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// The first line of a config this package wrote; anything else is not ours to change.
var ownerLine = regexp.MustCompile(`^# Managed by Manifold: inbound (\d+)\.`)

func configPath(dir, name string) string { return filepath.Join(dir, name+".conf") }

// configOwner returns the inbound a config was written for, or false for a
// config written by anyone else.
func configOwner(content string) (int, bool) {
	match := ownerLine.FindStringSubmatch(content)
	if match == nil {
		return 0, false
	}
	id, err := strconv.Atoi(match[1])
	return id, err == nil
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// renderConfig writes the interface as a wg-quick config. Routes are left to
// the manager (Table = off): it must add them for peers set after the start too.
func renderConfig(inst Instance) string {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	line("# Managed by Manifold: inbound %d. Changes made here are overwritten.", inst.Id)
	line("[Interface]")
	line("PrivateKey = %s", inst.PrivateKey)
	line("Address = %s", joinPrefixes(inst.Addresses))
	line("ListenPort = %d", inst.ListenPort)
	if inst.MTU > 0 {
		line("MTU = %d", inst.MTU)
	}
	line("Table = off")
	for _, hook := range []struct {
		key      string
		commands []string
	}{{"PreUp", inst.Hooks.PreUp}, {"PostUp", inst.Hooks.PostUp}, {"PreDown", inst.Hooks.PreDown}, {"PostDown", inst.Hooks.PostDown}} {
		for _, command := range hook.commands {
			line("%s = %s", hook.key, command)
		}
	}
	for _, p := range inst.Peers {
		line("")
		line("# %s", oneLine(p.Name))
		line("[Peer]")
		line("PublicKey = %s", p.PublicKey)
		if p.PresharedKey != "" {
			line("PresharedKey = %s", p.PresharedKey)
		}
		line("AllowedIPs = %s", joinPrefixes(p.AllowedIPs))
	}
	return b.String()
}
