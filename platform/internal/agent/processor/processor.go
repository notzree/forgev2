package processor

import (
	"context"
	"fmt"
	"time"

	"github.com/forge/platform/internal/k8s"
)

// Processor handles agent business logic and lifecycle operations
// TODO: This processor needs to be refactored per TASKS.md Task 4
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

// DeleteAgent removes an agent
func (p *Processor) DeleteAgent(ctx context.Context, userID, agentID string) error {
	podID := k8s.NewPodID(userID, agentID)
	return p.k8m.ClosePod(ctx, *podID)
}

func generateAgentID() string {
	return fmt.Sprintf("agent-%d", time.Now().UnixNano())
}
