package sub

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// resolveLegacySubID lets a link another panel handed out reach the user it was imported
// as: an exact old path, or a Marzban/PasarGuard token signed with that panel's secret.
func resolveLegacySubID(c *gin.Context) {
	for i, p := range c.Params {
		if p.Key == "subid" {
			if sub, ok := service.ResolveSubAlias(p.Value); ok {
				c.Params[i].Value = sub
			}
		}
	}
	c.Next()
}

// marzbanFormats are the client types Marzban and PasarGuard put after a token.
var marzbanFormats = map[string]string{
	"v2ray": "raw", "outline": "raw", "clash": "clash", "clash-meta": "clash",
	"sing-box": "singbox", "v2ray-json": "json",
}

// legacyTwoPart serves /sub/<token>/<client type> (Marzban, PasarGuard) and
// /sub/<username>/<key> (Marzneshin).
func (a *SUBController) legacyTwoPart(c *gin.Context) {
	key := c.Param("key")
	if kind, ok := marzbanFormats[key]; ok {
		switch {
		case kind == "clash" && a.clashEnabled:
			a.subClashs(c)
		case kind == "singbox" && a.singboxEnabled:
			a.subSingbox(c)
		case kind == "json" && a.jsonEnabled:
			a.subJsons(c)
		default:
			a.subs(c)
		}
		return
	}
	for i, p := range c.Params {
		if p.Key != "subid" {
			continue
		}
		sub, ok := service.ResolveSubAlias(p.Value + "/" + key)
		if !ok {
			c.Status(http.StatusNotFound)
			return
		}
		c.Params[i].Value = sub
	}
	a.subs(c)
}
