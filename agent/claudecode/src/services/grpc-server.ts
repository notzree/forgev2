import type { ConnectRouter } from "@connectrpc/connect";
import { fastifyConnectPlugin } from "@connectrpc/connect-fastify";
import { fastify, type FastifyInstance } from "fastify";
import type { AgentConfig } from "../config.ts";
import { AgentService as AgentServiceProto } from "../gen/agent/v1/agent_pb.ts";
import { AgentService } from "./agent-service.ts";
import { createConnectHandlers } from "./connect-handlers.ts";

/**
 * Creates the health check routes for Kubernetes probes
 */
function registerHealthRoutes(
  server: FastifyInstance,
  service: AgentService,
): void {
  server.get("/healthz", async () => ({ status: "ok" }));

  server.get("/readyz", async () => {
    const status = service.getStatus();
    return {
      status: status.state === 2 ? "error" : "ready", // AgentState.ERROR = 2
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
 * Creates and configures the Fastify server with Connect-RPC
 */
export async function createServer(
  config: AgentConfig,
): Promise<FastifyInstance> {
  const service = new AgentService(config);

  const server = fastify({
    logger: { level: "info" },
  });

  // Register Connect-RPC plugin
  await server.register(fastifyConnectPlugin, {
    routes: createRoutes(service),
  });

  // Register health check endpoints
  registerHealthRoutes(server, service);

  return server;
}
