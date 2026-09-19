// Package handlers contains the HTTP layer: bind the request, call one service,
// map the result onto the wire format. No business rules and no database access
// live here.
package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/skrisharam-web/live-polling/backend/internal/response"
)

// pinger is the small slice of MongoDB and Redis that the health check needs.
type pinger interface {
	Ping(ctx context.Context) error
}

// HealthHandler reports whether the process and its dependencies are usable.
type HealthHandler struct {
	mongo pinger
	redis pinger
}

func NewHealthHandler(mongo, redis pinger) *HealthHandler {
	return &HealthHandler{mongo: mongo, redis: redis}
}

type dependencyStatus struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type healthResponse struct {
	Status    string                      `json:"status"`
	Timestamp time.Time                   `json:"timestamp"`
	Services  map[string]dependencyStatus `json:"services"`
}

// Health answers GET /health. It returns 503 when a dependency is down so that a
// platform health check can pull the instance out of rotation, and it names which
// dependency failed without echoing the driver's error text.
func (h *HealthHandler) Health(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	services := map[string]dependencyStatus{
		"mongodb": check(ctx, h.mongo),
		"redis":   check(ctx, h.redis),
	}

	status := "ok"
	httpStatus := http.StatusOK
	for _, s := range services {
		if s.Status != "ok" {
			status = "degraded"
			httpStatus = http.StatusServiceUnavailable
		}
	}

	c.JSON(httpStatus, response.Envelope{
		Success: httpStatus == http.StatusOK,
		Data: healthResponse{
			Status:    status,
			Timestamp: time.Now().UTC(),
			Services:  services,
		},
	})
}

func check(ctx context.Context, p pinger) dependencyStatus {
	if p == nil {
		return dependencyStatus{Status: "unconfigured"}
	}
	if err := p.Ping(ctx); err != nil {
		return dependencyStatus{Status: "unreachable", Error: "dependency did not respond"}
	}
	return dependencyStatus{Status: "ok"}
}
