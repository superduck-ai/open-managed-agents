// Adapted from superduck-code/src/utils/sessionActivity.ts. Each OMA session
// owns its activity instead of sharing the reference implementation's globals.
export type SessionActivityReason = 'api_call' | 'tool_exec';

const SESSION_ACTIVITY_INTERVAL_MS = 30_000;

export interface CcrActivityClock {
  schedule(callback: () => void, delayMs: number): () => void;
}

export interface CcrActivityOptions {
  /** Override the reference environment switch for this plugin instance. */
  enabled?: boolean;
  clock?: CcrActivityClock;
}

// Copied from superduck-code/src/utils/envUtils.ts.
export function isEnvTruthy(envVar: string | boolean | undefined): boolean {
  if (!envVar) return false;
  if (typeof envVar === 'boolean') return envVar;
  const normalizedValue = envVar.toLowerCase().trim();
  return ['1', 'true', 'yes', 'on'].includes(normalizedValue);
}

const systemClock: CcrActivityClock = {
  schedule(callback, delayMs) {
    const timer = setTimeout(callback, delayMs);
    return () => clearTimeout(timer);
  },
};

/** Aggregate overlapping model/tool operations across a session's threads. */
export class CcrActivity {
  private readonly active = new Map<string, string>();
  private readonly clock: CcrActivityClock;
  private cancelTimer?: () => void;
  private request?: AbortController;
  private inFlight?: Promise<void>;
  private stopped = false;

  constructor(
    private readonly send: (signal: AbortSignal) => Promise<void>,
    private readonly signal: AbortSignal,
    private readonly options: CcrActivityOptions = {},
  ) {
    this.clock = options.clock ?? systemClock;
    signal.addEventListener('abort', this.stop, { once: true });
    if (signal.aborted) this.stop();
  }

  start(threadId: string, reason: SessionActivityReason, operationId: string): void {
    if (this.stopped) return;
    this.active.set(JSON.stringify([threadId, reason, operationId]), threadId);
    if (!this.cancelTimer) this.schedule();
  }

  end(threadId: string, reason: SessionActivityReason, operationId: string): void {
    this.active.delete(JSON.stringify([threadId, reason, operationId]));
    this.idle();
  }

  endThread(threadId: string): void {
    for (const [key, owner] of this.active) if (owner === threadId) this.active.delete(key);
    this.idle();
  }

  private idle(): void {
    if (this.active.size) return;
    this.cancelTimer?.();
    this.cancelTimer = undefined;
    // An obsolete keep_alive must not keep retrying once the work is idle.
    this.request?.abort();
  }

  private schedule(): void {
    this.cancelTimer = this.clock.schedule(() => {
      this.cancelTimer = undefined;
      if (this.stopped || !this.active.size) return;
      this.schedule();
      const enabled = this.options.enabled ?? isEnvTruthy(process.env.CLAUDE_CODE_REMOTE_SEND_KEEPALIVES);
      if (!enabled || this.inFlight) return;
      const request = this.request = new AbortController();
      this.inFlight = this.sendKeepAlive(request.signal).finally(() => {
        this.inFlight = undefined;
        this.request = undefined;
      });
    }, SESSION_ACTIVITY_INTERVAL_MS);
  }

  private async sendKeepAlive(signal: AbortSignal): Promise<void> {
    // These events never enter the durable outbox or gate model/tool work.
    // The CCR client still applies its shared authentication and lease policy.
    try { await this.send(signal); } catch { /* Retry on a later active tick. */ }
  }

  stop = (): void => {
    this.stopped = true;
    this.active.clear();
    this.idle();
    this.signal.removeEventListener('abort', this.stop);
  };

  async close(): Promise<void> {
    this.stop();
    await this.inFlight;
  }
}
