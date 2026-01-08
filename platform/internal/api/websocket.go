package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"

	agentv1 "github.com/forge/platform/gen/agent/v1"
	"github.com/forge/platform/internal/agent"
)

// WSMessageType represents the type of WebSocket message
type WSMessageType string

const (
	WSTypeUserMessage  WSMessageType = "user_message"
	WSTypeAgentMessage WSMessageType = "agent_message"
	WSTypeCatchUp      WSMessageType = "catch_up"
	WSTypeInterrupt    WSMessageType = "interrupt"
	WSTypeStatus       WSMessageType = "status"
	WSTypeError        WSMessageType = "error"
	WSTypeAck          WSMessageType = "ack"
)

// WSMessage is the envelope for WebSocket messages
type WSMessage struct {
	Type      WSMessageType   `json:"type"`
	RequestID string          `json:"request_id,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

// WSUserMessage is the payload for user messages
type WSUserMessage struct {
	Content string `json:"content"`
}

// WSCatchUpRequest is the payload for catch-up requests
type WSCatchUpRequest struct {
	FromSeq int64 `json:"from_seq"`
	Limit   int32 `json:"limit,omitempty"`
}

// WSError is the payload for error messages
type WSError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Proxy handles WebSocket to gRPC proxying
type Proxy struct {
	manager  *agent.Manager
	upgrader websocket.Upgrader
}

// NewProxy creates a new WebSocket proxy
func NewProxy(manager *agent.Manager) *Proxy {
	return &Proxy{
		manager: manager,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// TODO: Configure appropriately for production
				return true
			},
		},
	}
}

// HandleWebSocket handles the WebSocket connection and proxies to gRPC
func (p *Proxy) HandleWebSocket(c echo.Context) error {
	agentID := c.Param("id")

	// Upgrade to WebSocket
	ws, err := p.upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer ws.Close()

	ctx, cancel := context.WithCancel(c.Request().Context())
	defer cancel()

	// Connect to agent
	conn, err := p.manager.ConnectToAgent(ctx, agentID)
	if err != nil {
		p.sendError(ws, "", "AGENT_UNAVAILABLE", err.Error())
		return nil
	}

	// Create channels for coordination
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	// WebSocket → gRPC
	go func() {
		defer wg.Done()
		defer conn.Stream.CloseRequest()

		for {
			_, message, err := ws.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					errCh <- err
				}
				return
			}

			var wsMsg WSMessage
			if err := json.Unmarshal(message, &wsMsg); err != nil {
				p.sendError(ws, "", "INVALID_MESSAGE", "invalid message format")
				continue
			}

			cmd, err := p.wsToGRPC(wsMsg)
			if err != nil {
				p.sendError(ws, wsMsg.RequestID, "INVALID_COMMAND", err.Error())
				continue
			}

			if err := conn.Stream.Send(cmd); err != nil {
				errCh <- err
				return
			}
		}
	}()

	// gRPC → WebSocket
	go func() {
		defer wg.Done()

		for {
			event, err := conn.Stream.Receive()
			if err == io.EOF {
				return
			}
			if err != nil {
				errCh <- err
				return
			}

			wsMsg, err := p.grpcToWS(event)
			if err != nil {
				continue // Skip malformed events
			}

			data, _ := json.Marshal(wsMsg)
			if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
				errCh <- err
				return
			}
		}
	}()

	// Wait for either goroutine to finish
	select {
	case err := <-errCh:
		cancel()
		if err != nil && err != io.EOF {
			return err
		}
	case <-ctx.Done():
	}

	wg.Wait()
	return nil
}

func (p *Proxy) wsToGRPC(msg WSMessage) (*agentv1.AgentCommand, error) {
	cmd := &agentv1.AgentCommand{
		RequestId: msg.RequestID,
	}

	switch msg.Type {
	case WSTypeUserMessage:
		var payload WSUserMessage
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			return nil, err
		}
		cmd.Command = &agentv1.AgentCommand_SendMessage{
			SendMessage: &agentv1.SendMessageCommand{
				Content: payload.Content,
			},
		}

	case WSTypeCatchUp:
		var payload WSCatchUpRequest
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			return nil, err
		}
		// Note: CatchUp is a unary RPC, but we support it through the stream for convenience
		// The actual catch-up should use the REST endpoint
		return nil, fmt.Errorf("catch_up should use REST endpoint")

	case WSTypeInterrupt:
		cmd.Command = &agentv1.AgentCommand_Interrupt{
			Interrupt: &agentv1.InterruptCommand{},
		}

	case WSTypeStatus:
		// Status should use REST endpoint
		return nil, fmt.Errorf("status should use REST endpoint")

	default:
		return nil, fmt.Errorf("unknown message type: %s", msg.Type)
	}

	return cmd, nil
}

func (p *Proxy) grpcToWS(event *agentv1.AgentEvent) (WSMessage, error) {
	wsMsg := WSMessage{
		RequestID: event.RequestId,
	}

	switch e := event.Event.(type) {
	case *agentv1.AgentEvent_Message:
		wsMsg.Type = WSTypeAgentMessage
		payload, err := json.Marshal(e.Message)
		if err != nil {
			return wsMsg, err
		}
		wsMsg.Payload = payload

	case *agentv1.AgentEvent_Ack:
		wsMsg.Type = WSTypeAck
		payload, err := json.Marshal(map[string]interface{}{
			"success": e.Ack.Success,
			"message": e.Ack.Message,
		})
		if err != nil {
			return wsMsg, err
		}
		wsMsg.Payload = payload

	case *agentv1.AgentEvent_Error:
		wsMsg.Type = WSTypeError
		payload, err := json.Marshal(WSError{
			Code:    e.Error.Code,
			Message: e.Error.Message,
		})
		if err != nil {
			return wsMsg, err
		}
		wsMsg.Payload = payload

	default:
		return wsMsg, fmt.Errorf("unknown event type")
	}

	return wsMsg, nil
}

func (p *Proxy) sendError(ws *websocket.Conn, requestID, code, message string) {
	errPayload, _ := json.Marshal(WSError{Code: code, Message: message})
	wsMsg := WSMessage{
		Type:      WSTypeError,
		RequestID: requestID,
		Payload:   errPayload,
	}
	data, _ := json.Marshal(wsMsg)
	ws.WriteMessage(websocket.TextMessage, data)
}
