package service

import (
	"encoding/json"
	"net/netip"
	"slices"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/tunnel"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
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

func (s *TunnelService) desiredGroups() ([]tunnel.Group, error) {
	var rows []model.TunnelGroup
	if err := database.GetDB().Find(&rows).Error; err != nil {
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
	if err := database.GetDB().Where("id <> ?", except).Find(&rows).Error; err != nil {
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

func (s *TunnelService) checkGroup(id int, g tunnel.Group) error {
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
		if !tunnel.CanJoinGroup(t) {
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

// SaveGroup creates (id 0) or replaces a group and applies it.
func (s *TunnelService) SaveGroup(id int, req TunnelGroupRequest) (*TunnelGroupView, error) {
	g, err := tunnel.NormalizeGroup(tunnel.Group{ID: id, Name: req.Name, Mode: req.Mode, Members: req.Members, Backups: req.Backups, Sticky: req.Sticky, Forwards: req.Forwards, Enabled: req.Enable})
	if err != nil {
		return nil, common.NewError(err.Error())
	}
	if err := s.checkGroup(id, g); err != nil {
		return nil, err
	}
	row := model.TunnelGroup{Id: id}
	if id > 0 {
		if err := database.GetDB().First(&row, id).Error; err != nil {
			return nil, common.NewError("no such group")
		}
	}
	forwards, _ := json.Marshal(g.Forwards)
	row.Name, row.Mode, row.Members, row.Backups, row.Sticky, row.Forwards, row.Enable = g.Name, g.Mode, g.Members, g.Backups, g.Sticky, string(forwards), g.Enabled
	if err := database.GetDB().Save(&row).Error; err != nil {
		return nil, err
	}
	s.RequestReconcile()
	v := groupView(row)
	return &v, nil
}

// DeleteGroup removes a group; its ports stop being forwarded at the next reconcile.
func (s *TunnelService) DeleteGroup(id int) error {
	if err := database.GetDB().Delete(&model.TunnelGroup{}, id).Error; err != nil {
		return err
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
