package controller

import (
	"bytes"
	"io"

	"github.com/gin-gonic/gin"
)

// peekBody reads up to limit bytes of the request body for a middleware and leaves the
// whole body, past the limit too, for the handler.
func peekBody(c *gin.Context, limit int64) []byte {
	if c.Request.Body == nil {
		return nil
	}
	head, _ := io.ReadAll(io.LimitReader(c.Request.Body, limit))
	c.Request.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(head), c.Request.Body), c.Request.Body}
	return head
}
