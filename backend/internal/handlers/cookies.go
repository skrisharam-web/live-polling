package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/skrisharam-web/live-polling/backend/internal/config"
	"github.com/skrisharam-web/live-polling/backend/internal/middleware"
)

// CookieSettings turns configuration into the flags every cookie this API issues
// must carry. Keeping it in one place means the session cookie and the voter
// cookie cannot drift apart in their security properties.
type CookieSettings struct {
	Secure   bool
	Domain   string
	SameSite http.SameSite
}

// NewCookieSettings derives the cookie policy from configuration.
//
// SameSite is the interesting decision. In production the frontend and the API
// are served from different hosts, so the session cookie has to travel on
// cross-site requests, which requires SameSite=None — and browsers only accept
// None together with Secure. That combination gives up SameSite as a CSRF
// defence, so the API defends instead with an exact CORS origin allow-list and by
// requiring a JSON content type on every state-changing request (see
// middleware.RequireJSON).
//
// In development everything is on localhost, where Lax works and is stricter.
func NewCookieSettings(cfg *config.Config) CookieSettings {
	sameSite := http.SameSiteLaxMode
	if cfg.CookieSecure {
		sameSite = http.SameSiteNoneMode
	}
	return CookieSettings{
		Secure:   cfg.CookieSecure,
		Domain:   cfg.CookieDomain,
		SameSite: sameSite,
	}
}

// set writes a cookie with the configured policy. maxAge is in seconds; a
// negative value deletes the cookie.
func (s CookieSettings) set(c *gin.Context, name, value string, maxAge int) {
	c.SetSameSite(s.SameSite)
	// HttpOnly is always true: nothing in this application needs to read its own
	// cookies from JavaScript, and making them unreadable removes the value of an
	// XSS payload that tries to steal a session.
	c.SetCookie(name, value, maxAge, "/", s.Domain, s.Secure, true)
}

// SetSession issues the session cookie.
func (s CookieSettings) SetSession(c *gin.Context, token string, maxAgeSeconds int) {
	s.set(c, middleware.SessionCookieName, token, maxAgeSeconds)
}

// ClearSession removes the session cookie. The attributes must match the ones it
// was set with, or the browser keeps the original cookie.
func (s CookieSettings) ClearSession(c *gin.Context) {
	s.set(c, middleware.SessionCookieName, "", -1)
}
