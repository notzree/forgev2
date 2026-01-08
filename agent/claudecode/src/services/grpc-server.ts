import type { ConnectRouter } from "@connectrpc/connect";
import { fastifyConnectPlugin } from "@connectrpc/connect-fastify";
import { fastify, type FastifyInstance } from "fastify";
import { create } from "@bufbuild/protobuf";
import type { AgentConfig } from "../config.js";
import { AgentCore } from "./agent-core.js";
import {
  AgentService,
  type AgentCommand,
  type AgentEvent,
  type CatchUpRequest,
  type CatchUpResponse,
  type GetStatusResponse,
  type ShutdownRequest,
  type ShutdownResponse,
  AgentEventSchema,
  AckEventSchema,
  ErrorEventSchema,
  GetStatusResponseSchema,
  CatchUpResponseSchema,
  ShutdownResponseSchema,
  AgentState,
} from "../gen/agent/v1/agent_pb.js";
import type { AgentMessage } from "../gen/agent/v1/messages_pb.js";

export async function createServer(
  config: AgentConfig,
): Promise<FastifyInstance> {
  const agent = new AgentCore(config);

  // Track stored messages for catch-up (in production, use AgentFS)
  const messageStore: Map<bigint, AgentMessage> = new Map();

  const routes = (router: ConnectRouter) => {
    router.service(AgentService, {
      // Bidirectional streaming RPC
      async *connect(
        requests: AsyncIterable<AgentCommand>,
      ): AsyncIterable<AgentEvent> {
        for await (const command of requests) {
          const requestId = command.requestId;

          try {
            switch (command.command.case) {
              case "sendMessage": {
                const { content } = command.command.value;

                for await (const message of agent.processMessage(content)) {
                  // Store non-streaming messages for catch-up
                  if (message.seq > 0n) {
                    messageStore.set(message.seq, message);
                  }

                  yield create(AgentEventSchema, {
                    requestId,
                    event: {
                      case: "message",
                      value: message,
                    },
                  });
                }
                break;
              }

              case "interrupt": {
                await agent.interrupt();
                yield create(AgentEventSchema, {
                  requestId,
                  event: {
                    case: "ack",
                    value: create(AckEventSchema, {
                      success: true,
                      message: "Interrupted",
                    }),
                  },
                });
                break;
              }

              case "setPermissionMode": {
                const { mode } = command.command.value;
                await agent.setPermissionMode(mode);
                yield create(AgentEventSchema, {
                  requestId,
                  event: {
                    case: "ack",
                    value: create(AckEventSchema, {
                      success: true,
                      message: `Permission mode set to ${mode}`,
                    }),
                  },
                });
                break;
              }

              case "setModel": {
                const { model } = command.command.value;
                await agent.setModel(model);
                yield create(AgentEventSchema, {
                  requestId,
                  event: {
                    case: "ack",
                    value: create(AckEventSchema, {
                      success: true,
                      message: `Model set to ${model}`,
                    }),
                  },
                });
                break;
              }

              default:
                yield create(AgentEventSchema, {
                  requestId,
                  event: {
                    case: "error",
                    value: create(ErrorEventSchema, {
                      code: "UNKNOWN_COMMAND",
                      message: `Unknown command: ${command.command.case}`,
                      fatal: false,
                    }),
                  },
                });
            }
          } catch (error) {
            const errorMessage =
              error instanceof Error ? error.message : String(error);
            yield create(AgentEventSchema, {
              requestId,
              event: {
                case: "error",
                value: create(ErrorEventSchema, {
                  code: "PROCESSING_ERROR",
                  message: errorMessage,
                  fatal: false,
                }),
              },
            });
          }
        }
      },

      // Unary RPC for status
      async getStatus(): Promise<GetStatusResponse> {
        return create(GetStatusResponseSchema, {
          agentId: config.agentId,
          sessionId: agent.getSessionId() || "",
          state: agent.getState(),
          latestSeq: agent.getLatestSeq(),
          currentModel: config.model || "claude-sonnet-4-20250514",
          permissionMode: config.permissionMode,
          uptimeMs: agent.getUptimeMs(),
        });
      },

      // Unary RPC for catch-up
      async catchUp(request: CatchUpRequest): Promise<CatchUpResponse> {
        const fromSeq = request.fromSeq;
        const limit = request.limit || 100;

        const messages: AgentMessage[] = [];
        const sortedKeys = Array.from(messageStore.keys()).sort((a, b) =>
          a < b ? -1 : a > b ? 1 : 0,
        );

        let count = 0;
        for (const seq of sortedKeys) {
          if (seq > fromSeq && count < limit) {
            const msg = messageStore.get(seq);
            if (msg) {
              messages.push(msg);
              count++;
            }
          }
        }

        return create(CatchUpResponseSchema, {
          messages,
          latestSeq: agent.getLatestSeq(),
          hasMore: count === limit,
        });
      },

      // Unary RPC for shutdown
      async shutdown(request: ShutdownRequest): Promise<ShutdownResponse> {
        if (request.graceful) {
          await agent.interrupt();
        }

        // Schedule shutdown after response is sent
        setTimeout(() => {
          console.log("Shutting down agent...");
          process.exit(0);
        }, 100);

        return create(ShutdownResponseSchema, {
          success: true,
        });
      },
    });
  };

  const server = fastify({
    logger: {
      level: "info",
    },
  });

  await server.register(fastifyConnectPlugin, {
    routes,
  });

  // Health check endpoints for K8s
  server.get("/healthz", async () => ({ status: "ok" }));

  server.get("/readyz", async () => {
    const state = agent.getState();
    return {
      status: state === AgentState.ERROR ? "error" : "ready",
      state: AgentState[state],
      sessionId: agent.getSessionId(),
      latestSeq: agent.getLatestSeq().toString(),
    };
  });

  return server;
}
