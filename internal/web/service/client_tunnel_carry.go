package service

import (
	"net/netip"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type heldTunnelAddress struct {
	prefix netip.Prefix
	email  string
}

// tunnelAddressCarry decides whether the tunnel address an account's record carries may go
// on one WireGuard/AmneziaWG inbound it is being attached to, or that inbound allocates one.
type tunnelAddressCarry struct {
	inbound *model.Inbound
	pool    []netip.Prefix
	held    []heldTunnelAddress
}

// newTunnelAddressCarry returns nil for an inbound without tunnel addresses.
func newTunnelAddressCarry(inboundSvc *InboundService, ib *model.Inbound) (*tunnelAddressCarry, error) {
	if ib.Protocol != model.WireGuard && ib.Protocol != model.AmneziaWG {
		return nil, nil
	}
	existing, err := inboundSvc.GetClients(ib)
	if err != nil {
		return nil, err
	}
	carry := &tunnelAddressCarry{inbound: ib}
	if ib.Protocol == model.WireGuard {
		if carry.pool, err = wireguardPoolScopes(wireguardInboundBase(ib.Settings, existing), true); err != nil {
			return nil, err
		}
		for _, host := range wireguardServerHosts(ib.Settings) {
			carry.hold("", host)
		}
	}
	for _, c := range existing {
		carry.hold(c.Email, c.AllowedIPs...)
	}
	err = forEachOtherTunnelClient(database.GetDB(), inboundSvc, ib.Id, func(_ *model.Inbound, c model.Client) {
		carry.hold(c.Email, c.AllowedIPs...)
	})
	return carry, err
}

func (c *tunnelAddressCarry) hold(email string, entries ...string) {
	for _, entry := range entries {
		if prefix, ok := wireguardClaimedPrefix(entry); ok {
			c.held = append(c.held, heldTunnelAddress{prefix: prefix, email: strings.ToLower(email)})
		}
	}
}

// keep reports whether addresses may stay on the inbound for email, and holds them if so:
// they must lie in the inbound's own pool, and no other account may hold them anywhere.
func (c *tunnelAddressCarry) keep(email string, addresses []string) bool {
	if len(addresses) == 0 || !c.inPool(addresses) {
		return false
	}
	email = strings.ToLower(email)
	for _, entry := range addresses {
		prefix, ok := wireguardClaimedPrefix(entry)
		if !ok {
			return false
		}
		for _, held := range c.held {
			if held.email != email && held.prefix.Overlaps(prefix) {
				return false
			}
		}
	}
	c.hold(email, addresses...)
	return true
}

func (c *tunnelAddressCarry) inPool(addresses []string) bool {
	if c.inbound.Protocol == model.AmneziaWG {
		return addressesFitAmneziaWGInbound(addresses, c.inbound)
	}
	for _, entry := range addresses {
		host := wireguardHostAddr(entry)
		fits := false
		for _, scope := range c.pool {
			fits = fits || scope.Contains(host)
		}
		if !fits {
			return false
		}
	}
	return true
}
