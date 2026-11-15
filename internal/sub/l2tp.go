package sub

import (
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/l2tp"
)

// genL2TPLink returns the account's server, login and IPsec key for each
// advertised endpoint, each as an l2tp:// link. The body a proxy app imports
// gets none: L2TP is set up in the device's own VPN settings.
func (s *SubService) genL2TPLink(inbound *model.Inbound, email string) string {
	if inbound.Protocol != model.L2TP || s.subscriptionBody {
		return ""
	}
	server := l2tp.ServerOf(inbound.Settings)
	if server == nil {
		return ""
	}
	client, ok := s.clientForLink(inbound, email)
	if !ok || client.Password == "" {
		return ""
	}
	endpoints := s.advertisedEndpoints(inbound)
	links := make([]string, 0, len(endpoints))
	for _, e := range endpoints {
		links = append(links, l2tp.ShareLink(l2tp.Account{
			Host:          e.Address,
			Username:      client.Email,
			Password:      client.Password,
			PSK:           server.PSK,
			RequiresIPsec: server.RequiresIPsec(),
			Remark:        s.endpointRemark(inbound, email, e.ep, ""),
		}))
	}
	return strings.Join(links, "\n")
}
