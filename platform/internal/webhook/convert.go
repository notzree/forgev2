package webhook

import (
	"encoding/json"
	"time"

	agentv1 "github.com/forge/platform/gen/agent/v1"
)

// AgentEventToPayload converts a protobuf AgentEvent to a webhook Payload
func AgentEventToPayload(event *agentv1.AgentEvent, msg *agentv1.Message, agentID, requestID string) Payload {
	if msg == nil {
		return Payload{
			AgentID:   agentID,
			RequestID: requestID,
			Timestamp: time.Now(),
		}
	}

	seq := msg.GetSeq()
	timestamp := time.UnixMilli(msg.GetCreatedAt())
	if msg.GetCreatedAt() == 0 {
		timestamp = time.Now()
	}

	switch payload := msg.GetPayload().(type) {
	case *agentv1.Message_StreamEvent:
		return convertStreamEvent(payload.StreamEvent, msg, agentID, requestID, seq, timestamp)

	case *agentv1.Message_AssistantMessage:
		return convertAssistantMessage(payload.AssistantMessage, msg, agentID, requestID, seq, timestamp)

	case *agentv1.Message_ResultMessage:
		return convertResultMessage(payload.ResultMessage, msg, agentID, requestID, seq, timestamp)

	case *agentv1.Message_UserMessage:
		return convertUserMessage(payload.UserMessage, msg, agentID, requestID, seq, timestamp)

	case *agentv1.Message_SystemMessage:
		return convertSystemMessage(payload.SystemMessage, msg, agentID, requestID, seq, timestamp)

	default:
		return Payload{
			EventType: EventTypeMessage,
			AgentID:   agentID,
			RequestID: requestID,
			Seq:       seq,
			Timestamp: timestamp,
			Payload:   map[string]any{"raw": msg},
		}
	}
}

func convertStreamEvent(stream *agentv1.StreamEvent, msg *agentv1.Message, agentID, requestID string, seq int64, timestamp time.Time) Payload {
	// Parse the event_json to get the content block
	var eventData any
	if stream.GetEventJson() != "" {
		_ = json.Unmarshal([]byte(stream.GetEventJson()), &eventData)
	}

	return Payload{
		EventType: EventTypeStream,
		AgentID:   agentID,
		RequestID: requestID,
		Seq:       seq,
		Timestamp: timestamp,
		Payload: StreamPayload{
			SessionID:    msg.GetSessionId(),
			ContentBlock: eventData,
		},
	}
}

func convertAssistantMessage(assistant *agentv1.AssistantMessage, msg *agentv1.Message, agentID, requestID string, seq int64, timestamp time.Time) Payload {
	contentBlocks := make([]any, 0, len(assistant.GetContent()))
	for _, block := range assistant.GetContent() {
		contentBlocks = append(contentBlocks, convertContentBlock(block))
	}

	return Payload{
		EventType: EventTypeMessage,
		AgentID:   agentID,
		RequestID: requestID,
		Seq:       seq,
		Timestamp: timestamp,
		Payload: MessagePayload{
			UUID:          msg.GetUuid(),
			SessionID:     msg.GetSessionId(),
			Role:          "assistant",
			ContentBlocks: contentBlocks,
		},
	}
}

func convertResultMessage(result *agentv1.ResultMessage, msg *agentv1.Message, agentID, requestID string, seq int64, timestamp time.Time) Payload {
	usage := result.GetUsage()

	var usageStats UsageStats
	if usage != nil {
		usageStats = UsageStats{
			InputTokens:      int64(usage.GetInputTokens()),
			OutputTokens:     int64(usage.GetOutputTokens()),
			CacheReadTokens:  int64(usage.GetCacheReadInputTokens()),
			CacheWriteTokens: int64(usage.GetCacheCreationInputTokens()),
		}
	}

	status := "completed"
	if result.GetIsError() {
		status = "error"
	}

	return Payload{
		EventType: EventTypeResult,
		AgentID:   agentID,
		RequestID: requestID,
		Seq:       seq,
		Timestamp: timestamp,
		IsFinal:   true,
		Payload: ResultPayload{
			SessionID:  msg.GetSessionId(),
			Status:     status,
			Usage:      usageStats,
			CostUSD:    result.GetTotalCostUsd(),
			DurationMs: result.GetDurationMs(),
		},
	}
}

func convertUserMessage(user *agentv1.UserMessage, msg *agentv1.Message, agentID, requestID string, seq int64, timestamp time.Time) Payload {
	contentBlocks := []any{
		map[string]any{
			"type": "text",
			"text": user.GetContent(),
		},
	}

	for _, block := range user.GetAdditionalContent() {
		contentBlocks = append(contentBlocks, convertContentBlock(block))
	}

	return Payload{
		EventType: EventTypeMessage,
		AgentID:   agentID,
		RequestID: requestID,
		Seq:       seq,
		Timestamp: timestamp,
		Payload: MessagePayload{
			UUID:          msg.GetUuid(),
			SessionID:     msg.GetSessionId(),
			Role:          "user",
			ContentBlocks: contentBlocks,
		},
	}
}

func convertSystemMessage(system *agentv1.SystemMessage, msg *agentv1.Message, agentID, requestID string, seq int64, timestamp time.Time) Payload {
	return Payload{
		EventType: EventTypeMessage,
		AgentID:   agentID,
		RequestID: requestID,
		Seq:       seq,
		Timestamp: timestamp,
		Payload: MessagePayload{
			UUID:      msg.GetUuid(),
			SessionID: msg.GetSessionId(),
			Role:      "system",
			ContentBlocks: []any{
				map[string]any{
					"type":    "system",
					"subtype": system.GetSubtype(),
				},
			},
		},
	}
}

func convertContentBlock(block *agentv1.ContentBlock) any {
	if block == nil {
		return nil
	}

	switch b := block.GetBlock().(type) {
	case *agentv1.ContentBlock_Text:
		return map[string]any{
			"type": "text",
			"text": b.Text.GetText(),
		}

	case *agentv1.ContentBlock_ToolUse:
		// Parse the JSON input
		var input any
		if b.ToolUse.GetInputJson() != "" {
			_ = json.Unmarshal([]byte(b.ToolUse.GetInputJson()), &input)
		}
		return map[string]any{
			"type":  "tool_use",
			"id":    b.ToolUse.GetId(),
			"name":  b.ToolUse.GetName(),
			"input": input,
		}

	case *agentv1.ContentBlock_ToolResult:
		// Parse the JSON content
		var content any
		if b.ToolResult.GetContentJson() != "" {
			_ = json.Unmarshal([]byte(b.ToolResult.GetContentJson()), &content)
		}
		return map[string]any{
			"type":        "tool_result",
			"tool_use_id": b.ToolResult.GetToolUseId(),
			"content":     content,
			"is_error":    b.ToolResult.GetIsError(),
		}

	case *agentv1.ContentBlock_Thinking:
		return map[string]any{
			"type":    "thinking",
			"content": b.Thinking.GetThinking(),
		}

	case *agentv1.ContentBlock_Image:
		return map[string]any{
			"type":       "image",
			"media_type": b.Image.GetMediaType(),
			"data":       b.Image.GetData(),
		}

	default:
		return map[string]any{
			"type": "unknown",
		}
	}
}

// ErrorToPayload creates an error webhook payload
func ErrorToPayload(agentID, requestID string, seq int64, errorCode, message string, recoverable bool) Payload {
	return Payload{
		EventType: EventTypeError,
		AgentID:   agentID,
		RequestID: requestID,
		Seq:       seq,
		Timestamp: time.Now(),
		IsFinal:   true,
		Payload: ErrorPayload{
			ErrorCode:   errorCode,
			Message:     message,
			Recoverable: recoverable,
		},
	}
}
