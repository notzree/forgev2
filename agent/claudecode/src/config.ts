/**
 * Agent configuration - OpenCode only.
 */

export interface AgentConfig {
  agentId: string;
  port: number;
  cwd: string;
  // Model settings
  model: string;
  permissionMode: "default" | "acceptEdits" | "bypassPermissions";
  // OpenCode settings
  opencodeBaseUrl: string;
  opencodeApiKey?: string;
}

export function loadConfig(): AgentConfig {
  const agentId = process.env.AGENT_ID;
  if (!agentId) {
    throw new Error("AGENT_ID environment variable is required");
  }

  return {
    agentId,
    port: parseInt(process.env.PORT || "8080", 10),
    cwd: process.env.AGENT_CWD || process.cwd(),
    model: process.env.AGENT_MODEL || "opencode/claude-sonnet-4",
    permissionMode:
      (process.env.PERMISSION_MODE as AgentConfig["permissionMode"]) ||
      "acceptEdits",
    opencodeBaseUrl: process.env.OPENCODE_BASE_URL || "http://localhost:4096",
    opencodeApiKey: process.env.OPENCODE_API_KEY,
  };
}
