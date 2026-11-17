package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// LinkedIDs is ids with every copy of the inbounds among them added after them.
func LinkedIDs(ids []int) ([]int, error) {
	if len(ids) == 0 {
		return ids, nil
	}
	db := database.GetDB()
	var groups []string
	if err := db.Model(&model.Inbound{}).Where("id IN ? AND link_group <> ''", ids).
		Distinct().Pluck("link_group", &groups).Error; err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return ids, nil
	}
	var copies []int
	if err := db.Model(&model.Inbound{}).Where("link_group IN ?", groups).Order("id").Pluck("id", &copies).Error; err != nil {
		return nil, err
	}
	out := slices.Clone(ids)
	for _, id := range copies {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out, nil
}

// InboundDeployResult names the copies a deploy created.
type InboundDeployResult struct {
	Created []int `json:"created" example:"[12,13]"`
}

// DeployToNodes puts linked copies of an inbound on more hosts, 0 being this panel: same
// settings and port, its users attached, and kept in step from then on.
func (s *InboundService) DeployToNodes(id int, nodeIDs []int) (*InboundDeployResult, bool, error) {
	src, err := s.GetInbound(id)
	if err != nil {
		return nil, false, err
	}
	db := database.GetDB()
	if src.LinkGroup == "" {
		src.LinkGroup = uuid.NewString()
		if err := db.Model(&model.Inbound{}).Where("id = ?", id).Update("link_group", src.LinkGroup).Error; err != nil {
			return nil, false, err
		}
	}
	var held []model.Inbound
	if err := db.Select("id", "node_id").Where("link_group = ?", src.LinkGroup).Find(&held).Error; err != nil {
		return nil, false, err
	}
	hosts := map[int]bool{}
	for _, h := range held {
		hosts[hostOf(h.NodeID)] = true
	}
	var emails []string
	if err := db.Model(&model.ClientRecord{}).Joins("JOIN client_inbounds ON client_inbounds.client_id = clients.id").
		Where("client_inbounds.inbound_id = ?", id).Pluck("clients.email", &emails).Error; err != nil {
		return nil, false, err
	}
	result := &InboundDeployResult{Created: []int{}}
	needRestart := false
	for _, nodeID := range nodeIDs {
		if hosts[nodeID] {
			continue
		}
		cp, err := linkedCopy(src, nodeID)
		if err != nil {
			return result, needRestart, err
		}
		added, restart, err := s.AddInbound(cp)
		if err != nil {
			return result, needRestart, fmt.Errorf("%s: %w", hostName(nodeID), err)
		}
		needRestart = needRestart || restart
		result.Created = append(result.Created, added.Id)
		hosts[nodeID] = true
	}
	if len(result.Created) > 0 && len(emails) > 0 {
		attached, restart, err := s.clientService.BulkAttach(s, emails, result.Created)
		needRestart = needRestart || restart
		if err == nil && len(attached.Errors) > 0 {
			err = common.NewError("users not put on the copies: " + strings.Join(attached.Errors, "; "))
		}
		if err != nil {
			return result, needRestart, err
		}
	}
	return result, needRestart, nil
}

// linkedCopy is src for another host, without its users, traffic or tag; a custom share
// address belongs to src's host, so the copy shares its own node's.
func linkedCopy(src *model.Inbound, nodeID int) (*model.Inbound, error) {
	cp := *src
	cp.Id, cp.Tag, cp.Up, cp.Down, cp.LastTrafficResetTime = 0, "", 0, 0, 0
	cp.ClientStats, cp.OriginNodeGuid, cp.FallbackParent = nil, "", nil
	cp.NodeID = nil
	if nodeID != 0 {
		var node model.Node
		if err := database.GetDB().Select("id", "enable").First(&node, nodeID).Error; err != nil {
			return nil, common.NewErrorf("node %d does not exist", nodeID)
		}
		if !node.Enable {
			return nil, common.NewErrorf("node %d is disabled", nodeID)
		}
		cp.NodeID = &nodeID
	}
	if cp.ShareAddrStrategy == "custom" {
		cp.ShareAddrStrategy, cp.ShareAddr = "node", ""
	}
	settings, err := settingsWithoutClients(src.Settings)
	if err != nil {
		return nil, err
	}
	cp.Settings = settings
	return &cp, nil
}

// UpdateLinkedCopies carries an edit of one copy to the others; each keeps its own host,
// tag, traffic, users, switch and share address.
func (s *InboundService) UpdateLinkedCopies(edited *model.Inbound) (bool, error) {
	if edited.LinkGroup == "" {
		return false, nil
	}
	var siblings []model.Inbound
	if err := database.GetDB().Where("link_group = ? AND id <> ?", edited.LinkGroup, edited.Id).Find(&siblings).Error; err != nil {
		return false, err
	}
	needRestart := false
	var errs []error
	for _, sib := range siblings {
		next := *edited
		next.Id, next.Tag, next.NodeID, next.Enable = sib.Id, sib.Tag, sib.NodeID, sib.Enable
		next.Up, next.Down, next.LastTrafficResetTime = sib.Up, sib.Down, sib.LastTrafficResetTime
		next.ClientStats, next.OriginNodeGuid, next.FallbackParent = nil, sib.OriginNodeGuid, nil
		next.ShareAddrStrategy, next.ShareAddr = sib.ShareAddrStrategy, sib.ShareAddr
		_, restart, err := s.UpdateInbound(&next)
		needRestart = needRestart || restart
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", hostName(hostOf(sib.NodeID)), err))
		}
	}
	return needRestart, errors.Join(errs...)
}

// Unlink lets one copy go its own way; the others stay linked.
func (s *InboundService) Unlink(id int) error {
	return database.GetDB().Model(&model.Inbound{}).Where("id = ?", id).Update("link_group", "").Error
}

func hostOf(nodeID *int) int {
	if nodeID == nil {
		return 0
	}
	return *nodeID
}

func hostName(nodeID int) string {
	if nodeID == 0 {
		return "this panel"
	}
	var node model.Node
	if database.GetDB().Select("name").First(&node, nodeID).Error == nil {
		return "node " + node.Name
	}
	return fmt.Sprintf("node %d", nodeID)
}

// settingsWithoutClients is an inbound's settings with an empty user list, when it has one.
func settingsWithoutClients(settings string) (string, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(settings), &m); err != nil {
		return "", common.NewError("the inbound's settings are not valid JSON")
	}
	if _, has := m["clients"]; !has {
		return settings, nil
	}
	m["clients"] = []any{}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
