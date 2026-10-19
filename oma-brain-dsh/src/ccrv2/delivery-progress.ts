import type { CcrHeartbeatClock } from "./heartbeat.ts";

// received/processing renew the backend's per-event ACK mapping for 20 minutes.
// Worker heartbeats and keep_alive events do not renew that mapping.
const PROCESSING_INTERVAL_MS = 60_000;
const clock: Pick<CcrHeartbeatClock, "schedule"> = {
  schedule(callback, delayMs) {
    const timer = setTimeout(callback, delayMs);
    return () => clearTimeout(timer);
  },
};

/** Report immediately and periodically, without blocking work or piling up retries. */
export function startDeliveryProgress(
  send: () => Promise<void>,
  signal: AbortSignal,
  onFailure: (error: unknown) => void,
  timer: Pick<CcrHeartbeatClock, "schedule"> = clock,
  reportImmediately = true,
): () => void {
  let stopped = false;
  let pending = false;
  let cancelTimer: (() => void) | undefined;
  const stop = () => {
    stopped = true;
    cancelTimer?.();
    signal.removeEventListener("abort", stop);
  };
  const report = async () => {
    pending = true;
    try { await send(); }
    catch (error) { if (!signal.aborted) onFailure(error); }
    finally { pending = false; }
  };
  const tick = () => {
    if (stopped || signal.aborted) return;
    if (!pending) void report();
    if (!stopped && !signal.aborted) cancelTimer = timer.schedule(tick, PROCESSING_INTERVAL_MS);
  };
  signal.addEventListener("abort", stop, { once: true });
  if (signal.aborted) stop();
  else if (reportImmediately) tick();
  else cancelTimer = timer.schedule(tick, PROCESSING_INTERVAL_MS);
  return stop;
}
