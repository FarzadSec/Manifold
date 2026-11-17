package controller

import (
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"

	"github.com/gin-gonic/gin"
)

// IngressController serves the shared ports: one port, many services by name.
type IngressController struct {
	ingressService service.IngressService
}

// NewIngressController registers the shared port routes on g.
func NewIngressController(g *gin.RouterGroup) *IngressController {
	a := &IngressController{}
	g.GET("/list", a.list)
	g.POST("/save", a.save)
	g.POST("/del/:id", a.del)
	return a
}

func (a *IngressController) list(c *gin.Context) {
	rows, err := a.ingressService.List()
	jsonObj(c, rows, err)
}

func (a *IngressController) save(c *gin.Context) {
	var in model.IngressListener
	if err := c.ShouldBindJSON(&in); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.ingress.saveFailed"), err)
		return
	}
	if err := a.ingressService.Save(&in); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.ingress.saveFailed"), err)
		return
	}
	jsonObj(c, in, nil)
}

func (a *IngressController) del(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.ingress.deleteFailed"), a.ingressService.Delete(id))
}
