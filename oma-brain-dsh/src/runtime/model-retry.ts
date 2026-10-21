import { EMPTY_RESPONSE_CODE, type LlmFailure } from "@deepseek-ai/dsh-llm";

const RETRYABLE_CODES = new Set([EMPTY_RESPONSE_CODE, "RATE_LIMIT", "SERVER", "TIMEOUT", "TRANSPORT"]);

export interface ModelRetryOptions {
  maxRetries: number;
  baseDelayMs: number;
  maxDelayMs: number;
  random: () => number;
  sleep: (ms: number, signal: AbortSignal) => Promise<void>;
}

const DEFAULT_OPTIONS: ModelRetryOptions = {
  maxRetries: 4,
  baseDelayMs: 1000,
  maxDelayMs: 30_000,
  random: Math.random,
  sleep: abortableSleep,
};

export class ModelRetry {
  private step = "";
  private failures = 0;
  private readonly options: ModelRetryOptions;

  constructor(options: Partial<ModelRetryOptions> = {}) {
    this.options = { ...DEFAULT_OPTIONS, ...options };
  }

  async retry(failure: LlmFailure, turn: number, step: number, signal: AbortSignal,
    onRetry?: (attempt: number, delayMs: number) => void): Promise<boolean> {
    if (!RETRYABLE_CODES.has(failure.code)) return false;
    const key = `${turn}:${step}`;
    if (key !== this.step) {
      this.step = key;
      this.failures = 0;
    }
    if (this.failures >= this.options.maxRetries) return false;
    this.failures++;
    const delayMs = this.delay(failure.providerRetryAfterMs);
    onRetry?.(this.failures, delayMs);
    await this.options.sleep(delayMs, signal);
    signal.throwIfAborted();
    return true;
  }

  private delay(retryAfterMs?: number): number {
    const { baseDelayMs, maxDelayMs, random } = this.options;
    const base = retryAfterMs !== undefined && Number.isFinite(retryAfterMs) && retryAfterMs >= 0
      ? Math.min(retryAfterMs, maxDelayMs)
      : Math.min(baseDelayMs * 2 ** (this.failures - 1), maxDelayMs);
    return Math.round(base + random() * base * 0.2);
  }
}

function abortableSleep(ms: number, signal: AbortSignal): Promise<void> {
  signal.throwIfAborted();
  return new Promise((resolve, reject) => {
    const cancel = () => { clearTimeout(timer); reject(signal.reason); };
    const timer = setTimeout(() => { signal.removeEventListener("abort", cancel); resolve(); }, ms);
    signal.addEventListener("abort", cancel, { once: true });
  });
}
