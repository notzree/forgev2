# Forge

Forge is a platform for running AI coding agents in isolated Kubernetes containers. It provides stateless orchestration for [OpenCode](https://github.com/opencode-ai/opencode) agents, enabling remote agent execution with real-time streaming via webhooks.

## Architecture

```
┌─────────────────┐                 ┌─────────────────────────────────────────────────────────┐
│  Your App       │                 │                      FORGE                              │
│                 │   HTTP POST     │  ┌─────────────────┐      gRPC       ┌───────────────┐  │
│  - Stores msgs  │────────────────>│  │    Platform     │<───────────────>│  Agent (Pod)  │  │
│  - Business     │                 │  │     (Go)        │   Bidirectional │  (OpenCode)   │  │
│    logic        │   Webhook POST  │  └─────────────────┘     Stream      └───────────────┘  │
│  - User mgmt    │<────────────────│         │                                   │           │
│                 │  (async events) │         ▼                                   ▼           │
└─────────────────┘                 │  ┌─────────────────┐                 ┌───────────────┐  │
                                    │  │   Kubernetes    │                 │  Claude API   │  │
                                    │  └─────────────────┘                 └───────────────┘  │
                                    └─────────────────────────────────────────────────────────┘
```

**How it works:**
1. Your app sends a message to an agent via HTTP POST
2. Platform returns `202 Accepted` immediately
3. Platform streams the request to the agent pod via gRPC
4. Agent processes with Claude and streams events back
5. Platform delivers each event to your webhook URL
6. Final event includes `is_final: true`

## Project Structure

```
forge/
├── platform/                 # Go orchestration service (Echo, Uber Fx, Connect-RPC)
│   ├── cmd/server/           # Entry point
│   └── internal/
│       ├── agent/            # Agent lifecycle & messaging
│       ├── k8s/              # Kubernetes client
│       └── webhook/          # Webhook delivery
├── agent/claudecode/         # TypeScript agent (Bun, Fastify, OpenCode SDK)
│   └── src/
│       ├── services/         # gRPC handlers
│       └── opencode/         # OpenCode event handling
└── proto/agent/v1/           # Protocol Buffer definitions
```

## Getting Started

### Prerequisites

- Go 1.23+, Bun 1.0+, Docker
- Kubernetes cluster (or k3d for local dev)
- Anthropic API key

### Quick Start

```bash
# Clone and setup
git clone https://github.com/notzree/forge.git
cd forge
cp .env.example .env
# Add your ANTHROPIC_API_KEY to .env

# Install dependencies and start
just setup
just dev
```

This creates a k3d cluster, builds the agent image, and starts the platform.

## API Reference

### Create Agent

```bash
curl -X POST http://localhost:8080/api/v1/agents \
  -H "Content-Type: application/json" \
  -d '{"owner_id": "user123"}'
```

### List Agents

```bash
curl "http://localhost:8080/api/v1/agents?user_id=user123"
```

### Get Agent

```bash
curl "http://localhost:8080/api/v1/agents/{agent_id}?user_id=user123"
```

### Delete Agent

```bash
curl -X DELETE "http://localhost:8080/api/v1/agents/{agent_id}?user_id=user123"
```

### Send Message

```bash
curl -X POST "http://localhost:8080/api/v1/agents/{agent_id}/messages?user_id=user123" \
  -H "Content-Type: application/json" \
  -d '{
    "content": "Create a hello world Python script",
    "webhook_url": "https://your-app.com/webhook",
    "webhook_secret": "optional-hmac-secret"
  }'
```

**Response:** `202 Accepted`
```json
{"request_id": "req_abc123", "agent_id": "a1b2c3d4", "status": "processing"}
```

**Webhook payloads delivered to your endpoint:**
```json
{
  "event_type": "agent.event",
  "agent_id": "a1b2c3d4",
  "request_id": "req_abc123",
  "seq": 1,
  "is_final": false,
  "opencode_event_type": "message.part.updated",
  "event": { "type": "message.part.updated", "properties": { ... } }
}
```

Event types: `agent.event` (OpenCode events), `agent.error`, `agent.complete`

### Interrupt Agent

```bash
curl -X POST "http://localhost:8080/api/v1/agents/{agent_id}/interrupt?user_id=user123" \
  -H "Content-Type: application/json" \
  -d '{"webhook_url": "https://your-app.com/webhook"}'
```

## Design Decisions

### Why Webhooks?

| Webhooks | WebSockets |
|----------|------------|
| Platform stays stateless | Requires connection state |
| Built-in retry, `seq` for ordering | Lost messages on disconnect |
| Scales horizontally | Needs sticky sessions |
| Any HTTP endpoint works | Requires WS infrastructure |

Trade-off: Higher latency per event, but coding agents run for minutes anyway.

### Why Stateless?

- Your app owns the data (messages, history, users)
- Platform scales horizontally without shared state
- No database to operate

### Why One Pod Per Agent?

- Isolation: dedicated resources and filesystem per user
- Simplicity: no session multiplexing
- Security: environment isolation between users

Trade-off: Cold start latency when spinning up new pods.

## Current Limitations

| Feature | Status |
|---------|--------|
| File/image attachments | Not implemented |
| Session history retrieval | Not implemented |
| Permission request handling | Auto-approved (events streamed but no reply API) |
| Git authentication | Not implemented (use env vars as workaround) |
| API authentication | Not implemented |

## Development

```bash
just dev              # Start local k3d + platform
just dev-rebuild      # Rebuild agent image and restart
just run-platform     # Run platform only
just run-agent        # Run agent locally
just proto            # Generate protobuf code
just --list           # All commands
```

## License

MIT
