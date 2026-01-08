package agent

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	agentv1 "github.com/forge/platform/gen/agent/v1"
)

// Manager handles agent lifecycle operations
type Manager struct {
	registry *Registry
}

// NewManager creates a new agent manager
func NewManager(registry *Registry) *Manager {
	return &Manager{
		registry: registry,
	}
}

// CreateAgent creates a new agent (for now, just registers it - K8s integration comes later)
func (m *Manager) CreateAgent(ctx context.Context, ownerID, address string) (*Info, error) {
	agentID := generateAgentID()

	info := &Info{
		ID:             agentID,
		OwnerID:        ownerID,
		State:          StateStarting,
		Address:        address,
		CreatedAt:      time.Now(),
		LastActivityAt: time.Now(),
	}

	m.registry.Register(info)

	// Verify agent is reachable
	client := info.GetClient()
	statusResp, err := client.GetStatus(ctx, connect.NewRequest(&agentv1.GetStatusRequest{}))
	if err != nil {
		m.registry.Unregister(agentID)
		return nil, fmt.Errorf("failed to connect to agent: %w", err)
	}

	info.State = StateRunning
	info.SessionID = statusResp.Msg.SessionId
	info.LatestSeq = statusResp.Msg.LatestSeq

	return info, nil
}

// RegisterExistingAgent registers an already-running agent
func (m *Manager) RegisterExistingAgent(ownerID, agentID, address string) *Info {
	info := &Info{
		ID:             agentID,
		OwnerID:        ownerID,
		State:          StateRunning,
		Address:        address,
		CreatedAt:      time.Now(),
		LastActivityAt: time.Now(),
	}

	m.registry.Register(info)
	return info
}

// DeleteAgent removes an agent from the registry and optionally shuts it down
func (m *Manager) DeleteAgent(ctx context.Context, agentID string, graceful bool) error {
	info, exists := m.registry.Get(agentID)
	if !exists {
		return fmt.Errorf("agent not found: %s", agentID)
	}

	// Try to shut down gracefully
	if info.State == StateRunning {
		client := info.GetClient()
		_, err := client.Shutdown(ctx, connect.NewRequest(&agentv1.ShutdownRequest{
			Graceful: graceful,
		}))
		if err != nil {
			// Log but don't fail - agent might already be down
			fmt.Printf("Warning: failed to shutdown agent %s: %v\n", agentID, err)
		}
	}

	m.registry.Unregister(agentID)
	return nil
}

// GetStatus retrieves the current status of an agent
func (m *Manager) GetStatus(ctx context.Context, agentID string) (*agentv1.GetStatusResponse, error) {
	info, exists := m.registry.Get(agentID)
	if !exists {
		return nil, fmt.Errorf("agent not found: %s", agentID)
	}

	client := info.GetClient()
	resp, err := client.GetStatus(ctx, connect.NewRequest(&agentv1.GetStatusRequest{}))
	if err != nil {
		return nil, err
	}

	// Update cached state
	info.SessionID = resp.Msg.SessionId
	info.LatestSeq = resp.Msg.LatestSeq
	info.LastActivityAt = time.Now()

	return resp.Msg, nil
}

// CatchUp retrieves messages from an agent since a given sequence number
func (m *Manager) CatchUp(ctx context.Context, agentID string, fromSeq int64, limit int32) (*agentv1.CatchUpResponse, error) {
	info, exists := m.registry.Get(agentID)
	if !exists {
		return nil, fmt.Errorf("agent not found: %s", agentID)
	}

	client := info.GetClient()
	resp, err := client.CatchUp(ctx, connect.NewRequest(&agentv1.CatchUpRequest{
		FromSeq: fromSeq,
		Limit:   limit,
	}))
	if err != nil {
		return nil, err
	}

	return resp.Msg, nil
}

// ConnectToAgent establishes a bidirectional stream with an agent
func (m *Manager) ConnectToAgent(ctx context.Context, agentID string) (*StreamConnection, error) {
	info, exists := m.registry.Get(agentID)
	if !exists {
		return nil, fmt.Errorf("agent not found: %s", agentID)
	}

	if info.State != StateRunning {
		return nil, fmt.Errorf("agent not running: %s (state: %s)", agentID, info.State)
	}

	client := info.GetClient()
	stream := client.Connect(ctx)

	return &StreamConnection{
		AgentID: agentID,
		Stream:  stream,
	}, nil
}

func generateAgentID() string {
	return fmt.Sprintf("agent-%d", time.Now().UnixNano())
}
