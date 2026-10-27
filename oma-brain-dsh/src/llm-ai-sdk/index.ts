import type { Context } from "@deepseek-ai/cordis";
import { AiSdkAdapter } from "./adapter.ts";
import type { AiSdkAdapterOptions } from "./route.ts";

export { AiSdkAdapter } from "./adapter.ts";
export type { AiSdkAdapterOptions, AiSdkRoute } from "./route.ts";

export const name = "llm-ai-sdk";
export const inject = ["llm"];

export interface Config {
  providers: string[];
  adapter: AiSdkAdapterOptions;
}

export function apply(ctx: Context, config: Config): void {
  ctx.llm.registerAdapter(config.providers, new AiSdkAdapter(config.adapter));
}
