package runtime

import (
	"context"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/l2tp"
	"github.com/mhsanaei/3x-ui/v3/internal/openvpn"
	"github.com/mhsanaei/3x-ui/v3/internal/wgquick"
)

// nativeEngine serves a protocol with a daemon the panel manages, outside Xray.
type nativeEngine interface {
	// apply starts or updates the inbound's server; false means it has nothing to serve.
	apply(ib *model.Inbound) (served bool, err error)
	remove(inboundID int)
}

var nativeEngines = map[model.Protocol]nativeEngine{
	model.OpenVPN:   openvpnEngine{},
	model.WireGuard: wireguardEngine{},
	model.L2TP:      l2tpEngine{},
}

// IsNativeProtocol reports whether a protocol is served outside Xray by an
// engine of this package.
func IsNativeProtocol(p model.Protocol) bool {
	_, ok := nativeEngines[p]
	return ok
}

type openvpnEngine struct{}

func (openvpnEngine) apply(ib *model.Inbound) (bool, error) {
	inst, ok := openvpn.InstanceFromInbound(ib)
	if !ok {
		return false, nil
	}
	return true, openvpn.GetManager().Ensure(inst)
}

func (openvpnEngine) remove(inboundID int) { openvpn.GetManager().Remove(inboundID) }

type wireguardEngine struct{}

func (wireguardEngine) apply(ib *model.Inbound) (bool, error) {
	inst, ok := wgquick.InstanceFromInbound(ib)
	if !ok {
		return false, nil
	}
	return true, wgquick.GetManager().Ensure(inst)
}

func (wireguardEngine) remove(inboundID int) { wgquick.GetManager().Remove(inboundID) }

type l2tpEngine struct{}

func (l2tpEngine) apply(ib *model.Inbound) (bool, error) {
	inst, ok := l2tp.InstanceFromInbound(ib)
	if !ok {
		return false, nil
	}
	return true, l2tp.GetManager().Ensure(inst)
}

func (l2tpEngine) remove(inboundID int) { l2tp.GetManager().Remove(inboundID) }

func addNativeInbound(engine nativeEngine, ib *model.Inbound) error {
	served, err := engine.apply(ib)
	if !served {
		engine.remove(ib.Id)
	}
	return err
}

// updateNativeInbound applies an update without stopping a server that only
// needs its accounts changed; apply decides between that and a restart.
func (l *Local) updateNativeInbound(ctx context.Context, oldIb, newIb *model.Inbound) error {
	oldEngine, wasNative := nativeEngines[oldIb.Protocol]
	newEngine, isNative := nativeEngines[newIb.Protocol]
	if wasNative && oldIb.Protocol != newIb.Protocol {
		oldEngine.remove(oldIb.Id)
	}
	if !isNative {
		if !newIb.Enable {
			return nil
		}
		return l.AddInbound(ctx, newIb)
	}
	if !wasNative {
		_ = l.DelInbound(ctx, oldIb)
	}
	if !newIb.Enable {
		newEngine.remove(newIb.Id)
		return nil
	}
	return addNativeInbound(newEngine, newIb)
}
