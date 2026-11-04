
export class ConfigurationError extends Error {}

export function readBrainProtocol(env: Record<string, string | undefined> = process.env): "dsr1" {
  if (env.OMA_BRAIN_PROTOCOL !== undefined && env.OMA_BRAIN_PROTOCOL !== "dsr1") throw new ConfigurationError("OMA_BRAIN_PROTOCOL only supports dsr1");
  return "dsr1";
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

export const DEFAULT_SHUTDOWN_DRAIN_MS = 20_000;
export const SHUTDOWN_SETTLE_MS = 5_000;
const MAX_SHUTDOWN_DRAIN_MS = 600_000;

export function readShutdownDrainMs(env: Record<string, string | undefined> = process.env): number {
  const raw = env.OMA_BRAIN_SHUTDOWN_DRAIN_MS;
  if (raw === undefined) return DEFAULT_SHUTDOWN_DRAIN_MS;
  const value = raw.trim() ? Number(raw) : Number.NaN;
  if (!Number.isSafeInteger(value) || value < 0 || value > MAX_SHUTDOWN_DRAIN_MS)
    throw new ConfigurationError(`OMA_BRAIN_SHUTDOWN_DRAIN_MS must be an integer from 0 through ${MAX_SHUTDOWN_DRAIN_MS}`);
  return value;
}

export function required(name: string): string {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is required`);
  return value;
}
