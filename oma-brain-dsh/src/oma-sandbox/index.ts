import type { Context } from "@deepseek-ai/cordis";
import type { OmaApi } from "../api.ts";
import { SandboxMcp } from "./client.ts";

export const name = "oma-sandbox-provider";

export interface Config {
  api: OmaApi;
  sessionId: string;
}

/** The host mounts one provider in each root session's isolated service scope. */
export function apply(ctx: Context, config: Config): void {
  // Construction is local. MCP initialization follows the durable approval checkpoint.
  const executor = new SandboxMcp(config.api, config.sessionId);
  ctx.provide("sandboxExecutor", executor);
  ctx.effect(() => () => executor.close());
}
