import { loadConfig } from "./config.ts";
import { createServer } from "./services/grpc-server.ts";

async function main() {
  console.log("Starting Forge Agent...");

  // Load configuration from environment
  const config = loadConfig();
  console.log(`Agent ID: ${config.agentId}`);
  console.log(`Working directory: ${config.cwd}`);
  console.log(`Model: ${config.model}`);
  console.log(`Permission mode: ${config.permissionMode}`);

  // Create and start the server
  const server = await createServer(config);

  try {
    await server.listen({
      port: config.port,
      host: "0.0.0.0",
    });
    console.log(`Agent gRPC server listening on 0.0.0.0:${config.port}`);
    console.log(`Health check: http://0.0.0.0:${config.port}/healthz`);
    console.log(`Ready check: http://0.0.0.0:${config.port}/readyz`);
  } catch (err) {
    console.error("Failed to start server:", err);
    process.exit(1);
  }

  // Handle graceful shutdown
  const shutdown = async (signal: string) => {
    console.log(`Received ${signal}, shutting down gracefully...`);
    await server.close();
    process.exit(0);
  };

  process.on("SIGTERM", () => shutdown("SIGTERM"));
  process.on("SIGINT", () => shutdown("SIGINT"));
}

main().catch((err) => {
  console.error("Fatal error:", err);
  process.exit(1);
});
