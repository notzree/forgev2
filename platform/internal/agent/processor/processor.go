package processor

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	corev1 "k8s.io/api/core/v1"

	agentv1 "github.com/forge/platform/gen/agent/v1"
	"github.com/forge/platform/internal/agent"
	"github.com/forge/platform/internal/k8s"
)

// Processor handles agent business logic and lifecycle operations.
// It acts as the business logic layer between HTTP handlers and the
// underlying K8s/agent infrastructure.
type Processor struct {
	k8m *k8s.Manager
}

// NewProcessor creates a new agent processor
func NewProcessor(k8sManager *k8s.Manager) *Processor {
	return &Processor{
		k8m: k8sManager,
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
func (p *Processor) ConnectToAgent(ctx context.Context, userID, agentID string) (*connect.BidiStreamForClient[agentv1.AgentCommand, agentv1.AgentEvent], error) {
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
