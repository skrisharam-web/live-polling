package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// BodyLimit caps how much of a request body the server will read. Without it, a
// single client could stream an unbounded JSON document into the decoder and
// exhaust memory. The limit is enforced by the reader itself, so oversized
// payloads are cut off before any parsing happens.
func BodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}
