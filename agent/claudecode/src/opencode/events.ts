/**
 * OpenCode event filtering and types.
 * This module handles filtering OpenCode SSE events to only forward relevant ones.
 *
 * OpenCode Event Semantics:
 * - Messages: created/updated via "message.updated", deleted via "message.removed"
 * - Message parts: streamed via "message.part.updated" (with optional delta field)
 * - Session status: "idle" | "busy" | "retry" via "session.status"
 * - Completion: session.status with type="idle" or session.error
 * - Tools: state machine (pending → running → completed/error) inside message.part.updated
 */

/**
 * OpenCode event structure from the SDK
 */
export interface OpencodeEvent {
  type: string;
  properties?: {
    sessionId?: string;
    messageId?: string;
    // For session.status events
    status?: {
      type?: "idle" | "busy" | "retry";
      attempt?: number;
      message?: string;
      next?: number;
    };
    // For message.updated events
    message?: {
      role?: "user" | "assistant";
      time?: {
        created?: number;
        completed?: number;
      };
      finish?: "stop" | "tool-calls" | "max-tokens" | "unknown";
      error?: unknown;
    };
    // For message.part.updated events
    part?: {
      id?: string;
      type?: string;
      // For text/reasoning parts - streaming
      text?: string;
      delta?: string;
      // For tool parts
      state?: "pending" | "running" | "completed" | "error";
      input?: unknown;
      output?: string;
      error?: string;
    };
    [key: string]: unknown;
  };
}

/**
 * Events that should be forwarded to webhook consumers.
 * These provide meaningful information about the agent's activity.
 *
 * Based on OpenCode source code analysis - these are the actual event types emitted.
 */
const FORWARDED_EVENT_TYPES = new Set([
  // Message events (OpenCode actual names)
  "message.updated", // Message created or modified (no separate "created" event)
  "message.removed", // Message deleted
  "message.part.updated", // Part streaming (has delta for real-time text)
  "message.part.removed", // Part removed

  // Session events
  "session.created", // New session created
  "session.updated", // Session metadata changed
  "session.deleted", // Session deleted
  "session.status", // Status changes: idle/busy/retry - KEY FOR COMPLETION
  "session.idle", // Deprecated but still emitted when status → idle
  "session.error", // Fatal session error
  "session.diff", // File changes in session
  "session.compacted", // Messages were summarized to save context

  // Permission events (important for interactive UX)
  "permission.asked", // Agent needs permission to perform action
  "permission.replied", // Permission granted/denied/rejected

  // Question events (agent asking user questions)
  "question.asked", // Agent asking questions
  "question.replied", // User answered
  "question.rejected", // User rejected questions

  // File events
  "file.edited", // File was edited

  // Todo events
  "todo.updated", // Todo list changed
]);

/**
 * Events that should be filtered out (not forwarded).
 * These are internal/infrastructure events not useful to consumers.
 */
const FILTERED_EVENT_TYPES = new Set([
  "server.heartbeat", // Keep-alive ping
  "server.connected", // Initial connection established
  "server.instance.disposed", // Server cleanup
  "global.disposed", // Global cleanup
  "lsp.client.diagnostics", // LSP diagnostics (internal)
  "lsp.updated", // LSP state (internal)
  "file.watcher.updated", // File system watcher (internal)
  "installation.updated", // OpenCode version updates
  "installation.update-available", // Update notifications
  "project.updated", // Project metadata (internal)
  "mcp.tools.changed", // MCP tool changes (internal)
  "command.executed", // Command execution (internal)
  "vcs.branch.updated", // Git branch changes (internal)

  // TUI-specific events (not relevant for API consumers)
  "tui.prompt.append",
  "tui.command.execute",
  "tui.toast.show",
  "tui.session.select",

  // PTY events (terminal sessions - internal)
  "pty.created",
  "pty.updated",
  "pty.exited",
  "pty.deleted",
]);

/**
 * Checks if an event should be forwarded to webhook consumers.
 */
export function shouldForwardEvent(event: OpencodeEvent): boolean {
  // Always filter out known infrastructure events
  if (FILTERED_EVENT_TYPES.has(event.type)) {
    return false;
  }

  // Forward known useful events
  if (FORWARDED_EVENT_TYPES.has(event.type)) {
    return true;
  }

  // For unknown events, forward them (safer default - don't lose data)
  // Log for debugging
  console.log(`[OpenCode] Unknown event type: ${event.type}`);
  return true;
}

/**
 * Checks if an event belongs to a specific session.
 * Returns true if the event has no session ID (global event) or matches the session.
 */
export function isEventForSession(
  event: OpencodeEvent,
  sessionId: string,
): boolean {
  // Events without sessionId are global/broadcast events
  if (!event.properties?.sessionId) {
    return true;
  }

  return event.properties.sessionId === sessionId;
}

/**
 * Checks if an event indicates session-level completion.
 *
 * A session is "complete" (ready for next user input) when:
 * 1. session.status event arrives with type="idle"
 * 2. session.idle event arrives (deprecated but still emitted)
 * 3. session.error event arrives (fatal error)
 *
 * Note: This is different from message completion. A message can complete
 * while the session continues processing (e.g., multi-turn tool calls).
 */
export function isCompletionEvent(event: OpencodeEvent): boolean {
  // Session error is always a completion (fatal)
  if (event.type === "session.error") {
    return true;
  }

  // Session status idle means processing is done
  if (event.type === "session.status") {
    const status = event.properties?.status;
    return status?.type === "idle";
  }

  // Deprecated but still emitted for backwards compatibility
  if (event.type === "session.idle") {
    return true;
  }

  return false;
}

/**
 * Checks if a message.updated event indicates the assistant message is complete.
 *
 * An assistant message is complete when:
 * - It has role="assistant"
 * - It has time.completed set (timestamp when generation finished)
 *
 * The finish field indicates WHY it completed:
 * - "stop": Normal completion (no more work needed)
 * - "tool-calls": Ended with tool calls (may continue after tools run)
 * - "max-tokens": Hit token limit
 * - "unknown": Unknown reason
 */
export function isMessageComplete(event: OpencodeEvent): boolean {
  if (event.type !== "message.updated") {
    return false;
  }

  const message = event.properties?.message;

  // Must be an assistant message with completed timestamp
  return (
    message?.role === "assistant" && message?.time?.completed !== undefined
  );
}

/**
 * Gets the finish reason from a message.updated event.
 * Returns undefined if not a completed assistant message.
 */
export function getMessageFinishReason(
  event: OpencodeEvent,
): "stop" | "tool-calls" | "max-tokens" | "unknown" | undefined {
  if (!isMessageComplete(event)) {
    return undefined;
  }

  return event.properties?.message?.finish;
}

/**
 * Checks if an event indicates an error.
 */
export function isErrorEvent(event: OpencodeEvent): boolean {
  return event.type === "session.error";
}

/**
 * Checks if an event is a streaming text delta.
 * These events contain incremental text updates for real-time display.
 */
export function isStreamingDelta(event: OpencodeEvent): boolean {
  if (event.type !== "message.part.updated") {
    return false;
  }

  const part = event.properties?.part;
  return (
    (part?.type === "text" || part?.type === "reasoning") &&
    part?.delta !== undefined
  );
}

/**
 * Checks if an event is a tool state change.
 * Tool states: pending → running → completed/error
 */
export function isToolStateChange(event: OpencodeEvent): boolean {
  if (event.type !== "message.part.updated") {
    return false;
  }

  const part = event.properties?.part;
  return part?.type === "tool" && part?.state !== undefined;
}

/**
 * Gets the tool state from a message.part.updated event.
 */
export function getToolState(
  event: OpencodeEvent,
): "pending" | "running" | "completed" | "error" | undefined {
  if (!isToolStateChange(event)) {
    return undefined;
  }

  return event.properties?.part?.state;
}
