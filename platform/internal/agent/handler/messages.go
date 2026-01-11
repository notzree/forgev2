package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/forge/platform/internal/errors"
	"github.com/forge/platform/internal/webhook"
)

// SendMessageRequest is the request body for sending a message to an agent
type SendMessageRequest struct {
	Content       string `json:"content"`
	WebhookURL    string `json:"webhook_url"`
	WebhookSecret string `json:"webhook_secret,omitempty"`
	RequestID     string `json:"request_id,omitempty"`
}

// SendMessageResponse is the response for sending a message
type SendMessageResponse struct {
	RequestID string `json:"request_id"`
	AgentID   string `json:"agent_id"`
	Status    string `json:"status"`
}

// InterruptRequest is the request body for interrupting an agent
type InterruptRequest struct {
	WebhookURL    string `json:"webhook_url"`
	WebhookSecret string `json:"webhook_secret,omitempty"`
	RequestID     string `json:"request_id,omitempty"`
}

// SendMessage handles POST /api/v1/agents/:id/messages
func (h *Handler) SendMessage(c echo.Context) error {
	agentID := c.Param("id")
	userID := c.QueryParam("user_id")

	if userID == "" {
		return errors.BadRequest("user_id query param is required")
	}

	var req SendMessageRequest
	if err := c.Bind(&req); err != nil {
		return errors.BadRequest("invalid request body")
	}

	if req.Content == "" {
		return errors.BadRequest("content is required")
	}

	if req.WebhookURL == "" {
		return errors.BadRequest("webhook_url is required")
	}

	// Generate request ID if not provided
	requestID := req.RequestID
	if requestID == "" {
		requestID = generateRequestID()
	}

	webhookCfg := webhook.Config{
		URL:    req.WebhookURL,
		Secret: req.WebhookSecret,
	}

	// Start async processing
	go func() {
		// Use TODO context since HTTP request completes immediately with 202
		// The request context would be canceled as soon as we return
		ctx := context.TODO()
		_ = h.processor.SendMessageWithWebhook(ctx, userID, agentID, requestID, req.Content, webhookCfg)
	}()

	return c.JSON(http.StatusAccepted, SendMessageResponse{
		RequestID: requestID,
		AgentID:   agentID,
		Status:    "processing",
	})
}

// Interrupt handles POST /api/v1/agents/:id/interrupt
func (h *Handler) Interrupt(c echo.Context) error {
	agentID := c.Param("id")
	userID := c.QueryParam("user_id")

	if userID == "" {
		return errors.BadRequest("user_id query param is required")
	}

	var req InterruptRequest
	if err := c.Bind(&req); err != nil {
		return errors.BadRequest("invalid request body")
	}

	if req.WebhookURL == "" {
		return errors.BadRequest("webhook_url is required")
	}

	// Generate request ID if not provided
	requestID := req.RequestID
	if requestID == "" {
		requestID = generateRequestID()
	}

	webhookCfg := webhook.Config{
		URL:    req.WebhookURL,
		Secret: req.WebhookSecret,
	}

	// Start async processing
	go func() {
		// Use TODO context since HTTP request completes immediately with 202
		// The request context would be canceled as soon as we return
		ctx := context.TODO()
		_ = h.processor.InterruptWithWebhook(ctx, userID, agentID, requestID, webhookCfg)
	}()

	return c.JSON(http.StatusAccepted, SendMessageResponse{
		RequestID: requestID,
		AgentID:   agentID,
		Status:    "interrupting",
	})
}

func generateRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "req_" + hex.EncodeToString(b)
}
