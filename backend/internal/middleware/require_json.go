package middleware

import (
	"mime"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/skrisharam-web/live-polling/backend/internal/apperr"
	"github.com/skrisharam-web/live-polling/backend/internal/response"
)

// RequireJSON insists that state-changing requests declare a JSON body.
//
// This is a deliberate CSRF defence, not a formality. Session cookies must be
// sent cross-site in production (the frontend and API are on different hosts),
// which means a malicious page could otherwise auto-submit a form to this API and
// the browser would attach the user's cookie. A browser can only send a form as
// one of three content types, none of which is application/json; asking for JSON
// forces the request into the "non-simple" category, which triggers a CORS
// preflight that the origin allow-list then refuses.
func RequireJSON() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			c.Next()
			return
		}

		// A DELETE with no body is legitimate and carries no content type.
		if c.Request.ContentLength == 0 && c.Request.Method == http.MethodDelete {
			c.Next()
			return
		}

		contentType := c.GetHeader("Content-Type")
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || !strings.EqualFold(mediaType, "application/json") {
			response.Fail(c, apperr.CodeValidation, "This endpoint expects a JSON request body.")
			return
		}
		c.Next()
	}
}
