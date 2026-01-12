Feature Gap Analysis: OpenCode vs Forge Platform

### What Forge Currently Supports

| Feature | Proto Definition | Platform Handler | Agent Service |
|---------|------------------|------------------|---------------|
| Send Message | ✅ `SendMessageRequest` | ✅ `POST /agents/:id/messages` | ✅ `handleSendMessage` |
| Interrupt | ✅ `InterruptRequest` | ✅ `POST /agents/:id/interrupt` | ✅ `handleInterrupt` |
| Set Permission Mode | ✅ `SetPermissionModeRequest` | ❌ No HTTP endpoint | ✅ `handleSetPermissionMode` |
| Set Model | ✅ `SetModelRequest` | ❌ No HTTP endpoint | ✅ `handleSetModel` |
| Get Status | ✅ `GetStatusRequest` | ❌ No HTTP endpoint | ✅ `getStatus` |
| Shutdown | ✅ `ShutdownRequest` | ❌ No HTTP endpoint | ✅ `shutdown` |

---

### Missing Features (OpenCode Has, Forge Doesn't)

#### **High Priority - Core Message Features**

| Feature | OpenCode API | Description | Impact |
|---------|--------------|-------------|--------|
| **File/Image Attachments** | `parts: [{ type: "file", ... }]` | Send files, images with messages | High - Can't attach context |
| **Agent Selection** | `parts: [{ type: "agent", agent: "..." }]` | Choose which agent processes | High - No agent switching |
| **Multi-part Messages** | `parts: Part[]` | Send text + files + agents together | High - Limited input |

Our `SendMessageRequest` only has `content: string`. OpenCode's `PromptInput` supports:
```typescript
parts: [
  { type: "text", text: "..." },
  { type: "file", path: "...", preview: "..." },
  { type: "agent", agent: "code-reviewer" }
]
```

#### **High Priority - Session Management**

| Feature | OpenCode API | Description | Impact |
|---------|--------------|-------------|--------|
| **Create Session** | `POST /session` | Create new conversation | High - Can't start fresh |
| **Delete Session** | `DELETE /session/:id` | Delete conversation | Medium - Cleanup |
| **List Sessions** | `GET /session` | List all sessions | Medium - History |
| **Get Session** | `GET /session/:id` | Get session details | Medium - State |
| **Fork Session** | `POST /session/:id/fork` | Branch conversation | Medium - Experimentation |
| **Share Session** | `POST /session/:id/share` | Create shareable link | Low - Collaboration |

Currently Forge creates ONE session per agent pod and never exposes session management to products.

#### **High Priority - Message History**

| Feature | OpenCode API | Description | Impact |
|---------|--------------|-------------|--------|
| **Get Messages** | `GET /session/:id/message` | Retrieve message history | High - No history retrieval |
| **Get Single Message** | `GET /session/:id/message/:msgId` | Get specific message | Medium |
| **Revert Message** | `POST /session/:id/revert` | Undo message effects | Medium - Error recovery |
| **Unrevert** | `POST /session/:id/unrevert` | Restore reverted | Low |

#### **Medium Priority - Interactive Features**

| Feature | OpenCode API | Description | Impact |
|---------|--------------|-------------|--------|
| **Permission Requests** | `GET /permission`, `POST /permission/:id/reply` | Handle tool permission asks | High - Currently auto-approves |
| **Question Handling** | `GET /question`, `POST /question/:id/reply` | Handle agent questions | Medium |
| **Session Todos** | `GET /session/:id/todo` | Get task list | Low - Nice to have |

#### **Medium Priority - Session Features**

| Feature | OpenCode API | Description | Impact |
|---------|--------------|-------------|--------|
| **Abort Session** | `POST /session/:id/abort` | Cancel ongoing work | High - Same as interrupt? |
| **Summarize/Compact** | `POST /session/:id/summarize` | Compress context | Medium - Long sessions |
| **Get Diff** | `GET /session/:id/diff` | File changes | Medium - Code review |
| **Session Status** | `GET /session/status` | All session states | Medium |

#### **Lower Priority - Configuration**

| Feature | OpenCode API | Description | Impact |
|---------|--------------|-------------|--------|
| **Provider Auth** | `POST /auth/:provider` | Set API keys | Medium - Currently env var |
| **Get/Update Config** | `GET/PATCH /config` | System config | Low |
| **List Providers** | `GET /provider` | Available AI providers | Low |
| **List Agents** | `GET /agent` | Available agent types | Low |
| **List Tools** | `GET /experimental/tool` | Available tools | Low |
| **MCP Management** | `GET/POST /mcp` | MCP server config | Low |

#### **Lower Priority - File Operations**

| Feature | OpenCode API | Description | Impact |
|---------|--------------|-------------|--------|
| **List Files** | `GET /file` | Browse filesystem | Low - Agent does this |
| **Read File** | `GET /file/content` | Read file content | Low - Agent does this |
| **Find Text** | `GET /find` | Ripgrep search | Low - Agent does this |
| **Find Files** | `GET /find/file` | File search | Low - Agent does this |

---

### Summary: What's Missing for Feature Completeness

#### **Critical Gaps (Blocking Real Usage)**

1. **No file/image attachments** - Products can't send files to analyze
2. **No session management** - Products can't create/list/manage conversations  
3. **No message history retrieval** - Products can't fetch past messages
4. **No permission handling** - Agent auto-approves everything (security risk)

#### **Important Gaps (Reduced Functionality)**

5. **No agent selection** - Can't choose specialized agents (code-reviewer, etc.)
6. **No multi-part messages** - Can't combine text + files + agent selection
7. **No session compaction** - Long conversations will hit context limits
8. **No revert capability** - Can't undo agent mistakes
9. **No exposed status/model endpoints** - Proto defined but not HTTP exposed

#### **Nice to Have**

10. Question handling for interactive prompts
11. Todo list access
12. Diff/file change tracking
13. Session forking for experimentation
14. Session sharing

---

### Recommended Priority Order

1. **File attachments in SendMessage** - Expand proto and agent to support parts
2. **Session CRUD endpoints** - Expose create/list/get/delete session
3. **Message history endpoint** - Get messages for a session
4. **Permission request handling** - Forward permission.asked events, add reply endpoint
5. **Expose existing proto endpoints** - Add HTTP handlers for SetModel, SetPermissionMode, GetStatus
