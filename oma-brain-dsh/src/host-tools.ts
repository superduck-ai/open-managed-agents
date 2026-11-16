import type { Context } from "@deepseek-ai/cordis";
import type { ToolExecutionBridge, ToolPermissions } from "./runtime/contracts.ts";
import type { PortUrlClient } from "./port-tools/client.ts";
import * as PortTools from "./port-tools/index.ts";
import type { WebClient } from "./web-tools/client.ts";
import * as WebTools from "./web-tools/index.ts";

export const name = "host-tools";

export interface Config {
  web: WebClient;
  ports: PortUrlClient;
  bridge: ToolExecutionBridge;
  tools?: string[];
  permissions?: ToolPermissions;
}

export function apply(ctx: Context, config: Config): void {
  const shared = { bridge: config.bridge, tools: config.tools, permissions: config.permissions };
  WebTools.apply(ctx, { ...shared, client: config.web });
  PortTools.apply(ctx, { ...shared, client: config.ports });
}
