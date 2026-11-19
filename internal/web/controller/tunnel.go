package controller

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/entity"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"

	"github.com/gin-gonic/gin"
)

// TunnelController serves the tunnels between this server and others, the relays
// this panel manages, and the tunnel tests between two servers.
type TunnelController struct {
	tunnelService service.TunnelService
	benchService  service.TunnelBenchService
}

// NewTunnelController registers the tunnel routes on g.
func NewTunnelController(g *gin.RouterGroup) *TunnelController {
	a := &TunnelController{}
	g.Use(func(c *gin.Context) { c.Header(service.VersionHeader, config.GetProductVersion()) })
	g.GET("/list", a.list)
	g.GET("/status", a.status)
	g.GET("/suggest", a.suggest)
	g.POST("/add", a.add)
	g.POST("/update/:id", a.update)
	g.POST("/del/:id", a.del)
	g.POST("/pairing/:id", a.pairing)
	g.POST("/import", a.importCode)
	g.POST("/withdraw", a.withdraw)
	g.POST("/resync", a.resync)
	g.GET("/relays", a.relays)
	g.POST("/relays/add", a.addRelay)
	g.POST("/relays/update/:id", a.updateRelay)
	g.POST("/relays/del/:id", a.delRelay)
	g.GET("/relays/:id/tunnels", a.relayTunnels)
	g.GET("/groups", a.groups)
	g.POST("/groups/save", a.saveGroup)
	g.POST("/groups/del/:id", a.delGroup)
	g.GET("/bench/state", a.benchState)
	g.GET("/bench/history", a.benchHistory)
	g.GET("/bench/report/:id", a.benchReport)
	g.POST("/bench/link", a.benchLink)
	g.POST("/bench/start", a.benchStart)
	g.POST("/bench/cancel", a.benchCancel)
	g.POST("/bench/del/:id", a.benchDelete)
	g.POST("/bench/peer/:op", a.benchPeer)
	return a
}

func (a *TunnelController) benchState(c *gin.Context) {
	jsonObj(c, a.benchService.State(), nil)
}

func (a *TunnelController) benchHistory(c *gin.Context) {
	rows, err := a.benchService.History()
	jsonObj(c, rows, err)
}

func (a *TunnelController) benchReport(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	r, err := a.benchService.Report(id)
	jsonObj(c, r, err)
}

func (a *TunnelController) benchLink(c *gin.Context) {
	var req service.TunnelBenchLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.tunnels.bench.linkFailed"), err)
		return
	}
	link, err := a.benchService.MakeLink(req)
	jsonMsgObj(c, I18nWeb(c, "pages.tunnels.bench.linkFailed"), link, err)
}

func (a *TunnelController) benchStart(c *gin.Context) {
	var req service.TunnelBenchStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.tunnels.bench.startFailed"), err)
		return
	}
	r, err := a.benchService.Start(req)
	jsonMsgObj(c, I18nWeb(c, "pages.tunnels.bench.startFailed"), r, err)
}

func (a *TunnelController) benchCancel(c *gin.Context) {
	jsonMsg(c, I18nWeb(c, "pages.tunnels.bench.cancelled"), a.benchService.Cancel())
}

func (a *TunnelController) benchDelete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.tunnels.deleted"), a.benchService.Delete(id))
}

// benchPeer serves the other server of a test, and only it: the link's credential is
// the one way in, and its errors go back undecorated for that server to show.
func (a *TunnelController) benchPeer(c *gin.Context) {
	if scope, _ := c.Get("api_token_scope"); scope != model.ApiScopeTunnelBench {
		c.AbortWithStatusJSON(http.StatusForbidden, entity.Msg{Msg: "only the other server of a tunnel test calls this"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 8<<20))
	if err != nil {
		c.JSON(http.StatusRequestEntityTooLarge, entity.Msg{Msg: err.Error()})
		return
	}
	obj, err := a.benchService.Peer(c.Param("op"), body)
	switch {
	case errors.Is(err, service.ErrTunnelBenchGone):
		c.JSON(http.StatusGone, entity.Msg{Msg: err.Error()})
	case err != nil:
		c.JSON(http.StatusOK, entity.Msg{Msg: err.Error()})
	default:
		c.JSON(http.StatusOK, entity.Msg{Success: true, Obj: obj})
	}
}

func (a *TunnelController) groups(c *gin.Context) {
	rows, err := a.tunnelService.ListGroups()
	jsonObj(c, rows, err)
}

func (a *TunnelController) saveGroup(c *gin.Context) {
	var in struct {
		Id int `json:"id"`
		service.TunnelGroupRequest
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.tunnels.groups.saveFailed"), err)
		return
	}
	view, err := a.tunnelService.SaveGroup(in.Id, in.TunnelGroupRequest)
	jsonMsgObj(c, I18nWeb(c, "pages.tunnels.groups.saveFailed"), view, err)
}

func (a *TunnelController) delGroup(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.tunnels.groups.deleteFailed"), a.tunnelService.DeleteGroup(id))
}

func pathID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		jsonMsg(c, "id", err)
		return 0, false
	}
	return id, true
}

func (a *TunnelController) list(c *gin.Context) {
	views, err := a.tunnelService.List()
	jsonObj(c, views, err)
}

func (a *TunnelController) status(c *gin.Context) {
	jsonObj(c, a.tunnelService.Statuses(), nil)
}

func (a *TunnelController) suggest(c *gin.Context) {
	s, err := a.tunnelService.Suggest()
	jsonObj(c, s, err)
}

func (a *TunnelController) add(c *gin.Context) {
	var req service.TunnelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.tunnels.saveError"), err)
		return
	}
	v, err := a.tunnelService.Add(req)
	jsonMsgObj(c, I18nWeb(c, "pages.tunnels.saved"), v, err)
}

func (a *TunnelController) update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req service.TunnelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.tunnels.saveError"), err)
		return
	}
	v, err := a.tunnelService.Update(id, req)
	jsonMsgObj(c, I18nWeb(c, "pages.tunnels.saved"), v, err)
}

func (a *TunnelController) del(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.tunnels.deleted"), a.tunnelService.Delete(id))
}

func (a *TunnelController) pairing(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	p, err := a.tunnelService.Pairing(id, c.PostForm("address"))
	jsonObj(c, p, err)
}

func (a *TunnelController) importCode(c *gin.Context) {
	v, err := a.tunnelService.Import(c.PostForm("code"))
	jsonMsgObj(c, I18nWeb(c, "pages.tunnels.saved"), v, err)
}

func (a *TunnelController) withdraw(c *gin.Context) {
	jsonMsg(c, I18nWeb(c, "pages.tunnels.deleted"), a.tunnelService.Withdraw(c.PostForm("pairId")))
}

func (a *TunnelController) resync(c *gin.Context) {
	errs := a.tunnelService.Resync()
	failed := make([]string, 0, len(errs))
	for _, err := range errs {
		failed = append(failed, err.Error())
	}
	jsonObj(c, failed, nil)
}

func (a *TunnelController) relays(c *gin.Context) {
	rows, err := a.tunnelService.ListRelays()
	jsonObj(c, rows, err)
}

func (a *TunnelController) addRelay(c *gin.Context) {
	row, err := a.tunnelService.SaveRelay(0, c.PostForm("name"), c.PostForm("url"), c.PostForm("token"))
	jsonMsgObj(c, I18nWeb(c, "pages.tunnels.saved"), row, err)
}

func (a *TunnelController) updateRelay(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	row, err := a.tunnelService.SaveRelay(id, c.PostForm("name"), c.PostForm("url"), c.PostForm("token"))
	jsonMsgObj(c, I18nWeb(c, "pages.tunnels.saved"), row, err)
}

func (a *TunnelController) delRelay(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.tunnels.deleted"), a.tunnelService.DeleteRelay(id))
}

func (a *TunnelController) relayTunnels(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	views, err := a.tunnelService.RelayStatuses(id)
	jsonObj(c, views, err)
}
