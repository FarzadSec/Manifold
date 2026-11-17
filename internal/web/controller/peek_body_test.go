package controller

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPeekBodyLeavesTheHandlerAllOfIt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := bytes.Repeat([]byte("0123456789"), 300_000)
	var peeked, handled int
	r := gin.New()
	r.POST("/x", func(c *gin.Context) {
		peeked = len(peekBody(c, 1<<20))
		c.Next()
	}, func(c *gin.Context) {
		all, _ := io.ReadAll(c.Request.Body)
		handled = len(all)
		if !bytes.Equal(all, body) {
			handled = -1
		}
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(body)))
	if peeked != 1<<20 || handled != len(body) {
		t.Fatalf("middleware saw %d bytes and the handler %d, want %d and %d", peeked, handled, 1<<20, len(body))
	}
}
