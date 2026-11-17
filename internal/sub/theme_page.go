package sub

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/subtheme"
	"github.com/mhsanaei/3x-ui/v3/internal/util/jalali"
)

// activeTheme is the theme the page is drawn with: the custom folder when one is set,
// else the theme picked in the settings; nil for the panel's own page.
func (a *SUBController) activeTheme() (fs.FS, *template.Template) {
	if dir, _ := a.settingService.GetSubThemeDir(); dir != "" {
		tmpl, err := a.loadSubTemplate(dir)
		switch {
		case err != nil:
			logger.Error("sub: custom template parse failed, using default page:", err)
		case tmpl == nil:
			logger.Warning("sub: subThemeDir set but no usable template found, using default page:", dir)
		default:
			return os.DirFS(dir), tmpl
		}
		return nil, nil
	}
	id, _ := a.settingService.GetSubTheme()
	if id == "" {
		return nil, nil
	}
	fsys, err := subtheme.Files(id)
	if err != nil {
		logger.Warning("sub: theme", id, "is not available, using default page:", err)
		return nil, nil
	}
	tmpl, err := a.themes.Load(fsys, id)
	if err != nil {
		logger.Error("sub: theme", id, "failed to parse, using default page:", err)
		return nil, nil
	}
	return fsys, tmpl
}

// serveThemePage draws the page with the active theme into a buffer first, so a theme
// that fails half way never sends a broken page; false leaves the panel's page to answer.
func (a *SUBController) serveThemePage(c *gin.Context, data map[string]any) bool {
	_, tmpl := a.activeTheme()
	if tmpl == nil {
		return false
	}
	view := make(map[string]any, len(data)+3)
	for k, v := range data {
		view[k] = v
	}
	lang := subtheme.Lang(c.Query("lang"), c.GetHeader("Accept-Language"))
	view["lang"], view["dir"] = lang, subtheme.Dir(lang)
	view["assetBase"] = url.PathEscape(c.Param("subid")) + "/theme"
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, view); err != nil {
		logger.Error("sub: theme page failed to render, using default page:", err)
		return false
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", buf.Bytes())
	return true
}

// themeAsset serves the files beside the active theme's page: styles, scripts, images, fonts.
func (a *SUBController) themeAsset(c *gin.Context) {
	fsys, _ := a.activeTheme()
	if fsys == nil {
		c.Status(http.StatusNotFound)
		return
	}
	data, ctype, err := subtheme.Asset(fsys, strings.TrimPrefix(c.Param("file"), "/"))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, ctype, data)
}

// pageStatus is one word for the subscription's state, the reason first: a used-up or
// expired subscription is also switched off, and the reason is what its user needs.
func pageStatus(page PageData, now time.Time) string {
	switch {
	case page.Expire > 0 && page.Expire <= now.Unix():
		return "expired"
	case page.TotalByte > 0 && page.UploadByte+page.DownloadByte >= page.TotalByte:
		return "limited"
	case !page.Enabled:
		return "disabled"
	}
	return "active"
}

// expiryText writes the expiry date in the panel's calendar; "" for none.
func expiryText(expire int64, calendar string, loc *time.Location) string {
	if expire <= 0 {
		return ""
	}
	t := time.Unix(expire, 0).In(loc)
	if calendar == "persian" {
		y, m, d := jalali.Of(t)
		return fmt.Sprintf("%d/%02d/%02d", y, m, d)
	}
	return t.Format("2006-01-02")
}

// daysLeft rounds the time to expiry up to whole days; 0 once expired or with no expiry.
func daysLeft(expire int64, now time.Time) int {
	if expire <= 0 {
		return 0
	}
	left := expire - now.Unix()
	if left <= 0 {
		return 0
	}
	return int((left + 86399) / 86400)
}

// pageConfig is one connection on the page: its name as apps show it, protocol and link.
type pageConfig struct {
	Name     string
	Protocol string
	Link     string
}

func pageConfigs(links []string) []pageConfig {
	out := make([]pageConfig, 0, len(links))
	for _, link := range links {
		proto, rest, _ := strings.Cut(link, "://")
		name := ""
		if strings.EqualFold(proto, "vmess") {
			name = vmessName(rest)
		} else if i := strings.LastIndex(link, "#"); i >= 0 {
			if decoded, err := url.PathUnescape(link[i+1:]); err == nil {
				name = decoded
			} else {
				name = link[i+1:]
			}
		}
		if name == "" {
			name = proto
		}
		out = append(out, pageConfig{Name: name, Protocol: strings.ToLower(proto), Link: link})
	}
	return out
}

func vmessName(payload string) string {
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(payload)
	}
	if err != nil {
		return ""
	}
	var v struct {
		Ps string `json:"ps"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return v.Ps
}
