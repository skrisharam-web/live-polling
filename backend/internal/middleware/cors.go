package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// CORS answers cross-origin requests from the configured frontend origins only.
//
// The API authenticates with a cookie, which makes every browser call a
// credentialed request. The CORS specification forbids pairing
// Access-Control-Allow-Credentials with a wildcard origin, and doing so would in
// any case let any site on the internet drive the API as a logged-in user. So the
// allow-list is exact and the header echoes back only an origin that is on it.
func CORS(allowedOrigins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[strings.TrimRight(strings.TrimSpace(origin), "/")] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := strings.TrimRight(c.GetHeader("Origin"), "/")
		if origin != "" {
			if _, ok := allowed[origin]; ok {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Access-Control-Allow-Credentials", "true")
				c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				c.Header("Access-Control-Allow-Headers", "Content-Type, X-Request-ID")
				c.Header("Access-Control-Max-Age", "600")
			}
			// Responses differ per origin, so caches must key on it.
			c.Header("Vary", "Origin")
		}

		if c.Request.Method == http.MethodOptions {
			// A preflight for a disallowed origin gets no CORS headers, so the
			// browser blocks the real request regardless of this status.
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
