import type { Context } from "@deepseek-ai/cordis";
import { object } from "../json.ts";
import { ToolExecution } from "../runtime/execution.ts";
import type { ToolExecutionBridge } from "../runtime/contracts.ts";
import type { WebClient } from "./client.ts";
import { webToolDefinitions } from "./definitions.ts";

export const name = "web-tools";

export interface Config {
  client: WebClient;
  bridge: ToolExecutionBridge;
  tools?: string[];
}

export function apply(ctx: Context, config: Config): void {
  const execution = new ToolExecution(config.client, config.bridge);
  for (const tool of webToolDefinitions()) {
    if (config.tools && !config.tools.includes(tool.name)) continue;
    ctx.tools.register({
      name: tool.name, description: tool.description, parameters: tool.inputSchema,
      output: { schema: { type: "string" }, render: (_args, value) => [{ type: "text", text: String(value) }] },
      isConcurrencySafe: () => true,
      execute: (args, exec) => execution.execute(tool, object(args), exec),
    });
  }
}
