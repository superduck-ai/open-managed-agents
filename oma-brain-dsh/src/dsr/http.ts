import type { Claim } from "./wire.ts";

export interface HttpRequest {
  path: string;
  method: string;
  token: string;
  body?: unknown;
  headers?: Record<string, string>;
  signal?: AbortSignal;
  timeout?: number;
}

export interface Transport {
  request(options: HttpRequest): Promise<Response>;
  sleep?(ms: number, signal?: AbortSignal): Promise<void>;
}

export class DsrError extends Error {
  constructor(readonly status: number, readonly code: string, readonly nextSeq?: string, readonly retryAfterMs?: number) {
    super(`DSR ${code} (HTTP ${status})`);
  }
}

export function decimal(value: unknown, positive = false): string {
  if (typeof value !== "string" || !/^(0|[1-9]\d*)$/.test(value) || positive && value === "0")
    throw new DsrError(0, "invalid_response");
  return value;
}

export function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new DsrError(0, "invalid_response");
  return value as Record<string, unknown>;
}

export function parseClaim(value: unknown): Claim {
  const body = object(value);
  for (const name of ["code_session_id", "session_token", "model_token"])
    if (typeof body[name] !== "string" || !body[name]) throw new DsrError(0, "invalid_response");
  decimal(body.worker_epoch, true);
  if (typeof body.claim_expires_at !== "string" || !Number.isFinite(Date.parse(body.claim_expires_at))) throw new DsrError(0, "invalid_response");
  return body as unknown as Claim;
}

export async function errorOf(response: Response): Promise<DsrError> {
  const raw = response.headers.get("DSR-Error");
  const code = raw || (response.status === 429 ? "rate_limited" : response.status >= 500 ? "unavailable" : "unclassified_response");
  const cursor = response.headers.get("DSR-Next-Seq");
  const seconds = Number(response.headers.get("Retry-After"));
  await response.body?.cancel().catch(() => {});
  return new DsrError(response.status, code, cursor === null ? undefined : decimal(cursor),
    Number.isFinite(seconds) && seconds >= 0 ? Math.min(seconds * 1000, 30_000) : undefined);
}

export async function delay(transport: Transport, ms: number, signal?: AbortSignal): Promise<void> {
  signal?.throwIfAborted();
  if (transport.sleep) return transport.sleep(ms, signal);
  await new Promise<void>((resolve, reject) => {
    const complete = () => { signal?.removeEventListener("abort", cancel); resolve(); };
    const timer = setTimeout(complete, ms);
    const cancel = () => { clearTimeout(timer); reject(signal?.reason); };
    signal?.addEventListener("abort", cancel, { once: true });
  });
}

export async function request(transport: Transport, options: HttpRequest, attempts = 4): Promise<Response> {
  for (let attempt = 0; ; attempt++) {
    options.signal?.throwIfAborted();
    try {
      const response = await transport.request({ ...options, headers: { "DSR-Protocol": "1", ...options.headers } });
      if (response.ok) return response;
      const error = await errorOf(response);
      if (response.status !== 408 && response.status !== 429 && response.status < 500) throw error;
      if (attempt + 1 >= attempts) throw error;
      await delay(transport, error.retryAfterMs ?? Math.min(100 * 2 ** attempt, 5000), options.signal);
    } catch (error) {
      if (error instanceof DsrError || attempt + 1 >= attempts || options.signal?.aborted) throw error;
      await delay(transport, Math.min(100 * 2 ** attempt, 5000), options.signal);
    }
  }
}
