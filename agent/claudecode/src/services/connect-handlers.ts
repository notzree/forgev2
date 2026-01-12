/**
 * Connect-RPC handler definitions for AgentService.
 * This maps the protobuf service methods to our AgentService implementation.
 */

import type {
  AgentRequest,
  AgentResponse,
  GetStatusResponse,
  ShutdownRequest,
  ShutdownResponse,
} from "../gen/agent/v1/agent_pb.ts";
import type { AgentService } from "./agent-service.ts";

/**
 * Connect-RPC handler interface
 */
export interface ConnectHandlers {
  connect(requests: AsyncIterable<AgentRequest>): AsyncIterable<AgentResponse>;
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
      requests: AsyncIterable<AgentRequest>,
    ): AsyncIterable<AgentResponse> {
      for await (const request of requests) {
        const requestId = request.requestId;

        try {
          yield* handleCommand(service, requestId, request);
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
 * Routes a request to the appropriate handler method
 */
async function* handleCommand(
  service: AgentService,
  requestId: string,
  request: AgentRequest,
): AsyncIterable<AgentResponse> {
  switch (request.command.case) {
    case "sendMessage": {
      const { content } = request.command.value;
      yield* service.handleSendMessage(requestId, content);
      break;
    }

    case "interrupt": {
      yield await service.handleInterrupt(requestId);
      break;
    }

    case "setPermissionMode": {
      const { mode } = request.command.value;
      yield await service.handleSetPermissionMode(requestId, mode);
      break;
    }

    case "setModel": {
      const { model } = request.command.value;
      yield await service.handleSetModel(requestId, model);
      break;
    }

    default: {
      yield service.handleUnknownCommand(
        requestId,
        request.command.case ?? "undefined",
      );
      break;
    }
  }
}
