package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/access"
	"github.com/mhsanaei/3x-ui/v3/internal/web/entity"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/web/websocket"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func notifyClientsChanged() {
	websocket.BroadcastInvalidate(websocket.MessageTypeClients)
}

func parseInboundIdsQuery(raw string) []int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]int, 0, len(parts))
	for _, p := range parts {
		if id, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

type ClientController struct {
	clientService  service.ClientService
	inboundService service.InboundService
	xrayService    service.XrayService
	settingService service.SettingService
	happGenerator  service.HappLinkGenerator
}

func NewClientController(g *gin.RouterGroup) *ClientController {
	a := &ClientController{}
	a.happGenerator = service.NewHappService(&a.clientService, &a.settingService)
	a.initRouter(g)
	return a
}

func (a *ClientController) initRouter(g *gin.RouterGroup) {
	g.Use(guardClientAccess)
	g.GET("/list", a.list)
	g.GET("/list/paged", a.listPaged)
	g.GET("/get", a.get)
	g.GET("/get/:email", a.get)
	g.GET("/get/subId/:subId", a.getBySubId)
	g.GET("/get/tgId/:tgId", a.getByTgId)
	g.GET("/traffic/:email", a.getTrafficByEmail)
	g.GET("/subLinks/:subId", a.getSubLinks)
	g.GET("/links/:email", a.getClientLinks)
	g.POST("/happLink/:id", a.generateHappLink)

	g.POST("/add", a.create)
	g.POST("/renewalPreview", a.renewalPreview)
	g.POST("/update", a.update)
	g.POST("/update/:email", a.update)
	g.POST("/del", a.delete)
	g.POST("/del/:email", a.delete)
	g.POST("/:email/attach", a.attach)
	g.POST("/:email/detach", a.detach)
	g.POST("/:email/externalLinks", a.setExternalLinks)
	g.GET("/export", a.export)
	g.POST("/import", a.importClients)
	g.POST("/delOrphans", a.delOrphans)
	g.POST("/resetAllTraffics", a.resetAllTraffics)
	g.POST("/delDepleted", a.delDepleted)
	g.POST("/bulkAdjust", a.bulkAdjust)
	g.POST("/bulkEnable", a.bulkEnable)
	g.POST("/bulkDisable", a.bulkDisable)
	g.POST("/bulkDel", a.bulkDelete)
	g.POST("/bulkCreate", a.bulkCreate)
	g.POST("/bulkAttach", a.bulkAttach)
	g.POST("/bulkDetach", a.bulkDetach)
	g.POST("/bulkResetTraffic", a.bulkResetTraffic)
	g.POST("/resetTraffic/:email", a.resetTrafficByEmail)
	g.POST("/updateTraffic/:email", a.updateTrafficByEmail)
	g.POST("/ips/:email", a.getIps)
	g.POST("/clearIps/:email", a.clearIps)
	g.POST("/hwids/:email", a.getHwids)
	g.DELETE("/hwids/:email", a.clearHwids)
	g.DELETE("/hwids/:email/:id", a.deleteHwid)
	g.POST("/onlines", a.onlines)
	g.POST("/onlinesByGuid", a.onlinesByGuid)
	g.POST("/clientIpsByGuid", a.clientIpsByGuid)
	g.POST("/activeInbounds", a.activeInbounds)
	g.POST("/lastOnline", a.lastOnline)
	g.POST("/changeOwner", a.changeOwner)
}

func (a *ClientController) list(c *gin.Context) {
	rows, err := a.clientService.List()
	if err == nil {
		rows, err = ownedOnly(c, rows, func(r service.ClientWithAttachments) string { return r.Email })
	}
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.inbounds.toasts.obtain"), err)
		return
	}
	jsonObj(c, rows, nil)
}

func (a *ClientController) renewalPreview(c *gin.Context) {
	var request service.ClientRenewalPreviewRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		jsonObj(c, nil, err)
		return
	}
	preview, err := a.clientService.PreviewRenewal(request, &a.settingService)
	jsonObj(c, preview, err)
}

func (a *ClientController) listPaged(c *gin.Context) {
	var params service.ClientPageParams
	if err := c.ShouldBindQuery(&params); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.inbounds.toasts.obtain"), err)
		return
	}
	actor := access.ActorFrom(c)
	if actor.OwnsOnly() {
		params.ScopeOwner = &actor.UserId
	} else if params.Owner == "mine" {
		params.Owner = strconv.Itoa(actor.UserId)
	}
	resp, err := a.clientService.ListPaged(&a.inboundService, &a.settingService, params)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.inbounds.toasts.obtain"), err)
		return
	}
	jsonObj(c, resp, nil)
}

func (a *ClientController) buildClientPayload(rec *model.ClientRecord) (gin.H, error) {
	inboundIds, err := a.clientService.GetInboundIdsForRecord(rec.Id)
	if err != nil {
		return nil, err
	}
	externalLinks, err := a.clientService.GetExternalLinksForRecord(rec.Id)
	if err != nil {
		return nil, err
	}
	flow, err := a.clientService.EffectiveFlow(nil, rec.Id)
	if err != nil {
		return nil, err
	}
	rec.Flow = flow
	var usedTraffic int64
	if t, tErr := a.inboundService.GetClientTrafficByEmail(rec.Email); tErr == nil && t != nil {
		usedTraffic = t.Up + t.Down
	}
	tunnelAllowedIPs, err := a.clientService.TunnelAllowedIPsByInbound(&a.inboundService, rec.Email, inboundIds)
	if err != nil {
		return nil, err
	}
	return gin.H{
		"client":           rec,
		"inboundIds":       inboundIds,
		"externalLinks":    externalLinks,
		"usedTraffic":      usedTraffic,
		"tunnelAllowedIPs": tunnelAllowedIPs,
	}, nil
}

// Query aliases keep legacy dot-segment names addressable without changing path clients.
func clientEmailParam(c *gin.Context) (string, bool) {
	email := c.Param("email")
	if email == "" {
		email = c.Query("email")
	}
	if email == "" {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), errors.New("client email is required"))
		return "", false
	}
	return email, true
}

func (a *ClientController) get(c *gin.Context) {
	email, ok := clientEmailParam(c)
	if !ok {
		return
	}
	rec, err := a.clientService.GetRecordByEmail(nil, email)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.inbounds.toasts.obtain"), err)
		return
	}
	payload, err := a.buildClientPayload(rec)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.inbounds.toasts.obtain"), err)
		return
	}
	jsonObj(c, payload, nil)
}

func (a *ClientController) getBySubId(c *gin.Context) {
	records, err := a.clientService.GetRecordsBySubID(c.Param("subId"))
	if err == nil && len(records) == 0 {
		err = gorm.ErrRecordNotFound
	}
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.inbounds.toasts.obtain"), err)
		return
	}
	results := make([]gin.H, 0, len(records))
	for _, rec := range records {
		payload, err := a.buildClientPayload(rec)
		if err != nil {
			jsonMsg(c, I18nWeb(c, "pages.inbounds.toasts.obtain"), err)
			return
		}
		results = append(results, payload)
	}
	jsonObj(c, results, nil)
}

func (a *ClientController) getByTgId(c *gin.Context) {
	tgIdStr := c.Param("tgId")
	tgId, err := strconv.ParseInt(tgIdStr, 10, 64)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.inbounds.toasts.obtain"), err)
		return
	}
	records, err := a.clientService.GetRecordsByTgID(tgId)
	if err == nil {
		records, err = ownedOnly(c, records, func(r *model.ClientRecord) string { return r.Email })
	}
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.inbounds.toasts.obtain"), err)
		return
	}
	results := make([]gin.H, 0, len(records))
	for _, rec := range records {
		payload, err := a.buildClientPayload(rec)
		if err != nil {
			jsonMsg(c, I18nWeb(c, "get"), err)
			return
		}
		results = append(results, payload)
	}
	jsonObj(c, results, nil)
}

func (a *ClientController) create(c *gin.Context) {
	var payload service.ClientCreatePayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	owner, err := a.prepareCreate(c, []*service.ClientCreatePayload{&payload}, payload.PlanId, payload.OwnerId)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	needRestart, err := a.clientService.Create(&a.inboundService, &payload)
	if err == nil && owner > 0 {
		err = service.SetOwner([]string{payload.Client.Email}, owner)
	}
	// Flagged before the error check: a partly-applied create leaves clients
	// committed on the inbounds that succeeded, and those still need the restart.
	if needRestart {
		a.xrayService.SetToNeedRestart()
	}
	// A partly-applied call committed real clients; a rejected one touched
	// nothing, and broadcasting those would refetch every panel for nothing.
	if needRestart || err == nil {
		notifyClientsChanged()
	}
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsgObj(c, I18nWeb(c, "pages.inbounds.toasts.inboundClientAddSuccess"), pendingNodeObj(a.inboundService.AnyNodePending(payload.InboundIds)), nil)
}

func (a *ClientController) update(c *gin.Context) {
	email, ok := clientEmailParam(c)
	if !ok {
		return
	}
	body, err := c.GetRawData()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	inboundFilter := parseInboundIdsQuery(c.Query("inboundIds"))
	needRestart, err := a.clientService.UpdateByEmailJSON(&a.inboundService, email, body, inboundFilter...)
	// Flagged before the error check: a partly-applied edit leaves the change
	// committed on the inbounds that succeeded, and those still need the restart.
	if needRestart {
		a.xrayService.SetToNeedRestart()
	}
	// A partly-applied call committed real changes; a rejected one touched
	// nothing, and broadcasting those would refetch every panel for nothing.
	if needRestart || err == nil {
		notifyClientsChanged()
	}
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsgObj(c, I18nWeb(c, "pages.inbounds.toasts.inboundClientUpdateSuccess"), pendingNodeObj(a.clientService.HasPendingNode(&a.inboundService, email)), nil)
}

func (a *ClientController) delete(c *gin.Context) {
	email, ok := clientEmailParam(c)
	if !ok {
		return
	}
	keepTraffic := c.Query("keepTraffic") == "1"
	needRestart, err := a.clientService.DeleteByEmail(&a.inboundService, email, keepTraffic)
	// Flagged before the error check: a partly-applied delete already removed
	// the client from the inbounds that succeeded, and those need the restart.
	if needRestart {
		a.xrayService.SetToNeedRestart()
	}
	// A partly-applied call committed real removals; a rejected one touched
	// nothing, and broadcasting those would refetch every panel for nothing.
	if needRestart || err == nil {
		notifyClientsChanged()
	}
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.inbounds.toasts.inboundClientDeleteSuccess"), nil)
}

type attachDetachBody struct {
	InboundIds []int `json:"inboundIds"`
}

type externalLinksBody struct {
	ExternalLinks []service.ExternalLinkInput `json:"externalLinks"`
}

func (a *ClientController) attach(c *gin.Context) {
	email := c.Param("email")
	var body attachDetachBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	narrowed, err := service.ClientAccess{Actor: access.ActorFrom(c)}.NarrowInbounds(body.InboundIds)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	body.InboundIds = narrowed
	needRestart, err := a.clientService.AttachByEmail(&a.inboundService, email, body.InboundIds)
	if needRestart {
		a.xrayService.SetToNeedRestart()
	}
	// A partly-applied call committed real clients; a rejected one touched
	// nothing, and broadcasting those would refetch every panel for nothing.
	if needRestart || err == nil {
		notifyClientsChanged()
	}
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsgObj(c, I18nWeb(c, "pages.inbounds.toasts.inboundClientAddSuccess"), pendingNodeObj(a.inboundService.AnyNodePending(body.InboundIds)), nil)
}

func (a *ClientController) setExternalLinks(c *gin.Context) {
	email := c.Param("email")
	var body externalLinksBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if err := a.clientService.SetExternalLinksByEmail(email, body.ExternalLinks); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.inbounds.toasts.inboundClientUpdateSuccess"), nil)
	notifyClientsChanged()
}

func (a *ClientController) resetAllTraffics(c *gin.Context) {
	needRestart, err := a.clientService.ResetAllTraffics()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.inbounds.toasts.resetAllClientTrafficSuccess"), nil)
	if needRestart {
		a.xrayService.SetToNeedRestart()
	}
	notifyClientsChanged()
}

type bulkAdjustRequest struct {
	Emails    []string `json:"emails"`
	AddDays   int      `json:"addDays"`
	AddBytes  int64    `json:"addBytes"`
	Flow      string   `json:"flow"`
	LimitHwid *int     `json:"limitHwid"`
	AdTag     string   `json:"adTag"`
}

func (a *ClientController) bulkAdjust(c *gin.Context) {
	var req bulkAdjustRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	result, needRestart, err := a.clientService.BulkAdjust(&a.inboundService, req.Emails, req.AddDays, req.AddBytes, req.Flow, req.LimitHwid, req.AdTag)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, result, nil)
	if needRestart {
		a.xrayService.SetToNeedRestart()
	}
	notifyClientsChanged()
}

type bulkDeleteRequest struct {
	Emails      []string `json:"emails"`
	KeepTraffic bool     `json:"keepTraffic"`
}

type bulkAttachRequest struct {
	Emails     []string `json:"emails"`
	InboundIds []int    `json:"inboundIds"`
}

func (a *ClientController) bulkAttach(c *gin.Context) {
	var req bulkAttachRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	narrowed, err := service.ClientAccess{Actor: access.ActorFrom(c)}.NarrowInbounds(req.InboundIds)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	result, needRestart, err := a.clientService.BulkAttach(&a.inboundService, req.Emails, narrowed)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, result, nil)
	if needRestart {
		a.xrayService.SetToNeedRestart()
	}
	notifyClientsChanged()
}

type bulkDetachRequest struct {
	Emails     []string `json:"emails"`
	InboundIds []int    `json:"inboundIds"`
}

func (a *ClientController) bulkDetach(c *gin.Context) {
	var req bulkDetachRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	result, needRestart, err := a.clientService.BulkDetach(&a.inboundService, req.Emails, req.InboundIds)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, result, nil)
	if needRestart {
		a.xrayService.SetToNeedRestart()
	}
	notifyClientsChanged()
}

func (a *ClientController) bulkDelete(c *gin.Context) {
	var req bulkDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	result, needRestart, err := a.clientService.BulkDelete(&a.inboundService, req.Emails, req.KeepTraffic)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, result, nil)
	if needRestart {
		a.xrayService.SetToNeedRestart()
	}
	notifyClientsChanged()
}

type bulkEnableRequest struct {
	Emails []string `json:"emails"`
}

func (a *ClientController) bulkEnable(c *gin.Context) {
	a.bulkSetEnable(c, true)
}

func (a *ClientController) bulkDisable(c *gin.Context) {
	a.bulkSetEnable(c, false)
}

func (a *ClientController) bulkSetEnable(c *gin.Context, enable bool) {
	var req bulkEnableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	result, needRestart, err := a.clientService.BulkSetEnable(&a.inboundService, req.Emails, enable)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, result, nil)
	if needRestart {
		a.xrayService.SetToNeedRestart()
	}
	notifyClientsChanged()
}

func (a *ClientController) bulkCreate(c *gin.Context) {
	var payloads []service.ClientCreatePayload
	if err := c.ShouldBindJSON(&payloads); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	refs := make([]*service.ClientCreatePayload, len(payloads))
	for i := range payloads {
		refs[i] = &payloads[i]
	}
	planID, ownerID := 0, 0
	if len(payloads) > 0 {
		planID, ownerID = payloads[0].PlanId, payloads[0].OwnerId
	}
	owner, err := a.prepareCreate(c, refs, planID, ownerID)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	result, needRestart, err := a.clientService.BulkCreate(&a.inboundService, payloads)
	if err == nil && owner > 0 {
		emails := make([]string, 0, len(payloads))
		for _, p := range payloads {
			emails = append(emails, p.Client.Email)
		}
		err = service.SetOwner(emails, owner)
	}
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, result, nil)
	if needRestart {
		a.xrayService.SetToNeedRestart()
	}
	notifyClientsChanged()
}

func (a *ClientController) delDepleted(c *gin.Context) {
	deleted, needRestart, err := a.clientService.DelDepleted(&a.inboundService)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, gin.H{"deleted": deleted}, nil)
	if needRestart {
		a.xrayService.SetToNeedRestart()
	}
	notifyClientsChanged()
}

// export returns every client as a {client, inboundIds} list in the standard
// envelope. The frontend renders it in a read-only CodeMirror viewer (Copy /
// Download), so this hands back data rather than streaming a file attachment.
func (a *ClientController) export(c *gin.Context) {
	items, err := a.clientService.ExportAll()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, items, nil)
}

type importClientsRequest struct {
	Data string `json:"data"`
}

// importClients accepts the pasted export text as a JSON body { "data": "..." },
// mirroring the inbound import flow. The data string is itself a JSON-encoded
// []ClientCreatePayload, so it is unmarshalled in a second step.
func (a *ClientController) importClients(c *gin.Context) {
	var req importClientsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	var items []service.ClientCreatePayload
	if err := json.Unmarshal([]byte(req.Data), &items); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	result, needRestart, err := a.clientService.ImportClients(&a.inboundService, items)
	// Flagged before the error check: a failed traffic restore still leaves the
	// clients created before it committed, and those need the restart and refresh.
	if needRestart {
		a.xrayService.SetToNeedRestart()
	}
	if needRestart || result.Created > 0 || err == nil {
		notifyClientsChanged()
	}
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, result, nil)
}

func (a *ClientController) delOrphans(c *gin.Context) {
	deleted, err := a.clientService.DeleteOrphans()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, gin.H{"deleted": deleted}, nil)
	notifyClientsChanged()
}

func (a *ClientController) resetTrafficByEmail(c *gin.Context) {
	email := c.Param("email")
	needRestart, err := a.clientService.ResetTrafficByEmail(&a.inboundService, email)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.inbounds.toasts.resetInboundClientTrafficSuccess"), nil)
	if needRestart {
		a.xrayService.SetToNeedRestart()
	}
	notifyClientsChanged()
}

type trafficUpdateRequest struct {
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
}

func (a *ClientController) updateTrafficByEmail(c *gin.Context) {
	email := c.Param("email")
	var req trafficUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if err := a.inboundService.UpdateClientTrafficByEmail(email, req.Upload, req.Download); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.inbounds.toasts.inboundClientUpdateSuccess"), nil)
	notifyClientsChanged()
}

func (a *ClientController) getIps(c *gin.Context) {
	email := c.Param("email")
	infos, err := a.inboundService.GetClientIpsWithNodes(email)
	jsonObj(c, infos, err)
}

func (a *ClientController) clientIpsByGuid(c *gin.Context) {
	data, err := a.inboundService.GetClientIpsByGuid()
	jsonObj(c, data, err)
}

func (a *ClientController) clearIps(c *gin.Context) {
	email := c.Param("email")
	if err := a.inboundService.ClearClientIps(email); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.inbounds.toasts.updateSuccess"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.inbounds.toasts.logCleanSuccess"), nil)
}

func (a *ClientController) getHwids(c *gin.Context) {
	infos, err := a.clientService.ListClientHwids(c.Param("email"))
	jsonObj(c, infos, err)
}

func (a *ClientController) clearHwids(c *gin.Context) {
	if err := a.clientService.ClearClientHwids(c.Param("email")); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.inbounds.toasts.updateSuccess"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.inbounds.toasts.logCleanSuccess"), nil)
}

func (a *ClientController) deleteHwid(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if err := a.clientService.DeleteClientHwid(c.Param("email"), id); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.clients.hwidDeleted"), nil)
}

func (a *ClientController) onlines(c *gin.Context) {
	onlines, err := ownedOnly(c, a.inboundService.GetOnlineClients(), func(e string) string { return e })
	jsonObj(c, onlines, err)
}

func (a *ClientController) onlinesByGuid(c *gin.Context) {
	jsonObj(c, a.inboundService.GetOnlineClientsByGuid(), nil)
}

func (a *ClientController) activeInbounds(c *gin.Context) {
	jsonObj(c, a.inboundService.GetActiveInboundsByGuid(), nil)
}

func (a *ClientController) lastOnline(c *gin.Context) {
	data, err := a.inboundService.GetClientsLastOnline()
	if err == nil {
		data, err = ownedMap(c, data)
	}
	jsonObj(c, data, err)
}

func (a *ClientController) getTrafficByEmail(c *gin.Context) {
	email := c.Param("email")
	traffic, err := a.inboundService.GetClientTrafficByEmail(email)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.inbounds.toasts.trafficGetError"), err)
		return
	}
	jsonObj(c, traffic, nil)
}

func (a *ClientController) getSubLinks(c *gin.Context) {
	links, err := a.inboundService.GetSubLinks(resolveHost(c), c.Param("subId"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.inbounds.toasts.obtain"), err)
		return
	}
	jsonObj(c, links, nil)
}

func (a *ClientController) getClientLinks(c *gin.Context) {
	links, err := a.inboundService.GetAllClientLinks(resolveHost(c), c.Param("email"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.inbounds.toasts.obtain"), err)
		return
	}
	jsonObj(c, links, nil)
}

func (a *ClientController) generateHappLink(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	clientID, err := strconv.Atoi(c.Param("id"))
	if err != nil || clientID < 1 {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), service.ErrHappLinkUnavailable)
		return
	}
	result, err := a.happGenerator.Generate(c.Request.Context(), clientID, c.Request.Host)
	if err != nil {
		if errors.Is(err, service.ErrHappSourceTooLong) {
			// Keep the code exact so clients can localize it without exposing internal error details.
			c.JSON(http.StatusOK, entity.Msg{Success: false, Msg: "happ_source_too_long", Obj: nil})
			return
		}
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), service.ErrHappLinkUnavailable)
		return
	}
	jsonObj(c, result, nil)
}

func (a *ClientController) detach(c *gin.Context) {
	email := c.Param("email")
	var body attachDetachBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	needRestart, err := a.clientService.DetachByEmailMany(&a.inboundService, email, body.InboundIds)
	// Flagged before the error check: a partly-applied detach already removed
	// the client from the inbounds that succeeded, and those need the restart.
	if needRestart {
		a.xrayService.SetToNeedRestart()
	}
	// A partly-applied call committed real removals; a rejected one touched
	// nothing, and broadcasting those would refetch every panel for nothing.
	if needRestart || err == nil {
		notifyClientsChanged()
	}
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsgObj(c, I18nWeb(c, "pages.inbounds.toasts.inboundClientDeleteSuccess"), pendingNodeObj(a.inboundService.AnyNodePending(body.InboundIds)), nil)
}

type bulkResetRequest struct {
	Emails []string `json:"emails"`
}

func (a *ClientController) bulkResetTraffic(c *gin.Context) {
	var req bulkResetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	affected, err := a.clientService.BulkResetTraffic(&a.inboundService, req.Emails)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, gin.H{"affected": affected}, nil)
	a.xrayService.SetToNeedRestart()
	notifyClientsChanged()
}

// prepareCreate applies the actor's plan, owner and allowance to users about to be created.
func (a *ClientController) prepareCreate(c *gin.Context, payloads []*service.ClientCreatePayload, planID, ownerID int) (int, error) {
	options, err := a.inboundService.GetInboundOptions(inboundPool().Id)
	if err != nil {
		return 0, err
	}
	all := make([]int, 0, len(options))
	for _, o := range options {
		if model.Protocol(o.Protocol).HoldsClients() {
			all = append(all, o.Id)
		}
	}
	return service.ClientAccess{Actor: access.ActorFrom(c)}.PrepareCreate(payloads, planID, ownerID, all, time.Now())
}

type changeOwnerRequest struct {
	Emails  []string `json:"emails"`
	OwnerId int      `json:"ownerId"`
}

// changeOwner hands users to another account; a reseller's user limit is checked once for all.
func (a *ClientController) changeOwner(c *gin.Context) {
	var req changeOwnerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	err := a.clientService.ChangeOwner(&a.inboundService, req.Emails, req.OwnerId)
	if err == nil {
		notifyClientsChanged()
	}
	jsonMsg(c, I18nWeb(c, "pages.clients.ownerChanged"), err)
}
