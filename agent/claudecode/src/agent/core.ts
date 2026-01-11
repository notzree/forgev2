import {
  query,
  type SDKMessage,
  type Options,
} from "@anthropic-ai/claude-agent-sdk";
import type { AgentConfig } from "../config.ts";
import type { Message } from "../gen/agent/v1/messages_pb.ts";

import { AgentState } from "../gen/agent/v1/agent_pb.ts";
import { sdkMessageToProto, createUserMessage } from "./serde.ts";
export class AgentCore {
  private config: AgentConfig;
  private session_id: string | undefined;
  private current_query: ReturnType<typeof query> | null = null;
  private abort_controller: AbortController | null = null;
  private seq: bigint = 0n;
  private start_time: number;
  private state: AgentState = AgentState.IDLE;
  constructor(config: AgentConfig) {
    this.config = config;
    this.start_time = Date.now();
  }
  getSessionID(): string | undefined {
    return this.session_id;
  }
  getState(): AgentState {
    return this.state;
  }

  getLatestSeq(): bigint {
    return this.seq;
  }

  getUptimeMs(): bigint {
    return BigInt(Date.now() - this.start_time);
  }

  private nextSeq(): bigint {
    this.seq++;
    return this.seq;
  }

  async *processMessage(content: string): AsyncGenerator<Message> {
    // TODO: support multiple sessions per container. Right now, we assume theres only one (no forking)
    this.state = AgentState.PROCESSING;
    this.abort_controller = new AbortController();
    let yieldUserMessage = false;
    if (this.session_id) {
      // Emit user message first
      const userMessage = createUserMessage(
        content,
        this.session_id,
        this.nextSeq(),
      );
      yield userMessage;
      yieldUserMessage = true;
    }

    // Build options for Claude Agent SDK
    const options: Options = {
      cwd: this.config.cwd,
      model: this.config.model,
      permissionMode: this.config.permissionMode,
      allowedTools: this.config.allowedTools,
      abortController: this.abort_controller,
      includePartialMessages: true,
      resume: this.session_id,
    };

    try {
      console.log("[AgentCore] Starting query with options:", {
        cwd: options.cwd,
        model: options.model,
        permissionMode: options.permissionMode,
        allowedTools: options.allowedTools,
        resume: options.resume,
      });

      this.current_query = query({ prompt: content, options });
      console.log("[AgentCore] Query created, starting iteration...");

      for await (const sdkMessage of this.current_query) {
        console.log("[AgentCore] Received SDK message:", {
          type: sdkMessage.type,
          session_id: sdkMessage.session_id,
        });

        if (!this.session_id) {
          this.session_id = sdkMessage.session_id;
        }
        if (!yieldUserMessage) {
          const userMessage = createUserMessage(
            content,
            this.session_id,
            this.nextSeq(),
          );
          yieldUserMessage = true;
        }
        const protoMessage = sdkMessageToProto(sdkMessage, {
          sessionId: sdkMessage.session_id,
        });
        // Assign sequence number to all messages
        protoMessage.seq = this.nextSeq();
        yield protoMessage;
      }
      console.log("[AgentCore] Query iteration completed");
    } catch (error) {
      console.error("[AgentCore] Query failed with error:", error);
      console.error("[AgentCore] Error details:", {
        name: error instanceof Error ? error.name : "unknown",
        message: error instanceof Error ? error.message : String(error),
        stack: error instanceof Error ? error.stack : undefined,
      });
      this.state = AgentState.ERROR;
      throw error;
    } finally {
      this.current_query = null;
      this.abort_controller = null;
      this.state = AgentState.IDLE;
    }
  }

  async interrupt(): Promise<void> {
    if (this.abort_controller) {
      this.abort_controller.abort();
    }
    if (this.current_query) {
      await this.current_query.interrupt();
    }
  }

  async setPermissionMode(mode: string): Promise<void> {
    const validMode = mode as Options["permissionMode"];
    if (this.current_query && validMode) {
      await this.current_query.setPermissionMode(validMode);
    }
    this.config.permissionMode = mode as AgentConfig["permissionMode"];
  }

  async setModel(model: string): Promise<void> {
    if (this.current_query) {
      await this.current_query.setModel(model);
    }
    this.config.model = model;
  }
}
