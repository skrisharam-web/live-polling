package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"github.com/skrisharam-web/live-polling/backend/internal/apperr"
	"github.com/skrisharam-web/live-polling/backend/internal/response"
)

// Recovery turns a panic into a logged 500 with the standard error envelope. The
// stack trace goes to the log, never to the client.
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("panic recovered",
					"panic", recovered,
					"path", c.Request.URL.Path,
					"method", c.Request.Method,
					"request_id", c.GetString(ContextRequestID),
					"stack", string(debug.Stack()),
				)
				if c.Writer.Written() {
					// The handler already started a response; all we can do is stop.
					c.Abort()
					return
				}
				c.Header("Connection", "close")
				c.AbortWithStatusJSON(http.StatusInternalServerError, response.Envelope{
					Success: false,
					Error: &response.APIError{
						Code:    string(apperr.CodeInternal),
						Message: "Something went wrong. Please try again.",
					},
				})
			}
		}()
		c.Next()
	}
}
