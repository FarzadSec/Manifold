package controller

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// AppController edits how the app shows this panel's servers; the app itself
// talks to the subscription server.
type AppController struct {
	appService service.AppService
}

func NewAppController(g *gin.RouterGroup) *AppController {
	a := &AppController{}
	g.GET("/locations", a.locations)
	g.POST("/locations", a.saveLocation)
	g.POST("/locations/del/:id", a.deleteLocation)
	return a
}

func (a *AppController) locations(c *gin.Context) {
	locations, err := a.appService.Locations()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.app.toasts.locations"), err)
		return
	}
	jsonObj(c, locations, nil)
}

func (a *AppController) saveLocation(c *gin.Context) {
	var loc model.AppLocation
	if err := c.ShouldBindJSON(&loc); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.app.toasts.save"), err)
		return
	}
	saved, err := a.appService.SaveLocation(loc)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.app.toasts.save"), err)
		return
	}
	jsonMsgObj(c, I18nWeb(c, "pages.settings.app.toasts.saved"), saved, nil)
}

func (a *AppController) deleteLocation(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.app.toasts.delete"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.settings.app.toasts.deleted"), a.appService.DeleteLocation(id))
}
