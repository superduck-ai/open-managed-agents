export type CcrLeaseFailureReason =
  | "epoch_mismatch" | "authentication_rejected" | "token_expired" | "lease_expired" | "request_rejected"
  | "rate_limited" | "server_error" | "timeout" | "transport_error" | "credentials_unavailable"
  | "worker_not_registered";

export type CcrLeaseResult =
  | { status: "confirmed" }
  | {
    status: "rejected" | "unavailable";
    httpStatus?: number;
    reason?: CcrLeaseFailureReason;
    workerEpoch?: number;
  };

export interface CcrLeaseCheck {
  (signal: AbortSignal, timeoutMs: number): Promise<CcrLeaseResult>;
  /** Observe other writes and terminal worker-state GET failures. Do not emit this check's own results twice. */
  subscribe?(listener: (result: CcrLeaseResult) => void): () => void;
}
export type CcrLeaseFailure = Exclude<CcrLeaseResult, { status: "confirmed" }> & {
  consecutiveFailures: number;
};

export type CcrHeartbeatHealth = {
  consecutiveFailures: number;
  durationMs: number;
  workerEpoch?: number;
} & (
  | { type: "failure"; httpStatus?: number; reason?: CcrLeaseFailureReason }
  | { type: "recovered" }
);

export interface CcrHeartbeatOptions {
  intervalMs?: number;
  timeoutMs?: number;
  jitterFraction?: number;
  /** Zero keeps retrying transient heartbeat failures until the host stops the session. */
  maxFailures?: number;
  maxAuthFailures?: number;
}

export function resolveHeartbeatOptions(options: CcrHeartbeatOptions = {}): Required<CcrHeartbeatOptions> {
  const resolved = {
    intervalMs: options.intervalMs ?? 20_000,
    timeoutMs: options.timeoutMs ?? 5000,
    jitterFraction: options.jitterFraction ?? 0.1,
    maxFailures: options.maxFailures ?? 0,
    maxAuthFailures: options.maxAuthFailures ?? 10,
  };
  for (const key of ["intervalMs", "timeoutMs", "maxAuthFailures"] as const) {
    if (!Number.isSafeInteger(resolved[key]) || resolved[key] <= 0)
      throw new Error(`Heartbeat ${key} must be a positive safe integer`);
  }
  if (!Number.isSafeInteger(resolved.maxFailures) || resolved.maxFailures < 0)
    throw new Error("Heartbeat maxFailures must be a nonnegative safe integer");
  if (!Number.isFinite(resolved.jitterFraction) || resolved.jitterFraction < 0 || resolved.jitterFraction > 0.5)
    throw new Error("Heartbeat jitterFraction must be between 0 and 0.5");
  // JavaScript timers otherwise overflow into a near-immediate callback.
  if (resolved.intervalMs * (1 + resolved.jitterFraction) > 2_147_483_647 || resolved.timeoutMs > 2_147_483_647)
    throw new Error("Heartbeat interval with jitter and timeout must fit a 32-bit timer");
  return resolved;
}

export interface CcrHeartbeatConfig extends CcrHeartbeatOptions {
  check: CcrLeaseCheck;
  onLeaseLost(failure: CcrLeaseFailure): void;
  onHealth?(health: CcrHeartbeatHealth): void;
}

/** An injectable timer keeps lifecycle and race tests independent of wall time. */
export interface CcrHeartbeatClock {
  schedule(callback: () => void, delayMs: number): () => void;
  random(): number;
  now?(): number;
}

const systemClock: CcrHeartbeatClock = {
  schedule(callback, delayMs) {
    const timer = setTimeout(callback, delayMs);
    return () => clearTimeout(timer);
  },
  random: Math.random,
  now: () => performance.now(),
};

/** One claimed session owns one timer, request and failure counter. */
export class CcrHeartbeat {
  private readonly abort = new AbortController();
  private readonly intervalMs: number;
  private readonly timeoutMs: number;
  private readonly jitterFraction: number;
  private readonly maxFailures: number;
  private readonly maxAuthFailures: number;
  private cancelTimer?: () => void;
  private unsubscribe?: () => void;
  private inFlight?: Promise<void>;
  private failures = 0;
  private authFailures = 0;
  private healthFailures = 0;
  private firstFailureAt = 0;
  private lastFailureLogAt = 0;
  private failedWorkerEpoch?: number;

  constructor(
    private readonly config: CcrHeartbeatConfig,
    private readonly signal: AbortSignal,
    private readonly clock: CcrHeartbeatClock = systemClock,
  ) {
    const options = resolveHeartbeatOptions(config);
    this.intervalMs = options.intervalMs;
    this.timeoutMs = options.timeoutMs;
    this.jitterFraction = options.jitterFraction;
    this.maxFailures = options.maxFailures;
    this.maxAuthFailures = options.maxAuthFailures;
    signal.addEventListener("abort", this.stop, { once: true });
    if (signal.aborted) this.stop();
    else {
      this.unsubscribe = config.check.subscribe?.((result) => this.accept(result, false));
      this.schedule();
    }
  }

  private schedule(): void {
    // Adapted from superduck-code/src/cli/transports/ccrClient.ts startHeartbeat.
    const jitter = this.intervalMs * this.jitterFraction * (2 * this.clock.random() - 1);
    this.cancelTimer = this.clock.schedule(() => this.tick(), this.intervalMs + jitter);
  }

  private tick(): void {
    this.cancelTimer = undefined;
    if (this.abort.signal.aborted) return;
    // Keep the interval independent of request latency; skip an overlapping beat.
    this.schedule();
    if (this.inFlight) return;
    this.inFlight = this.beat().finally(() => { this.inFlight = undefined; });
    void this.inFlight.catch(() => console.error("CCRv2 lease-loss handler failed"));
  }

  private async beat(): Promise<void> {
    let result: CcrLeaseResult;
    try {
      result = await this.config.check(this.abort.signal, this.timeoutMs);
    } catch {
      result = { status: "unavailable" };
    }
    this.accept(result, true);
    if (!this.abort.signal.aborted) this.reportHealth(result);
  }

  private reportHealth(result: CcrLeaseResult): void {
    const now = this.clock.now?.() ?? performance.now();
    let health: CcrHeartbeatHealth;
    if (result.status === "confirmed") {
      if (!this.healthFailures) return;
      health = {
        type: "recovered", workerEpoch: this.failedWorkerEpoch,
        consecutiveFailures: this.healthFailures, durationMs: now - this.firstFailureAt,
      };
      this.healthFailures = 0;
      this.failedWorkerEpoch = undefined;
    } else {
      if (!this.healthFailures++) this.firstFailureAt = now;
      this.failedWorkerEpoch = result.workerEpoch;
      if (this.healthFailures > 1 && now - this.lastFailureLogAt < 60_000) return;
      this.lastFailureLogAt = now;
      health = {
        type: "failure", workerEpoch: result.workerEpoch, httpStatus: result.httpStatus,
        reason: result.reason, consecutiveFailures: this.healthFailures, durationMs: now - this.firstFailureAt,
      };
    }
    // Diagnostics cannot delay retries, reset counters or stop a healthy session.
    try { this.config.onHealth?.(health); } catch { /* Ignore a failed log sink. */ }
  }

  private accept(result: CcrLeaseResult, heartbeat: boolean): void {
    // A stopped generation must never report or count a late response.
    if (this.abort.signal.aborted) return;
    if (result.status === "confirmed") {
      // Like superduck's request(), all successful writes reset auth failures.
      this.authFailures = 0;
      if (heartbeat) this.failures = 0;
      return;
    }
    const authFailure = result.httpStatus === 401 || result.httpStatus === 403;
    if (authFailure) this.authFailures++;
    else if (heartbeat) this.failures++;
    const failures = authFailure ? this.authFailures : heartbeat ? this.failures : 1;
    if (result.status === "rejected" ||
      (authFailure ? this.authFailures >= this.maxAuthFailures
        : heartbeat && this.maxFailures > 0 && this.failures >= this.maxFailures)) {
      this.stop();
      this.config.onLeaseLost({ ...result, consecutiveFailures: failures });
    }
  }

  stop = (): void => {
    this.cancelTimer?.();
    this.cancelTimer = undefined;
    this.unsubscribe?.();
    this.unsubscribe = undefined;
    this.signal.removeEventListener("abort", this.stop);
    this.abort.abort();
  };

  async close(): Promise<void> {
    this.stop();
    await this.inFlight;
  }
}
