package sub

import (
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/openvpn"
)

// genOpenVPNLink returns the account's profile for each advertised endpoint,
// each wrapped as an openvpn:// link. The body a proxy app imports gets none:
// such an app cannot read an OpenVPN profile.
func (s *SubService) genOpenVPNLink(inbound *model.Inbound, email string) string {
	if inbound.Protocol != model.OpenVPN || s.subscriptionBody {
		return ""
	}
	server := openvpn.ServerForProfiles(inbound.Settings)
	if server == nil {
		return ""
	}
	client, ok := s.clientForLink(inbound, email)
	if !ok || client.Password == "" {
		return ""
	}
	settings := *server
	if s.signedInApp {
		settings.PromptCredentials = false
	}
	endpoints := s.advertisedEndpoints(inbound)
	links := make([]string, 0, len(endpoints))
	for _, e := range endpoints {
		profile := openvpn.Profile{
			Remark:   s.endpointRemark(inbound, email, e.ep, ""),
			Host:     e.Address,
			Port:     e.Port,
			Server:   settings,
			Username: client.Email,
			Password: client.Password,
		}
		links = append(links, openvpn.ShareLink(profile.Render()))
	}
	return strings.Join(links, "\n")
}
