// Package router wires HTTP routes to handlers. Keeping the route table in one
// file makes the entire public surface of the API readable at a glance, including
// which routes are public and which sit behind authentication.
package router

import (
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
	UserResolver middleware.UserResolver
}

// New builds the Gin engine with the global middleware chain and the route table.
func New(cfg *config.Config, deps Dependencies) *gin.Engine {
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()
	engine.RedirectTrailingSlash = false

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
		auth.POST("/register", deps.Auth.Register)
		auth.POST("/login", deps.Auth.Login)
		auth.POST("/logout", deps.Auth.Logout)
		auth.GET("/me", middleware.RequireAuth(deps.UserResolver), deps.Auth.Me)
	}

	requireAuth := middleware.RequireAuth(deps.UserResolver)

	// Public: anyone holding a share link can read the poll.
	api.GET("/polls/:id", deps.Poll.Get)

	// Everything that creates or changes a poll requires a session, and the
	// service layer additionally checks that the session owns the poll.
	polls := api.Group("/polls", requireAuth)
	{
		polls.POST("", deps.Poll.Create)
		polls.GET("/:id/manage", deps.Poll.Manage)
		polls.PATCH("/:id", deps.Poll.Update)
		polls.DELETE("/:id", deps.Poll.Delete)
		polls.POST("/:id/close", deps.Poll.Close)
	}

	api.GET("/me/polls", requireAuth, deps.Poll.List)

	return engine
}
