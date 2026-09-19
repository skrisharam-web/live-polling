package middleware

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/skrisharam-web/live-polling/backend/internal/apperr"
	"github.com/skrisharam-web/live-polling/backend/internal/response"
)

// Limiter is the counting primitive the rate limiter needs. Redis provides it,
// which is what makes the limit hold across every backend instance rather than
// per-process.
type Limiter interface {
	Allow(ctx context.Context, key string, limit int64, window time.Duration) (allowed bool, retryAfter time.Duration, err error)
}

// RateLimit caps how often one client may call a group of endpoints.
//
// The limiter fails open: if Redis cannot answer, the request is allowed and the
// failure is logged. That is a deliberate trade. Failing closed would turn a
// Redis blip into a total outage of voting and sign-in, and the limiter exists to
// slow down abuse, not to be the thing that decides whether the site works.
//
// The key is the client IP plus a scope, so a limit on sign-in attempts cannot be
// exhausted by someone voting.
func RateLimit(limiter Limiter, scope string, limit int64, window time.Duration) gin.HandlerFunc {
	if limiter == nil {
		// Nothing to count with: let everything through rather than refusing to
		// start. The deployment documentation covers running with Redis.
		return func(c *gin.Context) { c.Next() }
	}

	return func(c *gin.Context) {
		key := scope + ":" + c.ClientIP()

		allowed, retryAfter, err := limiter.Allow(c.Request.Context(), key, limit, window)
		if err != nil {
			slog.Warn("rate limiter unavailable, allowing the request",
				"scope", scope, "error", err, "request_id", c.GetString(ContextRequestID))
			c.Next()
			return
		}

		if !allowed {
			seconds := int(retryAfter.Seconds())
			if seconds < 1 {
				seconds = 1
			}
			c.Header("Retry-After", strconv.Itoa(seconds))
			response.Fail(c, apperr.CodeRateLimited, "Too many requests. Please wait a moment and try again.")
			return
		}

		c.Next()
	}
}
