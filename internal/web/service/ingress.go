package service

import (
	"encoding/json"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/ingress"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

var ingressManager = ingress.NewManager()

// IngressService keeps the shared ports in the database and bound on this server.
type IngressService struct{}

// IngressStatus is a listener as saved, and the reason it is not serving if it is not.
type IngressStatus struct {
	model.IngressListener
	Error string `json:"error,omitempty" example:""`
}

func (s *IngressService) List() ([]IngressStatus, error) {
	var rows []model.IngressListener
	if err := database.GetDB().Order("port, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	errs := ingressManager.Errors()
	out := make([]IngressStatus, 0, len(rows))
	for _, r := range rows {
		if r.Routes == nil {
			r.Routes = []model.IngressRoute{}
		}
		out = append(out, IngressStatus{IngressListener: r, Error: errs[r.Id]})
	}
	return out, nil
}

// Save creates or replaces a listener and binds the result at once.
func (s *IngressService) Save(in *model.IngressListener) error {
	if err := normalizeIngress(in); err != nil {
		return err
	}
	db := database.GetDB()
	if err := checkIngressPort(db, in); err != nil {
		return err
	}
	var err error
	if in.Id > 0 {
		err = db.Model(&model.IngressListener{}).Where("id = ?", in.Id).Updates(map[string]any{
			"remark": in.Remark, "enable": in.Enable, "listen": in.Listen, "port": in.Port,
			"routes": ingressRoutesJSON(in.Routes), "default_target": in.DefaultTarget,
			"default_proxy_protocol": in.DefaultProxyProtocol,
		}).Error
	} else {
		err = db.Create(in).Error
	}
	if err != nil {
		return err
	}
	return s.Apply()
}

func (s *IngressService) Delete(id int) error {
	if err := database.GetDB().Delete(&model.IngressListener{}, id).Error; err != nil {
		return err
	}
	return s.Apply()
}

// Apply binds every enabled listener as saved; one that cannot bind reports why in List.
func (s *IngressService) Apply() error {
	var rows []model.IngressListener
	if err := database.GetDB().Where("enable = ?", true).Find(&rows).Error; err != nil {
		return err
	}
	listeners := make([]ingress.Listener, 0, len(rows))
	for _, r := range rows {
		l := ingress.Listener{
			Id: r.Id, Listen: r.Listen, Port: r.Port,
			Default: ingress.Route{Target: r.DefaultTarget, ProxyProtocol: r.DefaultProxyProtocol},
		}
		for _, route := range r.Routes {
			l.Routes = append(l.Routes, ingress.Route{Hosts: route.Hosts, ALPN: route.ALPN, Target: route.Target, ProxyProtocol: route.ProxyProtocol})
		}
		listeners = append(listeners, l)
	}
	ingressManager.Apply(listeners)
	for id, reason := range ingressManager.Errors() {
		logger.Warning("ingress listener", id, "is not serving:", reason)
	}
	return nil
}

// ingressRoutesJSON is the routes column's stored form, for an Updates map that bypasses the serializer.
func ingressRoutesJSON(routes []model.IngressRoute) string {
	if routes == nil {
		routes = []model.IngressRoute{}
	}
	raw, _ := json.Marshal(routes)
	return string(raw)
}

func normalizeIngress(in *model.IngressListener) error {
	in.Remark = strings.TrimSpace(in.Remark)
	in.Listen = strings.TrimSpace(in.Listen)
	if in.Listen != "" {
		if _, err := netip.ParseAddr(in.Listen); err != nil {
			return common.NewError("listen address must be an IP address of this server, or empty for all")
		}
	}
	if in.Port < 1 || in.Port > 65535 {
		return common.NewError("port must be 1-65535")
	}
	if err := checkIngressTarget(in.DefaultTarget, in.DefaultProxyProtocol, true); err != nil {
		return err
	}
	if len(in.Routes) == 0 && in.DefaultTarget == "" {
		return common.NewError("a shared port needs a route or a default target")
	}
	for i := range in.Routes {
		r := &in.Routes[i]
		r.Remark = strings.TrimSpace(r.Remark)
		hosts := r.Hosts[:0]
		for _, h := range r.Hosts {
			h = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(h), "."))
			if h == "" {
				continue
			}
			if !ingress.ValidPattern(h) {
				return common.NewError("not a host name or pattern:", h)
			}
			hosts = append(hosts, h)
		}
		if len(hosts) == 0 {
			return common.NewError("every route names at least one host")
		}
		r.Hosts = hosts
		if err := checkIngressTarget(r.Target, r.ProxyProtocol, false); err != nil {
			return err
		}
	}
	return nil
}

func checkIngressTarget(target string, proxyProtocol int, optional bool) error {
	if proxyProtocol < 0 || proxyProtocol > 2 {
		return common.NewError("PROXY protocol must be 0 (off), 1 or 2")
	}
	if target == "" && optional {
		return nil
	}
	host, port, err := net.SplitHostPort(target)
	if err != nil || host == "" {
		return common.NewError("a target is host:port, such as 127.0.0.1:8443:", target)
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return common.NewError("target port must be 1-65535:", target)
	}
	return nil
}

// checkIngressPort refuses a port another shared port, a local inbound or the panel holds.
func checkIngressPort(db *gorm.DB, in *model.IngressListener) error {
	bind := bindAddr{listen: in.Listen}
	var others []model.IngressListener
	if err := db.Where("port = ? AND id <> ?", in.Port, in.Id).Find(&others).Error; err != nil {
		return err
	}
	for _, o := range others {
		if listenOverlaps(bindAddr{listen: o.Listen}, bind) {
			return common.NewError("another shared port already listens on", in.Port)
		}
	}
	var inbounds []*model.Inbound
	if err := db.Where("port = ? AND node_id IS NULL", in.Port).Find(&inbounds).Error; err != nil {
		return err
	}
	for _, ib := range inbounds {
		if inboundTransports(ib.Protocol, ib.StreamSettings, ib.Settings)&transportTCP != 0 &&
			listenOverlaps(inboundBindAddr(ib), bind) {
			return common.NewError("inbound", ib.Remark, "already listens on TCP port", in.Port)
		}
	}
	settings := &SettingService{}
	for name, get := range map[string]func() (int, error){"the panel": settings.GetPort, "the subscription server": settings.GetSubPort} {
		if p, err := get(); err == nil && p == in.Port {
			return common.NewError(name, "already listens on port", in.Port, "; route to it from the shared port instead")
		}
	}
	return nil
}

// ingressPortConflict is a shared port an inbound would collide with, for the inbound checks.
func ingressPortConflict(db *gorm.DB, inbound *model.Inbound, newBits transportBits) (*portConflictDetail, error) {
	if inbound.NodeID != nil || newBits&transportTCP == 0 {
		return nil, nil
	}
	var rows []model.IngressListener
	if err := db.Where("port = ?", inbound.Port).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		if listenOverlaps(bindAddr{listen: r.Listen}, inboundBindAddr(inbound)) {
			return &portConflictDetail{Tag: "ingress", Remark: strings.TrimSpace("shared port " + r.Remark), Listen: r.Listen, Port: r.Port, Transports: transportTCP}, nil
		}
	}
	return nil, nil
}
