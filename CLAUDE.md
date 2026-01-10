# CLAUDE.md - AI Assistant Guide for Forge

This document provides comprehensive guidance for AI assistants working with the Forge codebase.

## Project Overview

**Forge** is a Claude Agent platform that runs Claude agents in containerized environments with bidirectional communication between agents and a central control platform. The architecture follows a microservices pattern where the platform orchestrates agent containers running in Kubernetes.

### High-Level Architecture

```
┌─────────────────┐     WebSocket      ┌─────────────────┐      gRPC       ┌─────────────────┐
│   Web Client    │◄──────────────────►│    Platform     │◄───────────────►│  Agent (Pod)    │
│  (Browser UI)   │                    │   (Go/Echo)     │                 │  (Bun/TS)       │
└─────────────────┘                    └─────────────────┘                 └─────────────────┘
                                              │                                    │
                                              ▼                                    ▼
                                       ┌─────────────────┐                 ┌─────────────────┐
                                       │   Kubernetes    │                 │   Turso DB      │
                                       │  (Pod Mgmt)     │                 │  (State Store)  │
                                       └─────────────────┘                 └─────────────────┘
```

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
│   │   ├── agent/            # Agent registry & lifecycle management
│   │   ├── handler/          # HTTP handlers (WebSocket, REST)
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
└── IMPLEMENTATION_PLAN.md    # Architecture documentation
```

## Technology Stack

| Component | Technology | Purpose |
|-----------|-----------|---------|
| Agent Runtime | TypeScript + Bun | Fast startup, Claude Agent SDK |
| Agent Server | Fastify + Connect-RPC | gRPC server for agent communication |
| Platform | Go 1.25 | High-performance orchestration |
| Web Framework | Echo v4 | HTTP server with middleware |
| DI Framework | Uber Fx | Modular dependency injection |
| Logging | Zap | Structured logging |
| RPC | Connect-RPC/gRPC | Bidirectional streaming |
| API Contracts | Protocol Buffers v3 | Type-safe message definitions |
| Containers | Docker + Kubernetes | Agent isolation & orchestration |
| Database | Turso (libsql) | State persistence |

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

# Database
make db-start           # Start PostgreSQL container
make db-stop            # Stop PostgreSQL container
make db-reset           # Reset database (removes all data)
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

### WebSocket-to-gRPC Bridge

The platform translates WebSocket messages from web clients to gRPC calls:

- `internal/handler/websocket.go` handles the protocol translation
- Bidirectional goroutines for send/receive
- Clean shutdown coordination

### Registry Pattern

Agent instances are tracked in an in-memory registry:

- `internal/agent/registry.go` - Thread-safe agent tracking
- `internal/agent/manager.go` - Agent lifecycle (create, connect, delete)

## Environment Variables

### Platform

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | HTTP server port |
| `DEBUG` | `false` | Enable debug mode |
| `CORS_ORIGINS` | - | Comma-separated allowed origins |
| `TURSO_URL` | - | Turso database URL |
| `TURSO_AUTH_TOKEN` | - | Turso authentication token |
| `SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown timeout |
| `READ_TIMEOUT` | `10s` | HTTP read timeout |
| `WRITE_TIMEOUT` | `10s` | HTTP write timeout |

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
| `TURSO_URL` | - | Turso database URL |
| `TURSO_AUTH_TOKEN` | - | Turso auth token |

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

Currently, the project does not have a formal test suite. When adding tests:

- **Go**: Use standard `go test` with table-driven tests. Place in `*_test.go` files.
- **TypeScript**: Use Bun's built-in test runner (`bun test`).

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
4. Update platform's WebSocket handler if needed

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

## Key Files Reference

| Purpose | Go (Platform) | TypeScript (Agent) |
|---------|---------------|-------------------|
| Entry point | `cmd/server/main.go` | `src/index.ts` |
| Configuration | `internal/config/config.go` | `src/config.ts` |
| Agent logic | `internal/agent/manager.go` | `src/agent/core.ts` |
| Handlers | `internal/handler/*.go` | `src/services/*.ts` |
| Proto types | `gen/agent/v1/*.go` | `src/gen/agent/v1/*.ts` |
| Middleware | `internal/server/middleware.go` | - |
| Errors | `internal/errors/errors.go` | - |
| Serialization | - | `src/agent/serde.ts` |
