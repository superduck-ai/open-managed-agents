import { SseProtocolError, sse } from "../sse.ts";
import { DsrError, delay, errorOf, object } from "./http.ts";
import type { DsrClient } from "./client.ts";
import type { Input } from "./wire.ts";

export function parseInput(data: string): Input {
  let value: unknown;
  try {
    value = JSON.parse(data);
  } catch {
    throw new DsrError(0, "invalid_input");
  }
  const input = object(value);
  if (
    typeof input.id !== "string" ||
    !input.id ||
    typeof input.kind !== "string" ||
    !["task", "control"].includes(String(input.lane))
  )
    throw new DsrError(0, "invalid_input");
  if (
    input.lane === "task" &&
    (input.kind !== "user.message" || !Array.isArray(input.content))
  )
    throw new DsrError(0, "unsupported_task_input");
  if (input.lane === "control" && input.kind === "user.message")
    throw new DsrError(0, "invalid_input");
  return input as unknown as Input;
}

export class DsrInputStream {
  constructor(readonly client: DsrClient) {}

  async *read(signal: AbortSignal): AsyncGenerator<Input> {
    let failures = 0;
    while (!signal.aborted) {
      const connection = new AbortController();
      const lifetime = AbortSignal.any([signal, connection.signal]);
      let watchdog: ReturnType<typeof setTimeout> | undefined;
      let healthySince: number | undefined;
      let retryAfterMs: number | undefined;
      const arm = (ms: number) => {
        clearTimeout(watchdog);
        watchdog = setTimeout(
          () => connection.abort(new Error("DSR input stream timeout")),
          ms,
        );
      };
      const touch = () => {
        const now = Date.now();
        healthySince ??= now;
        if (now - healthySince >= 60000) failures = 0;
        arm(45000);
      };
      try {
        const headers: Record<string, string> = {
          "DSR-Protocol": "1",
          Accept: "text/event-stream",
        };
        arm(15000);
        const response = await this.client.transport.request({
          path: `${this.client.root}/inputs/stream`,
          method: "GET",
          token: this.client.claim.session_token,
          headers,
          timeout: 0,
          signal: lifetime,
        });
        if (!response.ok) throw await errorOf(response);
        if (!response.body) throw new DsrError(0, "invalid_response");
        arm(45000);
        for await (const frame of sse(response.body, lifetime, touch)) {
          if (!frame.data) continue;
          if (frame.event !== "input") throw new DsrError(0, "invalid_input");
          const input = parseInput(frame.data);
          if (input.lane !== "control") throw new DsrError(0, "invalid_input");
          yield input;
        }
        if (signal.aborted) return;
        throw new Error("DSR input stream ended");
      } catch (error) {
        if (signal.aborted) return;
        if (error instanceof SseProtocolError)
          throw new DsrError(0, "invalid_input");
        if (error instanceof DsrError && error.code === "lease_expired")
          await this.client.register(signal);
        else if (
          error instanceof DsrError &&
          error.status < 500 &&
          error.status !== 408 &&
          error.status !== 429
        )
          throw error;
        if (++failures >= 10) throw error;
        if (error instanceof DsrError) retryAfterMs = error.retryAfterMs;
      } finally {
        clearTimeout(watchdog);
        connection.abort();
      }
      const backoff = Math.min(1000 * 2 ** Math.max(0, failures - 1), 30000);
      const wait = Math.max(
        retryAfterMs ?? 0,
        backoff * (0.75 + Math.random() * 0.5),
      );
      try {
        await delay(this.client.transport, wait, signal);
      } catch (error) {
        if (signal.aborted) return;
        throw error;
      }
    }
  }
}
