export interface AgentConfig {
  port: number;
  cwd: string;
  model?: string;
  permissionMode: "default" | "acceptEdits" | "bypassPermissions";
  allowedTools: string[];
  tursoUrl?: string;
  tursoToken?: string;
  anthropicApiKey?: string;
}

export function loadConfig(): AgentConfig {
  const agentId = process.env.AGENT_ID;
  if (!agentId) {
    throw new Error("AGENT_ID environment variable is required");
  }

  return {
    port: parseInt(process.env.PORT || "8080", 10),
    cwd: process.env.AGENT_CWD || process.cwd(),
    model: process.env.CLAUDE_MODEL || "claude-sonnet-4-20250514",
    permissionMode:
      (process.env.PERMISSION_MODE as AgentConfig["permissionMode"]) ||
      "acceptEdits",
    allowedTools: process.env.ALLOWED_TOOLS?.split(",") || [
      "Read",
      "Write",
      "Edit",
      "Bash",
      "Glob",
      "Grep",
      "WebSearch",
      "WebFetch",
    ],
    tursoUrl: process.env.TURSO_URL,
    tursoToken: process.env.TURSO_AUTH_TOKEN,
    anthropicApiKey: process.env.ANTHROPIC_API_KEY,
  };
}
