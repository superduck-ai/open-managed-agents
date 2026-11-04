import { sse } from "../sse.ts";
import { DsrError, decimal, delay, errorOf, object } from "./http.ts";
import type { DsrClient } from "./client.ts";
import type { Input } from "./wire.ts";

function parseInput(value: unknown): Input {
  const input = object(value);
  if (typeof input.id !== "string" || !input.id || typeof input.kind !== "string" || !["task", "control"].includes(String(input.lane))) throw new DsrError(0, "invalid_input");
  if (input.lane === "task" && (input.kind !== "user.message" || !Array.isArray(input.content))) throw new DsrError(0, "unsupported_task_input");
  if (input.lane === "control" && input.kind === "user.message") throw new DsrError(0, "invalid_input");
  return input as unknown as Input;
}

export class DsrInputStream {
  private cursor = "0";
  constructor(readonly client: DsrClient) {}

  async *read(signal: AbortSignal): AsyncGenerator<Input & { transportSequence?: string }> {
    let failures = 0;
    while (!signal.aborted) {
      const connection = new AbortController();
      const lifetime = AbortSignal.any([signal, connection.signal]);
      let watchdog: ReturnType<typeof setTimeout> | undefined;
      const touch = () => {
        clearTimeout(watchdog);
        watchdog = setTimeout(() => connection.abort(new Error("DSR input stream idle")), 45000);
      };
      try {
        const headers: Record<string, string> = { "DSR-Protocol": "1", Accept: "text/event-stream" };
        if (this.cursor !== "0") headers["Last-Event-ID"] = this.cursor;
        const response = await this.client.transport.request({ path: `${this.client.root}/inputs/stream?from_sequence_num=${this.cursor}`,
          method: "GET", token: this.client.claim.session_token, headers, timeout: 0, signal: lifetime });
        if (!response.ok) throw await errorOf(response);
        if (!response.body) throw new DsrError(0, "invalid_response");
        failures = 0;
        touch();
        for await (const frame of sse(response.body, lifetime, touch)) {
          if (!frame.data) continue;
          if (frame.event !== "input") throw new DsrError(0, "invalid_input");
          const input = parseInput(JSON.parse(frame.data));
          if (frame.id) {
            decimal(frame.id);
            if (BigInt(frame.id) > BigInt(this.cursor)) this.cursor = frame.id;
          }
          yield { ...input, ...(frame.id ? { transportSequence: frame.id } : {}) };
        }
      } catch (error) {
        if (signal.aborted) return;
        if (error instanceof DsrError && error.code === "lease_expired") await this.client.register(signal);
        else if (error instanceof DsrError && error.status < 500 && error.status !== 408 && error.status !== 429) throw error;
        if (++failures >= 10) throw error;
      } finally {
        clearTimeout(watchdog);
        connection.abort();
      }
      if (!signal.aborted) await delay(this.client.transport, Math.min(1000 * 2 ** Math.max(0, failures - 1), 30000), signal);
    }
  }
}
