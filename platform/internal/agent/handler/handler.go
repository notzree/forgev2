package handler

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/forge/platform/internal/agent/processor"
	"github.com/forge/platform/internal/errors"
)

// Handler handles agent HTTP endpoints
// TODO: This handler needs to be updated per TASKS.md Task 5
type Handler struct {
	processor *processor.Processor
}

// NewHandler creates a new agent handler
func NewHandler(processor *processor.Processor) *Handler {
	return &Handler{
		processor: processor,
	}
}

// Register registers agent routes with Echo
func (h *Handler) Register(e *echo.Echo) {
	g := e.Group("/api/v1/agents")
	g.POST("", h.Create)
	g.GET("", h.List)
	g.GET("/:id", h.Get)
	g.DELETE("/:id", h.Delete)
}

// CreateAgentRequest is the request body for creating an agent
type CreateAgentRequest struct {
	OwnerID string `json:"owner_id"`
}

// AgentResponse is the response for agent operations
type AgentResponse struct {
	UserID  string `json:"user_id"`
	AgentID string `json:"agent_id"`
	PodName string `json:"pod_name"`
}

// Create handles POST /api/v1/agents
func (h *Handler) Create(c echo.Context) error {
	var req CreateAgentRequest
	if err := c.Bind(&req); err != nil {
		return errors.BadRequest("invalid request body")
	}

	if req.OwnerID == "" {
		return errors.BadRequest("owner_id is required")
	}

	podID, err := h.processor.CreateAgent(c.Request().Context(), req.OwnerID)
	if err != nil {
		return errors.ServiceUnavailable(err.Error())
	}

	return c.JSON(http.StatusCreated, AgentResponse{
		UserID:  podID.UserID,
		AgentID: podID.AgentID,
		PodName: podID.Name(),
	})
}

// List handles GET /api/v1/agents
func (h *Handler) List(c echo.Context) error {
	// TODO: Implement ListAgents in processor (TASKS.md Task 4)
	return c.JSON(http.StatusOK, map[string]interface{}{
		"agents": []interface{}{},
		"total":  0,
	})
}

// Get handles GET /api/v1/agents/:id
func (h *Handler) Get(c echo.Context) error {
	// TODO: Implement GetAgent in processor (TASKS.md Task 4)
	return errors.NotFound("not implemented")
}

// Delete handles DELETE /api/v1/agents/:id
func (h *Handler) Delete(c echo.Context) error {
	agentID := c.Param("id")
	userID := c.QueryParam("user_id")

	if userID == "" {
		return errors.BadRequest("user_id query param is required")
	}

	if err := h.processor.DeleteAgent(c.Request().Context(), userID, agentID); err != nil {
		return errors.NotFound(err.Error())
	}

	return c.NoContent(http.StatusNoContent)
}
