import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "./api.ts";
import { mountDsr, startDsrWorkers, dsrRuntimeServices } from "./dsr/worker.ts";
import { DsrClaims } from "./dsr/claims.ts";
import { createDsrTransport } from "./dsr/transport.ts";
import {
  required,
  readBrainProtocol,
  readBrainClaimOptions,
  readShutdownDrainMs,
} from "./config.ts";

export const name = "oma-brain";

export async function apply(ctx: Context): Promise<void> {
  readBrainProtocol();
  const base = required("OMA_API_URL");
  const maxSessions = Number(process.env.OMA_BRAIN_MAX_SESSIONS ?? "8");
  if (!Number.isSafeInteger(maxSessions) || maxSessions < 1)
    throw new Error("OMA_BRAIN_MAX_SESSIONS must be positive");
  const claim = readBrainClaimOptions();
  const api = new OmaApi(base.replace(/\/$/, ""));
  const clients = await mountDsr(ctx, api);
  const claims = new DsrClaims(
    createDsrTransport(api),
    claim.token,
    crypto.randomUUID(),
  );
  await ctx.plugin({
    inject: dsrRuntimeServices,
    apply: (scope: Context) =>
      startDsrWorkers(scope, api, clients, claims, {
        maxSessions,
        claimConcurrency: claim.concurrency,
        shutdownDrainMs: readShutdownDrainMs(),
      }),
  });
}
