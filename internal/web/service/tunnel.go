package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/crypto/nodetoken"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/tunnel"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"

	"gorm.io/gorm"
)

// tunnelEngine is what the service needs of the tunnel manager; tests substitute it.
type tunnelEngine interface {
	Reconcile(desired []tunnel.Tunnel)
	ReconcileGroups(groups []tunnel.Group)
	Statuses() []tunnel.TunnelStatus
}

var tunnelManager = func() tunnelEngine { return tunnel.GetManager() }

// TunnelService keeps the tunnels of this server and the relays it manages.
type TunnelService struct {
	settingService SettingService
}

// TunnelView is a tunnel end as the panel shows it, with how it is doing.
type TunnelView struct {
	Id        int                   `json:"id" example:"1"`
	PairId    string                `json:"pairId" example:"5d1c0a9e7b2f4c11a0e3d8f6"`
	Name      string                `json:"name" example:"uk-relay"`
	Kind      string                `json:"kind" example:"gre"`
	Role      string                `json:"role" example:"client"`
	Enable    bool                  `json:"enable" example:"true"`
	RelayId   int                   `json:"relayId" example:"0"`
	Settings  tunnel.TunnelSettings `json:"settings"`
	Status    *tunnel.TunnelStatus  `json:"status,omitempty"`
	CreatedAt int64                 `json:"createdAt" example:"1763300000000"`
	UpdatedAt int64                 `json:"updatedAt" example:"1763300000000"`
}

// TunnelRequest is what the tunnel form saves.
type TunnelRequest struct {
	Name     string                `json:"name" example:"uk-relay"`
	Kind     string                `json:"kind" example:"gre"`
	Role     string                `json:"role" example:"client"`
	Enable   bool                  `json:"enable" example:"true"`
	RelayId  int                   `json:"relayId" example:"0"`
	Settings tunnel.TunnelSettings `json:"settings"`
}

// TunnelPairing is the code the other end of a tunnel imports.
type TunnelPairing struct {
	Code    string `json:"code" example:"manifold-tunnel://eyJ2Ijox..."`
	Address string `json:"address" example:"203.0.113.10"`
}

// TunnelSuggestion is a free tunnel subnet, port and secret for a new tunnel.
type TunnelSuggestion struct {
	LocalIP string `json:"localIp" example:"10.80.0.2/30"`
	PeerIP  string `json:"peerIp" example:"10.80.0.1"`
	Port    int    `json:"port" example:"8443"`
	Token   string `json:"token" example:"3f9c0e..."`
	Address string `json:"address" example:"203.0.113.10"`
}

func tunnelFromRow(row *model.TunnelEnd) (tunnel.Tunnel, error) {
	var settings tunnel.TunnelSettings
	if strings.TrimSpace(row.Settings) != "" {
		if err := json.Unmarshal([]byte(row.Settings), &settings); err != nil {
			return tunnel.Tunnel{}, fmt.Errorf("tunnel %d: invalid settings: %w", row.Id, err)
		}
	}
	return tunnel.Tunnel{ID: row.Id, PairID: row.PairId, Name: row.Name, Kind: row.Kind, Role: row.Role, Enabled: row.Enable, Settings: settings}, nil
}

func viewOf(row *model.TunnelEnd, t tunnel.Tunnel) TunnelView {
	return TunnelView{
		Id: row.Id, PairId: row.PairId, Name: row.Name, Kind: row.Kind, Role: row.Role, Enable: row.Enable,
		RelayId: row.RelayId, Settings: t.Settings, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

// List returns every tunnel end with its live status.
func (s *TunnelService) List() ([]TunnelView, error) {
	var rows []model.TunnelEnd
	if err := database.GetDB().Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	statuses := map[int]tunnel.TunnelStatus{}
	for _, st := range tunnelManager().Statuses() {
		statuses[st.ID] = st
	}
	out := make([]TunnelView, 0, len(rows))
	for i := range rows {
		t, err := tunnelFromRow(&rows[i])
		if err != nil {
			logger.Warning(err)
		}
		v := viewOf(&rows[i], t)
		if st, ok := statuses[rows[i].Id]; ok {
			v.Status = &st
		}
		out = append(out, v)
	}
	return out, nil
}

// CheckAll returns every tunnel end with a status the command line measured itself,
// for when the panel's own view of its tunnels cannot be reached.
func (s *TunnelService) CheckAll(echoes int) ([]TunnelView, error) {
	var rows []model.TunnelEnd
	if err := database.GetDB().Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]TunnelView, 0, len(rows))
	for i := range rows {
		t, err := tunnelFromRow(&rows[i])
		v := viewOf(&rows[i], t)
		st := tunnel.TunnelStatus{ID: rows[i].Id, State: tunnel.StateError}
		if err != nil {
			st.Error = err.Error()
		} else {
			st = tunnel.GetManager().Check(t, echoes)
		}
		v.Status = &st
		out = append(out, v)
	}
	return out, nil
}

// Statuses returns how every running tunnel end is doing.
func (s *TunnelService) Statuses() []tunnel.TunnelStatus {
	return tunnelManager().Statuses()
}

// Desired returns every tunnel end the manager should run, for the tunnel job.
func (s *TunnelService) Desired() ([]tunnel.Tunnel, error) {
	var rows []model.TunnelEnd
	if err := database.GetDB().Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]tunnel.Tunnel, 0, len(rows))
	for i := range rows {
		t, err := tunnelFromRow(&rows[i])
		if err == nil {
			t, err = tunnel.Normalize(t)
		}
		if err != nil {
			logger.Warning("tunnel job: ", err)
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

var reconcileMu sync.Mutex

// tunnelReconcileRequests coalesces saves into one pending reconcile; only the
// server's loop drains it, so no reconcile outlives a shutdown or a test's database.
var tunnelReconcileRequests = make(chan struct{}, 1)

// RequestReconcile asks the running panel to apply the stored tunnels soon.
func (s *TunnelService) RequestReconcile() {
	select {
	case tunnelReconcileRequests <- struct{}{}:
	default:
	}
}

// groupCheckEvery is how soon a group moves off a tunnel that stopped answering.
const groupCheckEvery = 10 * time.Second

// ServeReconcileRequests applies the stored tunnels after each request until ctx ends,
// and between requests keeps each group on the tunnels that answer.
func (s *TunnelService) ServeReconcileRequests(ctx context.Context) {
	ticker := time.NewTicker(groupCheckEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tunnelReconcileRequests:
			s.Reconcile()
		case <-ticker.C:
			s.reconcileGroups()
		}
	}
}

// Reconcile applies the stored tunnels now instead of at the job's next run.
func (s *TunnelService) Reconcile() {
	reconcileMu.Lock()
	defer reconcileMu.Unlock()
	desired, err := s.Desired()
	if err != nil {
		logger.Warning("tunnel: reading tunnels failed: ", err)
		return
	}
	tunnelManager().Reconcile(desired)
	s.reconcileGroupsLocked()
}

func (s *TunnelService) reconcileGroups() {
	reconcileMu.Lock()
	defer reconcileMu.Unlock()
	s.reconcileGroupsLocked()
}

func (s *TunnelService) reconcileGroupsLocked() {
	groups, err := s.desiredGroups()
	if err != nil {
		logger.Warning("tunnel: reading tunnel groups failed: ", err)
		return
	}
	tunnelManager().ReconcileGroups(groups)
}

func (s *TunnelService) normalizeRequest(id int, pairID string, req TunnelRequest) (tunnel.Tunnel, error) {
	t, err := tunnel.Normalize(tunnel.Tunnel{
		ID: id, PairID: pairID, Name: req.Name, Kind: req.Kind, Role: req.Role, Enabled: req.Enable, Settings: req.Settings,
	})
	if err != nil {
		return t, common.NewError(err.Error())
	}
	if err := s.checkConflicts(t); err != nil {
		return t, err
	}
	if req.RelayId != 0 {
		var server model.TunnelRelay
		if err := database.GetDB().First(&server, req.RelayId).Error; err != nil {
			return t, common.NewErrorf("relay %d does not exist", req.RelayId)
		}
		if t.Role != tunnel.RoleClient {
			return t, common.NewError("only the panel's own end of a tunnel can manage a relay")
		}
	}
	return t, nil
}

// checkConflicts refuses a tunnel that would take a port, an address or a name
// another tunnel, an inbound or the panel itself already uses.
func (s *TunnelService) checkConflicts(t tunnel.Tunnel) error {
	var rows []model.TunnelEnd
	if err := database.GetDB().Where("id <> ?", t.ID).Find(&rows).Error; err != nil {
		return err
	}
	ports := t.ListenPorts()
	ownPrefix, _ := netip.ParsePrefix(t.Settings.LocalIP)
	for i := range rows {
		other, err := tunnelFromRow(&rows[i])
		if err != nil {
			continue
		}
		if t.PairID != "" && other.PairID == t.PairID {
			return common.NewErrorf("tunnel %q is already this tunnel's end on this server", other.Name)
		}
		for port := range other.ListenPorts() {
			if _, taken := ports[port]; taken {
				return common.NewErrorf("port %d is already used by tunnel %q", port, other.Name)
			}
		}
		if p, err := netip.ParsePrefix(other.Settings.LocalIP); err == nil && ownPrefix.IsValid() && p.Overlaps(ownPrefix) {
			return common.NewErrorf("tunnel addresses %s overlap those of tunnel %q (%s)", ownPrefix.Masked(), other.Name, p.Masked())
		}
	}
	if t.ID >= 0 {
		taken, err := groupPorts(0)
		if err != nil {
			return err
		}
		for port := range ports {
			if name, ok := taken[port]; ok {
				return common.NewErrorf("port %d is already used by group %q", port, name)
			}
		}
	}
	reserved := map[int]string{22: "SSH"}
	if port, err := s.settingService.GetPort(); err == nil {
		reserved[port] = "the panel"
	}
	if port, err := s.settingService.GetSubPort(); err == nil {
		reserved[port] = "the subscription server"
	}
	var l2tpHere int64
	if err := database.GetDB().Model(&model.Inbound{}).Where("protocol = ? AND enable = ? AND node_id IS NULL", model.L2TP, true).Count(&l2tpHere).Error; err == nil && l2tpHere > 0 {
		// strongSwan takes IKE and NAT traversal for the inbound's IPsec.
		reserved[500], reserved[4500] = "this server's L2TP inbound (IPsec)", "this server's L2TP inbound (IPsec)"
	}
	for port := range ports {
		if what, ok := reserved[port]; ok {
			return common.NewErrorf("port %d belongs to %s and cannot be forwarded or listened on", port, what)
		}
		var count int64
		if err := database.GetDB().Model(&model.Inbound{}).Where("port = ? AND enable = ?", port, true).Count(&count).Error; err == nil && count > 0 {
			return common.NewErrorf("port %d is used by an inbound on this server", port)
		}
	}
	return nil
}

func (s *TunnelService) save(row *model.TunnelEnd, t tunnel.Tunnel, relayID int) error {
	raw, err := json.Marshal(t.Settings)
	if err != nil {
		return err
	}
	row.PairId, row.Name, row.Kind, row.Role, row.Enable = t.PairID, t.Name, t.Kind, t.Role, t.Enabled
	row.RelayId, row.Settings = relayID, string(raw)
	return database.GetDB().Save(row).Error
}

// Add stores a new tunnel end, starts it and, when it has a relay, sets up the other end there.
func (s *TunnelService) Add(req TunnelRequest) (*TunnelView, error) {
	t, err := s.normalizeRequest(0, "", req)
	if err != nil {
		return nil, err
	}
	row := &model.TunnelEnd{}
	if err := s.save(row, t, req.RelayId); err != nil {
		return nil, err
	}
	t.ID = row.Id
	return s.afterSave(row, t)
}

// Update changes a tunnel end; its pair id stays, so the other end still matches it.
func (s *TunnelService) Update(id int, req TunnelRequest) (*TunnelView, error) {
	row := &model.TunnelEnd{}
	if err := database.GetDB().First(row, id).Error; err != nil {
		return nil, err
	}
	previousRelay := row.RelayId
	t, err := s.normalizeRequest(id, row.PairId, req)
	if err != nil {
		return nil, err
	}
	if err := s.save(row, t, req.RelayId); err != nil {
		return nil, err
	}
	if previousRelay != 0 && previousRelay != req.RelayId {
		if err := s.withdrawFrom(previousRelay, row.PairId); err != nil {
			logger.Warning("tunnel: withdrawing the old relay's end failed: ", err)
		}
	}
	return s.afterSave(row, t)
}

func (s *TunnelService) afterSave(row *model.TunnelEnd, t tunnel.Tunnel) (*TunnelView, error) {
	s.RequestReconcile()
	v := viewOf(row, t)
	if row.RelayId == 0 {
		return &v, nil
	}
	if err := s.pushTo(row.RelayId, t); err != nil {
		return &v, common.NewErrorf("saved here, but the relay was not updated: %v", err)
	}
	return &v, nil
}

// Delete removes a tunnel end here and, when it has a relay, the other end there.
func (s *TunnelService) Delete(id int) error {
	row := &model.TunnelEnd{}
	if err := database.GetDB().First(row, id).Error; err != nil {
		return err
	}
	if groups := groupsUsing(row.Id); len(groups) > 0 {
		return common.NewErrorf("tunnel %q is in group %q; take it out of the group first", row.Name, groups[0])
	}
	if err := database.GetDB().Delete(row).Error; err != nil {
		return err
	}
	s.RequestReconcile()
	if row.RelayId != 0 {
		if err := s.withdrawFrom(row.RelayId, row.PairId); err != nil {
			return common.NewErrorf("deleted here, but the relay still has its end: %v", err)
		}
	}
	return nil
}

// PublicAddress is how another server reaches this one when a tunnel names no address.
func (s *TunnelService) PublicAddress() string {
	if ip := KnownPublicIPv4(); ip != "" {
		return ip
	}
	RefreshPublicIPv4()
	return KnownPublicIPv4()
}

func (s *TunnelService) addressFor(t tunnel.Tunnel, requested string) (string, error) {
	addr := strings.TrimSpace(requested)
	if addr == "" {
		addr = t.Settings.PublicAddr
	}
	if addr == "" {
		addr = s.PublicAddress()
	}
	if addr == "" {
		return "", common.NewError("this server's public address could not be found; enter it in the tunnel")
	}
	return addr, nil
}

// Pairing returns the code that makes another panel this tunnel's other end.
func (s *TunnelService) Pairing(id int, address string) (*TunnelPairing, error) {
	row := &model.TunnelEnd{}
	if err := database.GetDB().First(row, id).Error; err != nil {
		return nil, err
	}
	t, err := tunnelFromRow(row)
	if err != nil {
		return nil, err
	}
	addr, err := s.addressFor(t, address)
	if err != nil {
		return nil, err
	}
	code, err := tunnel.PairingCode(t, addr)
	if err != nil {
		return nil, common.NewError(err.Error())
	}
	return &TunnelPairing{Code: code, Address: addr}, nil
}

// Import stores the end a pairing code describes, replacing the end with the same pair id.
func (s *TunnelService) Import(code string) (*TunnelView, error) {
	t, err := tunnel.ParsePairingCode(code)
	if err != nil {
		return nil, common.NewError(err.Error())
	}
	row := &model.TunnelEnd{}
	err = database.GetDB().Where("pair_id = ?", t.PairID).First(row).Error
	switch {
	case err == nil:
		t.ID = row.Id
		t.Enabled = row.Enable
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return nil, err
	}
	if err := s.checkConflicts(t); err != nil {
		return nil, err
	}
	if err := s.save(row, t, row.RelayId); err != nil {
		return nil, err
	}
	t.ID = row.Id
	s.RequestReconcile()
	v := viewOf(row, t)
	return &v, nil
}

// Withdraw removes the end with this pair id, for the panel that owns the tunnel.
func (s *TunnelService) Withdraw(pairID string) error {
	res := database.GetDB().Where("pair_id = ?", strings.TrimSpace(pairID)).Delete(&model.TunnelEnd{})
	if res.Error != nil {
		return res.Error
	}
	s.RequestReconcile()
	return nil
}

// Suggest returns a tunnel subnet no tunnel uses yet, a random port and a secret.
func (s *TunnelService) Suggest() (*TunnelSuggestion, error) {
	var rows []model.TunnelEnd
	if err := database.GetDB().Find(&rows).Error; err != nil {
		return nil, err
	}
	var used []netip.Prefix
	for i := range rows {
		if t, err := tunnelFromRow(&rows[i]); err == nil {
			if p, err := netip.ParsePrefix(t.Settings.LocalIP); err == nil {
				used = append(used, p)
			}
		}
	}
	base := netip.MustParseAddr("10.80.0.0").As4()
	for i := range 16384 {
		a := base
		a[2], a[3] = byte(i/64), byte(i%64*4)
		p := netip.PrefixFrom(netip.AddrFrom4(a), 30)
		free := true
		for _, u := range used {
			if u.Overlaps(p) {
				free = false
				break
			}
		}
		if !free {
			continue
		}
		peer := p.Addr().Next()
		local := peer.Next()
		return &TunnelSuggestion{
			LocalIP: netip.PrefixFrom(local, 30).String(), PeerIP: peer.String(),
			Port: 20000 + int(time.Now().UnixNano()%20000), Token: tunnel.NewToken(), Address: s.PublicAddress(),
		}, nil
	}
	return nil, common.NewError("no free tunnel subnet is left in 10.80.0.0/16")
}

// ListRelays returns the relays this panel manages.
func (s *TunnelService) ListRelays() ([]model.TunnelRelay, error) {
	var rows []model.TunnelRelay
	err := database.GetDB().Order("id").Find(&rows).Error
	return rows, err
}

func normalizeRelayURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", common.NewError("the relay's address must be its panel URL, such as https://relay.example.com:2053/path/")
	}
	u.RawQuery, u.Fragment = "", ""
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	return u.String(), nil
}

func relayTokenBinding(id int) []byte { return []byte(fmt.Sprintf("tunnel-relay:%d", id)) }

// SaveRelay adds a relay, or changes one when id is set; an empty token keeps the stored one.
func (s *TunnelService) SaveRelay(id int, name, rawURL, token string) (*model.TunnelRelay, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return nil, common.NewError("a relay's name is 1 to 64 characters")
	}
	u, err := normalizeRelayURL(rawURL)
	if err != nil {
		return nil, err
	}
	row := &model.TunnelRelay{}
	if id != 0 {
		if err := database.GetDB().First(row, id).Error; err != nil {
			return nil, err
		}
	} else if strings.TrimSpace(token) == "" {
		return nil, common.NewError("a relay needs the API token made on it with the tunnel scope")
	}
	row.Name, row.Url = name, u
	if err := database.GetDB().Save(row).Error; err != nil {
		return nil, err
	}
	if token = strings.TrimSpace(token); token != "" {
		sealed, err := nodetoken.EncryptBound(relayTokenBinding(row.Id), token)
		if err != nil {
			return nil, err
		}
		row.Token = sealed
		if err := database.GetDB().Model(row).Update("token", sealed).Error; err != nil {
			return nil, err
		}
	}
	return row, nil
}

// DeleteRelay forgets a relay; its tunnels stay here, managed by hand from then on.
func (s *TunnelService) DeleteRelay(id int) error {
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.TunnelEnd{}).Where("relay_id = ?", id).Update("relay_id", 0).Error; err != nil {
			return err
		}
		return tx.Delete(&model.TunnelRelay{}, id).Error
	})
}

// VersionHeader carries a panel's product version on its tunnel answers, so the panel
// that manages a relay sees when the two have drifted apart.
const VersionHeader = "X-Manifold-Version"

// relayCall sends one API request to a relay with its tunnel token.
func (s *TunnelService) relayCall(relayID int, method, path string, form url.Values, out any) error {
	row := &model.TunnelRelay{}
	if err := database.GetDB().First(row, relayID).Error; err != nil {
		return err
	}
	token, err := nodetoken.DecryptBound(relayTokenBinding(row.Id), row.Token)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, row.Url+"panel/api/tunnels/"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if v := resp.Header.Get(VersionHeader); v != "" && v != row.Version {
		database.GetDB().Model(row).Update("version", v)
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", row.Name, resp.Status)
	}
	var msg struct {
		Success bool            `json:"success"`
		Msg     string          `json:"msg"`
		Obj     json.RawMessage `json:"obj"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return fmt.Errorf("%s did not answer as a Manifold panel", row.Name)
	}
	if !msg.Success {
		return fmt.Errorf("%s: %s", row.Name, msg.Msg)
	}
	if out != nil && len(msg.Obj) > 0 {
		return json.Unmarshal(msg.Obj, out)
	}
	return nil
}

func (s *TunnelService) pushTo(relayID int, t tunnel.Tunnel) error {
	addr, err := s.addressFor(t, "")
	if err != nil {
		return err
	}
	code, err := tunnel.PairingCode(t, addr)
	if err != nil {
		return err
	}
	return s.relayCall(relayID, http.MethodPost, "import", url.Values{"code": {code}}, nil)
}

func (s *TunnelService) withdrawFrom(relayID int, pairID string) error {
	return s.relayCall(relayID, http.MethodPost, "withdraw", url.Values{"pairId": {pairID}}, nil)
}

// RelayStatuses reads, from one relay, how its ends of this panel's tunnels are doing.
func (s *TunnelService) RelayStatuses(relayID int) ([]TunnelView, error) {
	var views []TunnelView
	err := s.relayCall(relayID, http.MethodGet, "list", nil, &views)
	return views, err
}

// Resync pushes every relayed tunnel to its relay again, as after this panel moved to a new
// address, then the groups placed there, which name those tunnels.
func (s *TunnelService) Resync() []error {
	var rows []model.TunnelEnd
	if err := database.GetDB().Where("relay_id <> 0").Find(&rows).Error; err != nil {
		return []error{err}
	}
	var errs []error
	for i := range rows {
		t, err := tunnelFromRow(&rows[i])
		if err == nil {
			err = s.pushTo(rows[i].RelayId, t)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", rows[i].Name, err))
		}
	}
	var groups []model.TunnelGroup
	if err := database.GetDB().Where("relay_id <> 0").Find(&groups).Error; err != nil {
		return append(errs, err)
	}
	for i := range groups {
		if err := s.pushGroup(&groups[i]); err != nil {
			errs = append(errs, fmt.Errorf("group %s: %w", groups[i].Name, err))
		}
	}
	return errs
}
