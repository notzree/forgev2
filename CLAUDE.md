# CLAUDE.md - AI Assistant Guide for Forge

This document provides comprehensive guidance for AI assistants working with the Forge codebase.

## Project Overview

**Forge** is a Claude Agent **infrastructure** platform that runs Claude agents in containerized environments. It provides stateless orchestration and acts as a "dumb pipe" between agents and external products/clients.

### Architecture Philosophy: Infrastructure vs Product

This repository contains only the **Infrastructure** layer:

| Layer | Responsibility | This Repo? |
|-------|---------------|------------|
| **Infrastructure** | Agent orchestration, pod lifecycle, command routing, output streaming | ✅ Yes |
| **Product** | Message persistence, business logic, user management, UI | ❌ No (external) |

**Key principles:**
- Infrastructure is **stateless** - it does NOT store message history
- Infrastructure is a **dumb pipe** - it routes commands to agents and streams outputs to products
- Products register webhooks to receive agent output streams
- Products are responsible for persistence and business logic

### High-Level Architecture

```
┌─────────────────┐                 ┌─────────────────────────────────────────────────────────┐
│     Product     │                 │                    INFRASTRUCTURE                        │
│   (external)    │                 │                     (this repo)                          │
│                 │   HTTP POST     │  ┌─────────────────┐      gRPC       ┌───────────────┐  │
│  - Stores msgs  │────────────────>│  │    Platform     │◄───────────────►│  Agent (Pod)  │  │
│  - Business     │                 │  │   (Go/Echo)     │                 │  (Bun/TS)     │  │
│    logic        │   Webhook POST  │  └─────────────────┘                 └───────────────┘  │
│  - User mgmt    │<────────────────│         │                                   │           │
│  - Webhook      │  (async events) │         ▼                                   ▼           │
│    endpoint     │                 │  ┌─────────────────┐                 ┌───────────────┐  │
└─────────────────┘                 │  │   Kubernetes    │                 │ Claude SDK    │  │
                                    │  │  (Pod Mgmt)     │                 │ (AI Runtime)  │  │
                                    │  └─────────────────┘                 └───────────────┘  │
                                    └─────────────────────────────────────────────────────────┘
```

### Data Flow

1. **Product → Infrastructure**: HTTP POST with command + webhook URL
2. **Infrastructure → Product**: Immediate 202 Accepted response
3. **Infrastructure → Agent**: Route command via gRPC bidirectional stream
4. **Agent → Infrastructure**: Stream responses (text, tool use, thinking, etc.)
5. **Infrastructure → Product**: Push each event to product's webhook URL (async)

### Webhook-Based Communication

The platform uses **webhooks** (not WebSockets) to deliver agent events to products:

**Why Webhooks?**
- **Stateless**: No persistent connections to manage, platform scales horizontally
- **Reliable**: Built-in retry logic, products can replay missed messages
- **Async-friendly**: Coding agents run long tasks; users disconnect/reconnect naturally
- **Simple**: Products just need an HTTP endpoint, not WebSocket infrastructure

**How it works:**
1. Product sends command with `webhook_url` parameter
2. Platform returns `202 Accepted` immediately
3. Platform streams events from agent via gRPC
4. Each event is POSTed to product's webhook URL
5. Events include `seq` numbers for ordering and `uuid` for idempotency

See [WEBHOOK.md](./WEBHOOK.md) for detailed implementation plan.

## Directory Structure

```
forgev2/
├── agent/                    # TypeScript agent service
│   └── claudecode/
│       ├── src/
│       │   ├── agent/        # Agent core logic & serialization
│       │   ├── services/     # gRPC server & handlers
│       │   ├── gen/          # Generated protobuf code (do not edit)
│       │   ├── config.ts     # Configuration loading
│       │   └── index.ts      # Main entry point
│       ├── Dockerfile        # Multi-stage Bun build
│       └── package.json
├── platform/                 # Go orchestration platform
│   ├── cmd/server/           # Server entrypoint
│   ├── internal/
│   │   ├── agent/            # Agent lifecycle management
│   │   │   ├── processor/    # Business logic for agent operations
│   │   │   └── handler/      # HTTP handlers for agent API
│   │   ├── handler/          # HTTP handlers (health, WebSocket)
│   │   ├── server/           # Echo setup, middleware, modules
│   │   ├── logger/           # Zap logger configuration
│   │   ├── errors/           # Custom error types
│   │   ├── k8s/              # Kubernetes client & pod management
│   │   └── config/           # Environment configuration
│   ├── gen/                  # Generated protobuf code (do not edit)
│   └── go.mod
├── proto/                    # Protocol Buffer definitions
│   ├── agent/v1/
│   │   ├── agent.proto       # Service definition & commands
│   │   └── messages.proto    # Message types
│   └── buf.yaml              # Buf configuration
├── Makefile                  # Build, run, deploy targets
├── buf.gen.yaml              # Code generation config
└── TASKS.md                  # Implementation tasks
```

## Technology Stack

| Component | Technology | Purpose |
|-----------|-----------|---------|
| Agent Runtime | TypeScript + Bun | Fast startup, Claude Agent SDK |
| Agent Server | Fastify + Connect-RPC | gRPC server for agent communication |
| Platform | Go 1.23+ | High-performance orchestration |
| Web Framework | Echo v4 | HTTP server with middleware |
| DI Framework | Uber Fx | Modular dependency injection |
| Logging | Zap | Structured logging |
| RPC | Connect-RPC/gRPC | Bidirectional streaming |
| API Contracts | Protocol Buffers v3 | Type-safe message definitions |
| Containers | Docker + Kubernetes | Agent isolation & orchestration |

**Note**: The platform does NOT use a database - it is stateless. Products are responsible for persistence.

## Development Commands

### Setup and Dependencies

```bash
make setup              # Install buf, go mod tidy, bun install
```

### Protocol Buffers

```bash
make proto              # Generate Go + TypeScript code from protos
make proto-lint         # Lint proto files
make proto-breaking     # Check for breaking changes against main
make clean              # Remove generated code
```

### Local Development

```bash
make run-agent          # Run agent: bun run src/index.ts
make run-platform       # Run platform: go run ./cmd/server
```

### Container Builds

```bash
make build-agent        # Build agent Docker image
make build-agent-sha    # Build with git SHA tag
make push-agent         # Push to container registry
make release-agent      # Build + push agent image
make registry-login     # Login to container registry
```

### Agent-specific Commands

```bash
cd agent/claudecode
bun install             # Install dependencies
bun run dev             # Dev mode with watch
bun run start           # Start agent
bun run typecheck       # TypeScript type checking
bun run build           # Build to dist/
```

### Testing

```bash
cd platform
go test ./...           # Run all Go tests
go test ./internal/k8s/... -v  # Run k8s tests with verbose output
```

## Code Conventions

### Go (Platform)

- **Dependency Injection**: Use Uber Fx for all services. Create `module.go` files that export `fx.Option`.
- **Handler Pattern**: All HTTP handlers implement a `Register(e *echo.Echo)` method.
- **Error Handling**: Use custom `AppError` from `internal/errors` with factory functions:
  - `errors.NotFound("message")`
  - `errors.BadRequest("message")`
  - `errors.InternalError("message")`
- **Configuration**: Environment variables parsed via `github.com/caarlos0/env/v11`.
- **Logging**: Use Zap logger injected via Fx. Structured fields preferred.
- **Concurrency**: Use `sync.RWMutex` for shared state. Use goroutines with `sync.WaitGroup` for cleanup.
- **Interfaces**: Use interfaces for testability (e.g., `kubernetes.Interface` instead of `*kubernetes.Clientset`).

### TypeScript (Agent)

- **Runtime**: Bun (not Node.js). Use Bun-specific APIs where appropriate.
- **Module System**: ESM (`"type": "module"` in package.json).
- **Strict Mode**: TypeScript strict mode enabled.
- **Config**: Load from environment variables in `config.ts`.
- **Generated Code**: Never edit files in `src/gen/` - regenerate with `make proto`.

### Protocol Buffers

- **Versioning**: Use `v1`, `v2` package suffixes for API versions.
- **Package Path**: Go package is `github.com/forge/platform/gen/agent/v1;agentv1`.
- **Naming**: Use snake_case for fields, PascalCase for messages.
- **Oneofs**: Use `oneof` for variant types (commands, events, content blocks).

## Key Patterns

### Bidirectional Streaming (gRPC)

The platform-to-agent communication uses bidirectional streaming:

```protobuf
service AgentService {
  rpc Connect(stream AgentCommand) returns (stream AgentEvent);
}
```

- Platform sends `AgentCommand` (SendMessage, Interrupt, SetModel, etc.)
- Agent responds with `AgentEvent` (Message, Ack, Error)
- Use `request_id` to correlate commands with responses

### Message Serialization

The agent converts between Claude SDK types and protobuf messages:

- `src/agent/serde.ts` handles SDK ↔ Proto conversion
- Messages have sequence numbers (`seq`) for ordering
- UUIDs for unique identification

### Stateless Webhook Delivery (Infrastructure Pattern)

The platform acts as a stateless router with webhook-based delivery:

1. Receives commands from products via HTTP POST (with webhook URL)
2. Returns `202 Accepted` immediately
3. Routes command to appropriate agent pod via gRPC
4. Streams agent output and POSTs each event to product's webhook
5. **Does NOT persist** any messages - products handle storage

Products are responsible for:
- Providing a webhook endpoint to receive events
- Storing messages for history/replay
- Handling user reconnection (query stored messages)
- Implementing real-time UI updates (SSE/WebSocket to their users)

### Pod Lifecycle Management

The `k8s.Manager` handles Kubernetes operations:

- `CreatePod` - Spawn new agent container
- `WaitForPodReady` - Block until pod is ready with IP assigned
- `GetPodAddress` - Get gRPC endpoint for agent
- `ClosePod` - Terminate agent container
- `WatchPod` - Monitor pod state changes

## Environment Variables

### Platform

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | HTTP server port |
| `DEBUG` | `false` | Enable debug mode |
| `CORS_ORIGINS` | - | Comma-separated allowed origins |
| `SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown timeout |
| `READ_TIMEOUT` | `10s` | HTTP read timeout |
| `WRITE_TIMEOUT` | `10s` | HTTP write timeout |
| `KUBE_CONFIG_PATH` | - | Path to kubeconfig file |
| `AGENT_NAMESPACE` | `default` | Kubernetes namespace for agent pods |
| `AGENT_IMAGE` | - | Docker image for agent containers |

### Agent

| Variable | Default | Description |
|----------|---------|-------------|
| `AGENT_ID` | (required) | Unique agent identifier |
| `PORT` | `8080` | gRPC server port |
| `AGENT_CWD` | `cwd()` | Working directory for agent |
| `CLAUDE_MODEL` | `claude-sonnet-4-20250514` | Claude model to use |
| `PERMISSION_MODE` | `acceptEdits` | Permission mode (default, acceptEdits, bypassPermissions) |
| `ALLOWED_TOOLS` | Read,Write,Edit,Bash,Glob,Grep,WebSearch,WebFetch | Comma-separated allowed tools |
| `ANTHROPIC_API_KEY` | - | Anthropic API key |

## API Contracts

### AgentService (gRPC)

```protobuf
service AgentService {
  // Bidirectional streaming for real-time communication
  rpc Connect(stream AgentCommand) returns (stream AgentEvent);

  // Get agent status
  rpc GetStatus(GetStatusRequest) returns (GetStatusResponse);

  // Graceful shutdown
  rpc Shutdown(ShutdownRequest) returns (ShutdownResponse);
}
```

### Command Types

- `SendMessageCommand` - Send user message to agent
- `InterruptCommand` - Interrupt current operation
- `SetPermissionModeCommand` - Change permission mode
- `SetModelCommand` - Change Claude model

### Message Types

- `UserMessage` - User input with optional attachments
- `AssistantMessage` - Agent response with content blocks
- `StreamEvent` - Real-time streaming updates
- `ResultMessage` - Final result with usage stats

### Content Block Types

- `TextBlock` - Plain text content
- `ToolUseBlock` - Tool invocation
- `ToolResultBlock` - Tool execution result
- `ThinkingBlock` - Extended thinking output
- `ImageBlock` - Image data

## Testing

### Go Tests

```bash
go test ./...                    # Run all tests
go test ./internal/k8s/... -v    # Run k8s tests verbose
go test -race ./...              # Run with race detector
```

The k8s package has comprehensive tests using `k8s.io/client-go/kubernetes/fake`.

### TypeScript Tests

```bash
cd agent/claudecode
bun test                         # Run all tests
```

## Common Tasks

### Adding a New API Endpoint (Platform)

1. Create handler in `platform/internal/handler/`
2. Implement `Register(e *echo.Echo)` method
3. Add to handler module in `platform/internal/handler/module.go`

### Adding a New Proto Message

1. Edit `proto/agent/v1/messages.proto` or `agent.proto`
2. Run `make proto` to regenerate code
3. Update serde.ts if SDK conversion needed

### Adding a New Agent Command

1. Add command to `AgentCommand.oneof` in `agent.proto`
2. Run `make proto`
3. Handle in agent's `src/services/agent-service.ts`
4. Update platform's handler if needed

## Troubleshooting

### Proto Generation Fails

```bash
make clean && make setup && make proto
```

### Agent Can't Connect to Platform

- Verify `AGENT_ID` is set
- Check gRPC port matches platform expectation
- Ensure network connectivity between containers

### Type Errors After Proto Changes

```bash
make clean && make proto
cd agent/claudecode && bun run typecheck
```

### K8s Tests Failing

Ensure you're using a compatible Go version (1.23+):
```bash
go version
GOTOOLCHAIN=local go test ./internal/k8s/...
```

## Key Files Reference

| Purpose | Go (Platform) | TypeScript (Agent) |
|---------|---------------|-------------------|
| Entry point | `cmd/server/main.go` | `src/index.ts` |
| Configuration | `internal/config/config.go` | `src/config.ts` |
| Agent processor | `internal/agent/processor/processor.go` | `src/agent/core.ts` |
| HTTP Handlers | `internal/handler/*.go` | - |
| Agent Handlers | `internal/agent/handler/*.go` | `src/services/*.ts` |
| K8s Management | `internal/k8s/client.go` | - |
| Proto types | `gen/agent/v1/*.go` | `src/gen/agent/v1/*.ts` |
| Middleware | `internal/server/middleware.go` | - |
| Errors | `internal/errors/errors.go` | - |
| Serialization | - | `src/agent/serde.ts` |
