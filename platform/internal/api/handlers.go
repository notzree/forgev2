package api

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/forge/platform/internal/agent"
)

// Handlers contains all HTTP handlers
type Handlers struct {
	manager  *agent.Manager
	registry *agent.Registry
}

// NewHandlers creates a new handlers instance
func NewHandlers(manager *agent.Manager, registry *agent.Registry) *Handlers {
	return &Handlers{
		manager:  manager,
		registry: registry,
	}
}

// CreateAgentRequest is the request body for creating an agent
type CreateAgentRequest struct {
	OwnerID string `json:"owner_id"`
	Address string `json:"address"` // For now, we need the agent address; later K8s will provide this
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

// CreateAgent handles POST /api/v1/agents
func (h *Handlers) CreateAgent(c echo.Context) error {
	var req CreateAgentRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	if req.OwnerID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "owner_id is required")
	}
	if req.Address == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "address is required")
	}

	info, err := h.manager.CreateAgent(c.Request().Context(), req.OwnerID, req.Address)
	if err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, err.Error())
	}

	return c.JSON(http.StatusCreated, agentInfoToResponse(info))
}

// ListAgents handles GET /api/v1/agents
func (h *Handlers) ListAgents(c echo.Context) error {
	ownerID := c.QueryParam("owner_id")

	var agents []*agent.Info
	if ownerID != "" {
		agents = h.registry.GetByOwner(ownerID)
	} else {
		agents = h.registry.List()
	}

	responses := make([]AgentResponse, len(agents))
	for i, info := range agents {
		responses[i] = agentInfoToResponse(info)
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"agents": responses,
		"total":  len(responses),
	})
}

// GetAgent handles GET /api/v1/agents/:id
func (h *Handlers) GetAgent(c echo.Context) error {
	agentID := c.Param("id")

	info, exists := h.registry.Get(agentID)
	if !exists {
		return echo.NewHTTPError(http.StatusNotFound, "agent not found")
	}

	// Optionally refresh status from agent
	if c.QueryParam("refresh") == "true" && info.State == agent.StateRunning {
		status, err := h.manager.GetStatus(c.Request().Context(), agentID)
		if err == nil {
			info.SessionID = status.SessionId
			info.LatestSeq = status.LatestSeq
		}
	}

	return c.JSON(http.StatusOK, agentInfoToResponse(info))
}

// DeleteAgent handles DELETE /api/v1/agents/:id
func (h *Handlers) DeleteAgent(c echo.Context) error {
	agentID := c.Param("id")
	graceful := c.QueryParam("graceful") != "false" // default to graceful

	if err := h.manager.DeleteAgent(c.Request().Context(), agentID, graceful); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	}

	return c.NoContent(http.StatusNoContent)
}

// GetMessages handles GET /api/v1/agents/:id/messages
func (h *Handlers) GetMessages(c echo.Context) error {
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
	info, exists := h.registry.Get(agentID)
	if !exists {
		return echo.NewHTTPError(http.StatusNotFound, "agent not found")
	}

	// If agent is running, get messages from it
	if info.State == agent.StateRunning {
		resp, err := h.manager.CatchUp(c.Request().Context(), agentID, fromSeq, limit)
		if err != nil {
			return echo.NewHTTPError(http.StatusServiceUnavailable, err.Error())
		}

		return c.JSON(http.StatusOK, map[string]interface{}{
			"messages":   resp.Messages,
			"latest_seq": resp.LatestSeq,
			"has_more":   resp.HasMore,
			"source":     "agent",
		})
	}

	// TODO: Fall back to Turso for offline agents
	return c.JSON(http.StatusOK, map[string]interface{}{
		"messages":   []interface{}{},
		"latest_seq": info.LatestSeq,
		"has_more":   false,
		"source":     "cache",
	})
}

// Healthz handles GET /healthz
func (h *Handlers) Healthz(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// Readyz handles GET /readyz
func (h *Handlers) Readyz(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]interface{}{
		"status":      "ready",
		"agent_count": h.registry.Count(),
	})
}
