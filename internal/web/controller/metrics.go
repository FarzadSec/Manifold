package controller

import (
	"bytes"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/metrics"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// NewMetricsController serves the dashboard summary and the Prometheus scrape; server is
// the instance whose ticker keeps the status warm.
func NewMetricsController(api *gin.RouterGroup, server *service.ServerService) {
	api.GET("/metrics", func(c *gin.Context) {
		fams, err := service.Metrics(server.CurrentStatus(), service.MetricsOptions{PerUser: c.Query("users") == "1"}, time.Now())
		if err != nil {
			c.String(http.StatusInternalServerError, "# %v\n", err)
			return
		}
		var b bytes.Buffer
		_ = metrics.Write(&b, fams...)
		c.Data(http.StatusOK, metrics.ContentType, b.Bytes())
	})
	api.GET("/server/summary", func(c *gin.Context) {
		s, err := service.Summary(time.Now())
		jsonObj(c, s, err)
	})
}
