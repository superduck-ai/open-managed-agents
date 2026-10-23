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
import TokenMeter from "@deepseek-ai/dsh-token-meter";
import BasicCompaction from "@deepseek-ai/dsh-compaction-basic";
import * as ImageOffload from "@deepseek-ai/dsh-compaction-image-offload";
import ToolResultPruner from "@deepseek-ai/dsh-compaction-tool-result-pruner";

/** Native dsh runtime only. Persistence and platform adapters are installed by the host. */
export async function mountKernel(ctx: Context): Promise<void> {
  await ctx.plugin(LocalAttachmentStore);
  await ctx.plugin(LlmRuntime);
  await ctx.plugin(SessionStore);
  await ctx.plugin(SessionProjectionRegistry);
  await ctx.plugin(TokenMeter);
  await ctx.plugin(ToolResultPruner);
  await ctx.plugin(BasicCompaction);
  await ctx.plugin(ImageOffload);
  await ctx.plugin(SystemPrompt, {
    includeHarnessIdentity: false,
    includeRuntimeContext: false,
  });
  await ctx.plugin(ToolRuntime, { mode: "native" });
  await ctx.plugin(AgentRegistry);
  await ctx.plugin(TimeContext, { timeZone: "Asia/Shanghai" });
  await ctx.plugin(AgentLoop, { agents: [], maxParallelToolCalls: 25 });
}
