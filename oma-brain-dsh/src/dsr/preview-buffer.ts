import type { PublicEvent } from "./wire.ts";

export class DsrPreviewBuffer {
  private pending: PublicEvent[] = [];
  private bytes = 0;
  private timer?: ReturnType<typeof setTimeout>;
  private closed = false;

  constructor(private readonly send: (events: PublicEvent[]) => void) {}

  enqueue(event: PublicEvent): void {
    if (this.closed) return;
    const bytes = Buffer.byteLength(JSON.stringify(event));
    if (this.bytes + bytes > 4 * 1024 * 1024) this.flush();
    this.pending.push(event);
    this.bytes += bytes;
    if (this.pending.length >= 200 || this.bytes >= 4 * 1024 * 1024) {
      this.flush();
      return;
    }
    this.timer ??= setTimeout(() => this.flush(), 100);
    this.timer.unref();
  }

  flush(): void {
    clearTimeout(this.timer);
    this.timer = undefined;
    if (!this.pending.length) return;
    const pending = this.pending;
    this.pending = [];
    this.bytes = 0;
    this.send(pending);
  }

  close(): void {
    this.closed = true;
    clearTimeout(this.timer);
    this.timer = undefined;
    this.pending = [];
    this.bytes = 0;
  }
}
