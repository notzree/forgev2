package processor

import (
	"context"
	"fmt"
	"time"

	agentv1 "github.com/forge/platform/gen/agent/v1"
	"github.com/forge/platform/internal/agent"
	"github.com/forge/platform/internal/k8s"
)

// Processor handles agent business logic and lifecycle operations
type Processor struct {
	k8m *k8s.Manager
}

// NewProcessor creates a new agent processor
func NewProcessor(k8sManager *k8s.Manager) *Processor {
	return &Processor{
		k8m: k8sManager,
	}
}

// CreateAgent creates a new agent and registers it
func (p *Processor) CreateAgent(ctx context.Context, userID string) (*k8s.PodID, error) {
	podID := k8s.NewPodID(userID, generateAgentID())
	err := p.k8m.CreatePod(ctx, *podID)
	if err != nil {
		return nil, fmt.Errorf("error creating agent pod: %w", err)
	}
	return podID, nil
}

// DeleteAgent removes an agent from the registry and optionally shuts it down
func (p *Processor) DeleteAgent(ctx context.Context, userID, agentID string) error {
	podID := k8s.NewPodID(userID, agentID)
	return p.k8m.ClosePod(ctx, *podID)
}

// GetStatus retrieves the current status of an agent
func (p *Processor) GetStatus(ctx context.Context, agentID string) (*agentv1.GetStatusResponse, error) {

}

// ConnectToAgent establishes a bidirectional stream with an agent
func (p *Processor) ConnectToAgent(ctx context.Context, agentID string) (*agent.StreamConnection, error) {
	info, exists := p.registry.Get(agentID)
	if !exists {
		return nil, fmt.Errorf("agent not found: %s", agentID)
	}

	if info.State != agent.StateRunning {
		return nil, fmt.Errorf("agent not running: %s (state: %s)", agentID, info.State)
	}

	client := info.GetClient()
	stream := client.Connect(ctx)

	return &agent.StreamConnection{
		AgentID: agentID,
		Stream:  stream,
	}, nil
}

// GetAgent retrieves agent info by ID
func (p *Processor) GetAgent(agentID string) (*agent.Info, bool) {
	return p.registry.Get(agentID)
}

// ListAgents returns all agents, optionally filtered by owner
func (p *Processor) ListAgents(ownerID string) []*agent.Info {
	if ownerID != "" {
		return p.registry.GetByOwner(ownerID)
	}
	return p.registry.List()
}

func generateAgentID() string {
	return fmt.Sprintf("agent-%d", time.Now().UnixNano())
}
