# Platform ↔ Agent RPC Integration Tasks

This document defines the tasks required to enable the platform to communicate with claudecode agents running in Kubernetes via ConnectRPC.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         Platform (Go)                           │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────────────┐ │
│  │   Handler   │───▶│  Processor  │───▶│    K8s Manager      │ │
│  └─────────────┘    └──────┬──────┘    └──────────┬──────────┘ │
│                            │                      │             │
│                            │ CreateClient()       │ GetPodAddr  │
│                            ▼                      ▼             │
│                     ┌─────────────┐        ┌───────────┐        │
│                     │AgentClient  │        │ K8s API   │        │
│                     │  (Connect)  │        └───────────┘        │
│                     └──────┬──────┘                             │
└────────────────────────────┼────────────────────────────────────┘
                             │ ConnectRPC
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│                    K8s Cluster                                  │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  Agent Pod (user-123-agent-456)                          │  │
│  │  Labels: user-id=123, agent-id=456                       │  │
│  │  ┌────────────────────────────────────────────────────┐  │  │
│  │  │  claudecode container (port 8080)                  │  │  │
│  │  │  - AgentService.Connect (bidi stream)              │  │  │
│  │  │  - AgentService.GetStatus (unary)                  │  │  │
│  │  │  - AgentService.Shutdown (unary)                   │  │  │
│  │  └────────────────────────────────────────────────────┘  │  │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

**Key Principle**: Kubernetes is the source of truth. No in-memory registry needed.

---

## Task 1: Add Pod Address Resolution to K8s Manager

**File**: `platform/internal/k8s/client.go`

**Goal**: Add a method to retrieve the ConnectRPC address for an agent pod.

### Implementation

Add a new method `GetPodAddress` to the `Manager` struct:

```go
// GetPodAddress returns the ConnectRPC base URL for the given pod.
// Returns an error if the pod doesn't exist or doesn't have an IP assigned.
func (m *Manager) GetPodAddress(ctx context.Context, podID PodID) (string, error)
```

### Steps

1. Call `m.GetPod(ctx, podID)` to retrieve the pod
2. Check `pod.Status.PodIP` is not empty
3. Return formatted address: `http://<podIP>:8080`

### Considerations

- The port (8080) is hardcoded in both the agent and pod spec - consider making this configurable via `ContainerConfig` or a constant
- Pod IP is only available after the pod is scheduled and the network is set up
- If `PodIP` is empty, return a descriptive error indicating the pod isn't ready yet
- This method is intentionally simple - it doesn't wait for readiness, just returns current state

### Tests

- Pod exists with IP → returns `http://<ip>:8080`
- Pod exists without IP → returns error
- Pod doesn't exist → returns error

---

## Task 2: Add Pod Readiness Waiting to K8s Manager

**File**: `platform/internal/k8s/client.go`

**Goal**: Add a method that waits for a pod to be ready and returns the pod with its IP.

### Implementation

Add a new method `WaitForPodReady` that uses the existing `WatchPod` function:

```go
// WaitForPodReady blocks until the pod is in Ready condition or the context is cancelled.
// Returns the pod with its assigned IP address once ready.
func (m *Manager) WaitForPodReady(ctx context.Context, podID PodID) (*corev1.Pod, error)
```

### Steps

1. Call `m.WatchPod(ctx, podID)` to get the event channel
2. Iterate over events from the channel
3. For each `watch.Added` or `watch.Modified` event:
   - Check if pod phase is `corev1.PodRunning`
   - Check if all containers are ready via `pod.Status.ContainerStatuses`
   - Check if `pod.Status.PodIP` is assigned
4. When all conditions met, return the pod
5. Handle `watch.Deleted` → return error (pod was deleted while waiting)
6. Handle context cancellation → return `ctx.Err()`

### Helper Function

Create a helper to check pod readiness:

```go
// isPodReady returns true if the pod is running and all containers are ready
func isPodReady(pod *corev1.Pod) bool {
    if pod.Status.Phase != corev1.PodRunning {
        return false
    }
    if pod.Status.PodIP == "" {
        return false
    }
    for _, cs := range pod.Status.ContainerStatuses {
        if !cs.Ready {
            return false
        }
    }
    return true
}
```

### Considerations

- Use a context with timeout at the call site (don't hardcode timeout here)
- The existing `WatchPod` already handles the watch setup and error cases
- Consider adding an initial check before watching (pod might already be ready)
- Log progress for debugging (e.g., "waiting for pod X, current phase: Y")

### Tests

- Pod becomes ready → returns pod with IP
- Pod is deleted while waiting → returns error
- Context cancelled → returns context error
- Pod already ready when called → returns immediately

---

## Task 3: Create Agent Client Factory

**File**: `platform/internal/agent/client.go` (new file)

**Goal**: Provide a simple, stateless way to create ConnectRPC clients for agents.

### Implementation

```go
package agent

import (
    "net/http"
    "connectrpc.com/connect"
    "github.com/forge/platform/gen/agent/v1/agentv1connect"
)

// NewClient creates a new AgentService client for the given base URL.
// The baseURL should be in the format "http://<ip>:8080".
// Clients are stateless and safe to create per-request.
func NewClient(baseURL string) agentv1connect.AgentServiceClient {
    return agentv1connect.NewAgentServiceClient(
        http.DefaultClient,
        baseURL,
        connect.WithGRPC(),
    )
}
```

### Considerations

- Keep it simple - just a factory function, no struct needed
- Use `connect.WithGRPC()` since the agent uses gRPC protocol
- `http.DefaultClient` is fine for now; could be extended later for:
  - Custom timeouts
  - Connection pooling
  - TLS configuration
  - Tracing/metrics middleware
- Clients are cheap to create - no need to cache them
- Consider adding a variant that accepts `*http.Client` for testing

### Optional Enhancement

For better testability and future extensibility:

```go
// ClientConfig holds configuration for agent clients
type ClientConfig struct {
    HTTPClient *http.Client
    Options    []connect.ClientOption
}

// DefaultClientConfig returns the default client configuration
func DefaultClientConfig() *ClientConfig {
    return &ClientConfig{
        HTTPClient: http.DefaultClient,
        Options:    []connect.ClientOption{connect.WithGRPC()},
    }
}

// NewClientWithConfig creates a client with custom configuration
func NewClientWithConfig(baseURL string, cfg *ClientConfig) agentv1connect.AgentServiceClient
```

### Tests

- Creates valid client that implements the interface
- Client can be used to make calls (integration test with mock server)

---

## Task 4: Refactor Processor to Use K8s-Based Discovery

**File**: `platform/internal/agent/processor/processor.go`

**Goal**: Rewrite the Processor to use K8s as source of truth and create RPC clients on-demand.

### Current State

The processor has broken references to a deleted `registry` and incomplete method implementations.

### New Implementation

```go
package processor

import (
    "context"
    "fmt"
    "time"

    "connectrpc.com/connect"
    agentv1 "github.com/forge/platform/gen/agent/v1"
    "github.com/forge/platform/gen/agent/v1/agentv1connect"
    "github.com/forge/platform/internal/agent"
    "github.com/forge/platform/internal/k8s"
)

type Processor struct {
    k8m *k8s.Manager
}

func NewProcessor(k8sManager *k8s.Manager) *Processor {
    return &Processor{k8m: k8sManager}
}
```

### Methods to Implement

#### CreateAgent

```go
func (p *Processor) CreateAgent(ctx context.Context, userID string) (*k8s.PodID, error)
```

1. Generate agent ID
2. Create PodID from userID + agentID
3. Call `k8m.CreatePod(ctx, podID)`
4. Call `k8m.WaitForPodReady(ctx, podID)` - ensures pod is ready before returning
5. Return the PodID

**Consideration**: The caller should use a context with appropriate timeout.

#### DeleteAgent

```go
func (p *Processor) DeleteAgent(ctx context.Context, userID, agentID string, graceful bool) error
```

1. Create PodID from userID + agentID
2. If graceful:
   - Try to get pod address via `k8m.GetPodAddress(ctx, podID)`
   - If successful, create client and call `Shutdown(graceful: true)`
   - Ignore errors (pod might already be down)
3. Call `k8m.ClosePod(ctx, podID)`

#### GetStatus

```go
func (p *Processor) GetStatus(ctx context.Context, userID, agentID string) (*agentv1.GetStatusResponse, error)
```

1. Create PodID from userID + agentID
2. Get address via `k8m.GetPodAddress(ctx, podID)`
3. Create client via `agent.NewClient(address)`
4. Call `client.GetStatus(ctx, &agentv1.GetStatusRequest{})`
5. Return response

#### ConnectToAgent

```go
func (p *Processor) ConnectToAgent(ctx context.Context, userID, agentID string) (*connect.BidiStreamForClient[agentv1.AgentCommand, agentv1.AgentEvent], error)
```

1. Create PodID from userID + agentID
2. Get address via `k8m.GetPodAddress(ctx, podID)`
3. Create client via `agent.NewClient(address)`
4. Call `client.Connect(ctx)`
5. Return the stream

#### ListAgents

```go
func (p *Processor) ListAgents(ctx context.Context, userID string) ([]k8s.PodID, error)
```

1. Call `k8m.ListPodsForUser(ctx, userID)`
2. Extract PodIDs from pod labels
3. Return list

#### GetAgent

```go
func (p *Processor) GetAgent(ctx context.Context, userID, agentID string) (*corev1.Pod, error)
```

1. Create PodID
2. Call `k8m.GetPod(ctx, podID)`
3. Return pod

### Considerations

- Every method that needs to call the agent must: get pod → get address → create client → call
- This is intentionally stateless - no caching of clients or addresses
- Errors from K8s (pod not found) vs errors from agent (RPC failed) should be distinguishable
- Consider wrapping errors with more context

### Remove

- Delete references to `registry`
- Delete `agent.Info` and `agent.StreamConnection` types (no longer needed)
- Remove or update the `agent.Module` fx module

---

## Task 5: Update Handler to Match New Processor Interface

**File**: `platform/internal/agent/handler/handler.go`

**Goal**: Update the HTTP handler to work with the refactored Processor.

### Changes Required

1. **Update `CreateAgentRequest`**: Remove `Address` field (we don't need it - K8s assigns the IP)

2. **Update `Create` handler**:
   - Only require `owner_id` 
   - Call `processor.CreateAgent(ctx, ownerID)`
   - Return the created PodID info

3. **Update `AgentResponse`**: Change to match what we can get from K8s:
   ```go
   type AgentResponse struct {
       UserID    string `json:"user_id"`
       AgentID   string `json:"agent_id"`
       PodName   string `json:"pod_name"`
       PodIP     string `json:"pod_ip,omitempty"`
       Phase     string `json:"phase"`
       Ready     bool   `json:"ready"`
       CreatedAt string `json:"created_at,omitempty"`
   }
   ```

4. **Update `Get` handler**:
   - Parse userID from query param or auth context
   - Get agentID from path param
   - Call `processor.GetAgent(ctx, userID, agentID)`
   - Optionally call `processor.GetStatus()` if `?refresh=true`

5. **Update `Delete` handler**:
   - Need both userID and agentID
   - Pass graceful flag to processor

6. **Update `List` handler**:
   - Call `processor.ListAgents(ctx, userID)`
   - Convert to response format

### Considerations

- The handler needs access to userID - this should come from auth middleware eventually
- For now, require `user_id` as a query parameter or in request body
- Consider adding a `/api/v1/users/:user_id/agents` route structure for clearer REST semantics

---

## Task 6: Clean Up Deleted/Orphaned Code

**Files**: 
- `platform/internal/agent/module.go`
- `platform/internal/handler/module.go`

**Goal**: Remove references to deleted code and ensure fx wiring is correct.

### Changes

1. **`agent/module.go`**: 
   - Remove `NewRegistry` from fx.Provide (Registry no longer exists)
   - If module is empty, consider removing it entirely

2. **Verify fx wiring**:
   - `processor.Module` provides `NewProcessor`
   - `handler.Module` provides handler
   - Ensure `k8s.Manager` is provided somewhere and injected into Processor

3. **Delete orphaned types**:
   - `agent.Info` (was in registry.go)
   - `agent.State` (was in registry.go)  
   - `agent.StreamConnection` (was in registry.go)

### Verification

- Run `go build ./...` to ensure no compilation errors
- Run `go vet ./...` to catch issues
- Check for unused imports

---

## Task 7: Add Integration Test for Full Flow

**File**: `platform/internal/agent/processor/processor_test.go` (new file)

**Goal**: Test the full flow of creating an agent and communicating with it.

### Test Scenarios

1. **CreateAgent → GetStatus → DeleteAgent**
   - Create agent for a user
   - Wait for it to be ready
   - Call GetStatus and verify response
   - Delete the agent
   - Verify pod is gone

2. **CreateAgent → Connect → Send Message → Receive Response**
   - Create agent
   - Open bidirectional stream
   - Send a command
   - Receive events
   - Close stream

3. **ListAgents**
   - Create multiple agents for a user
   - List and verify all returned
   - Create agent for different user
   - List for first user, verify second user's agent not included

### Considerations

- These are integration tests requiring a K8s cluster (use kind/minikube for CI)
- Need the agent image built and available
- Use test-specific namespace to avoid conflicts
- Clean up pods after tests (even on failure)
- Consider using `testing.Short()` to skip in unit test runs

### Test Helpers

```go
func setupTestProcessor(t *testing.T) (*Processor, func()) {
    // Create k8s manager pointing to test cluster
    // Return processor and cleanup function
}

func waitForPodDeleted(ctx context.Context, k8m *k8s.Manager, podID k8s.PodID) error {
    // Helper to wait for pod deletion
}
```

---

## Task Order Summary

These tasks can be completed in parallel by different developers, with the following dependencies:

```
Task 1 (GetPodAddress) ─────┐
                            ├──▶ Task 4 (Processor) ──▶ Task 5 (Handler)
Task 2 (WaitForPodReady) ───┤                                  │
                            │                                  │
Task 3 (Client Factory) ────┘                                  │
                                                               │
Task 6 (Cleanup) ◀─────────────────────────────────────────────┘

Task 7 (Integration Tests) - Can start after Tasks 1-4, runs against completed system
```

**Parallel work possible:**
- Tasks 1, 2, 3 can all be done in parallel
- Task 4 needs 1, 2, 3 to be merged first
- Task 5 needs 4
- Task 6 can be done alongside 4-5
- Task 7 after everything else
