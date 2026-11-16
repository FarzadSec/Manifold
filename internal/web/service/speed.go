package service

import (
	"net/netip"
	"strings"
	"sync"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/l2tp"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/openvpn"
	"github.com/mhsanaei/3x-ui/v3/internal/speedlimit"
)

// hostLimiter is this host's one speed table; every panel, master or node, polices its own traffic.
var hostLimiter = speedlimit.New()

// speedSessions reads the tunnel sessions the speed limit follows; tests substitute their own.
var speedSessions = struct {
	openvpn func() map[string][]netip.Prefix
	l2tp    func() map[string][]string
}{
	openvpn: func() map[string][]netip.Prefix { return openvpn.GetManager().SessionAddresses() },
	l2tp:    func() map[string][]string { return l2tp.GetManager().SessionInterfaces() },
}

// SpeedService keeps the host's speed table in step with its limited users.
type SpeedService struct {
	inboundService InboundService
}

var (
	speedSyncMu  sync.Mutex
	lastSpeedErr string
)

// Sync polices exactly the limited users this host serves, on every engine it runs.
func (s *SpeedService) Sync() {
	speedSyncMu.Lock()
	defer speedSyncMu.Unlock()
	accounts, err := s.DesiredAccounts()
	if err == nil {
		err = hostLimiter.Apply(accounts)
	}
	// The same failure every few seconds would bury the log.
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	if msg != "" && msg != lastSpeedErr {
		logger.Warning("speed limit:", msg)
	}
	lastSpeedErr = msg
}

// DesiredAccounts lists every limited user with the tunnel addresses and interfaces this host carries for it.
func (s *SpeedService) DesiredAccounts() ([]speedlimit.Account, error) {
	var rows []model.ClientRecord
	if err := database.GetDB().Select("id", "email", "speed_limit").
		Where("speed_limit > 0 AND enable = ?", true).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	accounts := make([]speedlimit.Account, len(rows))
	byEmail := make(map[string]*speedlimit.Account, len(rows))
	for i, r := range rows {
		accounts[i] = speedlimit.Account{ID: r.Id, Mbps: r.SpeedLimit}
		byEmail[strings.ToLower(r.Email)] = &accounts[i]
	}
	addAddrs := func(email string, addrs []netip.Prefix) {
		if a := byEmail[strings.ToLower(email)]; a != nil {
			a.Addrs = append(a.Addrs, addrs...)
		}
	}
	wg, err := s.inboundService.DesiredWireGuardInstances()
	if err != nil {
		return nil, err
	}
	for _, inst := range wg {
		for _, peer := range inst.Peers {
			addAddrs(peer.Name, peer.AllowedIPs)
		}
	}
	awg, err := s.inboundService.DesiredAmneziaWGInstances()
	if err != nil {
		return nil, err
	}
	// Only a kernel-mode AmneziaWG forwards in the kernel; the embedded one rides Xray's marks.
	for _, inst := range awg {
		if !inst.KernelMode {
			continue
		}
		for _, peer := range inst.Peers {
			var addrs []netip.Prefix
			for _, raw := range peer.AllowedIPs {
				if p, err := netip.ParsePrefix(strings.TrimSpace(raw)); err == nil {
					addrs = append(addrs, p)
				}
			}
			addAddrs(peer.Email, addrs)
		}
	}
	for email, addrs := range speedSessions.openvpn() {
		addAddrs(email, addrs)
	}
	for email, ifaces := range speedSessions.l2tp() {
		if a := byEmail[strings.ToLower(email)]; a != nil {
			a.Ifaces = append(a.Ifaces, ifaces...)
		}
	}
	return accounts, nil
}

var speedKick = make(chan struct{}, 1)

// KickSpeedSync asks for a sync soon; any number of kicks before it runs make one.
func KickSpeedSync() {
	select {
	case speedKick <- struct{}{}:
	default:
	}
}

// RunSpeedKicks syncs after every kick until the panel stops; a session that just came up
// is policed now rather than at the next tick.
func (s *SpeedService) RunSpeedKicks() {
	for range speedKick {
		s.Sync()
	}
}
