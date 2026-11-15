package l2tp

import (
	"net"
	"net/url"
)

// ShareLinkScheme marks a link that carries an L2TP account. No client
// imports such a link; the panel's pages and the bot read it and show its parts.
const ShareLinkScheme = "l2tp://"

// Account is what a person types into the VPN settings of their device.
type Account struct {
	Host     string
	Username string
	Password string
	// PSK is the IPsec key. RequiresIPsec says whether the server takes a session without it.
	PSK           string
	RequiresIPsec bool
	Remark        string
}

// ShareLink renders an account as l2tp://user:password@host?ipsec=1&psk=key#remark,
// where ipsec=0 marks a server that also takes sessions without IPsec.
func ShareLink(a Account) string {
	query := url.Values{}
	query.Set("ipsec", "0")
	if a.RequiresIPsec {
		query.Set("ipsec", "1")
	}
	query.Set("psk", a.PSK)
	host := a.Host
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		host = "[" + host + "]"
	}
	link := url.URL{
		Scheme:   "l2tp",
		User:     url.UserPassword(a.Username, a.Password),
		Host:     host,
		RawQuery: query.Encode(),
		Fragment: a.Remark,
	}
	return link.String()
}
