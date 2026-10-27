import { readContent } from "./read.ts";
import type { ContentBlock } from "@deepseek-ai/dsh-llm";
import type { Context } from "@deepseek-ai/cordis";
import { object } from "../json.ts";
import { ToolExecution } from "../runtime/execution.ts";
import type { ToolExecutionBridge, ToolPermissions } from "../runtime/contracts.ts";
import { sandboxToolDefinitions } from "./definitions.ts";

export const name = "sandbox-tools";
export const inject = ["tools", "sandboxExecutor"];

export interface Config {
  bridge: ToolExecutionBridge;
  tools?: string[];
  permissions?: ToolPermissions;
  threadId?: string;
}

const bindings = {
  name: "tool-execution-bindings",
  apply(ctx: Context, config: Config) {
    ctx.provide("toolExecutionBridge", config.bridge);
  },
};

const registrations = {
  name: "sandbox-tools-register",
  inject: ["tools", "sandboxExecutor", "toolExecutionBridge"],
  apply: registerTools,
};

/** Consume the root-session executor; each agent owns its execution bridge and tools. */
export async function apply(ctx: Context, config: Config): Promise<void> {
  const scope = ctx.isolate("toolExecutionBridge");
  await scope.plugin(bindings, config);
  await scope.plugin(registrations, config);
}

function registerTools(ctx: Context, config: Config): void {
  const execution = new ToolExecution(ctx.sandboxExecutor, ctx.toolExecutionBridge, config.permissions, config.threadId);
  ctx.tools.restrict({ allow: [] });
  for (const tool of sandboxToolDefinitions()) {
    if (config.tools && !config.tools.includes(tool.name)) continue;
    ctx.tools.register({
      name: tool.name,
      description: tool.description,
      parameters: tool.inputSchema,
      output: tool.name === "Read" ? {
        schema: { type: "array", items: { type: "object" } },
        render: (_args, value) => value as ContentBlock[],
      } : {
        schema: { type: "string" },
        render: (_args, value) => [{ type: "text", text: String(value) }],
      },
      isConcurrencySafe: () => !!tool.readOnly,
      execute: (args, exec) => tool.name === "Read"
        ? execution.execute(tool, object(args), exec, result => readContent(ctx, result, exec))
        : execution.execute(tool, object(args), exec),
    });
  }
}
