import type { DsrClient } from "./client.ts";
import type { Registration } from "./wire.ts";
import { delay } from "./http.ts";

export class DsrLease {
  private readonly abort = new AbortController();
  private task?: Promise<void>;
  constructor(readonly client: DsrClient, readonly registration: Registration, readonly lost: (error: unknown) => void) {}
  start(): void { this.task ??= this.run().catch(error => { if (!this.abort.signal.aborted) this.lost(error); }); }
  private async run(): Promise<void> {
    while (!this.abort.signal.aborted) {
      await delay(this.client.transport, this.registration.lease.heartbeat_interval_ms, this.abort.signal);
      await this.client.heartbeat(AbortSignal.any([this.abort.signal, AbortSignal.timeout(this.registration.lease.ttl_ms)]));
    }
  }
  async close(): Promise<void> { this.abort.abort(); await this.task; }
}
