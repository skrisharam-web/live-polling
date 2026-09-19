// Package router wires HTTP routes to handlers. Keeping the route table in one
// file makes the entire public surface of the API readable at a glance.
package router

import (
	"github.com/gin-gonic/gin"

	"github.com/skrisharam-web/live-polling/backend/internal/config"
	"github.com/skrisharam-web/live-polling/backend/internal/handlers"
	"github.com/skrisharam-web/live-polling/backend/internal/middleware"
)

// Dependencies are the handlers the router needs. They are passed in rather than
// constructed here so tests can wire fakes.
type Dependencies struct {
	Health *handlers.HealthHandler
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

	return engine
}
