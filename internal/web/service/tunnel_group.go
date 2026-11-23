package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/tunnel"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"

	"gorm.io/gorm"
)

// TunnelGroupView is a group with the forwards it shares.
type TunnelGroupView struct {
	model.TunnelGroup
	Forwards []tunnel.PortForward `json:"forwards"`
}

// TunnelGroupRequest saves a group.
type TunnelGroupRequest struct {
	Name     string               `json:"name" example:"origins"`
	Mode     string               `json:"mode" example:"failover"`
	Members  []int                `json:"members" example:"[1,2]"`
	Backups  []int                `json:"backups" example:"[3]"`
	Sticky   bool                 `json:"sticky" example:"false"`
	Forwards []tunnel.PortForward `json:"forwards"`
	Enable   bool                 `json:"enable" example:"true"`
	// RelayId places the group on that managed relay; its members are this panel's ends of that relay's tunnels.
	RelayId int `json:"relayId" example:"0"`
}

// PlacedTunnelGroup is a group as the managing panel places it on a relay: tunnels by pair id.
type PlacedTunnelGroup struct {
	PairID   string               `json:"pairId" example:"9f3c2a7b1d4e5f6071829304"`
	Name     string               `json:"name" example:"uk paths"`
	Mode     string               `json:"mode" example:"balance"`
	Members  []string             `json:"members" example:"[\"abcdef0123456789abcdef01\"]"`
	Backups  []string             `json:"backups" example:"[]"`
	Sticky   bool                 `json:"sticky" example:"true"`
	Forwards []tunnel.PortForward `json:"forwards"`
	Enable   bool                 `json:"enable" example:"true"`
}

func groupFromRow(row *model.TunnelGroup) tunnel.Group {
	var forwards []tunnel.PortForward
	_ = json.Unmarshal([]byte(row.Forwards), &forwards)
	return tunnel.Group{ID: row.Id, Name: row.Name, Mode: row.Mode, Members: row.Members, Backups: row.Backups, Sticky: row.Sticky, Forwards: forwards, Enabled: row.Enable}
}

func groupView(row model.TunnelGroup) TunnelGroupView {
	g := groupFromRow(&row)
	if row.Members == nil {
		row.Members = []int{}
	}
	if row.Backups == nil {
		row.Backups = []int{}
	}
	if g.Forwards == nil {
		g.Forwards = []tunnel.PortForward{}
	}
	return TunnelGroupView{TunnelGroup: row, Forwards: g.Forwards}
}

func (s *TunnelService) ListGroups() ([]TunnelGroupView, error) {
	var rows []model.TunnelGroup
	if err := database.GetDB().Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]TunnelGroupView, 0, len(rows))
	for _, r := range rows {
		out = append(out, groupView(r))
	}
	return out, nil
}

// desiredGroups are the groups this server forwards; one placed on a relay runs there.
func (s *TunnelService) desiredGroups() ([]tunnel.Group, error) {
	var rows []model.TunnelGroup
	if err := database.GetDB().Where("relay_id = 0").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]tunnel.Group, 0, len(rows))
	for i := range rows {
		out = append(out, groupFromRow(&rows[i]))
	}
	return out, nil
}

// groupPorts are the ports every group but except takes on this relay, by group name.
func groupPorts(except int) (map[int]string, error) {
	var rows []model.TunnelGroup
	if err := database.GetDB().Where("id <> ? AND relay_id = 0", except).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := map[int]string{}
	for i := range rows {
		for port := range groupFromRow(&rows[i]).ListenPorts() {
			out[port] = rows[i].Name
		}
	}
	return out, nil
}

// checkGroup checks a group's tunnels and, for a group of this server's own, its ports;
// a group placed on a relay forwards the relay's ports, which the relay checks.
func (s *TunnelService) checkGroup(id, relayID int, g tunnel.Group) error {
	family := -1
	for _, member := range slices.Concat(g.Members, g.Backups) {
		var row model.TunnelEnd
		if err := database.GetDB().First(&row, member).Error; err != nil {
			return common.NewErrorf("tunnel %d does not exist", member)
		}
		t, err := tunnelFromRow(&row)
		if err != nil {
			return err
		}
		switch {
		case relayID > 0 && row.RelayId != relayID:
			return common.NewErrorf("tunnel %q goes to another relay, or to none this panel manages", t.Name)
		case relayID > 0 && !tunnel.CanCarryRelayGroup(t):
			return common.NewErrorf("tunnel %q has no tunnel address on this side, so the relay cannot balance over it", t.Name)
		case relayID == 0 && !tunnel.CanJoinGroup(t):
			return common.NewErrorf("tunnel %q is not a relay end with a tunnel address, so it cannot carry a group's ports", t.Name)
		}
		peer, err := netip.ParseAddr(t.Settings.PeerIP)
		if err != nil {
			return common.NewErrorf("tunnel %q has no address on the far side", t.Name)
		}
		v4 := 0
		if peer.Is4() {
			v4 = 1
		}
		if family >= 0 && family != v4 {
			return common.NewError("the tunnels of one group must carry the same address family")
		}
		family = v4
	}
	if relayID > 0 {
		return nil
	}
	ports := g.ListenPorts()
	others, err := groupPorts(id)
	if err != nil {
		return err
	}
	for port := range ports {
		if name, taken := others[port]; taken {
			return common.NewErrorf("port %d is already used by group %q", port, name)
		}
	}
	// A group's ports go through the same checks as one tunnel's forwards.
	return s.checkConflicts(tunnel.Tunnel{ID: -1, Role: tunnel.RoleServer, Settings: tunnel.TunnelSettings{Forwards: g.Forwards}})
}

// SaveGroup creates (id 0) or replaces a group and applies it here, or places it on its relay.
func (s *TunnelService) SaveGroup(id int, req TunnelGroupRequest) (*TunnelGroupView, error) {
	g, err := tunnel.NormalizeGroup(tunnel.Group{ID: id, Name: req.Name, Mode: req.Mode, Members: req.Members, Backups: req.Backups, Sticky: req.Sticky, Forwards: req.Forwards, Enabled: req.Enable})
	if err != nil {
		return nil, common.NewError(err.Error())
	}
	if req.RelayId > 0 {
		if err := database.GetDB().First(&model.TunnelRelay{}, req.RelayId).Error; err != nil {
			return nil, common.NewError("no such relay")
		}
	}
	if err := s.checkGroup(id, req.RelayId, g); err != nil {
		return nil, err
	}
	row := model.TunnelGroup{Id: id}
	if id > 0 {
		if err := database.GetDB().First(&row, id).Error; err != nil {
			return nil, common.NewError("no such group")
		}
	}
	previousRelay := row.RelayId
	if req.RelayId > 0 && row.PairId == "" {
		row.PairId = tunnel.NewPairID()
	}
	forwards, _ := json.Marshal(g.Forwards)
	row.Name, row.Mode, row.Members, row.Backups, row.Sticky, row.Forwards, row.Enable = g.Name, g.Mode, g.Members, g.Backups, g.Sticky, string(forwards), g.Enabled
	row.RelayId = req.RelayId
	if err := database.GetDB().Save(&row).Error; err != nil {
		return nil, err
	}
	s.RequestReconcile()
	v := groupView(row)
	if previousRelay > 0 && previousRelay != row.RelayId {
		if err := s.withdrawGroupFrom(previousRelay, row.PairId); err != nil {
			return &v, common.NewErrorf("saved here, but the old relay still has the group: %v", err)
		}
	}
	if row.RelayId > 0 {
		if err := s.pushGroup(&row); err != nil {
			return &v, common.NewErrorf("saved here, but the relay was not updated: %v", err)
		}
	}
	return &v, nil
}

// DeleteGroup removes a group; its ports stop being forwarded here at the next reconcile, or on its relay.
func (s *TunnelService) DeleteGroup(id int) error {
	var row model.TunnelGroup
	if err := database.GetDB().First(&row, id).Error; err != nil {
		return common.NewError("no such group")
	}
	if err := database.GetDB().Delete(&row).Error; err != nil {
		return err
	}
	s.RequestReconcile()
	if row.RelayId > 0 {
		if err := s.withdrawGroupFrom(row.RelayId, row.PairId); err != nil {
			return common.NewErrorf("deleted here, but the relay still has the group: %v", err)
		}
	}
	return nil
}

// placedGroup names a relay group's tunnels by pair id, as the relay knows them.
func placedGroup(row *model.TunnelGroup) (PlacedTunnelGroup, error) {
	g := groupFromRow(row)
	pairIDs := func(ids []int) ([]string, error) {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			var end model.TunnelEnd
			if err := database.GetDB().First(&end, id).Error; err != nil {
				return nil, fmt.Errorf("tunnel %d of group %q is gone", id, row.Name)
			}
			out = append(out, end.PairId)
		}
		return out, nil
	}
	members, err := pairIDs(g.Members)
	if err != nil {
		return PlacedTunnelGroup{}, err
	}
	backups, err := pairIDs(g.Backups)
	if err != nil {
		return PlacedTunnelGroup{}, err
	}
	return PlacedTunnelGroup{
		PairID: row.PairId, Name: g.Name, Mode: g.Mode, Members: members, Backups: backups,
		Sticky: g.Sticky, Forwards: g.Forwards, Enable: g.Enabled,
	}, nil
}

func (s *TunnelService) pushGroup(row *model.TunnelGroup) error {
	placed, err := placedGroup(row)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(placed)
	if err != nil {
		return err
	}
	return relayGroupCallError(s.relayCall(row.RelayId, http.MethodPost, "groups/import", url.Values{"group": {string(raw)}}, nil))
}

func (s *TunnelService) withdrawGroupFrom(relayID int, pairID string) error {
	return relayGroupCallError(s.relayCall(relayID, http.MethodPost, "groups/withdraw", url.Values{"pairId": {pairID}}, nil))
}

// A relay older than 0.24.0 has no group routes; it answers like any unknown page.
func relayGroupCallError(err error) error {
	if err != nil && strings.Contains(err.Error(), "404") {
		return fmt.Errorf("%w; update the relay to 0.24.0 or later, which takes groups from this panel", err)
	}
	return err
}

// ImportGroup stores a group the managing panel places on this relay, over this relay's ends
// of its tunnels, replacing the group of the same pair id.
func (s *TunnelService) ImportGroup(p PlacedTunnelGroup) (*TunnelGroupView, error) {
	if p.PairID = strings.TrimSpace(p.PairID); p.PairID == "" {
		return nil, common.NewError("a placed group needs its pair id")
	}
	localIDs := func(pairIDs []string) ([]int, error) {
		out := make([]int, 0, len(pairIDs))
		for _, pairID := range pairIDs {
			var end model.TunnelEnd
			if err := database.GetDB().Where("pair_id = ?", pairID).First(&end).Error; err != nil {
				return nil, common.NewErrorf("this relay has no end of tunnel %s; resend the tunnels first", pairID)
			}
			out = append(out, end.Id)
		}
		return out, nil
	}
	members, err := localIDs(p.Members)
	if err != nil {
		return nil, err
	}
	backups, err := localIDs(p.Backups)
	if err != nil {
		return nil, err
	}
	row := model.TunnelGroup{}
	err = database.GetDB().Where("pair_id = ? AND relay_id = 0", p.PairID).First(&row).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	v, err := s.SaveGroup(row.Id, TunnelGroupRequest{
		Name: p.Name, Mode: p.Mode, Members: members, Backups: backups, Sticky: p.Sticky, Forwards: p.Forwards, Enable: p.Enable,
	})
	if err != nil {
		return nil, err
	}
	if err := database.GetDB().Model(&model.TunnelGroup{}).Where("id = ?", v.Id).Update("pair_id", p.PairID).Error; err != nil {
		return nil, err
	}
	v.PairId = p.PairID
	return v, nil
}

// WithdrawGroup removes the placed group of this pair id, for the panel that placed it.
func (s *TunnelService) WithdrawGroup(pairID string) error {
	res := database.GetDB().Where("pair_id = ? AND relay_id = 0", strings.TrimSpace(pairID)).Delete(&model.TunnelGroup{})
	if res.Error != nil {
		return res.Error
	}
	s.RequestReconcile()
	return nil
}

// groupsUsing names the groups that list a tunnel, so it is not deleted from under them.
func groupsUsing(tunnelID int) []string {
	var rows []model.TunnelGroup
	database.GetDB().Find(&rows)
	var out []string
	for _, r := range rows {
		if slices.Contains(r.Members, tunnelID) || slices.Contains(r.Backups, tunnelID) {
			out = append(out, r.Name)
		}
	}
	return out
}
