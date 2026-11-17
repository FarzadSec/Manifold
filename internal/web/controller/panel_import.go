package controller

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/importer"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

const importFileLimit = 512 << 20

// PanelImportController moves users in from another panel.
type PanelImportController struct {
	importService service.PanelImportService
}

// PanelImportRemote names a panel to read over its API.
type PanelImportRemote struct {
	Kind     string `json:"kind" example:"marzban"`
	URL      string `json:"url" example:"https://old.example.com:8000"`
	Username string `json:"username" example:"admin"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
	Secret   string `json:"secret,omitempty"`
}

// NewPanelImportController registers the import routes on g.
func NewPanelImportController(g *gin.RouterGroup) *PanelImportController {
	a := &PanelImportController{}
	g.POST("/file", a.file)
	g.POST("/remote", a.remote)
	g.POST("/apply", a.apply)
	return a
}

func (a *PanelImportController) file(c *gin.Context) {
	fail := I18nWeb(c, "pages.import.readFailed")
	fh, err := c.FormFile("file")
	if err != nil {
		jsonMsg(c, fail, err)
		return
	}
	if fh.Size > importFileLimit {
		jsonMsg(c, fail, common.NewError("the file is larger than 512 MiB"))
		return
	}
	src, err := fh.Open()
	if err != nil {
		jsonMsg(c, fail, err)
		return
	}
	defer src.Close()
	tmp, err := os.CreateTemp("", "manifold-import-*")
	if err != nil {
		jsonMsg(c, fail, err)
		return
	}
	defer os.Remove(tmp.Name())
	head := make([]byte, 0, 512)
	buf := make([]byte, 32<<10)
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			if len(head) < cap(head) {
				head = append(head, buf[:min(n, cap(head)-len(head))]...)
			}
			if _, werr := tmp.Write(buf[:n]); werr != nil {
				tmp.Close()
				jsonMsg(c, fail, werr)
				return
			}
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			tmp.Close()
			jsonMsg(c, fail, rerr)
			return
		}
	}
	tmp.Close()
	var raw []byte
	if len(head) > 0 && (head[0] == '{' || head[0] == '[') {
		raw, _ = os.ReadFile(tmp.Name())
	} else {
		raw = head
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
	defer cancel()
	res, err := service.ReadPanelFile(ctx, tmp.Name(), raw)
	if err != nil {
		jsonMsg(c, fail, err)
		return
	}
	preview, err := a.importService.Preview(res)
	jsonMsgObj(c, fail, preview, err)
}

func (a *PanelImportController) remote(c *gin.Context) {
	fail := I18nWeb(c, "pages.import.readFailed")
	var in PanelImportRemote
	if err := c.ShouldBindJSON(&in); err != nil {
		jsonMsg(c, fail, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()
	r := importer.Remote{URL: in.URL, Username: in.Username, Password: in.Password, Token: in.Token}
	var res *importer.Result
	var err error
	switch in.Kind {
	case "marzban", "pasarguard":
		res, err = r.ReadMarzbanAPI(ctx, in.Secret)
	case "remnawave":
		res, err = r.ReadRemnawave(ctx)
	default:
		err = common.NewError("a panel read over its API is marzban, pasarguard or remnawave")
	}
	if err != nil {
		jsonMsg(c, fail, err)
		return
	}
	preview, err := a.importService.Preview(res)
	jsonMsgObj(c, fail, preview, err)
}

func (a *PanelImportController) apply(c *gin.Context) {
	fail := I18nWeb(c, "pages.import.applyFailed")
	var in service.ImportApplyRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		jsonMsg(c, fail, err)
		return
	}
	out, err := a.importService.Apply(in)
	jsonMsgObj(c, fail, out, err)
}
