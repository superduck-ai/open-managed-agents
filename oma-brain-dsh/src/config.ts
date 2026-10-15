import { resolveHeartbeatOptions, type CcrHeartbeatOptions } from "./ccrv2/heartbeat.ts";

export class ConfigurationError extends Error {}

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
  try {
    return resolveHeartbeatOptions(options);
  } catch (error) {
    // The validator reports field names and bounds, never supplied values.
    throw new ConfigurationError(error instanceof Error ? error.message : "Invalid CCRv2 heartbeat configuration");
  }
}

export function required(name: string): string {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is required`);
  return value;
}
