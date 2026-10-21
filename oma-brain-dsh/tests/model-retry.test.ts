import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import type { LlmFailure } from "@deepseek-ai/dsh-llm";
import { OmaApi } from "../src/api.ts";
import { retryAfterMs } from "../src/llm-ai-sdk/stream.ts";
import { IdleTimeout } from "../src/model-provider.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { mountOma } from "../src/plugin.ts";
import { ModelRetry } from "../src/runtime/model-retry.ts";
import { Backend, textEvents } from "./backend.ts";

const failure = (code: string, providerRetryAfterMs?: number): LlmFailure => ({ message: code, code, providerRetryAfterMs });

function retryFixture(maxRetries = 2) {
  const delays: number[] = [];
  const retry = new ModelRetry({ maxRetries, random: () => 0, sleep: async (ms) => { delays.push(ms); } });
  return { retry, delays, signal: new AbortController().signal };
}

test("model retry rejects non-transient failures without waiting", async () => {
  const { retry, delays, signal } = retryFixture();
  for (const code of ["AUTH", "QUOTA", "ACCOUNT_QUOTA", "CONTEXT_WINDOW_EXCEEDED", "INVALID_ARGS", "UNKNOWN"])
    expect(await retry.retry(failure(code), 1, 1, signal)).toBe(false);
  expect(delays).toEqual([]);
});

test("model retry stops after the per-step budget and resets on the next step", async () => {
  const { retry, delays, signal } = retryFixture();
  expect(await retry.retry(failure("SERVER"), 1, 1, signal)).toBe(true);
  expect(await retry.retry(failure("TRANSPORT"), 1, 1, signal)).toBe(true);
  expect(await retry.retry(failure("RATE_LIMIT"), 1, 1, signal)).toBe(false);
  expect(await retry.retry(failure("EMPTY_RESPONSE"), 1, 2, signal)).toBe(true);
  expect(delays).toEqual([1000, 2000, 1000]);
});

test("model retry honors and caps provider Retry-After", async () => {
  const { retry, delays, signal } = retryFixture();
  expect(await retry.retry(failure("RATE_LIMIT", 5000), 1, 1, signal)).toBe(true);
  expect(await retry.retry(failure("RATE_LIMIT", 120_000), 1, 1, signal)).toBe(true);
  expect(delays).toEqual([5000, 30_000]);
});

test("model retry wait is cancelled with the turn", async () => {
  const controller = new AbortController();
  const retry = new ModelRetry({ baseDelayMs: 60_000 });
  const waiting = retry.retry(failure("SERVER"), 1, 1, controller.signal);
  controller.abort(new Error("interrupted"));
  await expect(waiting).rejects.toThrow("interrupted");
});

test("Retry-After accepts seconds and HTTP dates and ignores invalid values", () => {
  const now = Date.parse("2026-01-01T00:00:00Z");
  expect(retryAfterMs(undefined)).toBeUndefined();
  expect(retryAfterMs({ "retry-after": "abc" })).toBeUndefined();
  expect(retryAfterMs({ "retry-after": "0" })).toBeUndefined();
  expect(retryAfterMs({ "Retry-After": "Wed, 31 Dec 2025 23:59:00 GMT" }, now)).toBeUndefined();
  expect(retryAfterMs({ "Retry-After": "2" })).toBe(2000);
  expect(retryAfterMs({ "retry-after": "Thu, 01 Jan 2026 00:00:03 GMT" }, now)).toBe(3000);
});

test("model idle timeout aborts a stalled HTTP response body", async () => {
  const server = Bun.serve({ port: 0, fetch: () => new Response(new ReadableStream<Uint8Array>({
    start(controller) { controller.enqueue(new TextEncoder().encode("first")); },
  })) });
  try {
    const idle = new IdleTimeout(100);
    const response = idle.watch(await fetch(server.url, { signal: idle.signal }));
    const reader = response.body!.getReader();
    expect(new TextDecoder().decode((await reader.read()).value)).toBe("first");
    await expect(reader.read()).rejects.toThrow();
    expect(idle.signal.aborted).toBe(true);
  } finally {
    server.stop(true);
  }
});

test("model idle timeout allows long responses that keep streaming", async () => {
  const idle = new IdleTimeout(60);
  let sent = 0;
  const body = new ReadableStream<Uint8Array>({
    async pull(controller) {
      if (sent === 6) { controller.close(); return; }
      await Bun.sleep(30);
      sent++;
      controller.enqueue(new TextEncoder().encode("x"));
    },
  });
  expect(await idle.watch(new Response(body)).text()).toBe("xxxxxx");
  expect(idle.signal.aborted).toBe(false);
});

test("Brain retries transient model failures and completes the turn", async () => {
  const backend = new Backend();
  await backend.ready;
  const responses: (() => Response | ReturnType<typeof textEvents>)[] = [
    () => Response.json({ type: "error", error: { type: "overloaded_error", message: "Overloaded" } }, { status: 529, headers: { "retry-after": "0.01" } }),
    () => Response.json({ type: "error", error: { type: "api_error", message: "Internal" } }, { status: 500, headers: { "retry-after": "0.01" } }),
    () => textEvents("recovered"),
  ];
  backend.onModel = async () => responses.shift()!();
  const context = new Context();
  let session: ReturnType<typeof createOmaSession> | undefined;
  try {
    const api = new OmaApi(backend.url);
    await mountOma(context, api);
    session = createOmaSession(context, api, backend.a);
    await session.open();
    await session.receive("retry_input", { content: "Hello" });
    expect(backend.requests).toHaveLength(3);
    const published = [...backend.publicLog.values()];
    expect(published.some((event) => event.type === "result" && event.subtype === "success")).toBe(true);
    expect(published.some((event) => event.type === "result" && event.is_error)).toBe(false);
  } finally {
    await session?.close();
    await context.fiber.dispose();
    backend.stop();
  }
});
