package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/skrisharam-web/live-polling/backend/internal/apperr"
	"github.com/skrisharam-web/live-polling/backend/internal/models"
	"github.com/skrisharam-web/live-polling/backend/internal/response"
)

// SessionCookieName is the cookie carrying the signed session token. It is
// HTTP-only, so no JavaScript on the page — including anything injected through
// an XSS hole — can read it.
const SessionCookieName = "lp_session"

// contextAuthUser is the gin context key holding the authenticated user.
const contextAuthUser = "auth_user"

// UserResolver turns a session token into a user. The auth service satisfies it;
// stating it as an interface here keeps the middleware testable and stops the
// middleware package depending on the service package's internals.
type UserResolver interface {
	UserFromToken(ctx context.Context, token string) (*models.User, error)
}

// RequireAuth rejects a request that has no valid session.
//
// The user is resolved from the cookie on every request rather than trusted from
// the request body. That is the whole point: an "ownerId" field in a payload is a
// client's claim about who it is, while the cookie is something the server itself
// signed.
func RequireAuth(resolver UserResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(SessionCookieName)
		if err != nil || token == "" {
			response.Fail(c, apperr.CodeUnauthorized, "Please sign in to continue.")
			return
		}

		user, err := resolver.UserFromToken(c.Request.Context(), token)
		if err != nil {
			response.Error(c, err)
			return
		}

		c.Set(contextAuthUser, user)
		c.Next()
	}
}

// CurrentUser returns the authenticated user for a request handled behind
// RequireAuth.
func CurrentUser(c *gin.Context) (*models.User, bool) {
	value, exists := c.Get(contextAuthUser)
	if !exists {
		return nil, false
	}
	user, ok := value.(*models.User)
	return user, ok
}

// CurrentUserID returns the authenticated user's ID.
func CurrentUserID(c *gin.Context) (bson.ObjectID, bool) {
	user, ok := CurrentUser(c)
	if !ok {
		return bson.NilObjectID, false
	}
	return user.ID, true
}
