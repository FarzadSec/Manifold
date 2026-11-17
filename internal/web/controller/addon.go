package controller

import (
	"io"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/web/access"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service/panel"
)

// AddonController installs addons from their manifest.
type AddonController struct {
	addonService panel.AddonService
}

// NewAddonController registers the addon routes on g.
func NewAddonController(g *gin.RouterGroup) *AddonController {
	a := &AddonController{}
	g.GET("/list", a.list)
	g.POST("/install", a.install)
	g.POST("/enable/:id", a.enable)
	g.POST("/disable/:id", a.disable)
	g.POST("/uninstall/:id", a.uninstall)
	return a
}

func (a *AddonController) list(c *gin.Context) {
	rows, err := a.addonService.List()
	jsonObj(c, rows, err)
}

func (a *AddonController) install(c *gin.Context) {
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 64<<10))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.addons.installFailed"), err)
		return
	}
	out, err := a.addonService.Install(raw, access.ActorFrom(c))
	jsonMsgObj(c, I18nWeb(c, "pages.addons.installFailed"), out, err)
}

func (a *AddonController) setEnabled(c *gin.Context, enable bool) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.addons.changeFailed"), a.addonService.SetEnabled(id, enable))
}

func (a *AddonController) enable(c *gin.Context)  { a.setEnabled(c, true) }
func (a *AddonController) disable(c *gin.Context) { a.setEnabled(c, false) }

func (a *AddonController) uninstall(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.addons.changeFailed"), a.addonService.Uninstall(id))
}
