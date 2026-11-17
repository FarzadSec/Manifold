package controller

import (
	"github.com/mhsanaei/3x-ui/v3/internal/eventbus"
	"github.com/mhsanaei/3x-ui/v3/internal/web/access"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"

	"github.com/gin-gonic/gin"
)

// WebhookController serves webhooks and their delivery logs; a reseller sees its own only.
type WebhookController struct {
	webhookService service.WebhookService
}

// NewWebhookController registers the webhook routes on g.
func NewWebhookController(g *gin.RouterGroup) *WebhookController {
	a := &WebhookController{}
	g.GET("/list", a.list)
	g.GET("/events", a.events)
	g.POST("/save", a.save)
	g.POST("/del/:id", a.del)
	g.POST("/test/:id", a.test)
	g.GET("/deliveries/:id", a.deliveries)
	g.POST("/replay/:id", a.replay)
	return a
}

func (a *WebhookController) list(c *gin.Context) {
	rows, err := a.webhookService.List(access.ActorFrom(c))
	jsonObj(c, rows, err)
}

func (a *WebhookController) events(c *gin.Context) {
	jsonObj(c, eventbus.AllEventTypes, nil)
}

func (a *WebhookController) save(c *gin.Context) {
	var in service.WebhookInput
	if err := c.ShouldBindJSON(&in); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.webhooks.saveFailed"), err)
		return
	}
	view, err := a.webhookService.Save(in, access.ActorFrom(c))
	jsonMsgObj(c, I18nWeb(c, "pages.webhooks.saveFailed"), view, err)
}

func (a *WebhookController) del(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.webhooks.deleteFailed"), a.webhookService.Delete(id, access.ActorFrom(c)))
}

func (a *WebhookController) test(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.webhooks.testFailed"), a.webhookService.Test(id, access.ActorFrom(c)))
}

func (a *WebhookController) deliveries(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	rows, err := a.webhookService.Deliveries(id, access.ActorFrom(c))
	jsonObj(c, rows, err)
}

func (a *WebhookController) replay(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.webhooks.replayFailed"), a.webhookService.Replay(id, access.ActorFrom(c)))
}
