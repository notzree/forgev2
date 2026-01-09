import { type SDKMessage, type Options } from "@anthropic-ai/claude-agent-sdk";
import * as fs from "fs";
import * as path from "path";

const DEFAULT_STATE_PATH = "/workspace/.agent/state.json";

export interface AgentStateOptions {
  path: string | undefined;
}

export interface FullAgentState {
  messages: SDKMessage[];
  options: Options;
}

export class LocalStateManager {
  private path: string = DEFAULT_STATE_PATH;
  private state: FullAgentState = {
    messages: [],
    options: {},
  };

  constructor(opts: AgentStateOptions) {
    if (opts.path) {
      this.path = opts.path;
    }
  }

  addMessages(newMessages: SDKMessage[]) {
    this.state.messages = this.state.messages.concat(newMessages);
  }

  setOptions(options: Options) {
    this.state.options = options;
  }

  getState(): FullAgentState {
    return this.state;
  }

  snapshot(): void {
    const dir = path.dirname(this.path);
    if (!fs.existsSync(dir)) {
      fs.mkdirSync(dir, { recursive: true });
    }
    const json = JSON.stringify(this.state, null, 2);
    fs.writeFileSync(this.path, json, "utf-8");
  }

  load(): void {
    if (!fs.existsSync(this.path)) {
      this.state = { messages: [], options: {} };
      return;
    }
    const json = fs.readFileSync(this.path, "utf-8");
    this.state = JSON.parse(json) as FullAgentState;
  }
}
