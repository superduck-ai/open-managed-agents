import { resolve } from "node:path";
import { Context } from "@deepseek-ai/cordis";
import LlmRuntime from "@deepseek-ai/dsh-llm";
import SessionStore from "@deepseek-ai/dsh-session";
import SessionProjectionRegistry from "@deepseek-ai/dsh-session-projection";
import SystemPrompt from "@deepseek-ai/dsh-system-prompt";
import ToolRuntime from "@deepseek-ai/dsh-tools";
import AgentRegistry from "@deepseek-ai/dsh-agent";
import AgentLoop from "@deepseek-ai/dsh-agent-loop";
import * as TimeContext from "@deepseek-ai/dsh-time-context";
import LocalAttachmentStore from "@deepseek-ai/dsh-attachment-local";

/** Native dsh runtime only. Persistence and platform adapters are installed by the host. */
export async function mountKernel(ctx: Context): Promise<void> {
  process.env.DSH_HOME = resolve(process.cwd(), ".dsh");
  await ctx.plugin(LocalAttachmentStore);
  await ctx.plugin(LlmRuntime);
  await ctx.plugin(SessionStore);
  await ctx.plugin(SessionProjectionRegistry);
  await ctx.plugin(SystemPrompt, {
    includeHarnessIdentity: false,
    includeRuntimeContext: false,
  });
  await ctx.plugin(ToolRuntime, { mode: "native" });
  await ctx.plugin(AgentRegistry);
  await ctx.plugin(TimeContext, { timeZone: "Asia/Shanghai" });
  await ctx.plugin(AgentLoop, { agents: [], maxParallelToolCalls: 25 });
}
