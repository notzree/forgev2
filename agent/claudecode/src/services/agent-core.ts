import {
  query,
  type SDKMessage,
  type Options,
} from "@anthropic-ai/claude-agent-sdk";
import { create } from "@bufbuild/protobuf";
import type { AgentConfig } from "../config.js";
import {
  type AgentMessage,
  type ContentBlock,
  AgentMessageSchema,
  UserMessageSchema,
  AssistantMessageSchema,
  SystemMessageSchema,
  StreamEventSchema,
  ResultMessageSchema,
  ContentBlockSchema,
  TextBlockSchema,
  ToolUseBlockSchema,
  ToolResultBlockSchema,
  ThinkingBlockSchema,
  UsageSchema,
} from "../gen/agent/v1/messages_pb.js";
import { AgentState } from "../gen/agent/v1/agent_pb.js";

export class AgentCore {
  private config: AgentConfig;
  private currentQuery: ReturnType<typeof query> | null = null;
  private abortController: AbortController | null = null;
  private sessionId: string | null = null;
  private currentSeq: bigint = 0n;
  private startTime: number;
  private state: AgentState = AgentState.IDLE;

  constructor(config: AgentConfig) {
    this.config = config;
    this.startTime = Date.now();
  }

  getState(): AgentState {
    return this.state;
  }

  getSessionId(): string | null {
    return this.sessionId;
  }

  getLatestSeq(): bigint {
    return this.currentSeq;
  }

  getUptimeMs(): bigint {
    return BigInt(Date.now() - this.startTime);
  }

  private nextSeq(): bigint {
    this.currentSeq++;
    return this.currentSeq;
  }

  async *processMessage(content: string): AsyncGenerator<AgentMessage> {
    this.state = AgentState.PROCESSING;
    this.abortController = new AbortController();

    // Emit user message first
    const userMessage = create(AgentMessageSchema, {
      uuid: crypto.randomUUID(),
      sessionId: this.sessionId || this.config.agentId,
      seq: this.nextSeq(),
      createdAt: BigInt(Date.now()),
      payload: {
        case: "userMessage",
        value: create(UserMessageSchema, { content }),
      },
    });
    yield userMessage;

    // Build options for Claude Agent SDK
    const options: Options = {
      cwd: this.config.cwd,
      model: this.config.model,
      permissionMode: this.config.permissionMode,
      allowedTools: this.config.allowedTools,
      abortController: this.abortController,
      includePartialMessages: true,
      resume: this.sessionId || undefined,
    };

    try {
      this.currentQuery = query({ prompt: content, options });

      for await (const sdkMessage of this.currentQuery) {
        const protoMessage = this.transformSDKMessage(sdkMessage);

        if (protoMessage) {
          // Assign sequence number for non-streaming messages
          if (this.shouldPersist(sdkMessage)) {
            protoMessage.seq = this.nextSeq();
          }
          yield protoMessage;
        }

        // Capture session ID from init message
        if (sdkMessage.type === "system" && sdkMessage.subtype === "init") {
          this.sessionId = sdkMessage.session_id;
        }
      }
    } catch (error) {
      this.state = AgentState.ERROR;
      throw error;
    } finally {
      this.currentQuery = null;
      this.abortController = null;
      this.state = AgentState.IDLE;
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

  async setPermissionMode(mode: string): Promise<void> {
    const validMode = mode as Options["permissionMode"];
    if (this.currentQuery && validMode) {
      await this.currentQuery.setPermissionMode(validMode);
    }
    this.config.permissionMode = mode as AgentConfig["permissionMode"];
  }

  async setModel(model: string): Promise<void> {
    if (this.currentQuery) {
      await this.currentQuery.setModel(model);
    }
    this.config.model = model;
  }

  private transformSDKMessage(sdkMessage: SDKMessage): AgentMessage | null {
    const base = {
      uuid: sdkMessage.uuid || crypto.randomUUID(),
      sessionId: sdkMessage.session_id || this.sessionId || this.config.agentId,
      seq: 0n, // Will be set by caller if needed
      createdAt: BigInt(Date.now()),
    };

    switch (sdkMessage.type) {
      case "user":
        return create(AgentMessageSchema, {
          ...base,
          payload: {
            case: "userMessage",
            value: create(UserMessageSchema, {
              content: this.extractTextContent(sdkMessage.message),
            }),
          },
        });

      case "assistant":
        return create(AgentMessageSchema, {
          ...base,
          payload: {
            case: "assistantMessage",
            value: create(AssistantMessageSchema, {
              content: this.transformContentBlocks(sdkMessage.message.content),
              parentToolUseId: sdkMessage.parent_tool_use_id || undefined,
            }),
          },
        });

      case "system":
        return create(AgentMessageSchema, {
          ...base,
          payload: {
            case: "systemMessage",
            value: create(SystemMessageSchema, {
              subtype: sdkMessage.subtype,
              cwd: "cwd" in sdkMessage ? sdkMessage.cwd : undefined,
              tools: "tools" in sdkMessage ? sdkMessage.tools : undefined,
              model: "model" in sdkMessage ? sdkMessage.model : undefined,
              permissionMode:
                "permissionMode" in sdkMessage
                  ? sdkMessage.permissionMode
                  : undefined,
            }),
          },
        });

      case "stream_event":
        return create(AgentMessageSchema, {
          ...base,
          payload: {
            case: "streamEvent",
            value: create(StreamEventSchema, {
              eventType: sdkMessage.event.type,
              eventJson: JSON.stringify(sdkMessage.event),
              parentToolUseId: sdkMessage.parent_tool_use_id || undefined,
            }),
          },
        });

      case "result":
        return create(AgentMessageSchema, {
          ...base,
          payload: {
            case: "resultMessage",
            value: create(ResultMessageSchema, {
              subtype: sdkMessage.subtype,
              isError: sdkMessage.is_error,
              result: "result" in sdkMessage ? sdkMessage.result : undefined,
              totalCostUsd: sdkMessage.total_cost_usd,
              numTurns: sdkMessage.num_turns,
              durationMs: BigInt(sdkMessage.duration_ms),
              durationApiMs: BigInt(sdkMessage.duration_api_ms),
              usage: create(UsageSchema, {
                inputTokens: sdkMessage.usage.input_tokens || 0,
                outputTokens: sdkMessage.usage.output_tokens || 0,
                cacheReadInputTokens:
                  sdkMessage.usage.cache_read_input_tokens || 0,
                cacheCreationInputTokens:
                  sdkMessage.usage.cache_creation_input_tokens || 0,
              }),
              errors: "errors" in sdkMessage ? sdkMessage.errors : [],
            }),
          },
        });

      default:
        console.warn(
          `Unknown message type: ${(sdkMessage as SDKMessage).type}`,
        );
        return null;
    }
  }

  private extractTextContent(message: { content: unknown }): string {
    if (typeof message.content === "string") {
      return message.content;
    }
    if (Array.isArray(message.content)) {
      return message.content
        .filter((block: { type: string }) => block.type === "text")
        .map((block: { text: string }) => block.text)
        .join("");
    }
    return "";
  }

  private transformContentBlocks(content: unknown[]): ContentBlock[] {
    return content.map((block: unknown) => {
      const b = block as { type: string; [key: string]: unknown };
      switch (b.type) {
        case "text":
          return create(ContentBlockSchema, {
            block: {
              case: "text",
              value: create(TextBlockSchema, { text: b.text as string }),
            },
          });
        case "tool_use":
          return create(ContentBlockSchema, {
            block: {
              case: "toolUse",
              value: create(ToolUseBlockSchema, {
                id: b.id as string,
                name: b.name as string,
                inputJson: JSON.stringify(b.input),
              }),
            },
          });
        case "tool_result":
          return create(ContentBlockSchema, {
            block: {
              case: "toolResult",
              value: create(ToolResultBlockSchema, {
                toolUseId: b.tool_use_id as string,
                contentJson: JSON.stringify(b.content),
                isError: (b.is_error as boolean) || false,
              }),
            },
          });
        case "thinking":
          return create(ContentBlockSchema, {
            block: {
              case: "thinking",
              value: create(ThinkingBlockSchema, {
                thinking: b.thinking as string,
                signature: (b.signature as string) || "",
              }),
            },
          });
        default:
          return create(ContentBlockSchema, {
            block: {
              case: "text",
              value: create(TextBlockSchema, { text: JSON.stringify(block) }),
            },
          });
      }
    });
  }

  private shouldPersist(message: SDKMessage): boolean {
    // Don't persist streaming events - they're ephemeral
    return message.type !== "stream_event";
  }
}
