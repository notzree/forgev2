import { create } from "@bufbuild/protobuf";
import type { AgentConfig } from "../config.ts";
import { AgentCore } from "../agent/core.ts";
import {
  type AgentCommand,
  type AgentEvent,
  type GetStatusResponse,
  type ShutdownRequest,
  type ShutdownResponse,
  AgentEventSchema,
  AckEventSchema,
  ErrorEventSchema,
  GetStatusResponseSchema,
  ShutdownResponseSchema,
} from "../gen/agent/v1/agent_pb.ts";
import type { Message } from "../gen/agent/v1/messages_pb.ts";

/**
 * AgentService implements the gRPC service handlers for the agent.
 * This is the main business logic layer that coordinates between
 * the gRPC interface and the AgentCore.
 */
export class AgentService {
  private core: AgentCore;
  private config: AgentConfig;
  private messageStore: Map<bigint, Message> = new Map();

  constructor(config: AgentConfig) {
    this.config = config;
    this.core = new AgentCore(config);
  }

  /**
   * Handle a SendMessage command - sends user input to Claude
   */
  async *handleSendMessage(
    requestId: string,
    content: string,
  ): AsyncGenerator<AgentEvent> {
    for await (const message of this.core.processMessage(content)) {
      // Store non-streaming messages for catch-up
      if (message.seq > 0n) {
        this.messageStore.set(message.seq, message);
      }

      yield this.createMessageEvent(requestId, message);
    }
  }

  /**
   * Handle an Interrupt command - cancels current generation
   */
  async handleInterrupt(requestId: string): Promise<AgentEvent> {
    await this.core.interrupt();
    return this.createAckEvent(requestId, "Interrupted");
  }

  /**
   * Handle a SetPermissionMode command
   */
  async handleSetPermissionMode(
    requestId: string,
    mode: string,
  ): Promise<AgentEvent> {
    await this.core.setPermissionMode(mode);
    return this.createAckEvent(requestId, `Permission mode set to ${mode}`);
  }

  /**
   * Handle a SetModel command
   */
  async handleSetModel(requestId: string, model: string): Promise<AgentEvent> {
    await this.core.setModel(model);
    return this.createAckEvent(requestId, `Model set to ${model}`);
  }

  /**
   * Handle unknown commands
   */
  handleUnknownCommand(requestId: string, commandCase: string): AgentEvent {
    return this.createErrorEvent(
      requestId,
      "UNKNOWN_COMMAND",
      `Unknown command: ${commandCase}`,
      false,
    );
  }

  /**
   * Handle processing errors
   */
  handleError(requestId: string, error: unknown): AgentEvent {
    const message = error instanceof Error ? error.message : String(error);
    return this.createErrorEvent(requestId, "PROCESSING_ERROR", message, false);
  }

  /**
   * Get current agent status
   */
  getStatus(): GetStatusResponse {
    return create(GetStatusResponseSchema, {
      sessionId: this.core.getSessionID() || "",
      state: this.core.getState(),
      latestSeq: this.core.getLatestSeq(),
      currentModel: this.config.model || "claude-sonnet-4-20250514",
      permissionMode: this.config.permissionMode,
      uptimeMs: this.core.getUptimeMs(),
    });
  }

  /**
   * Shutdown the agent
   */
  async shutdown(request: ShutdownRequest): Promise<ShutdownResponse> {
    if (request.graceful) {
      await this.core.interrupt();
    }

    // Schedule shutdown after response is sent
    this.scheduleShutdown();

    return create(ShutdownResponseSchema, {
      success: true,
    });
  }

  // --- Private helper methods ---

  private getMessagesSince(fromSeq: bigint, limit: number): Message[] {
    const messages: Message[] = [];
    const sortedKeys = Array.from(this.messageStore.keys()).sort((a, b) =>
      a < b ? -1 : a > b ? 1 : 0,
    );

    let count = 0;
    for (const seq of sortedKeys) {
      if (seq > fromSeq && count < limit) {
        const msg = this.messageStore.get(seq);
        if (msg) {
          messages.push(msg);
          count++;
        }
      }
    }

    return messages;
  }

  private createMessageEvent(requestId: string, message: Message): AgentEvent {
    return create(AgentEventSchema, {
      requestId,
      event: { case: "message", value: message },
    });
  }

  private createAckEvent(requestId: string, message: string): AgentEvent {
    return create(AgentEventSchema, {
      requestId,
      event: {
        case: "ack",
        value: create(AckEventSchema, { success: true, message }),
      },
    });
  }

  private createErrorEvent(
    requestId: string,
    code: string,
    message: string,
    fatal: boolean,
  ): AgentEvent {
    return create(AgentEventSchema, {
      requestId,
      event: {
        case: "error",
        value: create(ErrorEventSchema, { code, message, fatal }),
      },
    });
  }

  private scheduleShutdown(): void {
    setTimeout(() => {
      console.log("Shutting down agent...");
      process.exit(0);
    }, 100);
  }
}
