# Platform ↔ Agent RPC Integration Tasks

This document defines the tasks required to enable the platform to communicate with claudecode agents running in Kubernetes via ConnectRPC.

## Architecture Overview

**Important**: This repository is the **Infrastructure** layer only. It is stateless and acts as a "dumb pipe" between external Products and Agents. See CLAUDE.md for full architecture details.

```
                                    ┌─────────────────────────────────────────────────────────┐
                                    │                    INFRASTRUCTURE                        │
                                    │                     (this repo)                          │
┌─────────────────┐                 │  ┌─────────────────┐      gRPC       ┌───────────────┐  │
│     Product     │  HTTP/WebSocket │  │    Platform     │◄───────────────►│  Agent (Pod)  │  │
│   (external)    │◄───────────────►│  │   (Go/Echo)     │                 │  (Bun/TS)     │  │
│                 │                 │  └─────────────────┘                 └───────────────┘  │
│  - Stores msgs  │   Commands ──►  │         │                                   │           │
│  - Business     │   ◄── Stream    │         ▼                                   ▼           │
│    logic        │                 │  ┌─────────────────┐                 ┌───────────────┐  │
│  - User mgmt    │                 │  │   Kubernetes    │                 │ Claude SDK    │  │
└─────────────────┘                 │  │  (Pod Mgmt)     │                 │ (AI Runtime)  │  │
                                    │  └─────────────────┘                 └───────────────┘  │
                                    └─────────────────────────────────────────────────────────┘
```

**Key Principles:**
- Infrastructure is **stateless** - does NOT store message history
- Infrastructure is a **dumb pipe** - routes commands and streams output
- Products register webhooks/WebSocket to receive agent output streams
- Kubernetes is the source of truth for agent state (no in-memory registry)

---

## Task Status Summary

| Task | Status | Description |
|------|--------|-------------|
| Task 1 | ✅ Complete | GetPodAddress - Pod address resolution |
| Task 2 | ✅ Complete | WaitForPodReady - Pod readiness waiting |
| Task 3 | ✅ Complete | Agent Client Factory |
| Task 4 | ✅ Complete | Processor refactor with K8s-based discovery |
| Task 5 | 🟡 Stubbed | Handler updates (needs full implementation) |
| Task 6 | 🟡 Partial | Cleanup (registry removed, fx wiring needs verification) |
| Task 7 | ❌ Not Started | Integration tests |

---

## Task 1: Add Pod Address Resolution to K8s Manager ✅ COMPLETE

**File**: `platform/internal/k8s/client.go`

**Status**: Implemented and tested.

### Implementation

```go
// GetPodAddress returns the ConnectRPC base URL for the given pod.
// Returns an error if the pod doesn't exist or doesn't have an IP assigned.
func (m *Manager) GetPodAddress(ctx context.Context, podID PodID) (string, error)
```

Returns formatted address: `http://<podIP>:8080`

---

## Task 2: Add Pod Readiness Waiting to K8s Manager ✅ COMPLETE

**File**: `platform/internal/k8s/client.go`

**Status**: Implemented with tests in `client_test.go`.

### Implementation

```go
// WaitForPodReady blocks until the pod is in Ready condition or the context is cancelled.
// Returns the pod with its assigned IP address once ready.
func (m *Manager) WaitForPodReady(ctx context.Context, podID PodID) (*corev1.Pod, error)

// isPodReady returns true if the pod is running, has an IP, and all containers are ready
func isPodReady(pod *corev1.Pod) bool
```

### Key Features
- Initial check before watching (returns immediately if already ready)
- Uses cancellable child context for proper cleanup of WatchPod goroutine
- Handles pod deletion, context cancellation, and watch errors
- 12 unit tests covering all scenarios

---

## Task 3: Create Agent Client Factory ✅ COMPLETE

**File**: `platform/internal/agent/client.go`

**Status**: Implemented with tests in `client_test.go`.

### Implementation

```go
package agent

import (
    "net/http"
    "github.com/forge/platform/gen/agent/v1/agentv1connect"
)

// NewClient creates a new AgentService client for the given base URL.
// The baseURL should be in the format "http://<ip>:8080".
// Clients are stateless and safe to create per-request.
// Uses the Connect protocol (HTTP/1.1 compatible, human-readable).
func NewClient(baseURL string) agentv1connect.AgentServiceClient {
    return agentv1connect.NewAgentServiceClient(
        http.DefaultClient,
        baseURL,
    )
}

// NewClientWithHTTPClient creates a new AgentService client with a custom HTTP client.
// Useful for testing or when custom transport configuration is needed.
func NewClientWithHTTPClient(baseURL string, httpClient *http.Client) agentv1connect.AgentServiceClient {
    return agentv1connect.NewAgentServiceClient(
        httpClient,
        baseURL,
    )
}
```

### Key Features

- Two factory functions: `NewClient` (simple) and `NewClientWithHTTPClient` (for testing/custom configs)
- Uses Connect protocol (not gRPC) for HTTP/1.1 compatibility
- Stateless - clients are cheap to create per-request
- No struct needed - just simple factory functions

### Tests

- `TestNewClient_ReturnsValidClient` - Basic smoke test
- `TestNewClient_WithDifferentURLFormats` - URL format variations (port, localhost, DNS name)
- `TestNewClientWithHTTPClient_UsesProvidedClient` - Custom HTTP client injection
- `TestNewClient_CanCallGetStatus` - Integration test with mock server
- `TestNewClient_CanCallShutdown` - Integration test with mock server

---

## Task 4: Refactor Processor to Use K8s-Based Discovery ✅ COMPLETE

**File**: `platform/internal/agent/processor/processor.go`

**Status**: Implemented and tested.

### Implementation

All methods have been implemented with full K8s-based discovery and RPC support:

```go
type Processor struct {
    k8m *k8s.Manager
}

// Lifecycle methods
func (p *Processor) CreateAgent(ctx context.Context, userID string) (*k8s.PodID, error)
func (p *Processor) DeleteAgent(ctx context.Context, userID, agentID string, graceful bool) error

// Query methods
func (p *Processor) ListAgents(ctx context.Context, userID string) ([]k8s.PodID, error)
func (p *Processor) GetAgent(ctx context.Context, userID, agentID string) (*corev1.Pod, error)

// RPC methods
func (p *Processor) GetStatus(ctx context.Context, userID, agentID string) (*agentv1.GetStatusResponse, error)
func (p *Processor) ConnectToAgent(ctx context.Context, userID, agentID string) (*connect.BidiStreamForClient[agentv1.AgentCommand, agentv1.AgentEvent], error)
```

### Key Features

- **CreateAgent**: Creates pod and waits for it to become ready via `WaitForPodReady`. Includes rollback (pod cleanup) if waiting fails.
- **DeleteAgent**: Supports graceful shutdown via RPC `Shutdown` call before deleting pod. Graceful errors are ignored (agent may be unreachable).
- **ListAgents**: Lists all agent pods for a user by extracting PodIDs from pod labels.
- **GetAgent**: Returns full pod details from K8s.
- **GetStatus**: Gets real-time agent status via RPC call.
- **ConnectToAgent**: Establishes bidirectional streaming connection to agent.

### Tests

Comprehensive unit tests in `processor_test.go` covering:

- ListAgents: empty list, single agent, multiple agents (filtering by user)
- GetAgent: found, not found
- GetStatus: agent not found, agent not ready (no IP)
- DeleteAgent: force delete, not found, graceful with unreachable agent
- CreateAgent: success (with watch simulation), context cancellation (with cleanup verification)
- ConnectToAgent: agent not found, agent not ready, returns stream

### Handler Update

The handler's `Delete` endpoint was updated to support the new `graceful` parameter:

```go
// DELETE /api/v1/agents/:id?user_id=xxx&graceful=true
graceful := c.QueryParam("graceful") == "true"
h.processor.DeleteAgent(ctx, userID, agentID, graceful)
```

### Helper Added

Added `NewManagerWithClientset` to `k8s/client.go` for testing with fake clientsets.

---

## Task 5: Update Handler to Match New Processor Interface 🟡 STUBBED

**File**: `platform/internal/agent/handler/handler.go`

**Status**: Currently stubbed with basic structure. Needs full implementation.

### Current State (Stubbed)

- `Create` - Works but response is minimal
- `List` - Returns empty list (TODO)
- `Get` - Returns "not implemented" (TODO)
- `Delete` - Works but requires `user_id` query param

### Changes Required

1. **Update `AgentResponse`**: Change to match what we can get from K8s:
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

2. **Implement `Get` handler**:
   - Parse userID from query param or auth context
   - Get agentID from path param
   - Call `processor.GetAgent(ctx, userID, agentID)`
   - Optionally call `processor.GetStatus()` if `?refresh=true`

3. **Implement `List` handler**:
   - Call `processor.ListAgents(ctx, userID)`
   - Convert to response format

4. **Enhance `Create` handler**:
   - Return full `AgentResponse` with pod info

5. **Enhance `Delete` handler**:
   - Add `graceful` query param support
   - Pass graceful flag to processor

### Considerations

- The handler needs access to userID - this should come from auth middleware eventually
- For now, require `user_id` as a query parameter or in request body
- Consider adding a `/api/v1/users/:user_id/agents` route structure for clearer REST semantics

---

## Task 6: Clean Up Deleted/Orphaned Code 🟡 PARTIAL

**Status**: Registry references removed. Fx wiring needs verification.

### Completed

- [x] `agent/module.go` - `NewRegistry` commented out
- [x] `handler/health.go` - Registry dependency removed
- [x] `processor/processor.go` - Removed broken registry references

### Remaining

- [ ] Verify fx wiring is correct:
  - `processor.Module` should provide `NewProcessor`
  - `handler.Module` should provide handler
  - Ensure `k8s.Manager` is provided and injected into Processor
- [ ] Check if `agent/module.go` should be removed entirely (currently empty)
- [ ] Check for any remaining orphaned types in agent package

### Verification

```bash
go build ./...  # Ensure no compilation errors
go vet ./...    # Catch issues
```

---

## Task 7: Add Integration Test for Full Flow ❌ NOT STARTED

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

---

## Recommended Task Order

```
                    ┌──────────────────────┐
                    │ Task 3: Client       │
                    │ Factory ✅ COMPLETE  │
                    └──────────┬───────────┘
                               │
                               ▼
┌──────────────────────────────────────────────────────┐
│ Task 4: Processor ✅ COMPLETE                        │
└──────────────────────┬───────────────────────────────┘
                       │
                       ▼
┌──────────────────────────────────────────────────────┐
│ Task 5: Handler (enhance stubbed implementation)     │  ◄── NEXT
└──────────────────────┬───────────────────────────────┘
                       │
                       ▼
┌──────────────────────────────────────────────────────┐
│ Task 6: Cleanup (verify fx wiring)                   │
└──────────────────────┬───────────────────────────────┘
                       │
                       ▼
┌──────────────────────────────────────────────────────┐
│ Task 7: Integration Tests                            │
└──────────────────────────────────────────────────────┘
```

**Note**: Tasks 1, 2, 3, and 4 are complete. The next step is Task 5 (Handler updates), which should use the new processor methods to implement List, Get, and enhance Create/Delete endpoints.
