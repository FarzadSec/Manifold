package controller

import (
	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// CertificateController serves the certificate store.
type CertificateController struct {
	certService service.CertificateService
}

// NewCertificateController registers the certificate routes on g.
func NewCertificateController(g *gin.RouterGroup) *CertificateController {
	a := &CertificateController{}
	g.GET("/list", a.list)
	g.POST("/save", a.save)
	g.POST("/upload", a.upload)
	g.POST("/issue/:id", a.issue)
	g.POST("/del/:id", a.del)
	g.POST("/put", a.put)
	return a
}

func (a *CertificateController) list(c *gin.Context) {
	rows, err := a.certService.List()
	jsonObj(c, rows, err)
}

func (a *CertificateController) save(c *gin.Context) {
	var in service.CertificateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.certs.saveFailed"), err)
		return
	}
	view, err := a.certService.Save(in)
	jsonMsgObj(c, I18nWeb(c, "pages.certs.saveFailed"), view, err)
}

func (a *CertificateController) upload(c *gin.Context) {
	var in service.CertificateUpload
	if err := c.ShouldBindJSON(&in); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.certs.saveFailed"), err)
		return
	}
	view, err := a.certService.Upload(in)
	jsonMsgObj(c, I18nWeb(c, "pages.certs.saveFailed"), view, err)
}

func (a *CertificateController) issue(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.certs.issueFailed"), a.certService.Issue(id))
}

func (a *CertificateController) del(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.certs.deleteFailed"), a.certService.Delete(id))
}

// put takes a copy a master pushes to this node.
func (a *CertificateController) put(c *gin.Context) {
	var in struct {
		Name string `json:"name"`
		Cert string `json:"cert"`
		Key  string `json:"key"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.certs.saveFailed"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.certs.saveFailed"), a.certService.StorePushed(in.Name, []byte(in.Cert), []byte(in.Key)))
}
