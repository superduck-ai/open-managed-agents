import type { Context } from "@deepseek-ai/cordis";
import type { OmaApi } from "../api.ts";
import { SandboxCalls } from "./client.ts";

export const name = "oma-sandbox-provider";

export interface Config {
  api: OmaApi;
  sessionId: string;
}

export function apply(ctx: Context, config: Config): void {
  const executor = new SandboxCalls(config.api, config.sessionId);
  ctx.provide("sandboxExecutor", executor);
  ctx.effect(() => () => executor.close());
}
