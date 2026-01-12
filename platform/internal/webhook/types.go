package webhook

import (
	"encoding/json"
	"time"
)

// EventType represents the type of webhook event
type EventType string

const (
	// EventTypeEvent is for OpenCode events (passed through as raw JSON)
	EventTypeEvent EventType = "agent.event"
	// EventTypeError is for errors
	EventTypeError EventType = "agent.error"
	// EventTypeComplete is for stream completion
	EventTypeComplete EventType = "agent.complete"
)

// Config holds webhook delivery configuration
type Config struct {
	URL    string
	Secret string // optional HMAC secret
}

// Payload represents a webhook payload sent to consumers.
// The Event field contains raw OpenCode event JSON - the platform does not parse it.
type Payload struct {
	EventType EventType `json:"event_type"`
	AgentID   string    `json:"agent_id"`
	RequestID string    `json:"request_id"`
	SessionID string    `json:"session_id"`
	Seq       uint64    `json:"seq"`
	Timestamp time.Time `json:"timestamp"`
	IsFinal   bool      `json:"is_final,omitempty"`

	// Current agent state: "idle", "processing", "error"
	AgentState string `json:"agent_state,omitempty"`

	// For agent.event - raw OpenCode event (pass-through)
	// Consumers should use @opencode-ai/sdk types to parse this
	Event json.RawMessage `json:"event,omitempty"`

	// For agent.event - the OpenCode event type (e.g., "message.updated")
	// Provided for convenience so consumers can filter without parsing Event
	OpenCodeEventType string `json:"opencode_event_type,omitempty"`

	// For agent.error
	Error *ErrorPayload `json:"error,omitempty"`

	// For agent.complete
	Success bool `json:"success,omitempty"`
}

// ErrorPayload is the payload for agent.error events
type ErrorPayload struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Recoverable bool   `json:"recoverable"`
}

// DeliveryResult represents the result of a webhook delivery attempt
type DeliveryResult struct {
	Success    bool
	StatusCode int
	Error      error
	RetryAfter time.Duration
}
