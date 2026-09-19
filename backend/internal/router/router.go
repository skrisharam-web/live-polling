// Package router wires HTTP routes to handlers. Keeping the route table in one
// file makes the entire public surface of the API readable at a glance, including
// which routes are public and which sit behind authentication.
package router

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/skrisharam-web/live-polling/backend/internal/config"
	"github.com/skrisharam-web/live-polling/backend/internal/handlers"
	"github.com/skrisharam-web/live-polling/backend/internal/middleware"
)

// Dependencies are the handlers and collaborators the router needs. They are
// passed in rather than constructed here so tests can wire fakes.
type Dependencies struct {
	Health       *handlers.HealthHandler
	Auth         *handlers.AuthHandler
	Poll         *handlers.PollHandler
	Vote         *handlers.VoteHandler
	WebSocket    *handlers.WebSocketHandler
	UserResolver middleware.UserResolver
	Voter        *middleware.VoterIdentity
	// Limiter is the Redis-backed rate limiter. A nil limiter disables rate
	// limiting rather than failing, which keeps the router usable in tests.
	Limiter middleware.Limiter
}

// Rate limits. Sign-in is the tightest: it is the endpoint worth guessing
// passwords against. Voting is looser because a lecture hall of people voting at
// once shares very few source addresses.
const (
	authRateLimit    = 20
	authRateWindow   = 15 * time.Minute
	voteRateLimit    = 60
	voteRateWindow   = time.Minute
	createRateLimit  = 30
	createRateWindow = time.Hour
)

// New builds the Gin engine with the global middleware chain and the route table.
func New(cfg *config.Config, deps Dependencies) *gin.Engine {
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()
	engine.RedirectTrailingSlash = false

	// Only a named proxy's X-Forwarded-For is believed. With the default (none),
	// c.ClientIP() is the socket's own address, so a client cannot claim to be
	// somebody else and dodge the rate limiter.
	if err := engine.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		slog.Error("invalid TRUSTED_PROXIES, trusting none", "error", err)
		_ = engine.SetTrustedProxies(nil)
	}

	engine.Use(
		middleware.RequestID(),
		middleware.Recovery(),
		middleware.RequestLogger(),
		middleware.CORS(cfg.AllowedOrigins),
		middleware.BodyLimit(cfg.MaxRequestBodyBytes),
	)

	engine.GET("/health", deps.Health.Health)

	api := engine.Group("/api")
	api.Use(middleware.RequireJSON())

	// Public: anyone may create an account or sign in.
	auth := api.Group("/auth")
	{
		authLimit := middleware.RateLimit(deps.Limiter, "auth", authRateLimit, authRateWindow)
		auth.POST("/register", authLimit, deps.Auth.Register)
		auth.POST("/login", authLimit, deps.Auth.Login)
		auth.POST("/logout", deps.Auth.Logout)
		auth.GET("/me", middleware.RequireAuth(deps.UserResolver), deps.Auth.Me)
	}

	requireAuth := middleware.RequireAuth(deps.UserResolver)

	// Public: anyone holding a share link can read the poll, vote in it and see
	// the results. The voter middleware gives these routes an anonymous identity
	// so a vote can be tied to a browser without an account.
	withVoter := deps.Voter.Middleware()
	api.GET("/polls/:id", deps.Poll.Get)
	api.POST("/polls/:id/vote", middleware.RateLimit(deps.Limiter, "vote", voteRateLimit, voteRateWindow), withVoter, deps.Vote.Vote)
	api.GET("/polls/:id/results", withVoter, deps.Vote.Results)

	// Everything that creates or changes a poll requires a session, and the
	// service layer additionally checks that the session owns the poll.
	polls := api.Group("/polls", requireAuth)
	{
		polls.POST("", middleware.RateLimit(deps.Limiter, "create", createRateLimit, createRateWindow), deps.Poll.Create)
		polls.GET("/:id/manage", deps.Poll.Manage)
		polls.PATCH("/:id", deps.Poll.Update)
		polls.DELETE("/:id", deps.Poll.Delete)
		polls.POST("/:id/close", deps.Poll.Close)
	}

	api.GET("/me/polls", requireAuth, deps.Poll.List)

	// The realtime endpoint sits outside /api because it is not a REST resource
	// and because the body-limit and JSON middleware above make no sense for a
	// hijacked connection.
	if deps.WebSocket != nil {
		engine.GET("/ws/polls/:id", deps.WebSocket.Subscribe)
	}

	return engine
}
