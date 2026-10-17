import type { SubagentConfig } from "./runtime/contracts.ts";
import { sandboxToolDefinitions } from "./sandbox-tools/definitions.ts";
import { sandboxEndpoint } from "./oma-sandbox/config.ts";

export interface Assignment {
  session_id: string;
  worker_epoch: number;
  ingress_token: string;
  model_token: string;
  mcp_config: unknown;
  launch: {
    version: number;
    model: string;
    work_dir: string;
    system: string;
    subagents?: SubagentConfig;
    environment_prompt?: string;
    memory_catalog_prompt?: string;
    skills?: {
      name: string;
      description: string;
      path: string;
      version: string;
    }[];
  };
}
export function validateAssignment(a: Assignment): void {
  for (const key of [
    "session_id",
    "ingress_token",
    "model_token",
  ] as const) {
    if (typeof a[key] !== "string" || !a[key])
      throw new Error(`Missing assignment ${key}`);
  }
  if (!Number.isSafeInteger(a.worker_epoch) || a.worker_epoch <= 0)
    throw new Error("Invalid assignment epoch");
  sandboxEndpoint(a.mcp_config);
  if (![1, 2].includes(a.launch.version))
    throw new Error("Unsupported launch version");
  if (!a.launch.model || !a.launch.work_dir.startsWith("/"))
    throw new Error("Invalid launch model or work directory");
  const config = a.launch.subagents;
  if (config) {
    if (config.inlineAgents !== undefined && typeof config.inlineAgents !== "boolean")
      throw new Error("Invalid inlineAgents setting");
    if (config.agents !== undefined && !Array.isArray(config.agents))
      throw new Error("Invalid subagent definitions");
    const names = new Set<string>();
    const tools = new Set(sandboxToolDefinitions().map((tool) => tool.name));
    for (const agent of config.agents ?? []) {
      if (!agent || typeof agent.name !== "string" || !agent.name || names.has(agent.name)
        || typeof agent.description !== "string" || typeof agent.systemPrompt !== "string"
        || (agent.model !== undefined && (typeof agent.model !== "string" || !agent.model))
        || (agent.tools !== undefined && (!Array.isArray(agent.tools)
          || agent.tools.some((name) => !tools.has(name)))))
        throw new Error("Invalid subagent definition");
      names.add(agent.name);
    }
    if (names.size > 20 || (config.inlineAgents === false && !names.size))
      throw new Error("Subagents require 1–20 predefined agents when inline agents are disabled");
  }
}
