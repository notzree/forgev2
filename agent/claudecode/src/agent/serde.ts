/**
 * Serialization/Deserialization utilities for converting between
 * Claude Agent SDK messages and protobuf messages.
 */

import { create } from "@bufbuild/protobuf";
import type { SDKMessage } from "@anthropic-ai/claude-agent-sdk";
import {
  type Message,
  type ContentBlock,
  MessageSchema,
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
} from "../gen/agent/v1/messages_pb.ts";

/**
 * Context needed for message transformation
 */
export interface TransformContext {
  sessionId: string;
}

/**
 * Error thrown when parsing an SDK message fails
 */
export class ParseError extends Error {
  constructor(
    message: string,
    public readonly messageType?: string,
  ) {
    super(message);
    this.name = "ParseError";
  }
}

/**
 * Transforms a Claude SDK message to a protobuf AgentMessage.
 * @throws {ParseError} if the message type is unknown
 */
export function sdkMessageToProto(
  sdkMessage: SDKMessage,
  context: TransformContext,
): Message {
  const base = {
    uuid: sdkMessage.uuid || crypto.randomUUID(),
    sessionId: sdkMessage.session_id || context.sessionId,
    seq: 0n, // Will be set by caller
    createdAt: BigInt(Date.now()),
  };

  switch (sdkMessage.type) {
    case "user":
      return create(MessageSchema, {
        ...base,
        payload: {
          case: "userMessage",
          value: create(UserMessageSchema, {
            content: extractTextContent(sdkMessage.message),
          }),
        },
      });

    case "assistant":
      return create(MessageSchema, {
        ...base,
        payload: {
          case: "assistantMessage",
          value: create(AssistantMessageSchema, {
            content: transformContentBlocks(sdkMessage.message.content),
            parentToolUseId: sdkMessage.parent_tool_use_id || undefined,
          }),
        },
      });

    case "system":
      return create(MessageSchema, {
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
      return create(MessageSchema, {
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
      return create(MessageSchema, {
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

    default: {
      const unknownType = (sdkMessage as SDKMessage).type;
      throw new ParseError(
        `Unknown SDK message type: ${unknownType}`,
        unknownType,
      );
    }
  }
}

/**
 * Creates a user message protobuf from content string
 */
export function createUserMessage(
  content: string,
  session_id: string,
  seq: bigint = 0n,
): Message {
  return create(MessageSchema, {
    uuid: crypto.randomUUID(),
    sessionId: session_id,
    seq,
    createdAt: BigInt(Date.now()),
    payload: {
      case: "userMessage",
      value: create(UserMessageSchema, { content }),
    },
  });
}

// --- Helper Functions ---

/**
 * Extracts text content from an SDK message
 */
function extractTextContent(message: { content: unknown }): string {
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

/**
 * Transforms SDK content blocks to protobuf ContentBlocks
 */
function transformContentBlocks(content: unknown[]): ContentBlock[] {
  return content.map(transformContentBlock);
}

/**
 * Transforms a single SDK content block to protobuf
 */
function transformContentBlock(block: unknown): ContentBlock {
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
      // Unknown block type - serialize as text
      return create(ContentBlockSchema, {
        block: {
          case: "text",
          value: create(TextBlockSchema, { text: JSON.stringify(block) }),
        },
      });
  }
}
