/**
 * AgentService - Direct OpenCode SDK integration with event pass-through.
 *
 * This service:
 * 1. Manages one OpenCode session per pod
 * 2. Streams OpenCode events as raw JSON to the platform
 * 3. Filters out infrastructure events (heartbeats, etc.)
 */

import { create } from "@bufbuild/protobuf";
import { createOpencodeClient } from "@opencode-ai/sdk";
import type { AgentConfig } from "../config.ts";
import {
  type AgentResponse,
  type GetStatusResponse,
  type ShutdownRequest,
  type ShutdownResponse,
  AgentResponseSchema,
  AgentState,
  CompletePayloadSchema,
  ErrorPayloadSchema,
  EventPayloadSchema,
  GetStatusResponseSchema,
  ShutdownResponseSchema,
} from "../gen/agent/v1/agent_pb.ts";
import {
  type OpencodeEvent,
  shouldForwardEvent,
  isEventForSession,
  isCompletionEvent,
  isMessageComplete,
  isErrorEvent,
  getMessageFinishReason,
} from "../opencode/events.ts";

export class AgentService {
  private config: AgentConfig;
  private client: ReturnType<typeof createOpencodeClient>;
  private sessionId: string | undefined;
  private seq: bigint = 0n;
  private state: AgentState = AgentState.IDLE;
  private startTime: number;
  private abortController: AbortController | null = null;
  private currentModel: string;

  constructor(config: AgentConfig) {
    this.config = config;
    this.startTime = Date.now();
    this.currentModel = config.model;

    // Create OpenCode client
    this.client = createOpencodeClient({
      baseUrl: config.opencodeBaseUrl,
    });

    // Initialize session and auth at startup
    this.initialize().catch((err) => {
      console.error("[AgentService] Failed to initialize:", err);
    });
  }

  /**
   * Initialize OpenCode session and authentication.
   * Called at startup.
   */
  private async initialize(): Promise<void> {
    console.log("[AgentService] Initializing OpenCode client...");

    // Set up authentication if API key is provided
    if (this.config.opencodeApiKey) {
      const providerId = this.getProviderIdForModel(this.currentModel);
      console.log(`[AgentService] Setting up auth for provider: ${providerId}`);
      await this.client.auth.set({
        path: { id: providerId },
        body: { type: "api", key: this.config.opencodeApiKey },
      });
    }

    // Create session
    console.log("[AgentService] Creating OpenCode session...");
    const session = await this.client.session.create({
      body: { title: `Agent ${this.config.agentId}` },
    });

    if (!session.data?.id) {
      throw new Error("Failed to create OpenCode session");
    }

    this.sessionId = session.data.id;
    console.log(`[AgentService] Session created: ${this.sessionId}`);
  }

  /**
   * Ensure session is initialized before processing messages.
   */
  private async ensureSession(): Promise<string> {
    if (!this.sessionId) {
      await this.initialize();
    }
    if (!this.sessionId) {
      throw new Error("Session not initialized");
    }
    return this.sessionId;
  }

  private nextSeq(): bigint {
    this.seq++;
    return this.seq;
  }

  /**
   * Handle a SendMessage command - sends user input to OpenCode
   * and streams back raw events as JSON.
   */
  async *handleSendMessage(
    requestId: string,
    content: string,
  ): AsyncGenerator<AgentResponse> {
    const sessionId = await this.ensureSession();
    this.state = AgentState.PROCESSING;
    this.abortController = new AbortController();

    try {
      console.log(
        `[AgentService] Processing message for session: ${sessionId}`,
      );

      // Subscribe to SSE events before sending the prompt
      const eventSubscription = await this.client.event.subscribe();

      // Get model config
      const modelConfig = this.getModelConfig(this.currentModel);

      // Send the prompt (don't await - we'll process events as they stream)
      const promptPromise = this.client.session.prompt({
        path: { id: sessionId },
        body: {
          model: modelConfig,
          parts: [{ type: "text", text: content }],
        },
      });

      // Stream events
      try {
        for await (const event of eventSubscription.stream as AsyncIterable<OpencodeEvent>) {
          // Check for abort
          if (this.abortController?.signal.aborted) {
            console.log("[AgentService] Aborted, breaking event loop");
            break;
          }

          // Filter events for our session
          if (!isEventForSession(event, sessionId)) {
            continue;
          }

          // Filter out infrastructure events
          if (!shouldForwardEvent(event)) {
            continue;
          }

          console.log(`[AgentService] Forwarding event: ${event.type}`);

          // Yield the event as raw JSON
          yield this.createEventResponse(requestId, sessionId, event);

          // Log message completion (but don't break - session may continue with tools)
          if (isMessageComplete(event)) {
            const finishReason = getMessageFinishReason(event);
            console.log(
              `[AgentService] Assistant message completed (reason: ${finishReason})`,
            );
          }

          // Check for session-level completion (idle or error)
          if (isCompletionEvent(event)) {
            console.log(
              `[AgentService] Session completed (event: ${event.type})`,
            );
            break;
          }
        }
      } catch (streamError) {
        console.log("[AgentService] Event stream ended:", streamError);
      }

      // Wait for the prompt to complete
      const response = await promptPromise;

      if (response.error) {
        console.error("[AgentService] Prompt error:", response.error);
        yield this.createErrorResponse(
          requestId,
          sessionId,
          "PROMPT_ERROR",
          String(response.error),
          false,
        );
      }

      // Set state to IDLE before sending completion so webhook reflects correct state
      this.state = AgentState.IDLE;
      yield this.createCompleteResponse(requestId, sessionId, true);
      console.log("[AgentService] Message processing completed");
    } catch (error) {
      console.error("[AgentService] Error processing message:", error);
      yield this.createErrorResponse(
        requestId,
        sessionId,
        "PROCESSING_ERROR",
        error instanceof Error ? error.message : String(error),
        false,
      );
      // Set state to IDLE before sending completion so webhook reflects correct state
      this.state = AgentState.IDLE;
      yield this.createCompleteResponse(requestId, sessionId, false);
    } finally {
      // Ensure state is IDLE even if an error occurred during completion
      this.state = AgentState.IDLE;
      this.abortController = null;
    }
  }

  /**
   * Handle an Interrupt command - cancels current generation
   */
  async handleInterrupt(requestId: string): Promise<AgentResponse> {
    console.log("[AgentService] Interrupting...");

    if (this.abortController) {
      this.abortController.abort();
    }

    if (this.sessionId) {
      try {
        await this.client.session.abort({ path: { id: this.sessionId } });
      } catch (error) {
        console.error("[AgentService] Error aborting session:", error);
      }
    }

    return this.createCompleteResponse(requestId, this.sessionId || "", true);
  }

  /**
   * Handle a SetPermissionMode command
   */
  async handleSetPermissionMode(
    requestId: string,
    mode: string,
  ): Promise<AgentResponse> {
    console.log(`[AgentService] Setting permission mode: ${mode}`);
    // OpenCode permission modes are set per-session or via config
    // For now, we store it and apply on next prompt if needed
    this.config.permissionMode = mode as AgentConfig["permissionMode"];

    return this.createCompleteResponse(requestId, this.sessionId || "", true);
  }

  /**
   * Handle a SetModel command
   */
  async handleSetModel(
    requestId: string,
    model: string,
  ): Promise<AgentResponse> {
    console.log(`[AgentService] Setting model: ${model}`);
    this.currentModel = model;

    return this.createCompleteResponse(requestId, this.sessionId || "", true);
  }

  /**
   * Handle unknown commands
   */
  handleUnknownCommand(requestId: string, commandCase: string): AgentResponse {
    return this.createErrorResponse(
      requestId,
      this.sessionId || "",
      "UNKNOWN_COMMAND",
      `Unknown command: ${commandCase}`,
      false,
    );
  }

  /**
   * Handle processing errors
   */
  handleError(requestId: string, error: unknown): AgentResponse {
    const message = error instanceof Error ? error.message : String(error);
    return this.createErrorResponse(
      requestId,
      this.sessionId || "",
      "PROCESSING_ERROR",
      message,
      false,
    );
  }

  /**
   * Get current agent status
   */
  getStatus(): GetStatusResponse {
    return create(GetStatusResponseSchema, {
      agentId: this.config.agentId,
      sessionId: this.sessionId || "",
      state: this.state,
      latestSeq: this.seq,
      currentModel: this.currentModel,
      permissionMode: this.config.permissionMode,
      uptimeMs: BigInt(Date.now() - this.startTime),
    });
  }

  /**
   * Shutdown the agent
   */
  async shutdown(request: ShutdownRequest): Promise<ShutdownResponse> {
    console.log("[AgentService] Shutting down...");

    if (request.graceful && this.abortController) {
      this.abortController.abort();
    }

    // Clean up session
    if (this.sessionId) {
      try {
        await this.client.session.delete({ path: { id: this.sessionId } });
      } catch (error) {
        console.error("[AgentService] Error deleting session:", error);
      }
    }

    // Schedule shutdown after response is sent
    setTimeout(() => {
      console.log("Shutting down agent...");
      process.exit(0);
    }, 100);

    return create(ShutdownResponseSchema, {
      success: true,
    });
  }

  // --- Private helper methods ---

  private createEventResponse(
    requestId: string,
    sessionId: string,
    event: OpencodeEvent,
  ): AgentResponse {
    return create(AgentResponseSchema, {
      requestId,
      sessionId,
      seq: this.nextSeq(),
      timestamp: BigInt(Date.now()),
      state: this.state,
      payload: {
        case: "event",
        value: create(EventPayloadSchema, {
          eventType: event.type,
          eventJson: new TextEncoder().encode(JSON.stringify(event)),
        }),
      },
    });
  }

  private createErrorResponse(
    requestId: string,
    sessionId: string,
    code: string,
    message: string,
    fatal: boolean,
  ): AgentResponse {
    return create(AgentResponseSchema, {
      requestId,
      sessionId,
      seq: this.nextSeq(),
      timestamp: BigInt(Date.now()),
      state: this.state,
      payload: {
        case: "error",
        value: create(ErrorPayloadSchema, { code, message, fatal }),
      },
    });
  }

  private createCompleteResponse(
    requestId: string,
    sessionId: string,
    success: boolean,
  ): AgentResponse {
    return create(AgentResponseSchema, {
      requestId,
      sessionId,
      seq: this.nextSeq(),
      timestamp: BigInt(Date.now()),
      state: this.state,
      payload: {
        case: "complete",
        value: create(CompletePayloadSchema, { success }),
      },
    });
  }

  /**
   * Gets the model configuration for the prompt.
   */
  private getModelConfig(
    model: string,
  ): { providerID: string; modelID: string } | undefined {
    if (!model) return undefined;

    // OpenCode Zen models: "opencode/claude-sonnet-4" -> providerID: "opencode", modelID: "claude-sonnet-4"
    if (model.startsWith("opencode/")) {
      return {
        providerID: "opencode",
        modelID: model.substring("opencode/".length),
      };
    }

    // Direct provider models: "anthropic/claude-sonnet-4" format
    if (model.includes("/")) {
      const [providerID, modelID] = model.split("/", 2);
      return { providerID, modelID };
    }

    // Default: use the model name directly with inferred provider
    return {
      providerID: this.getProviderIdForModel(model),
      modelID: model,
    };
  }

  /**
   * Maps model IDs to provider IDs for OpenCode.
   */
  private getProviderIdForModel(model: string): string {
    if (model.startsWith("opencode/")) return "opencode";
    if (model.startsWith("claude")) return "anthropic";
    if (model.startsWith("gpt")) return "openai";
    if (model.startsWith("gemini")) return "google";
    if (model.startsWith("llama") || model.startsWith("mixtral")) return "groq";
    return "opencode";
  }
}
