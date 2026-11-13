import type { Context } from "@deepseek-ai/cordis";
import { object } from "../json.ts";
import { ToolExecution } from "../runtime/execution.ts";
import type { ToolExecutionBridge, ToolPermissions } from "../runtime/contracts.ts";
import type { PortUrlClient } from "./client.ts";
import { portToolDefinition } from "./definitions.ts";

export const name = "port-tools";

export interface Config {
  client: PortUrlClient;
  bridge: ToolExecutionBridge;
  tools?: string[];
  permissions?: ToolPermissions;
}

export function apply(ctx: Context, config: Config): void {
  const tool = portToolDefinition();
  if (!config.tools?.includes(tool.name)) return;
  const execution = new ToolExecution(config.client, config.bridge, config.permissions);
  ctx.tools.register({
    name: tool.name, description: tool.description, parameters: tool.inputSchema,
    output: { schema: { type: "string" }, render: (_args, value) => [{ type: "text", text: String(value) }] },
    isConcurrencySafe: () => true,
    execute: (args, exec) => execution.execute(tool, object(args), exec),
  });
}
