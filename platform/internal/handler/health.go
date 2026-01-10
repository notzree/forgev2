package handler

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// HealthHandler handles health check endpoints
// TODO: Registry was deleted, update when processor is refactored (see TASKS.md Task 6)
type HealthHandler struct{}

// NewHealthHandler creates a new health handler
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
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
		"status": "ready",
	})
}
