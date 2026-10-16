/** Delay calculation adapted from superduck-code's SerialBatchEventUploader.retryDelay. */
export function retryDelay(failures: number, retryAfterMs?: number, random = Math.random): number {
  const baseDelayMs = 500;
  const maxDelayMs = 30_000;
  const jitter = random() * 500;
  if (retryAfterMs !== undefined) {
    const clamped = Math.max(baseDelayMs, Math.min(retryAfterMs, maxDelayMs));
    return clamped + jitter;
  }
  return Math.min(baseDelayMs * 2 ** (failures - 1), maxDelayMs) + jitter;
}

export function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  signal?.throwIfAborted();
  return new Promise((resolve, reject) => {
    const cancel = () => { clearTimeout(timer); reject(signal!.reason); };
    const timer = setTimeout(() => { signal?.removeEventListener("abort", cancel); resolve(); }, ms);
    signal?.addEventListener("abort", cancel, { once: true });
  });
}

/** Also cancels waits supplied by hosts whose test clock does not accept a signal. */
export function abortable<T>(promise: Promise<T>, signal: AbortSignal): Promise<T> {
  return new Promise((resolve, reject) => {
    const cancel = () => reject(signal.reason);
    if (signal.aborted) cancel();
    else signal.addEventListener("abort", cancel, { once: true });
    promise.then(resolve, reject).finally(() => signal.removeEventListener("abort", cancel));
  });
}
