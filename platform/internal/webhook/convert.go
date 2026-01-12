package webhook

import (
	"encoding/json"
	"time"

	agentv1 "github.com/forge/platform/gen/agent/v1"
)

// AgentResponseToPayload converts a protobuf AgentResponse to a webhook Payload.
// This is a pass-through conversion - the platform does not parse the OpenCode event JSON.
func AgentResponseToPayload(resp *agentv1.AgentResponse, agentID, requestID string) Payload {
	timestamp := time.UnixMilli(resp.GetTimestamp())
	if resp.GetTimestamp() == 0 {
		timestamp = time.Now()
	}

	base := Payload{
		AgentID:   agentID,
		RequestID: requestID,
		SessionID: resp.GetSessionId(),
		Seq:       resp.GetSeq(),
		Timestamp: timestamp,
	}

	switch payload := resp.GetPayload().(type) {
	case *agentv1.AgentResponse_Event:
		return convertEventPayload(base, payload.Event)

	case *agentv1.AgentResponse_Error:
		return convertErrorPayload(base, payload.Error)

	case *agentv1.AgentResponse_Complete:
		return convertCompletePayload(base, payload.Complete)

	default:
		// Unknown payload type - return as error
		return Payload{
			EventType: EventTypeError,
			AgentID:   agentID,
			RequestID: requestID,
			SessionID: resp.GetSessionId(),
			Seq:       resp.GetSeq(),
			Timestamp: timestamp,
			IsFinal:   true,
			Error: &ErrorPayload{
				Code:        "UNKNOWN_PAYLOAD",
				Message:     "Unknown response payload type",
				Recoverable: false,
			},
		}
	}
}

func convertEventPayload(base Payload, event *agentv1.EventPayload) Payload {
	base.EventType = EventTypeEvent
	base.OpenCodeEventType = event.GetEventType()
	base.Event = json.RawMessage(event.GetEventJson())

	// Mark completion events as final
	if isCompletionEventType(event.GetEventType()) {
		base.IsFinal = true
	}

	return base
}

func convertErrorPayload(base Payload, err *agentv1.ErrorPayload) Payload {
	base.EventType = EventTypeError
	base.IsFinal = true
	base.Error = &ErrorPayload{
		Code:        err.GetCode(),
		Message:     err.GetMessage(),
		Recoverable: !err.GetFatal(),
	}
	return base
}

func convertCompletePayload(base Payload, complete *agentv1.CompletePayload) Payload {
	base.EventType = EventTypeComplete
	base.IsFinal = true
	base.Success = complete.GetSuccess()
	return base
}

// isCompletionEventType checks if an OpenCode event type indicates completion
func isCompletionEventType(eventType string) bool {
	switch eventType {
	case "session.completed", "session.error", "message.completed":
		return true
	default:
		return false
	}
}

// ErrorToPayload creates an error webhook payload
func ErrorToPayload(agentID, requestID string, seq uint64, errorCode, message string, recoverable bool) Payload {
	return Payload{
		EventType: EventTypeError,
		AgentID:   agentID,
		RequestID: requestID,
		Seq:       seq,
		Timestamp: time.Now(),
		IsFinal:   true,
		Error: &ErrorPayload{
			Code:        errorCode,
			Message:     message,
			Recoverable: recoverable,
		},
	}
}
