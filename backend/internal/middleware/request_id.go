// Package middleware holds the cross-cutting HTTP concerns: request identity,
// panic recovery, CORS, body limits, authentication and rate limiting. Each file
// does exactly one of those jobs.
package middleware

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/gin-gonic/gin"
)

// ContextRequestID is the gin context key holding the current request's ID.
const ContextRequestID = "request_id"

// headerRequestID is echoed back so a client (or a proxy) can correlate a failed
// request with a server log line.
const headerRequestID = "X-Request-ID"

// RequestID attaches an ID to every request. An upstream proxy may already have
// set one, in which case it is reused so a single ID spans the whole hop chain.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(headerRequestID)
		if id == "" || len(id) > 64 {
			id = newRequestID()
		}
		c.Set(ContextRequestID, id)
		c.Header(headerRequestID, id)
		c.Next()
	}
}

func newRequestID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand failing is not recoverable here and not worth killing the
		// request over: an empty ID simply means this line cannot be correlated.
		return ""
	}
	return hex.EncodeToString(buf)
}
