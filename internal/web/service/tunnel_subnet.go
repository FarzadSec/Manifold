package service

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"net/netip"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/l2tp"
	"github.com/mhsanaei/3x-ui/v3/internal/openvpn"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/wgquick"

	"gorm.io/gorm"
)

var tunnelProtocols = []model.Protocol{model.WireGuard, model.OpenVPN, model.L2TP}

// tunnelSubnets are the networks a WireGuard, OpenVPN or L2TP inbound hands out on its host.
func tunnelSubnets(ib *model.Inbound) []netip.Prefix {
	switch ib.Protocol {
	case model.WireGuard:
		return wgquick.TunnelSubnets(ib.Settings)
	case model.OpenVPN:
		if subnet, ok := openvpn.TunnelSubnet(ib.Settings); ok {
			return []netip.Prefix{subnet}
		}
	case model.L2TP:
		if subnet, ok := l2tp.TunnelSubnet(ib.Settings); ok {
			return []netip.Prefix{subnet}
		}
	}
	return nil
}

// tunnelSubnetGiven reports whether the admin chose the inbound's network; one left
// empty takes the engine's default, which every other such inbound would share.
func tunnelSubnetGiven(ib *model.Inbound) bool {
	switch ib.Protocol {
	case model.WireGuard:
		return wgquick.NamesSubnet(ib.Settings)
	case model.OpenVPN, model.L2TP:
		var parsed struct {
			Server *struct {
				Subnet string `json:"subnet"`
			} `json:"server"`
		}
		_ = json.Unmarshal([]byte(ib.Settings), &parsed)
		return parsed.Server != nil && parsed.Server.Subnet != ""
	}
	return true
}

// sameHostTunnelInbounds are the other WireGuard, OpenVPN and L2TP inbounds on ib's host.
func sameHostTunnelInbounds(db *gorm.DB, ib *model.Inbound) ([]*model.Inbound, error) {
	query := db.Model(model.Inbound{}).Where("protocol IN ? AND id <> ?", tunnelProtocols, ib.Id)
	if ib.NodeID == nil {
		query = query.Where("node_id IS NULL")
	} else {
		query = query.Where("node_id = ?", *ib.NodeID)
	}
	var others []*model.Inbound
	err := query.Find(&others).Error
	return others, err
}

// checkTunnelSubnetConflict refuses a tunnel network another inbound on the host already uses: the
// kernel routes each address to one interface, so the other inbound's peers lost their replies.
func checkTunnelSubnetConflict(db *gorm.DB, ib *model.Inbound) error {
	mine := tunnelSubnets(ib)
	if len(mine) == 0 {
		return nil
	}
	others, err := sameHostTunnelInbounds(db, ib)
	if err != nil {
		return err
	}
	for _, other := range others {
		for _, theirs := range tunnelSubnets(other) {
			for _, ours := range mine {
				if ours.Overlaps(theirs) {
					return common.NewErrorf("the tunnel network %s overlaps %s of inbound %q (port %d); give each WireGuard, OpenVPN and L2TP inbound a network of its own", ours, theirs, other.Remark, other.Port)
				}
			}
		}
	}
	return nil
}

// assignFreeTunnelSubnet moves a new inbound that left its network empty off the engine's default
// when that is taken on the host, to the next free network of the same size.
func assignFreeTunnelSubnet(db *gorm.DB, ib *model.Inbound, given bool) error {
	current := tunnelSubnets(ib)
	if given || len(current) != 1 {
		return nil
	}
	others, err := sameHostTunnelInbounds(db, ib)
	if err != nil {
		return err
	}
	var used []netip.Prefix
	for _, other := range others {
		used = append(used, tunnelSubnets(other)...)
	}
	if ib.NodeID == nil {
		used = append(used, hostNetworks()...)
	}
	free, ok := freeSubnetFrom(current[0], used)
	if !ok || free == current[0] {
		return nil
	}
	settings, err := withTunnelSubnet(ib.Protocol, ib.Settings, free)
	if err != nil {
		return err
	}
	ib.Settings = settings
	return nil
}

// freeSubnetFrom walks the networks of start's size inside 10.0.0.0/8 from start on.
func freeSubnetFrom(start netip.Prefix, used []netip.Prefix) (netip.Prefix, bool) {
	private := netip.MustParsePrefix("10.0.0.0/8")
	if !start.Addr().Is4() || !private.Contains(start.Addr()) || start.Bits() < 8 {
		return netip.Prefix{}, false
	}
	size := uint32(1) << (32 - start.Bits())
	base := binary.BigEndian.Uint32(start.Masked().Addr().AsSlice())
	span := uint32(1) << 24
	for i := uint32(0); i < span/size; i++ {
		offset := (base - 0x0a000000 + i*size) % span
		var raw [4]byte
		binary.BigEndian.PutUint32(raw[:], 0x0a000000+offset)
		candidate := netip.PrefixFrom(netip.AddrFrom4(raw), start.Bits())
		taken := false
		for _, u := range used {
			if candidate.Overlaps(u) {
				taken = true
				break
			}
		}
		if !taken {
			return candidate, true
		}
	}
	return netip.Prefix{}, false
}

func withTunnelSubnet(protocol model.Protocol, settings string, subnet netip.Prefix) (string, error) {
	root := map[string]any{}
	if err := json.Unmarshal([]byte(settings), &root); err != nil {
		return "", err
	}
	if protocol == model.WireGuard {
		root["subnetIp"] = subnet.Addr().String()
		root["subnetCidr"] = subnet.Bits()
	} else {
		server, _ := root["server"].(map[string]any)
		if server == nil {
			server = map[string]any{}
		}
		server["subnet"] = subnet.String()
		root["server"] = server
	}
	out, err := json.MarshalIndent(root, "", "  ")
	return string(out), err
}

// hostNetworks are the IPv4 networks this server's own interfaces sit on.
func hostNetworks() []netip.Prefix {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []netip.Prefix
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		prefix, err := netip.ParsePrefix(ipNet.String())
		if err == nil && prefix.Addr().Is4() && !prefix.Addr().IsLoopback() {
			out = append(out, prefix.Masked())
		}
	}
	return out
}
