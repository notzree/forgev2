package webhook

import (
	"time"
)

// EventType represents the type of webhook event
type EventType string

const (
	EventTypeStream  EventType = "agent.stream"
	EventTypeMessage EventType = "agent.message"
	EventTypeResult  EventType = "agent.result"
	EventTypeError   EventType = "agent.error"
)

// Config holds webhook delivery configuration
type Config struct {
	URL    string
	Secret string // optional HMAC secret
}

// Payload represents a webhook payload
type Payload struct {
	EventType EventType   `json:"event_type"`
	AgentID   string      `json:"agent_id"`
	RequestID string      `json:"request_id"`
	Seq       int64       `json:"seq"`
	Timestamp time.Time   `json:"timestamp"`
	IsFinal   bool        `json:"is_final,omitempty"`
	Payload   any `json:"payload"`
}

// StreamPayload is the payload for agent.stream events
type StreamPayload struct {
	SessionID    string      `json:"session_id"`
	ContentBlock any `json:"content_block"`
}

// MessagePayload is the payload for agent.message events
type MessagePayload struct {
	UUID          string        `json:"uuid"`
	SessionID     string        `json:"session_id"`
	Role          string        `json:"role"`
	ContentBlocks []any `json:"content_blocks"`
}

// ResultPayload is the payload for agent.result events
type ResultPayload struct {
	SessionID  string       `json:"session_id"`
	Status     string       `json:"status"`
	Usage      UsageStats   `json:"usage"`
	CostUSD    float64      `json:"cost_usd"`
	DurationMs int64        `json:"duration_ms"`
}

// UsageStats contains token usage information
type UsageStats struct {
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
}

// ErrorPayload is the payload for agent.error events
type ErrorPayload struct {
	ErrorCode   string `json:"error_code"`
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
