package handler

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/forge/platform/internal/agent"
	"github.com/forge/platform/internal/agent/processor"
	"github.com/forge/platform/internal/errors"
)

// Handler handles agent HTTP endpoints
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
	g.GET("/:id/messages", h.GetMessages)
}

// CreateAgentRequest is the request body for creating an agent
type CreateAgentRequest struct {
	OwnerID string `json:"owner_id"`
	Address string `json:"address"`
}

// AgentResponse is the response for agent operations
type AgentResponse struct {
	ID             string `json:"id"`
	OwnerID        string `json:"owner_id"`
	State          string `json:"state"`
	Address        string `json:"address"`
	SessionID      string `json:"session_id,omitempty"`
	LatestSeq      int64  `json:"latest_seq"`
	CreatedAt      string `json:"created_at"`
	LastActivityAt string `json:"last_activity_at"`
}

func agentInfoToResponse(info *agent.Info) AgentResponse {
	return AgentResponse{
		ID:             info.ID,
		OwnerID:        info.OwnerID,
		State:          string(info.State),
		Address:        info.Address,
		SessionID:      info.SessionID,
		LatestSeq:      info.LatestSeq,
		CreatedAt:      info.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		LastActivityAt: info.LastActivityAt.Format("2006-01-02T15:04:05Z07:00"),
	}
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
	if req.Address == "" {
		return errors.BadRequest("address is required")
	}

	info, err := h.processor.CreateAgent(c.Request().Context(), req.OwnerID, req.Address)
	if err != nil {
		return errors.ServiceUnavailable(err.Error())
	}

	return c.JSON(http.StatusCreated, agentInfoToResponse(info))
}

// List handles GET /api/v1/agents
func (h *Handler) List(c echo.Context) error {
	ownerID := c.QueryParam("owner_id")

	agents := h.processor.ListAgents(ownerID)

	responses := make([]AgentResponse, len(agents))
	for i, info := range agents {
		responses[i] = agentInfoToResponse(info)
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"agents": responses,
		"total":  len(responses),
	})
}

// Get handles GET /api/v1/agents/:id
func (h *Handler) Get(c echo.Context) error {
	agentID := c.Param("id")

	info, exists := h.processor.GetAgent(agentID)
	if !exists {
		return errors.NotFound("agent not found")
	}

	// Optionally refresh status from agent
	if c.QueryParam("refresh") == "true" && info.State == agent.StateRunning {
		status, err := h.processor.GetStatus(c.Request().Context(), agentID)
		if err == nil {
			info.SessionID = status.SessionId
			info.LatestSeq = status.LatestSeq
		}
	}

	return c.JSON(http.StatusOK, agentInfoToResponse(info))
}

// Delete handles DELETE /api/v1/agents/:id
func (h *Handler) Delete(c echo.Context) error {
	agentID := c.Param("id")
	graceful := c.QueryParam("graceful") != "false" // default to graceful

	if err := h.processor.DeleteAgent(c.Request().Context(), agentID, graceful); err != nil {
		return errors.NotFound(err.Error())
	}

	return c.NoContent(http.StatusNoContent)
}

// GetMessages handles GET /api/v1/agents/:id/messages
func (h *Handler) GetMessages(c echo.Context) error {
	agentID := c.Param("id")

	fromSeq := int64(0)
	if s := c.QueryParam("from_seq"); s != "" {
		if parsed, err := strconv.ParseInt(s, 10, 64); err == nil {
			fromSeq = parsed
		}
	}

	limit := int32(100)
	if l := c.QueryParam("limit"); l != "" {
		if parsed, err := strconv.ParseInt(l, 10, 32); err == nil {
			limit = int32(parsed)
		}
	}

	// Check if agent exists
	info, exists := h.processor.GetAgent(agentID)
	if !exists {
		return errors.NotFound("agent not found")
	}

	// TODO: Implement CatchUp RPC in proto and agent to fetch messages
	// For now, return cached info
	_ = fromSeq
	_ = limit
	return c.JSON(http.StatusOK, map[string]interface{}{
		"messages":   []interface{}{},
		"latest_seq": info.LatestSeq,
		"has_more":   false,
		"source":     "cache",
	})
}
