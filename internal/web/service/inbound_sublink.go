package service

import (
	"sync"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// SubEntry is one subscription link with the inbound and app location it comes from.
type SubEntry struct {
	Link        string `json:"link" example:"openvpn://IyBERS1PVlBO..."`
	InboundId   int    `json:"inboundId" example:"3"`
	Protocol    string `json:"protocol" example:"openvpn"`
	LocationId  int    `json:"locationId" example:"1"`
	Location    string `json:"location" example:"Germany"`
	CountryCode string `json:"countryCode" example:"DE"`
	City        string `json:"city" example:"Frankfurt"`
	SortOrder   int    `json:"sortOrder" example:"0"`
}

type SubLinkProvider interface {
	SubLinksForSubId(host, subId string) ([]string, error)
	SubEntriesForSubId(host, subId string) ([]SubEntry, error)
	LinksForClient(host string, inbound *model.Inbound, email string) []string
	LinksForInbounds(host string, inbounds []*model.Inbound) []string
}

var (
	subLinkProviderMu         sync.RWMutex
	registeredSubLinkProvider SubLinkProvider
)

func RegisterSubLinkProvider(p SubLinkProvider) {
	subLinkProviderMu.Lock()
	defer subLinkProviderMu.Unlock()
	registeredSubLinkProvider = p
}

// subLinkProvider guards the registry: startup writes it while link requests read it.
func subLinkProvider() SubLinkProvider {
	subLinkProviderMu.RLock()
	defer subLinkProviderMu.RUnlock()
	return registeredSubLinkProvider
}

func (s *InboundService) GetSubLinks(host, subId string) ([]string, error) {
	provider := subLinkProvider()
	if provider == nil {
		return nil, common.NewError("sub link provider not registered")
	}
	return provider.SubLinksForSubId(host, subId)
}

func (s *InboundService) GetSubEntries(host, subId string) ([]SubEntry, error) {
	provider := subLinkProvider()
	if provider == nil {
		return nil, common.NewError("sub link provider not registered")
	}
	return provider.SubEntriesForSubId(host, subId)
}

func (s *InboundService) GetAllInboundLinks(host string, userId int) ([]string, error) {
	provider := subLinkProvider()
	if provider == nil {
		return nil, common.NewError("sub link provider not registered")
	}
	inbounds, err := s.GetInbounds(userId)
	if err != nil {
		return nil, err
	}
	return provider.LinksForInbounds(host, inbounds), nil
}

func (s *InboundService) GetAllClientLinks(host string, email string) ([]string, error) {
	if email == "" {
		return nil, common.NewError("client email is required")
	}
	provider := subLinkProvider()
	if provider == nil {
		return nil, common.NewError("sub link provider not registered")
	}
	rec, err := s.clientService.GetRecordByEmail(nil, email)
	if err != nil {
		return nil, err
	}
	inboundIds, err := s.clientService.GetInboundIdsForRecord(rec.Id)
	if err != nil {
		return nil, err
	}
	var links []string
	for _, ibId := range inboundIds {
		inbound, getErr := s.GetInbound(ibId)
		if getErr != nil {
			return nil, getErr
		}
		links = append(links, provider.LinksForClient(host, inbound, email)...)
	}
	return links, nil
}
