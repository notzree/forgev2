package processor

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"

	agentv1 "github.com/forge/platform/gen/agent/v1"
	"github.com/forge/platform/internal/agent"
	"github.com/forge/platform/internal/k8s"
	"github.com/forge/platform/internal/webhook"
)

// Processor handles agent business logic and lifecycle operations.
// It acts as the business logic layer between HTTP handlers and the
// underlying K8s/agent infrastructure.
type Processor struct {
	k8m             *k8s.Manager
	webhookDelivery *webhook.DeliveryService
	logger          *zap.Logger
}

// NewProcessor creates a new agent processor
func NewProcessor(k8sManager *k8s.Manager, webhookDelivery *webhook.DeliveryService, logger *zap.Logger) *Processor {
	return &Processor{
		k8m:             k8sManager,
		webhookDelivery: webhookDelivery,
		logger:          logger,
	}
}

// ListAgents returns all agent pods belonging to a specific user.
func (p *Processor) ListAgents(ctx context.Context, userID string) ([]k8s.PodID, error) {
	podList, err := p.k8m.ListPodsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents for user %s: %w", userID, err)
	}

	podIDs := make([]k8s.PodID, 0, len(podList.Items))
	for _, pod := range podList.Items {
		userID := pod.Labels["user-id"]
		agentID := pod.Labels["agent-id"]
		if userID != "" && agentID != "" {
			podIDs = append(podIDs, k8s.PodID{
				UserID:  userID,
				AgentID: agentID,
			})
		}
	}

	return podIDs, nil
}

// GetAgent returns detailed pod information for a specific agent.
func (p *Processor) GetAgent(ctx context.Context, userID, agentID string) (*corev1.Pod, error) {
	podID := k8s.NewPodID(userID, agentID)
	pod, err := p.k8m.GetPod(ctx, *podID)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %s for user %s: %w", agentID, userID, err)
	}
	return pod, nil
}

// GetStatus retrieves real-time status from an agent via RPC.
func (p *Processor) GetStatus(ctx context.Context, userID, agentID string) (*agentv1.GetStatusResponse, error) {
	podID := k8s.NewPodID(userID, agentID)

	address, err := p.k8m.GetPodAddress(ctx, *podID)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent address: %w", err)
	}

	client := agent.NewClient(address)
	resp, err := client.GetStatus(ctx, connect.NewRequest(&agentv1.GetStatusRequest{}))
	if err != nil {
		return nil, fmt.Errorf("failed to get agent status: %w", err)
	}

	return resp.Msg, nil
}

// CreateAgent creates a new agent pod and waits for it to be ready.
func (p *Processor) CreateAgent(ctx context.Context, userID string) (*k8s.PodID, error) {
	podID := k8s.NewPodID(userID, generateAgentID())

	if err := p.k8m.CreatePod(ctx, *podID); err != nil {
		return nil, fmt.Errorf("failed to create agent pod: %w", err)
	}

	// Wait for the pod to be ready
	_, err := p.k8m.WaitForPodReady(ctx, *podID)
	if err != nil {
		// Best-effort cleanup - use background context to avoid cancellation issues
		_ = p.k8m.ClosePod(context.Background(), *podID)
		return nil, fmt.Errorf("agent pod created but failed to become ready: %w", err)
	}

	return podID, nil
}

// DeleteAgent removes an agent, optionally with graceful shutdown via RPC.
// If graceful is true, it attempts to send a shutdown RPC to the agent first.
// The pod is always deleted regardless of whether graceful shutdown succeeds.
func (p *Processor) DeleteAgent(ctx context.Context, userID, agentID string, graceful bool) error {
	podID := k8s.NewPodID(userID, agentID)

	if graceful {
		// Try graceful shutdown, but don't fail if agent is unreachable
		if address, err := p.k8m.GetPodAddress(ctx, *podID); err == nil {
			client := agent.NewClient(address)
			shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			// Ignore errors - pod might already be terminating
			_, _ = client.Shutdown(shutdownCtx, connect.NewRequest(&agentv1.ShutdownRequest{
				Graceful: true,
			}))
		}
	}

	if err := p.k8m.ClosePod(ctx, *podID); err != nil {
		return fmt.Errorf("failed to delete agent pod: %w", err)
	}

	return nil
}

// ConnectToAgent establishes a bidirectional streaming connection to an agent.
// The caller is responsible for managing the stream lifecycle (closing when done).
func (p *Processor) ConnectToAgent(ctx context.Context, userID, agentID string) (*connect.BidiStreamForClient[agentv1.AgentRequest, agentv1.AgentResponse], error) {
	podID := k8s.NewPodID(userID, agentID)

	address, err := p.k8m.GetPodAddress(ctx, *podID)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent address: %w", err)
	}

	client := agent.NewClient(address)
	stream := client.Connect(ctx)

	return stream, nil
}

func generateAgentID() string {
	return fmt.Sprintf("agent-%d", time.Now().UnixNano())
}

// SendMessageWithWebhook sends a message to an agent and delivers responses via webhook
func (p *Processor) SendMessageWithWebhook(ctx context.Context, userID, agentID, requestID, content string, webhookCfg webhook.Config) error {
	p.logger.Info("sending message to agent",
		zap.String("agent_id", agentID),
		zap.String("request_id", requestID),
	)

	// Create webhook delivery record
	if err := p.webhookDelivery.CreateDeliveryRecord(ctx, requestID, agentID, webhookCfg); err != nil {
		p.logger.Error("failed to create delivery record", zap.Error(err))
		// Continue anyway - we can still deliver webhooks without DB tracking
	}

	// Connect to agent
	stream, err := p.ConnectToAgent(ctx, userID, agentID)
	if err != nil {
		// Send error webhook
		errPayload := webhook.ErrorToPayload(agentID, requestID, 0, "AGENT_UNREACHABLE", err.Error(), false)
		p.webhookDelivery.DeliverAsync(webhookCfg, errPayload)
		return fmt.Errorf("failed to connect to agent: %w", err)
	}

	// Send the message request
	req := &agentv1.AgentRequest{
		RequestId: requestID,
		Command: &agentv1.AgentRequest_SendMessage{
			SendMessage: &agentv1.SendMessageRequest{
				Content: content,
			},
		},
	}

	if err := stream.Send(req); err != nil {
		stream.CloseRequest()
		errPayload := webhook.ErrorToPayload(agentID, requestID, 0, "SEND_FAILED", err.Error(), false)
		p.webhookDelivery.DeliverAsync(webhookCfg, errPayload)
		return fmt.Errorf("failed to send message request: %w", err)
	}

	// Close the request side immediately - we only send one request per connection.
	// This signals to the agent that no more requests are coming, allowing it to
	// complete its response stream and close cleanly.
	if err := stream.CloseRequest(); err != nil {
		p.logger.Warn("failed to close request stream", zap.Error(err))
	}

	// Stream responses to webhook until completion
	return p.streamToWebhook(ctx, stream, agentID, requestID, webhookCfg)
}

// InterruptWithWebhook interrupts an agent and delivers response via webhook
func (p *Processor) InterruptWithWebhook(ctx context.Context, userID, agentID, requestID string, webhookCfg webhook.Config) error {
	p.logger.Info("interrupting agent",
		zap.String("agent_id", agentID),
		zap.String("request_id", requestID),
	)

	// Create webhook delivery record
	if err := p.webhookDelivery.CreateDeliveryRecord(ctx, requestID, agentID, webhookCfg); err != nil {
		p.logger.Error("failed to create delivery record", zap.Error(err))
	}

	// Connect to agent
	stream, err := p.ConnectToAgent(ctx, userID, agentID)
	if err != nil {
		errPayload := webhook.ErrorToPayload(agentID, requestID, 0, "AGENT_UNREACHABLE", err.Error(), false)
		p.webhookDelivery.DeliverAsync(webhookCfg, errPayload)
		return fmt.Errorf("failed to connect to agent: %w", err)
	}

	// Send the interrupt request
	req := &agentv1.AgentRequest{
		RequestId: requestID,
		Command: &agentv1.AgentRequest_Interrupt{
			Interrupt: &agentv1.InterruptRequest{},
		},
	}

	if err := stream.Send(req); err != nil {
		stream.CloseRequest()
		errPayload := webhook.ErrorToPayload(agentID, requestID, 0, "SEND_FAILED", err.Error(), false)
		p.webhookDelivery.DeliverAsync(webhookCfg, errPayload)
		return fmt.Errorf("failed to send interrupt request: %w", err)
	}

	// Close the request side immediately - we only send one request per connection.
	if err := stream.CloseRequest(); err != nil {
		p.logger.Warn("failed to close request stream", zap.Error(err))
	}

	// Stream responses to webhook until completion
	return p.streamToWebhook(ctx, stream, agentID, requestID, webhookCfg)
}

// streamToWebhook reads from the agent gRPC stream and delivers events to the webhook.
// The platform acts as a "dumb pipe" - it does not parse the OpenCode event JSON,
// just forwards it to the webhook consumer.
func (p *Processor) streamToWebhook(
	ctx context.Context,
	stream *connect.BidiStreamForClient[agentv1.AgentRequest, agentv1.AgentResponse],
	agentID, requestID string,
	webhookCfg webhook.Config,
) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := stream.Receive()
		if err != nil {
			// Check if it's a normal EOF (stream completed)
			if err.Error() == "EOF" {
				p.logger.Debug("stream completed",
					zap.String("request_id", requestID),
				)
				// Mark delivery as completed
				_ = p.webhookDelivery.MarkDeliveryCompleted(ctx, requestID)
				return nil
			}

			p.logger.Error("stream receive error",
				zap.Error(err),
				zap.String("request_id", requestID),
			)

			// Send error webhook
			errPayload := webhook.ErrorToPayload(agentID, requestID, 0, "STREAM_ERROR", err.Error(), false)
			if deliveryErr := p.webhookDelivery.Deliver(ctx, webhookCfg, errPayload); deliveryErr != nil {
				p.logger.Error("failed to deliver error webhook", zap.Error(deliveryErr))
			}

			_ = p.webhookDelivery.MarkDeliveryFailed(ctx, requestID)
			return fmt.Errorf("stream receive error: %w", err)
		}

		// Convert response to webhook payload (pass-through)
		payload := webhook.AgentResponseToPayload(resp, agentID, requestID)

		// Update delivery tracking
		_ = p.webhookDelivery.UpdateDeliverySeq(ctx, requestID, int64(resp.GetSeq()), payload.EventType)

		// Deliver to webhook
		if err := p.webhookDelivery.Deliver(ctx, webhookCfg, payload); err != nil {
			p.logger.Error("failed to deliver webhook",
				zap.Error(err),
				zap.String("request_id", requestID),
				zap.Uint64("seq", resp.GetSeq()),
			)
			// Continue processing - don't fail the whole stream on delivery failure
		}

		// Check if this is the final message
		if payload.IsFinal {
			p.logger.Info("received final message",
				zap.String("request_id", requestID),
			)
			_ = p.webhookDelivery.MarkDeliveryCompleted(ctx, requestID)
			return nil
		}
	}
}
