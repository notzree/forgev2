package handler

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/forge/platform/internal/agent"
)

// HealthHandler handles health check endpoints
type HealthHandler struct {
	registry *agent.Registry
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(registry *agent.Registry) *HealthHandler {
	return &HealthHandler{registry: registry}
}

// Register registers health check routes
func (h *HealthHandler) Register(e *echo.Echo) {
	e.GET("/healthz", h.Healthz)
	e.GET("/readyz", h.Readyz)
}

// Healthz handles GET /healthz
func (h *HealthHandler) Healthz(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// Readyz handles GET /readyz
func (h *HealthHandler) Readyz(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]interface{}{
		"status":      "ready",
		"agent_count": h.registry.Count(),
	})
}
