package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/nodessh"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"

	"github.com/gin-gonic/gin"
)

// BackupController serves scheduled backup targets.
type BackupController struct {
	backupService service.BackupService
}

// NewBackupController registers the backup target routes on g.
func NewBackupController(g *gin.RouterGroup) *BackupController {
	a := &BackupController{}
	g.GET("/targets", a.list)
	g.POST("/targets/save", a.save)
	g.POST("/targets/del/:id", a.del)
	g.POST("/targets/run/:id", a.run)
	g.GET("/targets/files/:id", a.files)
	g.POST("/sshHostKey", a.sshHostKey)
	return a
}

func (a *BackupController) list(c *gin.Context) {
	rows, err := a.backupService.List()
	jsonObj(c, rows, err)
}

func (a *BackupController) save(c *gin.Context) {
	var in service.BackupTargetInput
	if err := c.ShouldBindJSON(&in); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.backups.saveFailed"), err)
		return
	}
	view, err := a.backupService.Save(in)
	jsonMsgObj(c, I18nWeb(c, "pages.backups.saveFailed"), view, err)
}

func (a *BackupController) del(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.backups.deleteFailed"), a.backupService.Delete(id))
}

// run backs up to one target now and answers with the outcome recorded on it.
func (a *BackupController) run(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	view, err := a.backupService.Run(id, time.Now())
	jsonMsgObj(c, I18nWeb(c, "pages.backups.runFailed"), view, err)
}

func (a *BackupController) files(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	files, err := a.backupService.Files(id)
	jsonMsgObj(c, I18nWeb(c, "pages.backups.listFailed"), files, err)
}

func (a *BackupController) sshHostKey(c *gin.Context) {
	var req struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if err := c.ShouldBind(&req); err != nil || strings.TrimSpace(req.Host) == "" {
		jsonMsg(c, I18nWeb(c, "pages.nodes.install.hostKeyFailed"), fmt.Errorf("a host is needed"))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	key, err := nodessh.HostKey(ctx, strings.TrimSpace(req.Host), req.Port)
	jsonMsgObj(c, I18nWeb(c, "pages.nodes.install.hostKeyFailed"), key, err)
}
