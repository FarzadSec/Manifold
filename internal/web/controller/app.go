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
	g.GET("/notices", a.notices)
	g.POST("/notices", a.publishNotice)
	g.POST("/notices/del/:id", a.deleteNotice)
	g.GET("/reports", a.reports)
	g.GET("/reports/unseen", a.unseenReports)
	g.GET("/reports/:id", a.report)
	g.POST("/reports/seen/:id", a.markReportSeen)
	g.POST("/reports/del/:id", a.deleteReport)
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

func (a *AppController) notices(c *gin.Context) {
	notices, err := a.appService.Notices()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.app.notices.toasts.load"), err)
		return
	}
	jsonObj(c, notices, nil)
}

func (a *AppController) publishNotice(c *gin.Context) {
	var notice model.AppNotice
	if err := c.ShouldBindJSON(&notice); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.app.notices.toasts.publish"), err)
		return
	}
	published, err := a.appService.PublishNotice(notice.Body, notice.Level)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.app.notices.toasts.publish"), err)
		return
	}
	jsonMsgObj(c, I18nWeb(c, "pages.settings.app.notices.toasts.published"), published, nil)
}

func (a *AppController) deleteNotice(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.app.notices.toasts.delete"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.settings.app.notices.toasts.deleted"), a.appService.DeleteNotice(id))
}

func (a *AppController) reports(c *gin.Context) {
	reports, err := a.appService.Reports()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.app.reports.toasts.load"), err)
		return
	}
	jsonObj(c, reports, nil)
}

func (a *AppController) unseenReports(c *gin.Context) {
	n, err := a.appService.UnseenReports()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.app.reports.toasts.load"), err)
		return
	}
	jsonObj(c, n, nil)
}

func (a *AppController) report(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.app.reports.toasts.load"), err)
		return
	}
	report, err := a.appService.Report(id)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.app.reports.toasts.load"), err)
		return
	}
	jsonObj(c, report, nil)
}

func (a *AppController) markReportSeen(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.app.reports.toasts.seen"), err)
		return
	}
	if err := a.appService.MarkReportSeen(id); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.app.reports.toasts.seen"), err)
		return
	}
	jsonObj(c, nil, nil)
}

func (a *AppController) deleteReport(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.app.reports.toasts.delete"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.settings.app.reports.toasts.deleted"), a.appService.DeleteReport(id))
}
