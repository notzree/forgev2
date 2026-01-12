import type { ConnectRouter } from "@connectrpc/connect";
import { fastifyConnectPlugin } from "@connectrpc/connect-fastify";
import { fastify, type FastifyInstance } from "fastify";
import type { Http2Server } from "node:http2";
import type { AgentConfig } from "../config.ts";
import {
  AgentService as AgentServiceProto,
  AgentState,
} from "../gen/agent/v1/agent_pb.ts";
import { AgentService } from "./agent-service.ts";
import { createConnectHandlers } from "./connect-handlers.ts";

type Http2FastifyInstance = FastifyInstance<Http2Server>;

/**
 * Creates the health check routes for Kubernetes probes
 */
function registerHealthRoutes(
  server: Http2FastifyInstance,
  service: AgentService,
): void {
  server.get("/healthz", async () => ({ status: "ok" }));

  server.get("/readyz", async () => {
    const status = service.getStatus();
    return {
      status: status.state === AgentState.ERROR ? "error" : "ready",
      state: status.state,
      sessionId: status.sessionId,
      latestSeq: status.latestSeq.toString(),
    };
  });
}

/**
 * Creates the Connect-RPC routes
 */
function createRoutes(service: AgentService): (router: ConnectRouter) => void {
  const handlers = createConnectHandlers(service);

  return (router: ConnectRouter) => {
    router.service(AgentServiceProto, handlers);
  };
}

/**
 * Creates and configures the Fastify server with Connect-RPC and HTTP/2
 */
export async function createServer(
  config: AgentConfig,
): Promise<Http2FastifyInstance> {
  const service = new AgentService(config);

  // Use HTTP/2 cleartext (h2c) for bidirectional streaming support
  const server = fastify({
    logger: { level: "info" },
    http2: true,
    // Disable HTTP/2 session timeout for long-running bidirectional streams.
    // Default is 72 seconds which causes premature disconnects during agent processing.
    // Value of 0 disables the timeout entirely.
    http2SessionTimeout: 0,
  });

  // Register Connect-RPC plugin
  await server.register(fastifyConnectPlugin, {
    routes: createRoutes(service),
  });

  // Register health check endpoints
  registerHealthRoutes(server, service);

  return server;
}
