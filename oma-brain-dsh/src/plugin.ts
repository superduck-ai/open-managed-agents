import { homedir } from "node:os";
import { join } from "node:path";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "./api.ts";
import { createOmaCcrClient, logOmaHeartbeatHealth, stopOmaSessionOnLeaseLoss } from "./oma-ccrv2.ts";
import type { CcrClient } from "./ccrv2";
import * as CcrBridge from "./ccrv2/index.ts";
import { mountKernel } from "./kernel.ts";
import * as LlmAiSdk from "./llm-ai-sdk/index.ts";
import { omaModelOptions } from "./model.ts";
import { startWorkers } from "./worker.ts";
import { BrainClaimClient } from "./claim.ts";
import { required, readBrainClaimOptions, readCcrHeartbeatOptions, readShutdownDrainMs } from "./config.ts";

export const name = "oma-brain";

/** Host composition: native kernel, CCRv2 bridge and OMA model route. */
export async function mountOma(
  ctx: Context, api: OmaApi, heartbeat: CcrBridge.CcrHeartbeatOptions = {}, localRoot?: string,
): Promise<CcrClient> {
  const heartbeatOptions = CcrBridge.resolveHeartbeatOptions(heartbeat);
  await mountKernel(ctx);
  const client = createOmaCcrClient(api);
  await ctx.plugin(CcrBridge, {
    backend: client,
    localRoot,
    heartbeat: {
      ...heartbeatOptions,
      bind: (id: string) => client.bindLease(id),
      onLeaseLost: stopOmaSessionOnLeaseLoss,
      onHealth: logOmaHeartbeatHealth,
    },
  });
  const agents = ctx.get("agents")!;
  await ctx.plugin(LlmAiSdk, { providers: ["oma"], adapter: omaModelOptions(api, ctx.get("attachments"), () => agents.currentInitiator()?.session.id) });
  return client;
}

export async function apply(ctx: Context): Promise<void> {
  const heartbeat = readCcrHeartbeatOptions();
  const base = required("OMA_API_URL");
  const maxSessions = Number(process.env.OMA_BRAIN_MAX_SESSIONS ?? "8");
  if (!Number.isSafeInteger(maxSessions) || maxSessions < 1)
    throw new Error("OMA_BRAIN_MAX_SESSIONS must be positive");
  const claim = readBrainClaimOptions();
  const shutdownDrainMs = readShutdownDrainMs();
  const api = new OmaApi(base.replace(/\/$/, ""));
  const client = await mountOma(ctx, api, heartbeat, process.env.OMA_BRAIN_TRANSCRIPT_DIR?.trim() || join(homedir(), ".oma", "brain", "transcripts"));
  const claims = new BrainClaimClient(api, claim.token, crypto.randomUUID());
  await ctx.plugin({
    inject: ["ccrv2"],
    apply: (scope: Context) => startWorkers(scope, api, client, claims, { maxSessions, claimConcurrency: claim.concurrency, shutdownDrainMs }),
  });
}
