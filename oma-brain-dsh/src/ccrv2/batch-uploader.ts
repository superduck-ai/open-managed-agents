import { abortable } from "./retry.ts";

type UploadGroup = {
  signal?: AbortSignal;
  window?: BufferWindow;
  resolve(): void;
  reject(error: unknown): void;
};

type BufferWindow = { ready: boolean; groups: number; cancelTimer?: () => void };
export type BatchSchedule = (callback: () => void, delayMs: number) => () => void;
const schedule: BatchSchedule = (callback, delayMs) => {
  const timer = setTimeout(callback, delayMs);
  return () => clearTimeout(timer);
};

type PendingUpdate<T> = { update: T; bytes: number; group: UploadGroup };
type Admission<T> = { items: PendingUpdate<T>[]; offset: number; group: UploadGroup };

/**
 * Adapted from superduck-code's SerialBatchEventUploader: one in-flight batch,
 * configurable batch/queue limits and backpressure until the buffer drains.
 * OMA callers await HTTP confirmation, so each update keeps its own completion.
 * Failed batches return to the queue's front; retries may include newer updates.
 */
export class CcrBatchUploader<T> {
  private readonly abort = new AbortController();
  private readonly parents = new Map<AbortSignal, () => void>();
  private pending: PendingUpdate<T>[] = [];
  private admissions: Admission<T>[] = [];
  private draining = false;
  private running?: Promise<void>;
  private window?: BufferWindow;

  constructor(
    private readonly send: (updates: T[], signal: AbortSignal) => Promise<void>,
    private readonly onClose: (error: unknown) => void,
    private readonly retry?: {
      allowed(error: unknown): boolean;
      wait(error: unknown, attempt: number, signal: AbortSignal): Promise<void>;
    },
    private readonly limits: { maxBatchSize: number; maxQueueSize: number; maxBatchBytes?: number } = {
      maxBatchSize: 64, maxQueueSize: 64,
    },
    private readonly buffer?: { delayMs: number; schedule?: BatchSchedule },
  ) {}

  get pendingCount(): number { return this.pending.length; }

  /** Signals belong to the session/claim, not to individual updates. */
  bind(signal: AbortSignal): void {
    if (this.abort.signal.aborted || this.parents.has(signal)) return;
    if (signal.aborted) { void this.close(signal.reason); return; }
    const stop = () => { void this.close(signal.reason); };
    this.parents.set(signal, stop);
    signal.addEventListener("abort", stop, { once: true });
  }

  enqueue(update: T): Promise<void> {
    return this.enqueueMany([update]);
  }

  /** Each call waits for all its events. Its signal cancels only that call. */
  async enqueueMany(updates: T[], signal?: AbortSignal, buffered = false): Promise<void> {
    this.abort.signal.throwIfAborted();
    signal?.throwIfAborted();
    if (!updates.length) return;
    const completion = Promise.withResolvers<void>();
    let remaining = updates.length;
    let settled = false;
    const finish = () => {
      settled = true;
      signal?.removeEventListener("abort", cancel);
      if (group.window && --group.window.groups === 0 && this.window === group.window)
        this.releaseWindow(group.window);
    };
    const group: UploadGroup = {
      signal,
      resolve: () => {
        if (!settled && --remaining === 0) {
          finish();
          completion.resolve();
        }
      },
      reject: (error) => {
        if (settled) return;
        finish();
        completion.reject(error);
      },
    };
    // Validate the whole call before admitting any of it.
    const items = updates.map((update) => ({ update, group,
      bytes: this.limits.maxBatchBytes === undefined ? 0 : Buffer.byteLength(JSON.stringify(update)) }));
    const cancel = () => {
      this.pending = this.pending.filter((entry) => entry.group !== group);
      this.admissions = this.admissions.filter((entry) => entry.group !== group);
      group.reject(signal!.reason);
      this.releaseBackpressure();
      this.drain();
    };
    signal?.throwIfAborted();
    if (buffered && this.buffer) {
      let window = this.window;
      if (!window) {
        const created: BufferWindow = { ready: false, groups: 0 };
        window = this.window = created;
        // Like superduck's stream buffer, later arrivals do not restart the timer.
        created.cancelTimer = (this.buffer.schedule ?? schedule)(() => {
          this.releaseWindow(created);
          this.drain();
        }, this.buffer.delayMs);
      }
      group.window = window;
      group.window.groups++;
    } else if (this.window) {
      // Non-stream output releases earlier buffered events before it is admitted.
      this.releaseWindow(this.window);
    }
    signal?.addEventListener("abort", cancel, { once: true });
    // Admit large calls incrementally, in call order, without exceeding the
    // pending limit or deadlocking when a call is larger than the whole queue.
    this.admissions.push({ items, offset: 0, group });
    this.releaseBackpressure();
    this.drain();
    return completion.promise;
  }

  private drain(): void {
    if (this.draining || this.abort.signal.aborted || this.pending[0]?.group.window?.ready === false) return;
    this.draining = true;
    this.running = this.run().finally(() => {
      this.draining = false;
      this.running = undefined;
      if (this.pending.length) this.drain();
    });
  }

  private async run(): Promise<void> {
    let failures = 0;
    while (this.pending.length && !this.abort.signal.aborted) {
      if (this.pending[0]!.group.window?.ready === false) return;
      const batch = this.takeBatch();
      const callSignal = batch[0]!.group.signal;
      const signal = callSignal ? AbortSignal.any([this.abort.signal, callSignal]) : this.abort.signal;
      try {
        const updates = batch.map((entry) => entry.update);
        try {
          signal.throwIfAborted();
          await abortable(this.send(updates, signal), signal);
          signal.throwIfAborted();
        } catch (error) {
          if (signal.aborted || !this.retry?.allowed(error)) throw error;
          this.pending = batch.concat(this.pending);
          await this.retry.wait(error, ++failures, signal);
          continue;
        }
      } catch (error) {
        for (const entry of batch) entry.group.reject(error);
        if (callSignal?.aborted && !this.abort.signal.aborted) {
          failures = 0;
          this.releaseBackpressure();
          continue;
        }
        // Do not await this.close() from inside its own drain.
        void this.close(error);
        return;
      }
      failures = 0;
      for (const entry of batch) entry.group.resolve();
      this.releaseBackpressure();
    }
  }

  /** Superduck counts serialized event bytes; an oversized first event is sent alone. */
  private takeBatch(): PendingUpdate<T>[] {
    let bytes = 0;
    let count = 0;
    while (count < this.pending.length && count < this.limits.maxBatchSize) {
      if (this.pending[count]!.group.window?.ready === false) break;
      // Keep independently cancellable calls out of each other's HTTP batch.
      if (count > 0 && this.pending[count]!.group.signal !== this.pending[0]!.group.signal) break;
      const itemBytes = this.pending[count]!.bytes;
      if (count > 0 && this.limits.maxBatchBytes !== undefined && bytes + itemBytes > this.limits.maxBatchBytes) break;
      bytes += itemBytes;
      count++;
    }
    return this.pending.splice(0, count);
  }

  private releaseBackpressure(): void {
    while (this.admissions.length && this.pending.length < this.limits.maxQueueSize) {
      const admission = this.admissions[0]!;
      const end = Math.min(admission.items.length, admission.offset + this.limits.maxQueueSize - this.pending.length);
      while (admission.offset < end) this.pending.push(admission.items[admission.offset++]!);
      if (admission.offset === admission.items.length) this.admissions.shift();
    }
  }

  private releaseWindow(window: BufferWindow): void {
    if (this.window !== window) return;
    window.ready = true;
    window.cancelTimer?.();
    this.window = undefined;
  }

  async close(error: unknown = new Error("CCRv2 event uploader stopped")): Promise<void> {
    if (!this.abort.signal.aborted) {
      this.abort.abort(error);
      if (this.window) this.releaseWindow(this.window);
      for (const entry of this.pending.splice(0)) entry.group.reject(error);
      for (const entry of this.admissions.splice(0)) entry.group.reject(error);
      for (const [signal, stop] of this.parents) signal.removeEventListener("abort", stop);
      this.parents.clear();
      this.onClose(error);
    }
    await this.running;
  }
}
