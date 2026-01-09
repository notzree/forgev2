import type {
  AgentCommand,
  AgentEvent,
  GetStatusResponse,
  ShutdownRequest,
  ShutdownResponse,
} from "../gen/agent/v1/agent_pb.ts";
import type { AgentService } from "./agent-service.ts";

/**
 * Connect-RPC handler definitions for AgentService.
 * This maps the protobuf service methods to our AgentService implementation.
 */
export interface ConnectHandlers {
  connect(requests: AsyncIterable<AgentCommand>): AsyncIterable<AgentEvent>;
  getStatus(): Promise<GetStatusResponse>;
  shutdown(request: ShutdownRequest): Promise<ShutdownResponse>;
}

/**
 * Creates the Connect-RPC handlers that delegate to AgentService
 */
export function createConnectHandlers(service: AgentService): ConnectHandlers {
  return {
    /**
     * Bidirectional streaming RPC - main communication channel
     */
    async *connect(
      requests: AsyncIterable<AgentCommand>,
    ): AsyncIterable<AgentEvent> {
      for await (const command of requests) {
        const requestId = command.requestId;

        try {
          yield* handleCommand(service, requestId, command);
        } catch (error) {
          yield service.handleError(requestId, error);
        }
      }
    },

    /**
     * Unary RPC - get current agent status
     */
    async getStatus(): Promise<GetStatusResponse> {
      return service.getStatus();
    },

    /**
     * Unary RPC - shutdown the agent
     */
    async shutdown(request: ShutdownRequest): Promise<ShutdownResponse> {
      return service.shutdown(request);
    },
  };
}

/**
 * Routes a command to the appropriate handler method
 */
async function* handleCommand(
  service: AgentService,
  requestId: string,
  command: AgentCommand,
): AsyncIterable<AgentEvent> {
  switch (command.command.case) {
    case "sendMessage": {
      const { content } = command.command.value;
      yield* service.handleSendMessage(requestId, content);
      break;
    }

    case "interrupt": {
      yield await service.handleInterrupt(requestId);
      break;
    }

    case "setPermissionMode": {
      const { mode } = command.command.value;
      yield await service.handleSetPermissionMode(requestId, mode);
      break;
    }

    case "setModel": {
      const { model } = command.command.value;
      yield await service.handleSetModel(requestId, model);
      break;
    }

    default: {
      yield service.handleUnknownCommand(
        requestId,
        command.command.case ?? "undefined",
      );
      break;
    }
  }
}
