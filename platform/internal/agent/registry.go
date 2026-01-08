package agent

import (
	"net/http"
	"sync"
	"time"

	"connectrpc.com/connect"
	agentv1 "github.com/forge/platform/gen/agent/v1"
	"github.com/forge/platform/gen/agent/v1/agentv1connect"
)

// State represents the current state of an agent
type State string

const (
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateStopped  State = "stopped"
	StateError    State = "error"
)

// Info holds metadata and connection info for an agent
type Info struct {
	ID             string
	OwnerID        string
	State          State
	Address        string // gRPC address (e.g., "http://10.0.0.5:8080")
	SessionID      string
	LatestSeq      int64
	CreatedAt      time.Time
	LastActivityAt time.Time

	// gRPC client (lazily initialized)
	client agentv1connect.AgentServiceClient
	mu     sync.RWMutex
}

// GetClient returns the gRPC client for this agent, creating one if necessary
func (a *Info) GetClient() agentv1connect.AgentServiceClient {
	a.mu.RLock()
	if a.client != nil {
		a.mu.RUnlock()
		return a.client
	}
	a.mu.RUnlock()

	a.mu.Lock()
	defer a.mu.Unlock()

	// Double-check after acquiring write lock
	if a.client != nil {
		return a.client
	}

	a.client = agentv1connect.NewAgentServiceClient(
		http.DefaultClient,
		a.Address,
		connect.WithGRPC(),
	)
	return a.client
}

// Registry tracks all running agents
type Registry struct {
	agents map[string]*Info
	mu     sync.RWMutex
}

// NewRegistry creates a new agent registry
func NewRegistry() *Registry {
	return &Registry{
		agents: make(map[string]*Info),
	}
}

// Register adds an agent to the registry
func (r *Registry) Register(info *Info) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[info.ID] = info
}

// Unregister removes an agent from the registry and returns it
func (r *Registry) Unregister(id string) *Info {
	r.mu.Lock()
	defer r.mu.Unlock()

	info, exists := r.agents[id]
	if exists {
		delete(r.agents, id)
	}
	return info
}

// Get retrieves an agent by ID
func (r *Registry) Get(id string) (*Info, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, exists := r.agents[id]
	return info, exists
}

// GetByOwner returns all agents owned by a given owner
func (r *Registry) GetByOwner(ownerID string) []*Info {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var results []*Info
	for _, info := range r.agents {
		if info.OwnerID == ownerID {
			results = append(results, info)
		}
	}
	return results
}

// List returns all agents
func (r *Registry) List() []*Info {
	r.mu.RLock()
	defer r.mu.RUnlock()

	results := make([]*Info, 0, len(r.agents))
	for _, info := range r.agents {
		results = append(results, info)
	}
	return results
}

// UpdateState updates the state of an agent
func (r *Registry) UpdateState(id string, state State) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if info, exists := r.agents[id]; exists {
		info.State = state
		info.LastActivityAt = time.Now()
		return true
	}
	return false
}

// Count returns the number of agents in the registry
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.agents)
}

// StreamConnection represents an active bidirectional stream to an agent
type StreamConnection struct {
	AgentID string
	Stream  *connect.BidiStreamForClient[agentv1.AgentCommand, agentv1.AgentEvent]
}
