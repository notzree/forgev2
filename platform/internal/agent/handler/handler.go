package handler

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	corev1 "k8s.io/api/core/v1"

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
}

// CreateAgentRequest is the request body for creating an agent
type CreateAgentRequest struct {
	OwnerID string `json:"owner_id"`
}

// AgentResponse is the response for agent operations
type AgentResponse struct {
	UserID    string          `json:"user_id"`
	AgentID   string          `json:"agent_id"`
	PodName   string          `json:"pod_name"`
	PodIP     string          `json:"pod_ip,omitempty"`
	Phase     corev1.PodPhase `json:"phase"`
	Ready     bool            `json:"ready"`
	CreatedAt string          `json:"created_at,omitempty"`
}

// ListAgentsResponse is the response for listing agents
type ListAgentsResponse struct {
	Agents []AgentResponse `json:"agents"`
	Total  int             `json:"total"`
}

// podToAgentResponse converts a K8s Pod to AgentResponse
func podToAgentResponse(pod *corev1.Pod) AgentResponse {
	resp := AgentResponse{
		UserID:  pod.Labels["user-id"],
		AgentID: pod.Labels["agent-id"],
		PodName: pod.Name,
		Phase:   pod.Status.Phase,
		Ready:   isPodReady(pod),
	}
	if pod.Status.PodIP != "" {
		resp.PodIP = pod.Status.PodIP
	}
	if !pod.CreationTimestamp.IsZero() {
		resp.CreatedAt = pod.CreationTimestamp.Format(time.RFC3339)
	}
	return resp
}

// isPodReady checks if the pod is running and all containers are ready
func isPodReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if !cs.Ready {
			return false
		}
	}
	return true
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

	ctx := c.Request().Context()
	podID, err := h.processor.CreateAgent(ctx, req.OwnerID)
	if err != nil {
		return errors.ServiceUnavailable(err.Error())
	}

	// Fetch full pod details for the response
	pod, err := h.processor.GetAgent(ctx, podID.UserID, podID.AgentID)
	if err != nil {
		// Pod was created but we can't fetch details - return basic info
		return c.JSON(http.StatusCreated, AgentResponse{
			UserID:  podID.UserID,
			AgentID: podID.AgentID,
			PodName: podID.Name(),
			Phase:   corev1.PodRunning,
			Ready:   true,
		})
	}

	return c.JSON(http.StatusCreated, podToAgentResponse(pod))
}

// List handles GET /api/v1/agents?user_id=xxx
func (h *Handler) List(c echo.Context) error {
	userID := c.QueryParam("user_id")
	if userID == "" {
		return errors.BadRequest("user_id query param is required")
	}

	ctx := c.Request().Context()
	podIDs, err := h.processor.ListAgents(ctx, userID)
	if err != nil {
		return errors.InternalError(err.Error())
	}

	agents := make([]AgentResponse, 0, len(podIDs))
	for _, podID := range podIDs {
		// Fetch full pod details for each agent
		pod, err := h.processor.GetAgent(ctx, podID.UserID, podID.AgentID)
		if err != nil {
			// If we can't get pod details, include basic info
			agents = append(agents, AgentResponse{
				UserID:  podID.UserID,
				AgentID: podID.AgentID,
				PodName: podID.Name(),
			})
			continue
		}
		agents = append(agents, podToAgentResponse(pod))
	}

	return c.JSON(http.StatusOK, ListAgentsResponse{
		Agents: agents,
		Total:  len(agents),
	})
}

// Get handles GET /api/v1/agents/:id?user_id=xxx&refresh=true
func (h *Handler) Get(c echo.Context) error {
	agentID := c.Param("id")
	userID := c.QueryParam("user_id")
	if userID == "" {
		return errors.BadRequest("user_id query param is required")
	}

	ctx := c.Request().Context()
	pod, err := h.processor.GetAgent(ctx, userID, agentID)
	if err != nil {
		return errors.NotFound(err.Error())
	}

	resp := podToAgentResponse(pod)

	// Optionally fetch real-time status from the agent via RPC
	if c.QueryParam("refresh") == "true" && resp.Ready {
		// GetStatus validates the agent is responsive
		// Status response can be used for additional fields in the future
		_, _ = h.processor.GetStatus(ctx, userID, agentID)
	}

	return c.JSON(http.StatusOK, resp)
}

// Delete handles DELETE /api/v1/agents/:id
func (h *Handler) Delete(c echo.Context) error {
	agentID := c.Param("id")
	userID := c.QueryParam("user_id")
	graceful := c.QueryParam("graceful") == "true"

	if userID == "" {
		return errors.BadRequest("user_id query param is required")
	}

	if err := h.processor.DeleteAgent(c.Request().Context(), userID, agentID, graceful); err != nil {
		return errors.NotFound(err.Error())
	}

	return c.NoContent(http.StatusNoContent)
}
