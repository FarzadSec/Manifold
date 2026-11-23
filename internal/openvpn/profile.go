package openvpn

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// Profile is what one account needs to connect to one OpenVPN inbound.
type Profile struct {
	Remark   string
	Host     string
	Port     int
	Server   ServerSettings
	Username string
	Password string
}

// ProfileFileName is the download name of an account's profile for an inbound.
func ProfileFileName(username string, inboundID int) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		}
		return '_'
	}, username)
	return fmt.Sprintf("%s-%d.ovpn", safe, inboundID)
}

func clientProto(proto string) string {
	if proto == ProtoTCP {
		return "tcp-client"
	}
	return "udp"
}

func inlineBlock(b *strings.Builder, tag, content string) {
	fmt.Fprintf(b, "<%s>\n%s\n</%s>\n", tag, strings.TrimSpace(content), tag)
}

// Render returns the .ovpn file an OpenVPN app imports.
func (p Profile) Render() string {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	if remark := strings.Join(strings.Fields(p.Remark), " "); remark != "" {
		line("# %s", remark)
	}
	line("client")
	line("dev tun")
	line("proto %s", clientProto(p.Server.Proto))
	line("remote %s %d", p.Host, p.Port)
	line("resolv-retry infinite")
	line("nobind")
	line("persist-key")
	line("persist-tun")
	line("remote-cert-tls server")
	// Over UDP only this tells the server the device left; without it the seat waits for keepalive.
	if p.Server.Proto != ProtoTCP {
		line("explicit-exit-notify 1")
	}
	serverName := p.Server.ServerName
	if serverName == "" {
		serverName = serverCommonName
	}
	line("verify-x509-name %s name", serverName)
	if len(p.Server.DataCiphers) > 0 {
		line("data-ciphers %s", strings.Join(p.Server.DataCiphers, ":"))
	}
	mtu, mssfix := p.Server.dataChannelSizes()
	if mtu > 0 {
		line("tun-mtu %d", mtu)
	}
	if mssfix > 0 {
		line("mssfix %d", mssfix)
	}
	if reneg := renegLine(p.Server.RenegSec); reneg != "" {
		line("%s", reneg)
	}
	// Windows would otherwise keep asking the DNS servers outside the tunnel.
	line("ignore-unknown-option block-outside-dns")
	line("setenv opt block-outside-dns")
	// Logins carry no client certificate; without saying so OpenVPN Connect asks for one.
	line("setenv CLIENT_CERT 0")
	line("verb 3")
	if p.Server.ClientDirectives != "" {
		b.WriteString(p.Server.ClientDirectives + "\n")
	}
	if p.Server.PromptCredentials || p.Username == "" {
		line("auth-user-pass")
	} else {
		inlineBlock(&b, "auth-user-pass", p.Username+"\n"+p.Password)
	}
	inlineBlock(&b, "ca", p.Server.CACert)
	switch p.Server.TLSMode {
	case TLSModeAuth:
		line("auth SHA256")
		line("key-direction 1")
		inlineBlock(&b, "tls-auth", p.Server.TLSCryptKey)
	case TLSModeNone:
	default:
		inlineBlock(&b, "tls-crypt", p.Server.TLSCryptKey)
	}
	return b.String()
}

// ShareLinkScheme marks a profile carried as a link, the form the panel and
// the subscription page pass around; no OpenVPN app reads it.
const ShareLinkScheme = "openvpn://"

// ShareLink wraps a rendered profile as one link line.
func ShareLink(profile string) string {
	return ShareLinkScheme + base64.RawURLEncoding.EncodeToString([]byte(profile))
}
