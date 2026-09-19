package handlers

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/skrisharam-web/live-polling/backend/internal/apperr"
	"github.com/skrisharam-web/live-polling/backend/internal/middleware"
	"github.com/skrisharam-web/live-polling/backend/internal/response"
	"github.com/skrisharam-web/live-polling/backend/internal/services"
)

// authService is the behaviour the auth handler needs from the service layer.
type authService interface {
	Register(ctx context.Context, input services.RegisterInput) (*services.Session, error)
	Login(ctx context.Context, input services.LoginInput) (*services.Session, error)
}

// AuthHandler exposes registration and session management over HTTP.
type AuthHandler struct {
	auth    authService
	cookies middleware.CookieSettings
}

func NewAuthHandler(auth authService, cookies middleware.CookieSettings) *AuthHandler {
	return &AuthHandler{auth: auth, cookies: cookies}
}

type registerRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// authResponse is what a successful register or login returns. The token is not
// included: it lives in the HTTP-only cookie, where page JavaScript cannot reach
// it. Returning it in the body as well would undo that protection.
type authResponse struct {
	User UserResponse `json:"user"`
}

// Register handles POST /api/auth/register.
func (h *AuthHandler) Register(c *gin.Context) {
	var req registerRequest
	if !bindJSON(c, &req) {
		return
	}

	session, err := h.auth.Register(c.Request.Context(), services.RegisterInput{
		Name:     req.Name,
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		response.Error(c, err)
		return
	}

	h.startSession(c, session)
	response.Created(c, authResponse{User: newUserResponse(session.User)})
}

// Login handles POST /api/auth/login.
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if !bindJSON(c, &req) {
		return
	}

	session, err := h.auth.Login(c.Request.Context(), services.LoginInput{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		response.Error(c, err)
		return
	}

	h.startSession(c, session)
	response.OK(c, authResponse{User: newUserResponse(session.User)})
}

// Logout handles POST /api/auth/logout. It always succeeds: asking to be signed
// out when you already are is not an error worth surfacing.
func (h *AuthHandler) Logout(c *gin.Context) {
	h.cookies.ClearSession(c)
	response.NoContent(c)
}

// Me handles GET /api/auth/me and backs the frontend's "am I signed in?" check.
func (h *AuthHandler) Me(c *gin.Context) {
	user, ok := middleware.CurrentUser(c)
	if !ok {
		response.Fail(c, apperr.CodeUnauthorized, "Please sign in to continue.")
		return
	}
	response.OK(c, authResponse{User: newUserResponse(user)})
}

// startSession puts the signed token into the session cookie, with the cookie's
// lifetime matched to the token's so the browser stops sending a token that the
// server would reject anyway.
func (h *AuthHandler) startSession(c *gin.Context, session *services.Session) {
	maxAge := int(time.Until(session.ExpiresAt).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	h.cookies.SetSession(c, session.Token, maxAge)
}
