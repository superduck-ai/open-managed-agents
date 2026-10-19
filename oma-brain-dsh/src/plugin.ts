import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "./api.ts";
import { createOmaCcrClient, logOmaHeartbeatHealth, stopOmaSessionOnLeaseLoss } from "./oma-ccrv2.ts";
import type { CcrClient } from "./ccrv2";
import * as CcrBridge from "./ccrv2/index.ts";
import { mountKernel } from "./kernel.ts";
import { OmaModelAdapter } from "./model-router.ts";
import { startWorkers } from "./worker.ts";
import { BrainClaimClient } from "./claim.ts";
import { required, readBrainClaimOptions, readCcrHeartbeatOptions } from "./config.ts";

export const name = "oma-brain";

/** Host composition: native kernel, CCRv2 bridge and OMA model route. */
export async function mountOma(
  ctx: Context, api: OmaApi, heartbeat: CcrBridge.CcrHeartbeatOptions = {},
): Promise<CcrClient> {
  const heartbeatOptions = CcrBridge.resolveHeartbeatOptions(heartbeat);
  await mountKernel(ctx);
  const client = createOmaCcrClient(api);
  await ctx.plugin(CcrBridge, {
    backend: client,
    heartbeat: {
      ...heartbeatOptions,
      bind: (id: string) => client.bindLease(id),
      onLeaseLost: stopOmaSessionOnLeaseLoss,
      onHealth: logOmaHeartbeatHealth,
    },
  });
  await ctx.plugin({
    inject: ["llm"],
    apply: (scope: Context) => {
      scope.llm.registerAdapter(["oma"], new OmaModelAdapter(api));
    },
  });
  return client;
}

export async function apply(ctx: Context): Promise<void> {
  const heartbeat = readCcrHeartbeatOptions();
  const base = required("OMA_API_URL");
  const maxSessions = Number(process.env.OMA_BRAIN_MAX_SESSIONS ?? "8");
  if (!Number.isSafeInteger(maxSessions) || maxSessions < 1)
    throw new Error("OMA_BRAIN_MAX_SESSIONS must be positive");
  const claim = readBrainClaimOptions();
  const api = new OmaApi(base.replace(/\/$/, ""));
  const client = await mountOma(ctx, api, heartbeat);
  const claims = new BrainClaimClient(api, claim.token, crypto.randomUUID());
  await ctx.plugin({
    inject: ["ccrv2"],
    apply: (scope: Context) => startWorkers(scope, api, client, claims, { maxSessions, claimConcurrency: claim.concurrency }),
  });
}
