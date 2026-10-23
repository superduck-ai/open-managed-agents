import type { SubagentConfig, ToolPermissions } from "./runtime/contracts.ts";
import { sandboxToolDefinitions } from "./sandbox-tools/definitions.ts";
import { sandboxEndpoint } from "./oma-sandbox/config.ts";
import { webToolDefinitions } from "./web-tools/definitions.ts";

export type ModelOptions = { max_output_tokens?: number; input_modalities?: ("text" | "image")[] };

export interface Assignment {
  session_id: string;
  worker_epoch: number;
  ingress_token: string;
  model_token: string;
  mcp_config: unknown;
  external_mcp_config?: unknown;
  launch: {
    version: number;
    model: string;
    model_options?: Record<string, ModelOptions>;
    model_context_windows?: Record<string, number>;
    model_backends?: Record<string, "messages" | "chat_completions">;
    work_dir: string;
    system: string;
    tools: string[];
    subagents?: SubagentConfig;
    tool_permissions?: ToolPermissions;
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
  if (a.launch.model_backends !== undefined && (!a.launch.model_backends || typeof a.launch.model_backends !== "object"
    || Array.isArray(a.launch.model_backends) || Object.values(a.launch.model_backends).some((value) => value !== "messages" && value !== "chat_completions")))
    throw new Error("Invalid model API backends");
  validateModelContextWindows(a.launch.model_context_windows);
  validateModelOptions(a.launch.model_options);
  const tools = new Set([...sandboxToolDefinitions(), ...webToolDefinitions()].map((tool) => tool.name));
  tools.add("ask_user_question");
  if (!Array.isArray(a.launch.tools)
    || a.launch.tools.some((name) => !tools.has(name)))
    throw new Error("Invalid launch tools");
  validateToolPermissions(a.launch.tool_permissions);
  const config = a.launch.subagents;
  if (config) {
    if (config.inlineAgents !== undefined && typeof config.inlineAgents !== "boolean")
      throw new Error("Invalid inlineAgents setting");
    if (config.agents !== undefined && !Array.isArray(config.agents))
      throw new Error("Invalid subagent definitions");
    const names = new Set<string>();
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

export function validateToolPermissions(permissions: unknown): asserts permissions is ToolPermissions | undefined {
  if (permissions !== undefined && (!permissions || typeof permissions !== "object" || Array.isArray(permissions)
    || Object.values(permissions).some((policy) => !["allow", "ask", "deny"].includes(policy))))
    throw new Error("Invalid launch tool permissions");
}

export function validateModelContextWindows(value: unknown): asserts value is Record<string, number> | undefined {
  if (value === undefined) return;
  if (!value || typeof value !== "object" || Array.isArray(value)
    || Object.entries(value).some(([id, window]) => !id || typeof window !== "number" || !Number.isSafeInteger(window) || window <= 0))
    throw new Error("Invalid model context windows");
}

export function validateModelOptions(value: unknown): asserts value is Record<string, ModelOptions> | undefined {
  if (value === undefined) return;
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Invalid model options");
  for (const [id, options] of Object.entries(value)) {
    if (!id || !options || typeof options !== "object" || Array.isArray(options))
      throw new Error("Invalid model options");
    const { max_output_tokens: output, input_modalities: inputs } = options;
    if (output !== undefined && (!Number.isSafeInteger(output) || output <= 0))
      throw new Error("Invalid model output tokens");
    if (
      inputs !== undefined &&
      (!Array.isArray(inputs) ||
        inputs.length === 0 ||
        inputs.length > 2 ||
        new Set(inputs).size !== inputs.length ||
        inputs.some((input) => input !== "text" && input !== "image"))
    )
      throw new Error("Invalid model input modalities");
  }
}
