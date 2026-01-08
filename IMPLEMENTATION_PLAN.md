# Claude Code Agent with AgentFS - Implementation Plan

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Web Client                                       │
│                         (WebSocket Connection)                               │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    │ WebSocket
                                    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                        Platform (Go/Echo)                                    │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐ │
│  │  WebSocket  │  │    gRPC     │  │   Agent     │  │   Turso Read        │ │
│  │   Handler   │──│   Client    │──│  Registry   │──│   Fallback          │ │
│  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    │ gRPC (bidirectional streaming)
                                    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                     Agent Container (TypeScript)                             │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐ │
│  │   gRPC      │  │   Claude    │  │   AgentFS   │  │   Message           │ │
│  │   Server    │──│  Agent SDK  │──│     SDK     │──│   Persistence       │ │
│  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    │ libsql (embedded + sync)
                                    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Turso Cloud Database                                 │
│                    (Durable storage, cross-region sync)                      │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Technology Choices

| Component | Technology | Rationale |
|-----------|------------|-----------|
| Agent Runtime | TypeScript (Bun) | Claude Agent SDK is TypeScript-first; Bun for fast startup |
| Platform | Go (Echo) | High performance, strong K8s client support |
| Agent ↔ Platform | gRPC | Type-safe, bidirectional streaming, shared protobuf types |
| Platform ↔ Client | WebSocket | Browser-native, real-time bidirectional |
| Persistence | AgentFS + Turso | Embedded SQLite with cloud sync, built for agents |
| Container Runtime | gVisor | Strong isolation, syscall filtering |

---

## Phase 1: Protocol Definition & Shared Types

**Goal:** Define the gRPC protocol between agent and platform, establishing shared types.

### 1.1 Proto Definitions

```
proto/
├── agent/
│   └── v1/
│       ├── agent.proto       # Main service definition
│       ├── messages.proto    # Message types
│       └── events.proto      # Streaming events
└── buf.yaml
```

**Tasks:**
- [ ] Set up buf.build for proto management
- [ ] Define core message types matching Claude Agent SDK's `SDKMessage`
- [ ] Define agent service with bidirectional streaming
- [ ] Generate Go and TypeScript stubs

**Proto Service Definition:**

```protobuf
syntax = "proto3";
package agent.v1;

// Core message types (maps to Claude Agent SDK SDKMessage)
message AgentMessage {
  string uuid = 1;
  string session_id = 2;
  int64 seq = 3;  // Monotonic sequence number for ordering
  int64 created_at = 4;  // Unix timestamp millis
  
  oneof payload {
    UserMessage user_message = 10;
    AssistantMessage assistant_message = 11;
    SystemMessage system_message = 12;
    StreamEvent stream_event = 13;
    ResultMessage result_message = 14;
  }
}

message UserMessage {
  string content = 1;
  repeated Attachment attachments = 2;
}

message AssistantMessage {
  repeated ContentBlock content = 1;
  string parent_tool_use_id = 2;
}

message ContentBlock {
  oneof block {
    TextBlock text = 1;
    ToolUseBlock tool_use = 2;
    ToolResultBlock tool_result = 3;
    ThinkingBlock thinking = 4;
  }
}

message TextBlock {
  string text = 1;
}

message ToolUseBlock {
  string id = 1;
  string name = 2;
  bytes input = 3;  // JSON-encoded tool input
}

message ToolResultBlock {
  string tool_use_id = 1;
  bytes content = 2;  // JSON-encoded result
  bool is_error = 3;
}

message ThinkingBlock {
  string thinking = 1;
}

message StreamEvent {
  string event_type = 1;  // content_block_start, content_block_delta, etc.
  bytes event_data = 2;   // JSON-encoded event from Anthropic SDK
}

message SystemMessage {
  string subtype = 1;  // "init", "compact_boundary"
  bytes metadata = 2;  // JSON-encoded system data
}

message ResultMessage {
  string subtype = 1;  // "success", "error_max_turns", etc.
  bool is_error = 2;
  string result = 3;
  double total_cost_usd = 4;
  int32 num_turns = 5;
  int64 duration_ms = 6;
  Usage usage = 7;
  repeated string errors = 8;
}

message Usage {
  int32 input_tokens = 1;
  int32 output_tokens = 2;
  int32 cache_read_input_tokens = 3;
  int32 cache_creation_input_tokens = 4;
}

message Attachment {
  string filename = 1;
  string mime_type = 2;
  bytes content = 3;
}

// Commands from platform to agent
message AgentCommand {
  string request_id = 1;
  
  oneof command {
    SendMessage send_message = 10;
    CatchUp catch_up = 11;
    Interrupt interrupt = 12;
    SetPermissionMode set_permission_mode = 13;
    GetStatus get_status = 14;
    Shutdown shutdown = 15;
  }
}

message SendMessage {
  string content = 1;
  repeated Attachment attachments = 2;
}

message CatchUp {
  int64 from_seq = 1;  // Get messages where seq > from_seq
}

message Interrupt {}

message SetPermissionMode {
  string mode = 1;  // "default", "acceptEdits", "bypassPermissions"
}

message GetStatus {}

message Shutdown {
  bool graceful = 1;
}

// Response from agent to platform
message AgentResponse {
  string request_id = 1;
  
  oneof response {
    CatchUpResponse catch_up_response = 10;
    StatusResponse status_response = 11;
    AckResponse ack = 12;
    ErrorResponse error = 13;
  }
}

message CatchUpResponse {
  repeated AgentMessage messages = 1;
  int64 latest_seq = 2;
}

message StatusResponse {
  string state = 1;  // "idle", "processing", "error"
  string session_id = 2;
  int64 latest_seq = 3;
  string current_model = 4;
  string permission_mode = 5;
}

message AckResponse {
  bool success = 1;
}

message ErrorResponse {
  string code = 1;
  string message = 2;
}

// The main agent service
service AgentService {
  // Bidirectional stream for real-time communication
  rpc Connect(stream AgentCommand) returns (stream AgentMessage);
  
  // Unary RPCs for simple operations
  rpc GetStatus(GetStatusRequest) returns (StatusResponse);
  rpc CatchUp(CatchUpRequest) returns (CatchUpResponse);
}

message GetStatusRequest {
  string agent_id = 1;
}

message CatchUpRequest {
  string agent_id = 1;
  int64 from_seq = 2;
}
```

### 1.2 Code Generation Setup

**Tasks:**
- [ ] Configure buf.gen.yaml for Go (platform)
- [ ] Configure buf.gen.yaml for TypeScript (agent)
- [ ] Set up shared types package
- [ ] Create Makefile/scripts for regeneration

**buf.gen.yaml:**
```yaml
version: v2
plugins:
  # Go generation
  - remote: buf.build/protocolbuffers/go
    out: platform/gen/proto
    opt:
      - paths=source_relative
  - remote: buf.build/grpc/go
    out: platform/gen/proto
    opt:
      - paths=source_relative
  
  # TypeScript generation
  - remote: buf.build/connectrpc/es
    out: agent/gen/proto
  - remote: buf.build/bufbuild/es
    out: agent/gen/proto
```

---

## Phase 2: Agent Process Foundation

**Goal:** Build the agent runtime with Claude Agent SDK + AgentFS integration.

### 2.1 Project Structure

```
agent/
├── src/
│   ├── index.ts              # Entry point
│   ├── grpc-server.ts        # gRPC service implementation
│   ├── agent-core.ts         # Claude Agent SDK wrapper
│   ├── persistence/
│   │   ├── agentfs.ts        # AgentFS initialization
│   │   ├── messages.ts       # Message CRUD operations
│   │   └── schema.ts         # Database schema
│   ├── streaming/
│   │   ├── transformer.ts    # SDK message → protobuf converter
│   │   └── buffer.ts         # Message buffering for catch-up
│   └── config.ts             # Environment configuration
├── proto/                    # Generated protobuf types
├── package.json
├── tsconfig.json
└── Dockerfile
```

### 2.2 AgentFS Integration

**Tasks:**
- [ ] Install and configure AgentFS SDK
- [ ] Define database schema for messages
- [ ] Implement message persistence layer
- [ ] Configure Turso cloud sync

**Database Schema (AgentFS):**

```typescript
// agent/src/persistence/schema.ts

export const SCHEMA = `
  -- Messages table with sequence numbers for ordering
  CREATE TABLE IF NOT EXISTS messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    uuid TEXT NOT NULL UNIQUE,
    session_id TEXT NOT NULL,
    seq INTEGER NOT NULL,
    
    -- Message classification
    type TEXT NOT NULL,           -- 'user', 'assistant', 'system', 'stream_event', 'result'
    subtype TEXT,                 -- For system/result messages
    
    -- Content (JSON-encoded based on type)
    content TEXT NOT NULL,
    
    -- Metadata
    parent_tool_use_id TEXT,
    created_at INTEGER NOT NULL DEFAULT (unixepoch() * 1000),
    
    -- Indexes
    UNIQUE(session_id, seq)
  );

  CREATE INDEX IF NOT EXISTS idx_messages_session_seq 
    ON messages(session_id, seq);

  CREATE INDEX IF NOT EXISTS idx_messages_created_at 
    ON messages(created_at);

  -- Session metadata
  CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL DEFAULT (unixepoch() * 1000),
    last_activity_at INTEGER NOT NULL DEFAULT (unixepoch() * 1000),
    state TEXT NOT NULL DEFAULT 'idle',  -- 'idle', 'processing', 'error'
    latest_seq INTEGER NOT NULL DEFAULT 0,
    
    -- Configuration snapshot
    model TEXT,
    permission_mode TEXT DEFAULT 'default',
    cwd TEXT
  );

  -- Key-value store for agent state (uses AgentFS KV)
  CREATE TABLE IF NOT EXISTS agent_state (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at INTEGER NOT NULL DEFAULT (unixepoch() * 1000)
  );
`;
```

**Message Persistence:**

```typescript
// agent/src/persistence/messages.ts

import { agentfs } from "agentfs-sdk";
import type { AgentMessage } from "../gen/proto/agent/v1/messages_pb";
import { SCHEMA } from "./schema";

export class MessageStore {
  private fs: Awaited<ReturnType<typeof agentfs>>;
  private sessionId: string;
  private currentSeq: number = 0;

  static async create(agentId: string, tursoUrl?: string, tursoToken?: string): Promise<MessageStore> {
    const store = new MessageStore();
    
    // Initialize AgentFS with Turso sync
    store.fs = await agentfs({
      id: agentId,
      syncRemoteUrl: tursoUrl,
      syncRemoteAuthToken: tursoToken,
    });
    
    // Initialize schema
    await store.fs.db.execute(SCHEMA);
    
    // Load or create session
    await store.initSession(agentId);
    
    return store;
  }

  private async initSession(sessionId: string): Promise<void> {
    this.sessionId = sessionId;
    
    // Try to load existing session
    const result = await this.fs.db.execute({
      sql: "SELECT latest_seq FROM sessions WHERE id = ?",
      args: [sessionId]
    });
    
    if (result.rows.length > 0) {
      this.currentSeq = result.rows[0].latest_seq as number;
    } else {
      // Create new session
      await this.fs.db.execute({
        sql: "INSERT INTO sessions (id, state) VALUES (?, 'idle')",
        args: [sessionId]
      });
    }
  }

  async persistMessage(message: Partial<AgentMessage>): Promise<AgentMessage> {
    const seq = ++this.currentSeq;
    const uuid = message.uuid || crypto.randomUUID();
    const createdAt = Date.now();

    await this.fs.db.execute({
      sql: `
        INSERT INTO messages (uuid, session_id, seq, type, subtype, content, parent_tool_use_id, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
      `,
      args: [
        uuid,
        this.sessionId,
        seq,
        message.payload?.case || 'unknown',
        this.getSubtype(message),
        JSON.stringify(message.payload?.value || {}),
        this.getParentToolUseId(message),
        createdAt
      ]
    });

    // Update session
    await this.fs.db.execute({
      sql: "UPDATE sessions SET latest_seq = ?, last_activity_at = ? WHERE id = ?",
      args: [seq, createdAt, this.sessionId]
    });

    // Trigger sync to Turso
    await this.fs.sync?.push();

    return {
      uuid,
      sessionId: this.sessionId,
      seq: BigInt(seq),
      createdAt: BigInt(createdAt),
      payload: message.payload,
    } as AgentMessage;
  }

  async getMessages(fromSeq: number): Promise<AgentMessage[]> {
    const result = await this.fs.db.execute({
      sql: `
        SELECT uuid, session_id, seq, type, subtype, content, parent_tool_use_id, created_at
        FROM messages
        WHERE session_id = ? AND seq > ?
        ORDER BY seq ASC
      `,
      args: [this.sessionId, fromSeq]
    });

    return result.rows.map(row => this.rowToMessage(row));
  }

  async getLatestSeq(): Promise<number> {
    return this.currentSeq;
  }

  private getSubtype(message: Partial<AgentMessage>): string | null {
    const payload = message.payload?.value;
    if (payload && 'subtype' in payload) {
      return payload.subtype as string;
    }
    return null;
  }

  private getParentToolUseId(message: Partial<AgentMessage>): string | null {
    const payload = message.payload?.value;
    if (payload && 'parentToolUseId' in payload) {
      return payload.parentToolUseId as string;
    }
    return null;
  }

  private rowToMessage(row: any): AgentMessage {
    // Reconstruct protobuf message from database row
    // Implementation depends on generated types
    return {
      uuid: row.uuid,
      sessionId: row.session_id,
      seq: BigInt(row.seq),
      createdAt: BigInt(row.created_at),
      payload: this.reconstructPayload(row.type, row.subtype, row.content, row.parent_tool_use_id),
    } as AgentMessage;
  }

  private reconstructPayload(type: string, subtype: string | null, content: string, parentToolUseId: string | null) {
    const parsed = JSON.parse(content);
    // Map type to protobuf oneof case
    // Implementation depends on generated types
    return { case: type, value: parsed };
  }
}
```

### 2.3 Claude Agent SDK Integration

**Tasks:**
- [ ] Install `@anthropic-ai/claude-agent-sdk`
- [ ] Create agent core wrapper with streaming
- [ ] Implement SDK message to protobuf transformation
- [ ] Handle tool execution and permissions

**Agent Core:**

```typescript
// agent/src/agent-core.ts

import { query, type SDKMessage, type Options } from "@anthropic-ai/claude-agent-sdk";
import type { AgentMessage, AgentCommand } from "./gen/proto/agent/v1/agent_pb";
import { MessageStore } from "./persistence/messages";

export interface AgentConfig {
  agentId: string;
  cwd: string;
  model?: string;
  permissionMode?: 'default' | 'acceptEdits' | 'bypassPermissions';
  allowedTools?: string[];
  tursoUrl?: string;
  tursoToken?: string;
}

export class AgentCore {
  private store: MessageStore;
  private config: AgentConfig;
  private currentQuery: ReturnType<typeof query> | null = null;
  private abortController: AbortController | null = null;
  private sessionId: string | null = null;

  static async create(config: AgentConfig): Promise<AgentCore> {
    const core = new AgentCore();
    core.config = config;
    core.store = await MessageStore.create(
      config.agentId,
      config.tursoUrl,
      config.tursoToken
    );
    return core;
  }

  async *processMessage(
    content: string,
    attachments?: Array<{ filename: string; content: Uint8Array }>
  ): AsyncGenerator<AgentMessage> {
    // Create abort controller for this query
    this.abortController = new AbortController();

    // Persist user message first
    const userMessage = await this.store.persistMessage({
      payload: {
        case: 'userMessage',
        value: { content }
      }
    });
    yield userMessage;

    // Build options for Claude Agent SDK
    const options: Options = {
      cwd: this.config.cwd,
      model: this.config.model,
      permissionMode: this.config.permissionMode || 'acceptEdits',
      allowedTools: this.config.allowedTools || [
        'Read', 'Write', 'Edit', 'Bash', 'Glob', 'Grep', 'WebSearch', 'WebFetch'
      ],
      abortController: this.abortController,
      includePartialMessages: true,  // Enable streaming events
      resume: this.sessionId || undefined,
    };

    // Start the query
    this.currentQuery = query({ prompt: content, options });

    try {
      for await (const sdkMessage of this.currentQuery) {
        // Transform SDK message to protobuf
        const protoMessage = this.transformSDKMessage(sdkMessage);
        
        // Persist non-streaming messages
        if (this.shouldPersist(sdkMessage)) {
          const persisted = await this.store.persistMessage(protoMessage);
          yield persisted;
        } else {
          // Streaming events get a temporary seq
          yield {
            ...protoMessage,
            seq: BigInt(-1),  // Indicate this is a streaming event
          } as AgentMessage;
        }

        // Capture session ID from init message
        if (sdkMessage.type === 'system' && sdkMessage.subtype === 'init') {
          this.sessionId = sdkMessage.session_id;
        }
      }
    } finally {
      this.currentQuery = null;
      this.abortController = null;
    }
  }

  async interrupt(): Promise<void> {
    if (this.abortController) {
      this.abortController.abort();
    }
    if (this.currentQuery) {
      await this.currentQuery.interrupt();
    }
  }

  async catchUp(fromSeq: number): Promise<AgentMessage[]> {
    return this.store.getMessages(fromSeq);
  }

  async getStatus(): Promise<{
    state: string;
    sessionId: string | null;
    latestSeq: number;
    model: string;
    permissionMode: string;
  }> {
    return {
      state: this.currentQuery ? 'processing' : 'idle',
      sessionId: this.sessionId,
      latestSeq: await this.store.getLatestSeq(),
      model: this.config.model || 'claude-sonnet-4-20250514',
      permissionMode: this.config.permissionMode || 'default',
    };
  }

  async setPermissionMode(mode: string): Promise<void> {
    if (this.currentQuery) {
      await this.currentQuery.setPermissionMode(mode as any);
    }
    this.config.permissionMode = mode as any;
  }

  private transformSDKMessage(sdkMessage: SDKMessage): Partial<AgentMessage> {
    switch (sdkMessage.type) {
      case 'user':
        return {
          uuid: sdkMessage.uuid,
          sessionId: sdkMessage.session_id,
          payload: {
            case: 'userMessage',
            value: {
              content: this.extractTextContent(sdkMessage.message),
            }
          }
        };

      case 'assistant':
        return {
          uuid: sdkMessage.uuid,
          sessionId: sdkMessage.session_id,
          payload: {
            case: 'assistantMessage',
            value: {
              content: this.transformContentBlocks(sdkMessage.message.content),
              parentToolUseId: sdkMessage.parent_tool_use_id || '',
            }
          }
        };

      case 'system':
        return {
          uuid: sdkMessage.uuid,
          sessionId: sdkMessage.session_id,
          payload: {
            case: 'systemMessage',
            value: {
              subtype: sdkMessage.subtype,
              metadata: new TextEncoder().encode(JSON.stringify(sdkMessage)),
            }
          }
        };

      case 'stream_event':
        return {
          uuid: sdkMessage.uuid,
          sessionId: sdkMessage.session_id,
          payload: {
            case: 'streamEvent',
            value: {
              eventType: sdkMessage.event.type,
              eventData: new TextEncoder().encode(JSON.stringify(sdkMessage.event)),
            }
          }
        };

      case 'result':
        return {
          uuid: sdkMessage.uuid,
          sessionId: sdkMessage.session_id,
          payload: {
            case: 'resultMessage',
            value: {
              subtype: sdkMessage.subtype,
              isError: sdkMessage.is_error,
              result: 'result' in sdkMessage ? sdkMessage.result : '',
              totalCostUsd: sdkMessage.total_cost_usd,
              numTurns: sdkMessage.num_turns,
              durationMs: BigInt(sdkMessage.duration_ms),
              usage: {
                inputTokens: sdkMessage.usage.input_tokens || 0,
                outputTokens: sdkMessage.usage.output_tokens || 0,
                cacheReadInputTokens: sdkMessage.usage.cache_read_input_tokens || 0,
                cacheCreationInputTokens: sdkMessage.usage.cache_creation_input_tokens || 0,
              },
              errors: 'errors' in sdkMessage ? sdkMessage.errors : [],
            }
          }
        };

      default:
        throw new Error(`Unknown message type: ${(sdkMessage as any).type}`);
    }
  }

  private extractTextContent(message: any): string {
    if (typeof message.content === 'string') {
      return message.content;
    }
    if (Array.isArray(message.content)) {
      return message.content
        .filter((block: any) => block.type === 'text')
        .map((block: any) => block.text)
        .join('');
    }
    return '';
  }

  private transformContentBlocks(content: any[]): any[] {
    return content.map(block => {
      switch (block.type) {
        case 'text':
          return { block: { case: 'text', value: { text: block.text } } };
        case 'tool_use':
          return {
            block: {
              case: 'toolUse',
              value: {
                id: block.id,
                name: block.name,
                input: new TextEncoder().encode(JSON.stringify(block.input)),
              }
            }
          };
        case 'tool_result':
          return {
            block: {
              case: 'toolResult',
              value: {
                toolUseId: block.tool_use_id,
                content: new TextEncoder().encode(JSON.stringify(block.content)),
                isError: block.is_error || false,
              }
            }
          };
        case 'thinking':
          return { block: { case: 'thinking', value: { thinking: block.thinking } } };
        default:
          return { block: { case: 'text', value: { text: JSON.stringify(block) } } };
      }
    });
  }

  private shouldPersist(message: SDKMessage): boolean {
    // Don't persist streaming events - they're ephemeral
    return message.type !== 'stream_event';
  }
}
```

### 2.4 gRPC Server

**Tasks:**
- [ ] Implement gRPC service with Connect-ES
- [ ] Handle bidirectional streaming
- [ ] Implement health checks for K8s probes

**gRPC Server:**

```typescript
// agent/src/grpc-server.ts

import { ConnectRouter } from "@connectrpc/connect";
import { fastifyConnectPlugin } from "@connectrpc/connect-fastify";
import { fastify } from "fastify";
import { AgentService } from "./gen/proto/agent/v1/agent_connect";
import { AgentCore, type AgentConfig } from "./agent-core";

export async function createServer(config: AgentConfig) {
  const agent = await AgentCore.create(config);
  
  const routes = (router: ConnectRouter) => {
    router.service(AgentService, {
      // Bidirectional streaming RPC
      async *connect(requests) {
        for await (const command of requests) {
          switch (command.command.case) {
            case 'sendMessage': {
              const msg = command.command.value;
              for await (const response of agent.processMessage(msg.content)) {
                yield response;
              }
              break;
            }

            case 'catchUp': {
              const { fromSeq } = command.command.value;
              const messages = await agent.catchUp(Number(fromSeq));
              for (const message of messages) {
                yield message;
              }
              break;
            }

            case 'interrupt': {
              await agent.interrupt();
              break;
            }

            case 'setPermissionMode': {
              const { mode } = command.command.value;
              await agent.setPermissionMode(mode);
              break;
            }

            case 'getStatus': {
              // Status is returned via a separate unary RPC
              break;
            }

            case 'shutdown': {
              const { graceful } = command.command.value;
              if (graceful) {
                await agent.interrupt();
              }
              process.exit(0);
            }
          }
        }
      },

      // Unary RPC for status
      async getStatus(request) {
        const status = await agent.getStatus();
        return {
          state: status.state,
          sessionId: status.sessionId || '',
          latestSeq: BigInt(status.latestSeq),
          currentModel: status.model,
          permissionMode: status.permissionMode,
        };
      },

      // Unary RPC for catch-up (alternative to streaming)
      async catchUp(request) {
        const messages = await agent.catchUp(Number(request.fromSeq));
        return {
          messages,
          latestSeq: BigInt(await agent.getStatus().then(s => s.latestSeq)),
        };
      },
    });
  };

  const server = fastify({ http2: true });
  
  await server.register(fastifyConnectPlugin, {
    routes,
  });

  // Health check endpoints for K8s
  server.get('/healthz', async () => ({ status: 'ok' }));
  server.get('/readyz', async () => {
    const status = await agent.getStatus();
    return { status: 'ready', state: status.state };
  });

  return server;
}

// Entry point
async function main() {
  const config: AgentConfig = {
    agentId: process.env.AGENT_ID || 'default',
    cwd: process.env.AGENT_CWD || process.cwd(),
    model: process.env.CLAUDE_MODEL,
    permissionMode: (process.env.PERMISSION_MODE as any) || 'acceptEdits',
    tursoUrl: process.env.TURSO_URL,
    tursoToken: process.env.TURSO_AUTH_TOKEN,
  };

  const server = await createServer(config);
  const port = parseInt(process.env.PORT || '8080', 10);
  
  await server.listen({ port, host: '0.0.0.0' });
  console.log(`Agent server listening on port ${port}`);
}

main().catch(console.error);
```

### 2.5 Dockerfile

```dockerfile
# agent/Dockerfile
FROM oven/bun:1.1-slim AS builder

WORKDIR /app

# Install dependencies
COPY package.json bun.lockb ./
RUN bun install --frozen-lockfile

# Copy source and build
COPY . .
RUN bun build ./src/index.ts --outdir ./dist --target bun

# Runtime stage
FROM oven/bun:1.1-slim

WORKDIR /app

# Copy built artifacts
COPY --from=builder /app/dist ./dist
COPY --from=builder /app/node_modules ./node_modules

# AgentFS needs a writable directory for the database
RUN mkdir -p /data && chown -R bun:bun /data
ENV AGENTFS_DB_PATH=/data

# Environment variables (set at runtime)
ENV PORT=8080
ENV AGENT_ID=""
ENV AGENT_CWD=/workspace
ENV TURSO_URL=""
ENV TURSO_AUTH_TOKEN=""
ENV ANTHROPIC_API_KEY=""

# Create workspace directory
RUN mkdir -p /workspace && chown -R bun:bun /workspace

USER bun

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:8080/healthz || exit 1

CMD ["bun", "run", "dist/index.js"]
```

**Deliverable:** Agent container that can:
1. Accept gRPC connections
2. Process messages via Claude Agent SDK
3. Persist messages to AgentFS/Turso
4. Stream responses back to caller
5. Support catch-up for reconnection

---

## Phase 3: Platform Foundation

**Goal:** Build the Go platform that manages agent containers and proxies WebSocket ↔ gRPC.

### 3.1 Project Structure

```
platform/
├── cmd/
│   └── server/
│       └── main.go
├── internal/
│   ├── api/
│   │   ├── router.go         # Echo router setup
│   │   ├── handlers.go       # HTTP handlers
│   │   └── websocket.go      # WebSocket connection management
│   ├── agent/
│   │   ├── manager.go        # Agent lifecycle management
│   │   ├── registry.go       # Track running agents
│   │   ├── grpc_client.go    # gRPC client to agents
│   │   └── proxy.go          # WebSocket ↔ gRPC proxy
│   ├── turso/
│   │   └── client.go         # Read-only fallback queries
│   └── config/
│       └── config.go
├── gen/
│   └── proto/                # Generated Go protobuf code
├── go.mod
└── Dockerfile
```

### 3.2 Agent Registry & Manager

**Tasks:**
- [ ] Implement agent registry (in-memory tracking)
- [ ] Implement agent lifecycle management
- [ ] Add gRPC client pool management
- [ ] Handle container health monitoring

**Agent Registry:**

```go
// platform/internal/agent/registry.go

package agent

import (
    "context"
    "sync"
    "time"
    
    "google.golang.org/grpc"
    agentv1 "github.com/yourorg/forge/platform/gen/proto/agent/v1"
)

type AgentState string

const (
    StateStarting   AgentState = "starting"
    StateRunning    AgentState = "running"
    StateStopping   AgentState = "stopping"
    StateStopped    AgentState = "stopped"
    StateError      AgentState = "error"
)

type AgentInfo struct {
    ID              string
    OwnerID         string
    State           AgentState
    Address         string  // gRPC address (e.g., "10.0.0.5:8080")
    SessionID       string
    LatestSeq       int64
    CreatedAt       time.Time
    LastActivityAt  time.Time
    
    // gRPC connection (lazily initialized)
    conn   *grpc.ClientConn
    client agentv1.AgentServiceClient
    mu     sync.RWMutex
}

func (a *AgentInfo) GetClient(ctx context.Context) (agentv1.AgentServiceClient, error) {
    a.mu.RLock()
    if a.client != nil {
        a.mu.RUnlock()
        return a.client, nil
    }
    a.mu.RUnlock()
    
    a.mu.Lock()
    defer a.mu.Unlock()
    
    // Double-check after acquiring write lock
    if a.client != nil {
        return a.client, nil
    }
    
    conn, err := grpc.DialContext(ctx, a.Address,
        grpc.WithInsecure(), // Use TLS in production
        grpc.WithBlock(),
    )
    if err != nil {
        return nil, err
    }
    
    a.conn = conn
    a.client = agentv1.NewAgentServiceClient(conn)
    return a.client, nil
}

func (a *AgentInfo) Close() error {
    a.mu.Lock()
    defer a.mu.Unlock()
    
    if a.conn != nil {
        return a.conn.Close()
    }
    return nil
}

type Registry struct {
    agents map[string]*AgentInfo
    mu     sync.RWMutex
}

func NewRegistry() *Registry {
    return &Registry{
        agents: make(map[string]*AgentInfo),
    }
}

func (r *Registry) Register(info *AgentInfo) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.agents[info.ID] = info
}

func (r *Registry) Unregister(id string) *AgentInfo {
    r.mu.Lock()
    defer r.mu.Unlock()
    
    info, exists := r.agents[id]
    if exists {
        delete(r.agents, id)
    }
    return info
}

func (r *Registry) Get(id string) (*AgentInfo, bool) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    
    info, exists := r.agents[id]
    return info, exists
}

func (r *Registry) GetByOwner(ownerID string) []*AgentInfo {
    r.mu.RLock()
    defer r.mu.RUnlock()
    
    var results []*AgentInfo
    for _, info := range r.agents {
        if info.OwnerID == ownerID {
            results = append(results, info)
        }
    }
    return results
}

func (r *Registry) UpdateState(id string, state AgentState) bool {
    r.mu.Lock()
    defer r.mu.Unlock()
    
    if info, exists := r.agents[id]; exists {
        info.State = state
        info.LastActivityAt = time.Now()
        return true
    }
    return false
}
```

### 3.3 WebSocket ↔ gRPC Proxy

**Tasks:**
- [ ] Implement WebSocket handler with gorilla/websocket
- [ ] Create bidirectional proxy to agent gRPC stream
- [ ] Handle reconnection with catch-up
- [ ] Implement connection multiplexing

**WebSocket Proxy:**

```go
// platform/internal/agent/proxy.go

package agent

import (
    "context"
    "encoding/json"
    "io"
    "sync"
    "time"

    "github.com/gorilla/websocket"
    "github.com/labstack/echo/v4"
    agentv1 "github.com/yourorg/forge/platform/gen/proto/agent/v1"
)

// WebSocket message types (for client communication)
type WSMessageType string

const (
    WSTypeUserMessage    WSMessageType = "user_message"
    WSTypeAgentMessage   WSMessageType = "agent_message"
    WSTypeCatchUp        WSMessageType = "catch_up"
    WSTypeInterrupt      WSMessageType = "interrupt"
    WSTypeStatus         WSMessageType = "status"
    WSTypeError          WSMessageType = "error"
)

type WSMessage struct {
    Type      WSMessageType   `json:"type"`
    RequestID string          `json:"request_id,omitempty"`
    Payload   json.RawMessage `json:"payload"`
}

type WSUserMessage struct {
    Content     string       `json:"content"`
    Attachments []Attachment `json:"attachments,omitempty"`
}

type Attachment struct {
    Filename string `json:"filename"`
    MimeType string `json:"mime_type"`
    Content  []byte `json:"content"`
}

type WSCatchUpRequest struct {
    FromSeq int64 `json:"from_seq"`
}

type Proxy struct {
    registry *Registry
    upgrader websocket.Upgrader
}

func NewProxy(registry *Registry) *Proxy {
    return &Proxy{
        registry: registry,
        upgrader: websocket.Upgrader{
            CheckOrigin: func(r *http.Request) bool {
                // Configure appropriately for production
                return true
            },
        },
    }
}

func (p *Proxy) HandleWebSocket(c echo.Context) error {
    agentID := c.Param("id")
    
    // Get agent info
    info, exists := p.registry.Get(agentID)
    if !exists {
        return echo.NewHTTPError(404, "agent not found")
    }
    
    if info.State != StateRunning {
        return echo.NewHTTPError(503, "agent not ready")
    }
    
    // Upgrade to WebSocket
    ws, err := p.upgrader.Upgrade(c.Response(), c.Request(), nil)
    if err != nil {
        return err
    }
    defer ws.Close()
    
    ctx, cancel := context.WithCancel(c.Request().Context())
    defer cancel()
    
    // Get gRPC client
    client, err := info.GetClient(ctx)
    if err != nil {
        return err
    }
    
    // Start bidirectional gRPC stream
    stream, err := client.Connect(ctx)
    if err != nil {
        return err
    }
    
    // Create channels for coordination
    errCh := make(chan error, 2)
    var wg sync.WaitGroup
    wg.Add(2)
    
    // WebSocket → gRPC
    go func() {
        defer wg.Done()
        defer stream.CloseSend()
        
        for {
            _, message, err := ws.ReadMessage()
            if err != nil {
                if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
                    errCh <- err
                }
                return
            }
            
            var wsMsg WSMessage
            if err := json.Unmarshal(message, &wsMsg); err != nil {
                p.sendError(ws, "", "invalid message format")
                continue
            }
            
            cmd, err := p.wsToGRPC(wsMsg)
            if err != nil {
                p.sendError(ws, wsMsg.RequestID, err.Error())
                continue
            }
            
            if err := stream.Send(cmd); err != nil {
                errCh <- err
                return
            }
        }
    }()
    
    // gRPC → WebSocket
    go func() {
        defer wg.Done()
        
        for {
            msg, err := stream.Recv()
            if err == io.EOF {
                return
            }
            if err != nil {
                errCh <- err
                return
            }
            
            wsMsg := p.grpcToWS(msg)
            data, _ := json.Marshal(wsMsg)
            
            if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
                errCh <- err
                return
            }
        }
    }()
    
    // Wait for either goroutine to finish
    select {
    case err := <-errCh:
        cancel()
        return err
    case <-ctx.Done():
        return ctx.Err()
    }
}

func (p *Proxy) wsToGRPC(msg WSMessage) (*agentv1.AgentCommand, error) {
    cmd := &agentv1.AgentCommand{
        RequestId: msg.RequestID,
    }
    
    switch msg.Type {
    case WSTypeUserMessage:
        var payload WSUserMessage
        if err := json.Unmarshal(msg.Payload, &payload); err != nil {
            return nil, err
        }
        cmd.Command = &agentv1.AgentCommand_SendMessage{
            SendMessage: &agentv1.SendMessage{
                Content: payload.Content,
            },
        }
        
    case WSTypeCatchUp:
        var payload WSCatchUpRequest
        if err := json.Unmarshal(msg.Payload, &payload); err != nil {
            return nil, err
        }
        cmd.Command = &agentv1.AgentCommand_CatchUp{
            CatchUp: &agentv1.CatchUp{
                FromSeq: payload.FromSeq,
            },
        }
        
    case WSTypeInterrupt:
        cmd.Command = &agentv1.AgentCommand_Interrupt{
            Interrupt: &agentv1.Interrupt{},
        }
        
    case WSTypeStatus:
        cmd.Command = &agentv1.AgentCommand_GetStatus{
            GetStatus: &agentv1.GetStatus{},
        }
        
    default:
        return nil, fmt.Errorf("unknown message type: %s", msg.Type)
    }
    
    return cmd, nil
}

func (p *Proxy) grpcToWS(msg *agentv1.AgentMessage) WSMessage {
    payload, _ := json.Marshal(msg)
    return WSMessage{
        Type:    WSTypeAgentMessage,
        Payload: payload,
    }
}

func (p *Proxy) sendError(ws *websocket.Conn, requestID string, message string) {
    errPayload, _ := json.Marshal(map[string]string{"error": message})
    wsMsg := WSMessage{
        Type:      WSTypeError,
        RequestID: requestID,
        Payload:   errPayload,
    }
    data, _ := json.Marshal(wsMsg)
    ws.WriteMessage(websocket.TextMessage, data)
}
```

### 3.4 Turso Read Fallback

**Tasks:**
- [ ] Implement libsql Go client
- [ ] Create fallback query path for offline agents
- [ ] Handle seamless transition between live/fallback

**Turso Client:**

```go
// platform/internal/turso/client.go

package turso

import (
    "context"
    "database/sql"
    "encoding/json"
    "fmt"
    
    _ "github.com/tursodatabase/libsql-client-go/libsql"
    agentv1 "github.com/yourorg/forge/platform/gen/proto/agent/v1"
)

type Client struct {
    db *sql.DB
}

func NewClient(url, authToken string) (*Client, error) {
    connStr := fmt.Sprintf("%s?authToken=%s", url, authToken)
    db, err := sql.Open("libsql", connStr)
    if err != nil {
        return nil, err
    }
    return &Client{db: db}, nil
}

func (c *Client) GetMessages(ctx context.Context, agentID string, fromSeq int64) ([]*agentv1.AgentMessage, error) {
    rows, err := c.db.QueryContext(ctx, `
        SELECT uuid, session_id, seq, type, subtype, content, parent_tool_use_id, created_at
        FROM messages
        WHERE session_id = ? AND seq > ?
        ORDER BY seq ASC
    `, agentID, fromSeq)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    
    var messages []*agentv1.AgentMessage
    for rows.Next() {
        var (
            uuid, sessionID, msgType, content string
            seq, createdAt                    int64
            subtype, parentToolUseID          sql.NullString
        )
        
        if err := rows.Scan(&uuid, &sessionID, &seq, &msgType, &subtype, &content, &parentToolUseID, &createdAt); err != nil {
            return nil, err
        }
        
        msg := &agentv1.AgentMessage{
            Uuid:      uuid,
            SessionId: sessionID,
            Seq:       seq,
            CreatedAt: createdAt,
        }
        
        // Reconstruct payload based on type
        msg.Payload = c.reconstructPayload(msgType, subtype.String, content, parentToolUseID.String)
        
        messages = append(messages, msg)
    }
    
    return messages, rows.Err()
}

func (c *Client) GetLatestSeq(ctx context.Context, agentID string) (int64, error) {
    var seq int64
    err := c.db.QueryRowContext(ctx, `
        SELECT COALESCE(MAX(seq), 0) FROM messages WHERE session_id = ?
    `, agentID).Scan(&seq)
    return seq, err
}

func (c *Client) reconstructPayload(msgType, subtype, content, parentToolUseID string) isAgentMessage_Payload {
    var parsed map[string]interface{}
    json.Unmarshal([]byte(content), &parsed)
    
    // Reconstruct based on type
    // Implementation depends on your specific message structures
    return nil
}

func (c *Client) Close() error {
    return c.db.Close()
}
```

### 3.5 REST API Handlers

**Tasks:**
- [ ] Implement agent CRUD endpoints
- [ ] Add authentication middleware
- [ ] Create status and health endpoints

**API Router:**

```go
// platform/internal/api/router.go

package api

import (
    "github.com/labstack/echo/v4"
    "github.com/labstack/echo/v4/middleware"
    
    "github.com/yourorg/forge/platform/internal/agent"
    "github.com/yourorg/forge/platform/internal/turso"
)

type Server struct {
    echo     *echo.Echo
    registry *agent.Registry
    manager  *agent.Manager
    turso    *turso.Client
    proxy    *agent.Proxy
}

func NewServer(registry *agent.Registry, manager *agent.Manager, turso *turso.Client) *Server {
    s := &Server{
        echo:     echo.New(),
        registry: registry,
        manager:  manager,
        turso:    turso,
        proxy:    agent.NewProxy(registry),
    }
    
    s.setupMiddleware()
    s.setupRoutes()
    
    return s
}

func (s *Server) setupMiddleware() {
    s.echo.Use(middleware.Logger())
    s.echo.Use(middleware.Recover())
    s.echo.Use(middleware.CORS())
    
    // TODO: Add auth middleware
}

func (s *Server) setupRoutes() {
    // Health checks
    s.echo.GET("/healthz", s.healthz)
    s.echo.GET("/readyz", s.readyz)
    
    // Agent management
    api := s.echo.Group("/api/v1")
    
    agents := api.Group("/agents")
    agents.POST("", s.createAgent)
    agents.GET("", s.listAgents)
    agents.GET("/:id", s.getAgent)
    agents.DELETE("/:id", s.deleteAgent)
    agents.GET("/:id/messages", s.getMessages)
    
    // WebSocket endpoint
    agents.GET("/:id/stream", s.proxy.HandleWebSocket)
}

func (s *Server) Start(addr string) error {
    return s.echo.Start(addr)
}

// Handler implementations...

func (s *Server) createAgent(c echo.Context) error {
    var req struct {
        OwnerID string `json:"owner_id"`
        Model   string `json:"model,omitempty"`
    }
    if err := c.Bind(&req); err != nil {
        return err
    }
    
    info, err := s.manager.CreateAgent(c.Request().Context(), req.OwnerID, req.Model)
    if err != nil {
        return err
    }
    
    return c.JSON(201, info)
}

func (s *Server) getMessages(c echo.Context) error {
    agentID := c.Param("id")
    fromSeq := c.QueryParam("from_seq")
    
    // Try live agent first
    if info, exists := s.registry.Get(agentID); exists && info.State == agent.StateRunning {
        client, err := info.GetClient(c.Request().Context())
        if err == nil {
            resp, err := client.CatchUp(c.Request().Context(), &agentv1.CatchUpRequest{
                AgentId: agentID,
                FromSeq: parseSeq(fromSeq),
            })
            if err == nil {
                return c.JSON(200, resp)
            }
        }
    }
    
    // Fallback to Turso
    messages, err := s.turso.GetMessages(c.Request().Context(), agentID, parseSeq(fromSeq))
    if err != nil {
        return err
    }
    
    latestSeq, _ := s.turso.GetLatestSeq(c.Request().Context(), agentID)
    
    return c.JSON(200, map[string]interface{}{
        "messages":   messages,
        "latest_seq": latestSeq,
        "source":     "turso",  // Indicate data came from fallback
    })
}
```

**Deliverable:** Platform that can:
1. Create/destroy agent containers
2. Proxy WebSocket connections to agents via gRPC
3. Fall back to Turso for message history when agents are offline
4. Track agent state in registry

---

## Phase 4: Kubernetes Integration

**Goal:** Deploy agent containers on K8s with gVisor isolation.

### 4.1 K8s Manifests

**Tasks:**
- [ ] Create agent pod template
- [ ] Configure gVisor RuntimeClass
- [ ] Set up network policies
- [ ] Create RBAC for platform

**Agent Pod Template:**

```yaml
# k8s/agent-pod-template.yaml
apiVersion: v1
kind: Pod
metadata:
  name: agent-${AGENT_ID}
  namespace: agents
  labels:
    app: claude-agent
    agent-id: ${AGENT_ID}
    owner-id: ${OWNER_ID}
  annotations:
    prometheus.io/scrape: "true"
    prometheus.io/port: "8080"
spec:
  runtimeClassName: gvisor
  
  serviceAccountName: agent-runner
  
  containers:
  - name: agent
    image: ${AGENT_IMAGE}
    ports:
    - name: grpc
      containerPort: 8080
      protocol: TCP
    
    env:
    - name: AGENT_ID
      value: ${AGENT_ID}
    - name: AGENT_CWD
      value: /workspace
    - name: PORT
      value: "8080"
    - name: TURSO_URL
      valueFrom:
        secretKeyRef:
          name: turso-credentials
          key: url
    - name: TURSO_AUTH_TOKEN
      valueFrom:
        secretKeyRef:
          name: turso-credentials
          key: token
    - name: ANTHROPIC_API_KEY
      valueFrom:
        secretKeyRef:
          name: anthropic-credentials
          key: api-key
    
    resources:
      requests:
        memory: "256Mi"
        cpu: "250m"
      limits:
        memory: "1Gi"
        cpu: "1000m"
    
    readinessProbe:
      httpGet:
        path: /readyz
        port: 8080
      initialDelaySeconds: 5
      periodSeconds: 10
    
    livenessProbe:
      httpGet:
        path: /healthz
        port: 8080
      initialDelaySeconds: 15
      periodSeconds: 20
    
    volumeMounts:
    - name: workspace
      mountPath: /workspace
    - name: data
      mountPath: /data
  
  volumes:
  - name: workspace
    emptyDir: {}
  - name: data
    emptyDir: {}
  
  restartPolicy: Never
  
  # Security context
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    fsGroup: 1000
```

**gVisor RuntimeClass:**

```yaml
# k8s/runtime-class.yaml
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: gvisor
handler: runsc
scheduling:
  nodeSelector:
    sandbox.gvisor.dev/runtime: gvisor
```

**Network Policy:**

```yaml
# k8s/network-policy.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: agent-network-policy
  namespace: agents
spec:
  podSelector:
    matchLabels:
      app: claude-agent
  policyTypes:
  - Ingress
  - Egress
  ingress:
  # Only allow from platform
  - from:
    - namespaceSelector:
        matchLabels:
          name: platform
    ports:
    - protocol: TCP
      port: 8080
  egress:
  # Allow Turso
  - to:
    - ipBlock:
        cidr: 0.0.0.0/0
    ports:
    - protocol: TCP
      port: 443
  # Allow Anthropic API
  - to:
    - ipBlock:
        cidr: 0.0.0.0/0
    ports:
    - protocol: TCP
      port: 443
  # Allow DNS
  - to:
    - namespaceSelector: {}
      podSelector:
        matchLabels:
          k8s-app: kube-dns
    ports:
    - protocol: UDP
      port: 53
```

### 4.2 Platform K8s Client

**Tasks:**
- [ ] Implement K8s client wrapper
- [ ] Add pod lifecycle management
- [ ] Implement pod watcher for state updates
- [ ] Handle pod IP discovery

**K8s Manager:**

```go
// platform/internal/agent/k8s_manager.go

package agent

import (
    "context"
    "fmt"
    "os"
    "strings"
    "text/template"
    "time"

    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/watch"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/rest"
)

type K8sManager struct {
    client       *kubernetes.Clientset
    registry     *Registry
    namespace    string
    agentImage   string
    podTemplate  *template.Template
}

func NewK8sManager(registry *Registry, namespace, agentImage string) (*K8sManager, error) {
    config, err := rest.InClusterConfig()
    if err != nil {
        return nil, err
    }
    
    client, err := kubernetes.NewForConfig(config)
    if err != nil {
        return nil, err
    }
    
    tmpl, err := template.ParseFiles("k8s/agent-pod-template.yaml")
    if err != nil {
        return nil, err
    }
    
    m := &K8sManager{
        client:      client,
        registry:    registry,
        namespace:   namespace,
        agentImage:  agentImage,
        podTemplate: tmpl,
    }
    
    // Start watching for pod events
    go m.watchPods()
    
    return m, nil
}

func (m *K8sManager) CreateAgent(ctx context.Context, ownerID, model string) (*AgentInfo, error) {
    agentID := generateAgentID()
    
    // Register in registry first
    info := &AgentInfo{
        ID:        agentID,
        OwnerID:   ownerID,
        State:     StateStarting,
        CreatedAt: time.Now(),
    }
    m.registry.Register(info)
    
    // Create pod
    pod := m.buildPod(agentID, ownerID, model)
    
    created, err := m.client.CoreV1().Pods(m.namespace).Create(ctx, pod, metav1.CreateOptions{})
    if err != nil {
        m.registry.Unregister(agentID)
        return nil, err
    }
    
    // Wait for pod to be ready
    if err := m.waitForReady(ctx, created.Name); err != nil {
        m.DeleteAgent(ctx, agentID)
        return nil, err
    }
    
    // Get pod IP
    pod, err = m.client.CoreV1().Pods(m.namespace).Get(ctx, created.Name, metav1.GetOptions{})
    if err != nil {
        m.DeleteAgent(ctx, agentID)
        return nil, err
    }
    
    info.Address = fmt.Sprintf("%s:8080", pod.Status.PodIP)
    info.State = StateRunning
    
    return info, nil
}

func (m *K8sManager) DeleteAgent(ctx context.Context, agentID string) error {
    podName := fmt.Sprintf("agent-%s", agentID)
    
    // Update registry
    if info := m.registry.Unregister(agentID); info != nil {
        info.Close()
    }
    
    // Delete pod
    return m.client.CoreV1().Pods(m.namespace).Delete(ctx, podName, metav1.DeleteOptions{})
}

func (m *K8sManager) buildPod(agentID, ownerID, model string) *corev1.Pod {
    // Build pod from template with variable substitution
    // Implementation details...
    return &corev1.Pod{
        ObjectMeta: metav1.ObjectMeta{
            Name:      fmt.Sprintf("agent-%s", agentID),
            Namespace: m.namespace,
            Labels: map[string]string{
                "app":      "claude-agent",
                "agent-id": agentID,
                "owner-id": ownerID,
            },
        },
        Spec: corev1.PodSpec{
            RuntimeClassName: stringPtr("gvisor"),
            Containers: []corev1.Container{
                {
                    Name:  "agent",
                    Image: m.agentImage,
                    Ports: []corev1.ContainerPort{
                        {Name: "grpc", ContainerPort: 8080},
                    },
                    Env: []corev1.EnvVar{
                        {Name: "AGENT_ID", Value: agentID},
                        {Name: "PORT", Value: "8080"},
                        // ... other env vars from secrets
                    },
                    Resources: corev1.ResourceRequirements{
                        Limits: corev1.ResourceList{
                            corev1.ResourceMemory: resource.MustParse("1Gi"),
                            corev1.ResourceCPU:    resource.MustParse("1000m"),
                        },
                        Requests: corev1.ResourceList{
                            corev1.ResourceMemory: resource.MustParse("256Mi"),
                            corev1.ResourceCPU:    resource.MustParse("250m"),
                        },
                    },
                },
            },
            RestartPolicy: corev1.RestartPolicyNever,
        },
    }
}

func (m *K8sManager) waitForReady(ctx context.Context, podName string) error {
    watcher, err := m.client.CoreV1().Pods(m.namespace).Watch(ctx, metav1.ListOptions{
        FieldSelector: fmt.Sprintf("metadata.name=%s", podName),
    })
    if err != nil {
        return err
    }
    defer watcher.Stop()
    
    timeout := time.After(2 * time.Minute)
    
    for {
        select {
        case event := <-watcher.ResultChan():
            pod, ok := event.Object.(*corev1.Pod)
            if !ok {
                continue
            }
            
            if pod.Status.Phase == corev1.PodRunning {
                for _, cond := range pod.Status.Conditions {
                    if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
                        return nil
                    }
                }
            }
            
            if pod.Status.Phase == corev1.PodFailed {
                return fmt.Errorf("pod failed to start")
            }
            
        case <-timeout:
            return fmt.Errorf("timeout waiting for pod to be ready")
            
        case <-ctx.Done():
            return ctx.Err()
        }
    }
}

func (m *K8sManager) watchPods() {
    for {
        watcher, err := m.client.CoreV1().Pods(m.namespace).Watch(context.Background(), metav1.ListOptions{
            LabelSelector: "app=claude-agent",
        })
        if err != nil {
            time.Sleep(5 * time.Second)
            continue
        }
        
        for event := range watcher.ResultChan() {
            pod, ok := event.Object.(*corev1.Pod)
            if !ok {
                continue
            }
            
            agentID := pod.Labels["agent-id"]
            
            switch event.Type {
            case watch.Modified:
                if pod.Status.Phase == corev1.PodFailed {
                    m.registry.UpdateState(agentID, StateError)
                }
            case watch.Deleted:
                if info := m.registry.Unregister(agentID); info != nil {
                    info.Close()
                }
            }
        }
        
        watcher.Stop()
    }
}

func stringPtr(s string) *string { return &s }
```

**Deliverable:** Platform can create/destroy K8s pods with gVisor isolation.

---

## Phase 5: Production Hardening

**Goal:** Add auth, observability, and error handling.

### 5.1 Authentication

**Tasks:**
- [ ] Add JWT validation middleware to platform
- [ ] Implement agent ownership checks
- [ ] Secure platform → agent communication
- [ ] Add API key management

### 5.2 Observability

**Tasks:**
- [ ] Add structured logging (zerolog)
- [ ] Implement Prometheus metrics
- [ ] Add distributed tracing (optional)
- [ ] Create Grafana dashboards

**Metrics:**

```go
// platform/internal/metrics/metrics.go

package metrics

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    ActiveAgents = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "forge_active_agents",
        Help: "Number of currently running agent pods",
    })
    
    AgentCreations = promauto.NewCounter(prometheus.CounterOpts{
        Name: "forge_agent_creations_total",
        Help: "Total number of agent creations",
    })
    
    AgentErrors = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "forge_agent_errors_total",
        Help: "Total number of agent errors by type",
    }, []string{"error_type"})
    
    MessageLatency = promauto.NewHistogram(prometheus.HistogramOpts{
        Name:    "forge_message_latency_seconds",
        Help:    "Latency of message processing",
        Buckets: prometheus.ExponentialBuckets(0.1, 2, 10),
    })
    
    WebSocketConnections = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "forge_websocket_connections",
        Help: "Number of active WebSocket connections",
    })
    
    GRPCStreamDuration = promauto.NewHistogram(prometheus.HistogramOpts{
        Name:    "forge_grpc_stream_duration_seconds",
        Help:    "Duration of gRPC streams",
        Buckets: prometheus.ExponentialBuckets(1, 2, 15),
    })
)
```

### 5.3 Error Handling & Recovery

**Tasks:**
- [ ] Implement circuit breaker for agent connections
- [ ] Add automatic agent restart on crash
- [ ] Implement graceful shutdown
- [ ] Add connection retry with backoff

---

## Phase 6: Testing Strategy

### 6.1 Unit Tests

- Agent message transformation
- Message persistence
- Registry operations
- WebSocket ↔ gRPC conversion

### 6.2 Integration Tests

- End-to-end message flow
- Catch-up after reconnection
- Turso fallback behavior
- K8s pod lifecycle

### 6.3 Load Tests

- Concurrent agent instances
- Message throughput
- WebSocket connection limits
- Memory/CPU under load

---

## Implementation Order

1. **Week 1-2: Protocol & Agent Core**
   - Define protobuf schemas
   - Set up code generation
   - Implement AgentFS persistence
   - Integrate Claude Agent SDK
   - Create gRPC server

2. **Week 3-4: Platform Foundation**
   - Build agent registry
   - Implement WebSocket ↔ gRPC proxy
   - Add Turso fallback
   - Create REST API

3. **Week 5: Containerization**
   - Create Dockerfile
   - Test with Docker locally
   - Validate AgentFS sync

4. **Week 6-7: Kubernetes**
   - Set up K8s cluster
   - Configure gVisor
   - Implement K8s manager
   - Test pod lifecycle

5. **Week 8-9: Production Hardening**
   - Add authentication
   - Implement observability
   - Error handling
   - Performance tuning

6. **Week 10: Testing & Documentation**
   - Integration tests
   - Load tests
   - API documentation
   - Deployment guides

---

## Key Technical Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Agent ↔ Platform Protocol | gRPC with Connect | Type-safe, bidirectional streaming, generates both Go and TS |
| Platform ↔ Client Protocol | WebSocket | Browser-native, real-time bidirectional |
| Agent Runtime | Bun | Fast startup, native TypeScript, small footprint |
| Message Persistence | AgentFS + Turso | Built for agents, embedded + cloud sync |
| Container Runtime | gVisor | Strong isolation without full VM overhead |
| Service Framework (Go) | Echo | Fast, minimal, good middleware ecosystem |

---

## Resources

- [Claude Agent SDK Documentation](https://platform.claude.com/docs/en/agent-sdk/overview)
- [AgentFS Documentation](https://docs.turso.tech/agentfs)
- [Connect-ES (gRPC for TypeScript)](https://connectrpc.com/docs/introduction)
- [gVisor Documentation](https://gvisor.dev/docs/)
- [Turso libsql Go Client](https://github.com/tursodatabase/libsql-client-go)
