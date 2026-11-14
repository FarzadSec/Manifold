package service

import (
	"context"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/openvpn"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
)

// isNativeProtocol reports whether a panel-managed daemon, not Xray, serves the protocol.
func isNativeProtocol(p model.Protocol) bool { return runtime.IsNativeProtocol(p) }

// applyLocalNative pushes a local native inbound's current clients to its
// engine right away, instead of waiting for the engine's next reconcile.
func (s *InboundService) applyLocalNative(inboundId int) {
	inbound, err := s.GetInbound(inboundId)
	if err != nil || inbound == nil || !isNativeProtocol(inbound.Protocol) || inbound.NodeID != nil {
		return
	}
	rt, err := s.runtimeFor(inbound)
	if err != nil {
		return
	}
	payload := inbound
	if inbound.Enable {
		if built, bErr := s.buildInboundForLocalRuntime(database.GetDB(), inbound); bErr == nil {
			payload = built
		}
	}
	if err := rt.UpdateInbound(context.Background(), inbound, payload); err != nil {
		logger.Warningf("%s: applying inbound %d: %v", inbound.Protocol, inboundId, err)
	}
}

// DesiredOpenVPNInstances returns the servers this panel should be running:
// its own enabled OpenVPN inbounds, without the accounts a limit switched off.
func (s *InboundService) DesiredOpenVPNInstances() ([]openvpn.Instance, error) {
	db := database.GetDB()
	var inbounds []*model.Inbound
	err := db.Model(model.Inbound{}).
		Where("protocol = ? AND enable = ? AND node_id IS NULL", model.OpenVPN, true).
		Find(&inbounds).Error
	if err != nil || len(inbounds) == 0 {
		return nil, err
	}
	instances := make([]openvpn.Instance, 0, len(inbounds))
	var emails []string
	for _, ib := range inbounds {
		inst, ok := openvpn.InstanceFromInbound(ib)
		if !ok {
			continue
		}
		instances = append(instances, inst)
		for _, user := range inst.Users {
			emails = append(emails, user.Name)
		}
	}
	disabled, err := trafficDisabledEmails(db, emails)
	if err != nil {
		return nil, err
	}
	for i := range instances {
		kept := make([]openvpn.User, 0, len(instances[i].Users))
		for _, user := range instances[i].Users {
			if _, off := disabled[user.Name]; !off {
				kept = append(kept, user)
			}
		}
		instances[i].Users = kept
	}
	return instances, nil
}

// prepareOpenVPNInbound completes and validates an OpenVPN inbound before it
// is stored; oldSettings is the stored row's settings on an update.
func prepareOpenVPNInbound(inbound *model.Inbound, oldSettings string) error {
	if inbound.Protocol != model.OpenVPN {
		return nil
	}
	settings, err := openvpn.NormalizeSettings(inbound.Settings, oldSettings)
	if err != nil {
		return common.NewError(err.Error())
	}
	inbound.Settings = settings
	if inbound.NodeID == nil && inbound.Enable {
		if _, err := openvpn.FindBinary(); err != nil {
			return common.NewError("OpenVPN is not installed on this server. Install the openvpn package, then save again.")
		}
	}
	return nil
}

// openvpnTransport is the one transport an OpenVPN inbound listens on.
func openvpnTransport(settings string) transportBits {
	if server := openvpn.ServerForProfiles(settings); server != nil && server.Proto == openvpn.ProtoTCP {
		return transportTCP
	}
	return transportUDP
}
