import { resolveHeartbeatOptions, type CcrHeartbeatOptions } from "./ccrv2/heartbeat.ts";

export class ConfigurationError extends Error {}

export const BRAIN_WORKER_LEASE_MS = 20_000;
const DEFAULT_HEARTBEAT_INTERVAL_MS = 6_000;

export function readCcrHeartbeatOptions(
  env: Record<string, string | undefined> = process.env,
): Required<CcrHeartbeatOptions> {
  const options: CcrHeartbeatOptions = {};
  const variables = {
    intervalMs: "OMA_CCR_HEARTBEAT_INTERVAL_MS",
    timeoutMs: "OMA_CCR_HEARTBEAT_TIMEOUT_MS",
    jitterFraction: "OMA_CCR_HEARTBEAT_JITTER_FRACTION",
    maxFailures: "OMA_CCR_HEARTBEAT_MAX_FAILURES",
    maxAuthFailures: "OMA_CCR_HEARTBEAT_MAX_AUTH_FAILURES",
  } as const;
  for (const key of Object.keys(variables) as (keyof typeof variables)[]) {
    const name = variables[key];
    const raw = env[name];
    if (raw === undefined) continue;
    if (!raw.trim() || !Number.isFinite(Number(raw)))
      throw new ConfigurationError(`${name} must be a finite number`);
    options[key] = Number(raw);
  }
  options.intervalMs ??= DEFAULT_HEARTBEAT_INTERVAL_MS;
  let resolved: Required<CcrHeartbeatOptions>;
  try {
    resolved = resolveHeartbeatOptions(options);
  } catch (error) {
    // The validator reports field names and bounds, never supplied values.
    throw new ConfigurationError(error instanceof Error ? error.message : "Invalid CCRv2 heartbeat configuration");
  }
  if (resolved.intervalMs * (1 + resolved.jitterFraction) + resolved.timeoutMs >= BRAIN_WORKER_LEASE_MS)
    throw new ConfigurationError("OMA_CCR_HEARTBEAT_INTERVAL_MS with jitter plus OMA_CCR_HEARTBEAT_TIMEOUT_MS must stay below the 20000 ms worker lease");
  return resolved;
}

export function readBrainClaimOptions(
  env: Record<string, string | undefined> = process.env,
): { token: string; concurrency: number } {
  const token = env.OMA_BRAIN_CLAIM_TOKEN?.trim();
  if (!token) throw new ConfigurationError("OMA_BRAIN_CLAIM_TOKEN is required");
  const rawConcurrency = env.OMA_BRAIN_CLAIM_CONCURRENCY;
  const concurrency = rawConcurrency === undefined ? 4 : rawConcurrency.trim() ? Number(rawConcurrency) : Number.NaN;
  if (!Number.isSafeInteger(concurrency) || concurrency < 1 || concurrency > 32)
    throw new ConfigurationError("OMA_BRAIN_CLAIM_CONCURRENCY must be an integer from 1 through 32");
  return { token, concurrency };
}

export function required(name: string): string {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is required`);
  return value;
}
