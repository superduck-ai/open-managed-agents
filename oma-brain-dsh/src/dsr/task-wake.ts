import { delay, type Transport } from "./http.ts";

export class DsrTaskWake {
  version = 0;
  private waiting?: () => void;

  notify(): void {
    this.version++;
    this.waiting?.();
  }

  async wait(
    version: number,
    transport: Transport,
    signal: AbortSignal,
  ): Promise<void> {
    signal.throwIfAborted();
    if (version !== this.version) return;
    const wake = Promise.withResolvers<void>();
    const timer = new AbortController();
    this.waiting = wake.resolve;
    try {
      await Promise.race([
        wake.promise,
        delay(transport, 1000, AbortSignal.any([signal, timer.signal])),
      ]);
    } finally {
      this.waiting = undefined;
      timer.abort();
    }
  }
}
