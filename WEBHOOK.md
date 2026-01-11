# Webhook Implementation Plan

This document outlines the implementation plan for webhook-based event delivery from the Forge platform to products, with forgecoder as the reference product implementation.

## Overview

The platform delivers agent events to products via HTTP webhooks instead of maintaining persistent WebSocket connections. This approach aligns with the "stateless infrastructure" philosophy and scales better for long-running coding agent tasks.

**Reference Implementation:** The `forgecoder` Next.js application serves as both the test consumer and reference product implementation.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────────────────────┐
│                                       FORGECODER (Product)                                   │
│                                     Next.js + Prisma + Supabase                              │
│                                                                                              │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐    ┌──────────────┐              │
│  │   Dashboard  │    │  Agent Page  │    │  API Routes  │    │   Webhook    │              │
│  │   /dashboard │◄──►│  /agent/[id] │◄──►│  /api/...    │◄───│   Endpoint   │              │
│  └──────────────┘    └──────────────┘    └──────────────┘    └──────┬───────┘              │
│         │                   │                   │                    │                      │
│         └───────────────────┴───────────────────┴────────────────────┤                      │
│                                     │                                │                      │
│                              ┌──────▼──────┐                         │                      │
│                              │   Prisma    │                         │                      │
│                              │   (v7 ORM)  │                         │                      │
│                              └──────┬──────┘                         │                      │
│                                     │                                │                      │
│                              ┌──────▼──────┐                         │                      │
│                              │  Supabase   │                         │                      │
│                              │  (Postgres) │                         │                      │
│                              └─────────────┘                         │                      │
└──────────────────────────────────────────────────────────────────────┼──────────────────────┘
                                                                       │
                      HTTP POST with webhook_url                       │ Webhook POST
                              │                                        │ (async events)
                              ▼                                        │
┌─────────────────────────────────────────────────────────────────────────────────────────────┐
│                                    FORGE PLATFORM (Infrastructure)                           │
│                                        Go + Echo + sqlc/goose                                │
│                                                                                              │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐    ┌──────────────┐              │
│  │   HTTP API   │───►│  Processor   │───►│   Webhook    │───►│   Delivery   │──────────────┼─┘
│  │   Handlers   │    │   (routing)  │    │   Service    │    │   (retries)  │              │
│  └──────────────┘    └──────────────┘    └──────────────┘    └──────────────┘              │
│         │                   │                                                               │
│         │            ┌──────▼──────┐                                                        │
│         │            │    gRPC     │                                                        │
│         │            │   Connect   │                                                        │
│         │            └──────┬──────┘                                                        │
│         │                   │                                                               │
│  ┌──────▼──────┐     ┌──────▼──────┐                                                        │
│  │  Postgres   │     │    K8s      │                                                        │
│  │ (sqlc/goose)│     │  Agent Pod  │                                                        │
│  └─────────────┘     └─────────────┘                                                        │
└─────────────────────────────────────────────────────────────────────────────────────────────┘
```

### Data Flow

```
Forgecoder                      Platform                         Agent Pod
   │                               │                                │
   │ POST /api/v1/agents/:id/messages                               │
   │ { content, webhook_url }      │                                │
   │──────────────────────────────>│                                │
   │                               │                                │
   │ 202 Accepted                  │ Store request in DB            │
   │ { request_id }                │ (for delivery tracking)        │
   │<──────────────────────────────│                                │
   │                               │  gRPC: SendMessageCommand      │
   │                               │───────────────────────────────>│
   │                               │                                │
   │                               │        (Claude SDK processing) │
   │                               │                                │
   │                               │  gRPC: AgentEvent (stream)     │
   │   POST /api/webhooks/forge    │<───────────────────────────────│
   │   { event: StreamEvent }      │                                │
   │<──────────────────────────────│                                │
   │                               │                                │
   │   (store in Prisma DB)        │  gRPC: AgentEvent (stream)     │
   │   (push via SSE to browser)   │<───────────────────────────────│
   │<──────────────────────────────│                                │
   │                               │                                │
   │   POST /api/webhooks/forge    │  gRPC: AgentEvent (final)      │
   │   { event: ResultMessage }    │<───────────────────────────────│
   │<──────────────────────────────│                                │
```

## API Design

### Platform API (forgev2)

#### Send Message Endpoint

```
POST /api/v1/agents/:id/messages
```

**Request:**
```json
{
  "content": "Help me refactor the authentication module",
  "webhook_url": "https://forgecoder.example.com/api/webhooks/forge",
  "webhook_secret": "optional-hmac-secret",
  "request_id": "optional-client-provided-id"
}
```

**Response (202 Accepted):**
```json
{
  "request_id": "req_abc123",
  "agent_id": "agent-1234567890",
  "status": "processing"
}
```

**Error Responses:**
- `400 Bad Request` - Missing content or webhook_url
- `404 Not Found` - Agent not found
- `503 Service Unavailable` - Agent not ready

#### Interrupt Endpoint

```
POST /api/v1/agents/:id/interrupt
```

**Request:**
```json
{
  "webhook_url": "https://forgecoder.example.com/api/webhooks/forge",
  "request_id": "optional-client-provided-id"
}
```

**Response (202 Accepted):**
```json
{
  "request_id": "req_xyz789",
  "agent_id": "agent-1234567890",
  "status": "interrupting"
}
```

## Webhook Payload Format

### Common Envelope

All webhook deliveries use this envelope:

```json
{
  "event_type": "agent.message" | "agent.stream" | "agent.result" | "agent.error",
  "agent_id": "agent-1234567890",
  "request_id": "req_abc123",
  "seq": 42,
  "timestamp": "2025-01-10T12:34:56.789Z",
  "payload": { ... }
}
```

### Event Types

#### `agent.stream` - Real-time streaming updates

```json
{
  "event_type": "agent.stream",
  "agent_id": "agent-1234567890",
  "request_id": "req_abc123",
  "seq": 1,
  "timestamp": "2025-01-10T12:34:56.789Z",
  "payload": {
    "session_id": "session_xyz",
    "content_block": {
      "type": "text",
      "text": "I'll help you refactor..."
    }
  }
}
```

#### `agent.message` - Complete assistant message

```json
{
  "event_type": "agent.message",
  "agent_id": "agent-1234567890",
  "request_id": "req_abc123",
  "seq": 15,
  "timestamp": "2025-01-10T12:34:58.123Z",
  "payload": {
    "uuid": "msg_abc123",
    "session_id": "session_xyz",
    "role": "assistant",
    "content_blocks": [
      {
        "type": "text",
        "text": "I've analyzed the authentication module..."
      },
      {
        "type": "tool_use",
        "id": "tool_123",
        "name": "Read",
        "input": { "file_path": "/src/auth/login.ts" }
      }
    ]
  }
}
```

#### `agent.result` - Final result with usage stats

```json
{
  "event_type": "agent.result",
  "agent_id": "agent-1234567890",
  "request_id": "req_abc123",
  "seq": 50,
  "timestamp": "2025-01-10T12:35:30.456Z",
  "is_final": true,
  "payload": {
    "session_id": "session_xyz",
    "status": "completed",
    "usage": {
      "input_tokens": 1500,
      "output_tokens": 3200,
      "cache_read_tokens": 500,
      "cache_write_tokens": 200
    },
    "cost_usd": 0.0234,
    "duration_ms": 34567
  }
}
```

#### `agent.error` - Error event

```json
{
  "event_type": "agent.error",
  "agent_id": "agent-1234567890",
  "request_id": "req_abc123",
  "seq": 51,
  "timestamp": "2025-01-10T12:35:31.000Z",
  "is_final": true,
  "payload": {
    "error_code": "AGENT_TIMEOUT",
    "message": "Agent did not respond within 5 minutes",
    "recoverable": false
  }
}
```

## Webhook Delivery

### Security

**HMAC Signature (optional but recommended):**

If `webhook_secret` is provided, all webhook requests include:

```
X-Forge-Signature: sha256=<hmac-sha256-of-body>
X-Forge-Timestamp: 1704891234
```

Products should verify:
1. Timestamp is within 5 minutes of current time
2. HMAC matches: `HMAC-SHA256(timestamp + "." + body, secret)`

### Retry Policy

| Attempt | Delay | Total Time |
|---------|-------|------------|
| 1 | 0s | 0s |
| 2 | 1s | 1s |
| 3 | 5s | 6s |
| 4 | 30s | 36s |
| 5 | 60s | 96s |

**Retry conditions:**
- HTTP 5xx responses
- Connection timeouts (10s)
- Connection refused

**No retry:**
- HTTP 2xx (success)
- HTTP 4xx (client error - product's problem)

### Timeout & Circuit Breaking

- Individual webhook timeout: 10 seconds
- If 5 consecutive failures: circuit opens for 60 seconds
- During circuit open: events are dropped with warning log
- After circuit timeout: half-open state, try one request

### Idempotency

Products should handle duplicate deliveries using:
- `request_id` + `seq` as idempotency key
- Store processed event IDs and skip duplicates

---

## Database Schemas

### Platform Database (forgev2 - Postgres via sqlc/goose)

The platform database tracks webhook delivery state and request metadata. It does NOT store message content (that's the product's job).

**Note:** Agent/pod state is managed by Kubernetes directly - no `agent_sessions` table needed. The platform queries K8s for pod IP/status when routing requests. Agents can handle multiple concurrent requests; future work will add request options (queue, interrupt current execution, etc.).

**File:** `platform/internal/db/migrations/001_webhook_deliveries.sql`

```sql
-- +goose Up

-- Webhook delivery tracking for retry and circuit breaker logic
CREATE TABLE webhook_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    webhook_url TEXT NOT NULL,
    webhook_secret_hash TEXT, -- SHA256 of secret for verification

    -- Delivery state
    seq BIGINT NOT NULL DEFAULT 0,
    last_event_type TEXT,
    status TEXT NOT NULL DEFAULT 'pending', -- pending, delivering, completed, failed

    -- Retry tracking
    attempt_count INT NOT NULL DEFAULT 0,
    last_attempt_at TIMESTAMPTZ,
    next_retry_at TIMESTAMPTZ,
    last_error TEXT,

    -- Circuit breaker state (per webhook_url)
    consecutive_failures INT NOT NULL DEFAULT 0,
    circuit_open_until TIMESTAMPTZ,

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,

    UNIQUE(request_id)
);

CREATE INDEX idx_webhook_deliveries_agent ON webhook_deliveries(agent_id);
CREATE INDEX idx_webhook_deliveries_status ON webhook_deliveries(status) WHERE status != 'completed';
CREATE INDEX idx_webhook_deliveries_retry ON webhook_deliveries(next_retry_at) WHERE status = 'pending';

-- +goose Down
DROP TABLE IF EXISTS webhook_deliveries;
```

**File:** `platform/internal/sqlc/queries/webhook.sql`

```sql
-- name: CreateWebhookDelivery :one
INSERT INTO webhook_deliveries (
    request_id, agent_id, webhook_url, webhook_secret_hash
) VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetWebhookDelivery :one
SELECT * FROM webhook_deliveries WHERE request_id = $1;

-- name: UpdateDeliverySeq :exec
UPDATE webhook_deliveries
SET seq = $2, last_event_type = $3, updated_at = NOW()
WHERE request_id = $1;

-- name: MarkDeliveryCompleted :exec
UPDATE webhook_deliveries
SET status = 'completed', completed_at = NOW(), updated_at = NOW()
WHERE request_id = $1;

-- name: RecordDeliveryFailure :exec
UPDATE webhook_deliveries
SET attempt_count = attempt_count + 1,
    last_attempt_at = NOW(),
    last_error = $2,
    consecutive_failures = consecutive_failures + 1,
    next_retry_at = $3,
    updated_at = NOW()
WHERE request_id = $1;

-- name: ResetConsecutiveFailures :exec
UPDATE webhook_deliveries
SET consecutive_failures = 0, updated_at = NOW()
WHERE request_id = $1;

-- name: OpenCircuit :exec
UPDATE webhook_deliveries
SET circuit_open_until = $2, updated_at = NOW()
WHERE webhook_url = $1 AND circuit_open_until IS NULL;

-- name: GetPendingRetries :many
SELECT * FROM webhook_deliveries
WHERE status = 'pending'
  AND next_retry_at <= NOW()
  AND (circuit_open_until IS NULL OR circuit_open_until <= NOW())
ORDER BY next_retry_at
LIMIT $1;
```

### Forgecoder Database (Prisma v7 on Supabase)

**File:** `forgecoder/prisma/schema.prisma`

```prisma
generator client {
  provider = "prisma-client-js"
}

datasource db {
  provider  = "postgresql"
  url       = env("DATABASE_URL")
  directUrl = env("DIRECT_URL")
}

// Agent configuration templates
model AgentConfig {
  id          String   @id @default(cuid())
  name        String
  description String?

  // Claude settings
  model          String @default("claude-sonnet-4-20250514")
  permissionMode String @default("acceptEdits") @map("permission_mode")

  // Tool settings
  allowedTools String[] @default(["Read", "Write", "Edit", "Bash", "Glob", "Grep"]) @map("allowed_tools")

  // Network settings
  allowOutbound   Boolean  @default(true) @map("allow_outbound")
  allowedDomains  String[] @default([]) @map("allowed_domains")

  // Repository settings
  defaultBranch    String? @map("default_branch")
  autoCommit       Boolean @default(false) @map("auto_commit")
  commitTemplate   String? @map("commit_template")

  createdAt DateTime @default(now()) @map("created_at")
  updatedAt DateTime @updatedAt @map("updated_at")

  agents Agent[]

  @@map("agent_configs")
}

// Agent instances
model Agent {
  id       String @id // Matches platform agent_id
  configId String @map("config_id")

  // Platform-provided info
  podName String? @map("pod_name")
  podIp   String? @map("pod_ip")

  // State tracking
  phase    String @default("Pending") // Pending, Running, Succeeded, Failed, Unknown
  state    String @default("IDLE")    // IDLE, PROCESSING, ERROR
  ready    Boolean @default(false)
  archived Boolean @default(false)

  // Timestamps
  createdAt    DateTime  @default(now()) @map("created_at")
  updatedAt    DateTime  @updatedAt @map("updated_at")
  lastActiveAt DateTime? @map("last_active_at")

  config   AgentConfig @relation(fields: [configId], references: [id])
  sessions Session[]

  @@map("agents")
}

// Conversation sessions
model Session {
  id        String @id @default(cuid())
  agentId   String @map("agent_id")

  // Session state
  status    String @default("active") // active, completed, interrupted, error

  // Usage tracking
  inputTokens      Int @default(0) @map("input_tokens")
  outputTokens     Int @default(0) @map("output_tokens")
  cacheReadTokens  Int @default(0) @map("cache_read_tokens")
  cacheWriteTokens Int @default(0) @map("cache_write_tokens")
  costUsd          Decimal @default(0) @map("cost_usd") @db.Decimal(10, 6)
  durationMs       Int @default(0) @map("duration_ms")

  // Timestamps
  createdAt   DateTime  @default(now()) @map("created_at")
  updatedAt   DateTime  @updatedAt @map("updated_at")
  completedAt DateTime? @map("completed_at")

  agent    Agent     @relation(fields: [agentId], references: [id], onDelete: Cascade)
  messages Message[]
  requests Request[]

  @@map("sessions")
}

// Individual messages in a session
model Message {
  id        String @id @default(cuid())
  sessionId String @map("session_id")

  // Message identity from platform
  uuid String @unique // Platform-provided UUID
  seq  Int            // Sequence number for ordering

  role String // user, assistant, system

  // Content stored as JSON (array of content blocks)
  content Json

  // Timestamps
  createdAt DateTime @default(now()) @map("created_at")

  session Session @relation(fields: [sessionId], references: [id], onDelete: Cascade)

  @@index([sessionId, seq])
  @@map("messages")
}

// Platform requests (for idempotency and tracking)
model Request {
  id        String @id // Platform request_id
  sessionId String @map("session_id")

  // Request content
  content String

  // State
  status       String @default("pending") // pending, processing, completed, error
  lastSeq      Int    @default(0) @map("last_seq")
  lastEventType String? @map("last_event_type")

  // Error tracking
  errorCode    String? @map("error_code")
  errorMessage String? @map("error_message")

  // Timestamps
  createdAt   DateTime  @default(now()) @map("created_at")
  updatedAt   DateTime  @updatedAt @map("updated_at")
  completedAt DateTime? @map("completed_at")

  session Session @relation(fields: [sessionId], references: [id], onDelete: Cascade)

  @@map("requests")
}

// Processed webhook events (for idempotency)
model ProcessedEvent {
  id        String   @id // Format: "{request_id}:{seq}"
  processedAt DateTime @default(now()) @map("processed_at")

  @@map("processed_events")
}
```

---

## Implementation Tasks

### Phase 1: Platform Infrastructure (forgev2) ✅ COMPLETED

#### Task 1.1: Database Setup with goose/sqlc ✅

**Status:** COMPLETED

**Files Created:**
- `platform/internal/sqlc/migrations/00001_init.sql` - Migration with `webhook_deliveries` table
- `platform/internal/sqlc/queries/webhook.sql` - All CRUD queries
- `platform/sqlc.yaml` - sqlc configuration
- `platform/internal/db/db.go` - Connection pool with Fx lifecycle

**Implementation Details:**
```yaml
# platform/sqlc.yaml
version: "2"
sql:
  - schema: "internal/sqlc/migrations"
    queries: "internal/sqlc/queries"
    engine: "postgresql"
    gen:
      go:
        package: "sqlc"
        out: "internal/sqlc/gen"
        sql_package: "pgx/v5"
```

#### Task 1.2: Webhook Delivery Service ✅

**Status:** COMPLETED

**Files Created:**
- `platform/internal/webhook/types.go` - Payload types and event constants
- `platform/internal/webhook/delivery.go` - HTTP delivery with retries and circuit breaker
- `platform/internal/webhook/module.go` - Fx dependency injection module

**Implementation Details:**
```go
type DeliveryService struct {
    client        *http.Client
    logger        *zap.Logger
    queries       *sqlc.Queries
    pool          *pgxpool.Pool
    cfg           *config.Config
    circuitMu     sync.RWMutex
    circuitStates map[string]*circuitState  // Per-URL circuit breaker state
}

// Features implemented:
// - HMAC-SHA256 signing (X-Forge-Signature header)
// - Retry with exponential backoff (up to 3 attempts)
// - Circuit breaker (5 failures opens for 60s)
// - Async delivery via DeliverAsync()
```

#### Task 1.3: Message Handler ✅

**Status:** COMPLETED

**File Created:** `platform/internal/agent/handler/messages.go`

**Implementation Details:**
```go
// POST /api/v1/agents/:id/messages?user_id=xxx
func (h *Handler) SendMessage(c echo.Context) error
// - Validates content and webhook_url required
// - Returns 202 Accepted immediately
// - Spawns goroutine to process via SendMessageWithWebhook

// POST /api/v1/agents/:id/interrupt?user_id=xxx
func (h *Handler) Interrupt(c echo.Context) error
// - Validates webhook_url required
// - Returns 202 Accepted immediately
// - Spawns goroutine to process via InterruptWithWebhook
```

#### Task 1.4: Stream-to-Webhook Bridge ✅

**Status:** COMPLETED

**File Modified:** `platform/internal/agent/processor/processor.go`

**Implementation Details:**
```go
// SendMessageWithWebhook sends a message to agent and delivers responses via webhook
func (p *Processor) SendMessageWithWebhook(ctx, userID, agentID, requestID, content, webhookCfg) error

// InterruptWithWebhook interrupts an agent and delivers response via webhook
func (p *Processor) InterruptWithWebhook(ctx, userID, agentID, requestID, webhookCfg) error

// streamToWebhook reads from agent gRPC stream and delivers events to webhook
func (p *Processor) streamToWebhook(ctx, stream, agentID, requestID, webhookCfg) error
// - Creates delivery record in DB
// - Loops reading from gRPC stream
// - Converts each event to webhook payload
// - Delivers via webhook service
// - Marks delivery completed/failed
```

#### Task 1.5: Proto-to-Webhook Conversion ✅

**Status:** COMPLETED

**File Created:** `platform/internal/webhook/convert.go`

**Implementation Details:**
```go
// AgentEventToPayload converts protobuf AgentEvent to webhook Payload
func AgentEventToPayload(event *agentv1.AgentEvent, msg *agentv1.Message, agentID, requestID string) Payload

// Supported message types:
// - StreamEvent → EventTypeStream
// - AssistantMessage → EventTypeMessage (with content blocks)
// - ResultMessage → EventTypeResult (with usage stats, cost, duration)
// - UserMessage → EventTypeMessage
// - SystemMessage → EventTypeMessage

// Content block types:
// - TextBlock, ToolUseBlock, ToolResultBlock, ThinkingBlock, ImageBlock

// ErrorToPayload creates error webhook payloads
func ErrorToPayload(agentID, requestID string, seq int64, errorCode, message string, recoverable bool) Payload
```

### Phase 2: Forgecoder Product Implementation ⏳ PENDING

#### Task 2.1: Prisma Setup ⏳

**Commands:**
```bash
cd forgecoder
npm install prisma@latest @prisma/client@latest
npx prisma init
# Create schema.prisma as defined above
npx prisma generate
npx prisma db push  # For development
```

**Files:**
- `forgecoder/prisma/schema.prisma`
- `forgecoder/lib/prisma.ts` (singleton client)

```typescript
// forgecoder/lib/prisma.ts
import { PrismaClient } from '@prisma/client'

const globalForPrisma = globalThis as unknown as {
  prisma: PrismaClient | undefined
}

export const prisma = globalForPrisma.prisma ?? new PrismaClient()

if (process.env.NODE_ENV !== 'production') globalForPrisma.prisma = prisma
```

#### Task 2.2: Webhook Endpoint ⏳

**File:** `forgecoder/app/api/webhooks/forge/route.ts`

```typescript
import { NextRequest, NextResponse } from 'next/server'
import { prisma } from '@/lib/prisma'
import crypto from 'crypto'

// Webhook event types
type WebhookEvent = {
  event_type: 'agent.stream' | 'agent.message' | 'agent.result' | 'agent.error'
  agent_id: string
  request_id: string
  seq: number
  timestamp: string
  is_final?: boolean
  payload: Record<string, unknown>
}

export async function POST(request: NextRequest) {
  // Verify HMAC signature if configured
  const signature = request.headers.get('X-Forge-Signature')
  const timestamp = request.headers.get('X-Forge-Timestamp')
  const body = await request.text()

  if (process.env.WEBHOOK_SECRET && signature) {
    const expected = crypto
      .createHmac('sha256', process.env.WEBHOOK_SECRET)
      .update(`${timestamp}.${body}`)
      .digest('hex')

    if (!crypto.timingSafeEqual(
      Buffer.from(`sha256=${expected}`),
      Buffer.from(signature)
    )) {
      return NextResponse.json({ error: 'Invalid signature' }, { status: 401 })
    }
  }

  const event: WebhookEvent = JSON.parse(body)

  // Idempotency check
  const eventId = `${event.request_id}:${event.seq}`
  const existing = await prisma.processedEvent.findUnique({ where: { id: eventId } })
  if (existing) {
    return NextResponse.json({ status: 'ok', duplicate: true })
  }

  // Process based on event type
  switch (event.event_type) {
    case 'agent.stream':
      await handleStreamEvent(event)
      break
    case 'agent.message':
      await handleMessageEvent(event)
      break
    case 'agent.result':
      await handleResultEvent(event)
      break
    case 'agent.error':
      await handleErrorEvent(event)
      break
  }

  // Mark as processed
  await prisma.processedEvent.create({ data: { id: eventId } })

  return NextResponse.json({ status: 'ok' })
}

async function handleStreamEvent(event: WebhookEvent) {
  // Stream events update real-time UI but aren't persisted individually
  // Push to connected clients via SSE or similar
  await broadcastToClients(event.agent_id, event)
}

async function handleMessageEvent(event: WebhookEvent) {
  const payload = event.payload as {
    uuid: string
    session_id: string
    role: string
    content_blocks: unknown[]
  }

  // Find or create session
  let session = await prisma.session.findFirst({
    where: { agentId: event.agent_id, status: 'active' }
  })

  if (!session) {
    session = await prisma.session.create({
      data: { agentId: event.agent_id }
    })
  }

  // Store message
  await prisma.message.upsert({
    where: { uuid: payload.uuid },
    create: {
      uuid: payload.uuid,
      sessionId: session.id,
      seq: event.seq,
      role: payload.role,
      content: payload.content_blocks as any
    },
    update: {
      content: payload.content_blocks as any
    }
  })

  // Update agent state
  await prisma.agent.update({
    where: { id: event.agent_id },
    data: { lastActiveAt: new Date() }
  })

  // Broadcast to connected clients
  await broadcastToClients(event.agent_id, event)
}

async function handleResultEvent(event: WebhookEvent) {
  const payload = event.payload as {
    session_id: string
    status: string
    usage: {
      input_tokens: number
      output_tokens: number
      cache_read_tokens: number
      cache_write_tokens: number
    }
    cost_usd: number
    duration_ms: number
  }

  // Update session with final stats
  await prisma.session.updateMany({
    where: { agentId: event.agent_id, status: 'active' },
    data: {
      status: payload.status,
      inputTokens: payload.usage.input_tokens,
      outputTokens: payload.usage.output_tokens,
      cacheReadTokens: payload.usage.cache_read_tokens,
      cacheWriteTokens: payload.usage.cache_write_tokens,
      costUsd: payload.cost_usd,
      durationMs: payload.duration_ms,
      completedAt: new Date()
    }
  })

  // Update agent state
  await prisma.agent.update({
    where: { id: event.agent_id },
    data: {
      state: 'IDLE',
      lastActiveAt: new Date()
    }
  })

  // Update request
  await prisma.request.updateMany({
    where: { id: event.request_id },
    data: {
      status: 'completed',
      completedAt: new Date()
    }
  })

  await broadcastToClients(event.agent_id, event)
}

async function handleErrorEvent(event: WebhookEvent) {
  const payload = event.payload as {
    error_code: string
    message: string
    recoverable: boolean
  }

  // Update request with error
  await prisma.request.updateMany({
    where: { id: event.request_id },
    data: {
      status: 'error',
      errorCode: payload.error_code,
      errorMessage: payload.message,
      completedAt: new Date()
    }
  })

  // Update agent state
  await prisma.agent.update({
    where: { id: event.agent_id },
    data: {
      state: payload.recoverable ? 'IDLE' : 'ERROR',
      lastActiveAt: new Date()
    }
  })

  await broadcastToClients(event.agent_id, event)
}

async function broadcastToClients(agentId: string, event: WebhookEvent) {
  // TODO: Implement SSE broadcast
  // This will push to connected browser clients viewing this agent
  console.log(`[SSE] Broadcasting to agent ${agentId}:`, event.event_type)
}
```

#### Task 2.3: Agent API Routes ⏳

**File:** `forgecoder/app/api/agents/route.ts`

```typescript
import { NextRequest, NextResponse } from 'next/server'
import { prisma } from '@/lib/prisma'

// GET /api/agents - List all agents
export async function GET() {
  const agents = await prisma.agent.findMany({
    include: { config: true },
    orderBy: { createdAt: 'desc' }
  })
  return NextResponse.json(agents)
}

// POST /api/agents - Create agent
export async function POST(request: NextRequest) {
  const body = await request.json()
  const { configId } = body

  // Call platform to create agent
  const platformRes = await fetch(`${process.env.PLATFORM_URL}/api/v1/agents`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ config_id: configId })
  })

  if (!platformRes.ok) {
    const error = await platformRes.json()
    return NextResponse.json(error, { status: platformRes.status })
  }

  const platformAgent = await platformRes.json()

  // Store in local DB
  const agent = await prisma.agent.create({
    data: {
      id: platformAgent.agent_id,
      configId,
      podName: platformAgent.pod_name
    },
    include: { config: true }
  })

  return NextResponse.json(agent, { status: 201 })
}
```

**File:** `forgecoder/app/api/agents/[id]/route.ts`

```typescript
import { NextRequest, NextResponse } from 'next/server'
import { prisma } from '@/lib/prisma'

// GET /api/agents/:id
export async function GET(
  request: NextRequest,
  { params }: { params: Promise<{ id: string }> }
) {
  const { id } = await params
  const agent = await prisma.agent.findUnique({
    where: { id },
    include: {
      config: true,
      sessions: {
        orderBy: { createdAt: 'desc' },
        take: 1,
        include: {
          messages: { orderBy: { seq: 'asc' } }
        }
      }
    }
  })

  if (!agent) {
    return NextResponse.json({ error: 'Agent not found' }, { status: 404 })
  }

  return NextResponse.json(agent)
}

// DELETE /api/agents/:id
export async function DELETE(
  request: NextRequest,
  { params }: { params: Promise<{ id: string }> }
) {
  const { id } = await params

  // Call platform to terminate agent
  await fetch(`${process.env.PLATFORM_URL}/api/v1/agents/${id}`, {
    method: 'DELETE'
  })

  // Delete from local DB
  await prisma.agent.delete({ where: { id } })

  return NextResponse.json({ status: 'deleted' })
}
```

**File:** `forgecoder/app/api/agents/[id]/messages/route.ts`

```typescript
import { NextRequest, NextResponse } from 'next/server'
import { prisma } from '@/lib/prisma'
import { randomUUID } from 'crypto'

// POST /api/agents/:id/messages - Send message to agent
export async function POST(
  request: NextRequest,
  { params }: { params: Promise<{ id: string }> }
) {
  const { id: agentId } = await params
  const body = await request.json()
  const { content } = body

  // Get or create active session
  let session = await prisma.session.findFirst({
    where: { agentId, status: 'active' }
  })

  if (!session) {
    session = await prisma.session.create({
      data: { agentId }
    })
  }

  const requestId = `req_${randomUUID().replace(/-/g, '').slice(0, 12)}`

  // Store the request
  await prisma.request.create({
    data: {
      id: requestId,
      sessionId: session.id,
      content,
      status: 'pending'
    }
  })

  // Store user message
  const userMsgUuid = `msg_${randomUUID().replace(/-/g, '').slice(0, 12)}`
  const lastMsg = await prisma.message.findFirst({
    where: { sessionId: session.id },
    orderBy: { seq: 'desc' }
  })
  const nextSeq = (lastMsg?.seq ?? 0) + 1

  await prisma.message.create({
    data: {
      uuid: userMsgUuid,
      sessionId: session.id,
      seq: nextSeq,
      role: 'user',
      content: [{ type: 'text', text: content }]
    }
  })

  // Update agent state
  await prisma.agent.update({
    where: { id: agentId },
    data: { state: 'PROCESSING' }
  })

  // Call platform API
  const webhookUrl = `${process.env.PUBLIC_URL}/api/webhooks/forge`

  const platformRes = await fetch(
    `${process.env.PLATFORM_URL}/api/v1/agents/${agentId}/messages`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        content,
        webhook_url: webhookUrl,
        webhook_secret: process.env.WEBHOOK_SECRET,
        request_id: requestId
      })
    }
  )

  if (!platformRes.ok) {
    const error = await platformRes.json()

    // Update request with error
    await prisma.request.update({
      where: { id: requestId },
      data: { status: 'error', errorMessage: error.message }
    })

    await prisma.agent.update({
      where: { id: agentId },
      data: { state: 'ERROR' }
    })

    return NextResponse.json(error, { status: platformRes.status })
  }

  const result = await platformRes.json()

  // Update request status
  await prisma.request.update({
    where: { id: requestId },
    data: { status: 'processing' }
  })

  return NextResponse.json({
    request_id: result.request_id,
    session_id: session.id,
    status: 'processing'
  }, { status: 202 })
}
```

#### Task 2.4: Real-time Updates (Supabase Realtime) ⏳

Instead of manual SSE (which doesn't work in serverless/multi-instance deployments), we use Supabase Realtime. The architecture:

```
Browser                    Supabase Realtime           Forgecoder              Platform
   │                              │                         │                      │
   │  Subscribe to channel        │                         │                      │
   │  "agent-events"              │                         │                      │
   │═════════════════════════════>│                         │                      │
   │                              │                         │                      │
   │                              │                         │  Webhook POST        │
   │                              │                         │<─────────────────────│
   │                              │                         │                      │
   │                              │  1. INSERT to messages  │                      │
   │                              │  2. Broadcast streaming │                      │
   │                              │<────────────────────────│                      │
   │                              │                         │                      │
   │  Realtime push               │                         │                      │
   │<═════════════════════════════│                         │                      │
```

**Two broadcast mechanisms:**

1. **Database changes** - For persisted messages (auto-broadcast on INSERT)
2. **Broadcast channels** - For ephemeral streaming events (no storage)

**File:** `forgecoder/lib/supabase.ts`

```typescript
import { createClient } from '@supabase/supabase-js'

// Server-side client (for webhook handler)
export const supabaseAdmin = createClient(
  process.env.NEXT_PUBLIC_SUPABASE_URL!,
  process.env.SUPABASE_SERVICE_ROLE_KEY!
)

// Client-side singleton
let browserClient: ReturnType<typeof createClient> | null = null

export function getSupabaseBrowserClient() {
  if (!browserClient) {
    browserClient = createClient(
      process.env.NEXT_PUBLIC_SUPABASE_URL!,
      process.env.NEXT_PUBLIC_SUPABASE_ANON_KEY!
    )
  }
  return browserClient
}
```

**File:** `forgecoder/lib/realtime.ts`

```typescript
import { supabaseAdmin } from './supabase'

// Broadcast ephemeral streaming events (not persisted)
export async function broadcastStreamEvent(agentId: string, event: unknown) {
  const channel = supabaseAdmin.channel('agent-events')
  await channel.send({
    type: 'broadcast',
    event: 'stream',
    payload: { agentId, ...event }
  })
}

// For persisted messages, just INSERT - Supabase Realtime auto-broadcasts
// No need to manually broadcast, subscribers to 'postgres_changes' will receive it
```

**File:** `forgecoder/hooks/useAgentEvents.ts`

```typescript
'use client'

import { useEffect, useState } from 'react'
import { getSupabaseBrowserClient } from '@/lib/supabase'
import type { RealtimeChannel } from '@supabase/supabase-js'

type AgentEvent = {
  event_type: string
  agent_id: string
  request_id: string
  seq: number
  payload: unknown
}

export function useAgentEvents(agentId: string) {
  const [events, setEvents] = useState<AgentEvent[]>([])
  const [isConnected, setIsConnected] = useState(false)

  useEffect(() => {
    const supabase = getSupabaseBrowserClient()
    let channel: RealtimeChannel

    async function subscribe() {
      channel = supabase.channel(`agent-${agentId}`)

      // Listen for ephemeral streaming events (broadcast)
      channel.on('broadcast', { event: 'stream' }, ({ payload }) => {
        if (payload.agentId === agentId) {
          setEvents(prev => [...prev, payload as AgentEvent])
        }
      })

      // Listen for persisted message inserts (database changes)
      channel.on(
        'postgres_changes',
        {
          event: 'INSERT',
          schema: 'public',
          table: 'messages',
          filter: `agent_id=eq.${agentId}`
        },
        (payload) => {
          setEvents(prev => [...prev, {
            event_type: 'agent.message',
            agent_id: agentId,
            request_id: payload.new.request_id,
            seq: payload.new.seq,
            payload: payload.new
          }])
        }
      )

      const status = await channel.subscribe()
      setIsConnected(status === 'SUBSCRIBED')
    }

    subscribe()

    return () => {
      channel?.unsubscribe()
      setIsConnected(false)
    }
  }, [agentId])

  return { events, isConnected, clearEvents: () => setEvents([]) }
}
```

**Update webhook handler to broadcast:**

In `forgecoder/app/api/webhooks/forge/route.ts`, update `handleStreamEvent`:

```typescript
import { broadcastStreamEvent } from '@/lib/realtime'

async function handleStreamEvent(event: WebhookEvent) {
  // Stream events are ephemeral - broadcast without persisting
  await broadcastStreamEvent(event.agent_id, event)
}
```

#### Task 2.5: Update Dashboard and Agent Pages ⏳

Update existing pages to use real data:
- `forgecoder/app/dashboard/page.tsx` - Fetch from `/api/agents`
- `forgecoder/app/agent/[id]/page.tsx` - Fetch from `/api/agents/:id`, use `useAgentEvents` hook

### Phase 3: Integration Testing ⏳ PENDING

#### Task 3.1: Local Development Setup ⏳

**File:** `forgecoder/.env.local.example`

```env
# Prisma / Supabase Database
DATABASE_URL="postgresql://..."
DIRECT_URL="postgresql://..."

# Supabase Realtime (for browser subscriptions)
NEXT_PUBLIC_SUPABASE_URL="https://xxx.supabase.co"
NEXT_PUBLIC_SUPABASE_ANON_KEY="eyJ..."
SUPABASE_SERVICE_ROLE_KEY="eyJ..."  # Server-side only, for broadcasting

# Platform connection
PLATFORM_URL="http://localhost:8080"

# Webhook configuration
PUBLIC_URL="http://localhost:3000"
WEBHOOK_SECRET="dev-secret-key"
```

**File:** `forgev2/platform/.env.local.example`

```env
# Database
DATABASE_URL="postgresql://..."

# Server
PORT=8080
DEBUG=true

# Webhook
WEBHOOK_TIMEOUT=10s
WEBHOOK_MAX_RETRIES=5
```

#### Task 3.2: Docker Compose for Local Testing ⏳

**File:** `docker-compose.dev.yml`

```yaml
version: '3.8'

services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_USER: forge
      POSTGRES_PASSWORD: forge
      POSTGRES_DB: forge
    ports:
      - "5432:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data

  platform:
    build:
      context: ./forgev2/platform
      dockerfile: Dockerfile
    ports:
      - "8080:8080"
    environment:
      DATABASE_URL: "postgresql://forge:forge@postgres:5432/forge"
      DEBUG: "true"
    depends_on:
      - postgres

  forgecoder:
    build:
      context: ./forgecoder
      dockerfile: Dockerfile
    ports:
      - "3000:3000"
    environment:
      DATABASE_URL: "postgresql://forge:forge@postgres:5432/forgecoder"
      PLATFORM_URL: "http://platform:8080"
      PUBLIC_URL: "http://localhost:3000"
      WEBHOOK_SECRET: "dev-secret"
    depends_on:
      - postgres
      - platform

volumes:
  pgdata:
```

#### Task 3.3: Integration Tests ⏳

**File:** `forgev2/platform/internal/webhook/delivery_test.go`

```go
func TestWebhookDelivery(t *testing.T) {
    // Start mock webhook server
    received := make(chan WebhookPayload, 10)
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        var payload WebhookPayload
        json.NewDecoder(r.Body).Decode(&payload)
        received <- payload
        w.WriteHeader(200)
    }))
    defer srv.Close()

    // Test delivery
    svc := NewDeliveryService(...)
    err := svc.Deliver(ctx, WebhookConfig{URL: srv.URL}, WebhookPayload{
        EventType: "agent.message",
        AgentID:   "test-agent",
        RequestID: "test-request",
        Seq:       1,
    })

    require.NoError(t, err)

    select {
    case p := <-received:
        assert.Equal(t, "agent.message", p.EventType)
    case <-time.After(time.Second):
        t.Fatal("timeout waiting for webhook")
    }
}
```

---

## Task Order Summary

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                    PHASE 1: Platform (forgev2) ✅ COMPLETED                  │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌────────────────────────────────┐                                         │
│  │ Task 1.1: Database Setup    ✅ │                                         │
│  │ - goose migrations             │                                         │
│  │ - sqlc queries                 │                                         │
│  │ - Connection pool              │                                         │
│  └──────────────┬─────────────────┘                                         │
│                 │                                                           │
│                 ▼                                                           │
│  ┌────────────────────────────────┐                                         │
│  │ Task 1.2: Webhook Delivery  ✅ │                                         │
│  │ - HTTP client with retries     │                                         │
│  │ - HMAC signing                 │                                         │
│  │ - Circuit breaker              │                                         │
│  └──────────────┬─────────────────┘                                         │
│                 │                                                           │
│                 ▼                                                           │
│  ┌────────────────────────────────┐                                         │
│  │ Task 1.3: Message Handler   ✅ │                                         │
│  │ - POST /agents/:id/messages    │                                         │
│  │ - POST /agents/:id/interrupt   │                                         │
│  └──────────────┬─────────────────┘                                         │
│                 │                                                           │
│                 ▼                                                           │
│  ┌────────────────────────────────┐   ┌────────────────────────────────┐   │
│  │ Task 1.4: Stream-to-Webhook ✅ │   │ Task 1.5: Proto Conversion  ✅ │   │
│  │ - Read gRPC stream             │   │ - AgentEvent → WebhookPayload  │   │
│  │ - Deliver to webhook           │   │                                │   │
│  └────────────────────────────────┘   └────────────────────────────────┘   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                 PHASE 2: Forgecoder (Product) ⏳ PENDING                     │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌────────────────────────────────┐                                         │
│  │ Task 2.1: Prisma Setup      ⏳ │                                         │
│  │ - Schema definition            │                                         │
│  │ - Supabase connection          │                                         │
│  │ - Client singleton             │                                         │
│  └──────────────┬─────────────────┘                                         │
│                 │                                                           │
│                 ▼                                                           │
│  ┌────────────────────────────────┐                                         │
│  │ Task 2.2: Webhook Endpoint  ⏳ │                                         │
│  │ - /api/webhooks/forge          │                                         │
│  │ - HMAC verification            │                                         │
│  │ - Idempotency handling         │                                         │
│  │ - Event processing             │                                         │
│  └──────────────┬─────────────────┘                                         │
│                 │                                                           │
│                 ▼                                                           │
│  ┌────────────────────────────────┐   ┌────────────────────────────────┐   │
│  │ Task 2.3: Agent API Routes  ⏳ │   │ Task 2.4: Supabase Realtime ⏳ │   │
│  │ - /api/agents                  │   │ - Broadcast channels           │   │
│  │ - /api/agents/:id              │   │ - postgres_changes listener    │   │
│  │ - /api/agents/:id/messages     │   │ - useAgentEvents hook          │   │
│  └────────────────────────────────┘   └────────────────────────────────┘   │
│                 │                                   │                       │
│                 └───────────────┬───────────────────┘                       │
│                                 ▼                                           │
│  ┌────────────────────────────────┐                                         │
│  │ Task 2.5: Update UI Pages   ⏳ │                                         │
│  │ - Dashboard with real data     │                                         │
│  │ - Agent page with SSE          │                                         │
│  │ - Remove mock data             │                                         │
│  └────────────────────────────────┘                                         │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                 PHASE 3: Integration Testing ⏳ PENDING                      │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌────────────────────────────────┐   ┌────────────────────────────────┐   │
│  │ Task 3.1: Local Dev Setup   ⏳ │   │ Task 3.2: Docker Compose    ⏳ │   │
│  │ - Environment files            │   │ - Full stack local dev         │   │
│  │ - Database connections         │   │ - Hot reload support           │   │
│  └────────────────────────────────┘   └────────────────────────────────┘   │
│                 │                                   │                       │
│                 └───────────────┬───────────────────┘                       │
│                                 ▼                                           │
│  ┌────────────────────────────────┐                                         │
│  │ Task 3.3: Integration Tests ⏳ │                                         │
│  │ - End-to-end webhook flow      │                                         │
│  │ - Retry/circuit breaker tests  │                                         │
│  │ - Supabase Realtime tests      │                                         │
│  └────────────────────────────────┘                                         │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Configuration

### Platform Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | - | Postgres connection string |
| `PORT` | `8080` | HTTP server port |
| `DEBUG` | `false` | Enable debug mode |
| `WEBHOOK_TIMEOUT` | `10s` | Timeout for webhook HTTP requests |
| `WEBHOOK_MAX_RETRIES` | `5` | Maximum retry attempts |
| `WEBHOOK_CIRCUIT_THRESHOLD` | `5` | Failures before circuit opens |
| `WEBHOOK_CIRCUIT_TIMEOUT` | `60s` | Circuit breaker reset timeout |

### Forgecoder Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | - | Prisma/Supabase connection string |
| `DIRECT_URL` | - | Direct Postgres URL (for migrations) |
| `NEXT_PUBLIC_SUPABASE_URL` | - | Supabase project URL |
| `NEXT_PUBLIC_SUPABASE_ANON_KEY` | - | Supabase anon key (client-side) |
| `SUPABASE_SERVICE_ROLE_KEY` | - | Supabase service role key (server-side broadcasting) |
| `PLATFORM_URL` | - | Forge platform API URL |
| `PUBLIC_URL` | - | Public URL for webhook callbacks |
| `WEBHOOK_SECRET` | - | HMAC secret for webhook verification |

## Testing

### Local Development

```bash
# Terminal 1: Start platform
cd forgev2/platform
go run ./cmd/server

# Terminal 2: Start forgecoder
cd forgecoder
npm run dev

# Terminal 3: Start ngrok for external testing (optional)
ngrok http 3000
```

### End-to-End Flow Test

```bash
# 1. Create an agent via forgecoder
curl -X POST http://localhost:3000/api/agents \
  -H "Content-Type: application/json" \
  -d '{"configId": "default-config-id"}'

# 2. Send a message
curl -X POST http://localhost:3000/api/agents/AGENT_ID/messages \
  -H "Content-Type: application/json" \
  -d '{"content": "Hello, agent!"}'

# 3. Watch SSE events
curl -N http://localhost:3000/api/agents/AGENT_ID/events
```

## Future Enhancements

1. **Request Options** - Support different request handling modes:
   - `queue` - Queue request behind current execution
   - `interrupt` - Stop current execution and start new request immediately
   - `priority` - Priority levels for request ordering
2. **Webhook Registration API** - Allow products to register webhooks per-agent instead of per-request
3. **Event Filtering** - Let products subscribe to specific event types only
4. **Batch Delivery** - Option to batch multiple events into single webhook call
5. **Dead Letter Queue** - Store failed deliveries for manual retry/inspection
6. **Webhook Analytics** - Track delivery success rates, latencies
7. **Multi-tenant Support** - API keys and tenant isolation for forgecoder
