import { expect, spyOn, test } from "bun:test";
import { CcrBatchUploader } from "../src/ccrv2/batch-uploader.ts";
import { CcrClient, type CcrHttpRequest } from "../src/ccrv2/client.ts";
import { CcrHeartbeat } from "../src/ccrv2/heartbeat.ts";
import type { ClientEvent } from "../src/ccrv2/wire.ts";

type Body = { session_id: string; worker_epoch: number; events: ClientEvent[] };
const body = (request: CcrHttpRequest) => request.body as Body;
const settle = () => new Promise<void>((resolve) => setImmediate(resolve));

function fixture(request: (request: CcrHttpRequest) => Promise<Response>) {
  const credentials = { token: "private-token", workerEpoch: 7 };
  const waits: number[] = [];
  const client = new CcrClient({ request, credentials: () => credentials, sessionIds: () => ["s"],
    sleep: async (ms) => { waits.push(ms); }, random: () => 0 });
  return { client, credentials, waits };
}

test("all output sources share a FIFO queue with 100-event batches and HTTP completion per call", async () => {
  const first = Promise.withResolvers<void>();
  const second = Promise.withResolvers<void>();
  const sendingSecond = Promise.withResolvers<void>();
  const requests: CcrHttpRequest[] = [];
  let active = 0;
  let maxActive = 0;
  const { client } = fixture(async (request) => {
    requests.push(request);
    const attempt = requests.length;
    maxActive = Math.max(maxActive, ++active);
    try {
      if (attempt === 1) await first.promise;
      if (attempt === 2) { sendingSecond.resolve(); await second.promise; }
      return new Response(null, { status: 204 });
    } finally { active--; }
  });
  const payloads = Array.from({ length: 120 }, (_, index) => ({
    type: ["event_start", "event_delta", "event_delta", "agent.message"][index % 4]!, uuid: `event-${index}`,
    session_thread_id: "child", delta: { text: String(index) },
  }));
  const keepAlive = new AbortController();
  const confirmed: string[] = [];
  const pending = Promise.allSettled([
    client.events("s", payloads).then(() => { confirmed.push("output"); }),
    client.events("s", [{ type: "control_response", uuid: "control" }]).then(() => { confirmed.push("control"); }),
    client.events("s", [{ type: "keep_alive", uuid: "keep" }], keepAlive.signal).then(() => { confirmed.push("keep"); }),
    client.events("s", [{ type: "agent.message", uuid: "tail" }]).then(() => { confirmed.push("tail"); }),
  ]);
  try {
    expect(requests).toHaveLength(1);
    expect(confirmed).toEqual([]);
    first.resolve(); await sendingSecond.promise; await settle();
    expect(confirmed).toEqual([]);
    second.resolve();
    expect((await pending).every((result) => result.status === "fulfilled")).toBe(true);
    expect(maxActive).toBe(1);
    expect(requests.map((request) => body(request).events.length)).toEqual([100, 21, 1, 1]);
    expect(requests.flatMap((request) => body(request).events.map((event) => event.payload.uuid)))
      .toEqual([...payloads.map((payload) => payload.uuid), "control", "keep", "tail"]);
    expect(requests.flatMap((request) => body(request).events).slice(0, 120))
      .toEqual(payloads.map((payload, index) => ({ payload, ephemeral: index % 4 < 3 })));
    expect(confirmed).toEqual(["output", "control", "keep", "tail"]);
    for (const request of requests) {
      expect(request).toMatchObject({ path: "/v1/code/sessions/s/worker/events", method: "POST", timeout: 10_000 });
      expect(body(request)).toMatchObject({ session_id: "s", worker_epoch: 7 });
    }
  } finally { first.resolve(); second.resolve(); await pending; }
});

test("buffered output batches use UTF-8 byte limits and preserve raw deltas without an accumulator", async () => {
  const requests: CcrHttpRequest[] = [];
  const { client } = fixture(async (request) => { requests.push(request); return new Response(null, { status: 204 }); });
  let flush = () => {};
  client.config.schedule = (callback, delayMs) => {
    expect(delayMs).toBe(100); flush = callback; return () => {};
  };
  const texts = ["A", "汉".repeat(2 * 1024 * 1024), "汉".repeat(2 * 1024 * 1024), "x".repeat(11 * 1024 * 1024), "B"];
  const payloads = texts.map((text, index) => ({ type: "event_delta", event_id: "message", uuid: `delta-${index}`,
    delta: { type: "content_delta", content: { type: "text", text } } }));
  const pending = client.events("s", payloads);
  expect(requests).toHaveLength(0);
  flush();
  expect(requests).toHaveLength(1);
  await pending;
  const batches = requests.map((request) => body(request).events);
  expect(batches.map((batch) => batch.length)).toEqual([2, 1, 1, 1]);
  expect(batches.flat()).toEqual(payloads.map((payload) => ({ payload, ephemeral: true })));
  const bytes = batches.map((batch) => batch.reduce((size, event) => size + Buffer.byteLength(JSON.stringify(event)), 0));
  expect(bytes[0]! + bytes[1]!).toBeGreaterThan(10 * 1024 * 1024);
  expect(bytes[2]!).toBeGreaterThan(10 * 1024 * 1024);
});

test("bulk admission bounds pending events at 100000 and close rejects backpressured calls", async () => {
  let sends = 0;
  const queue = new CcrBatchUploader<number>(async (batch) => {
    sends++; expect(batch).toHaveLength(100); await new Promise<void>(() => {});
  }, () => {}, undefined, { maxBatchSize: 100, maxQueueSize: 100_000, maxBatchBytes: 10 * 1024 * 1024 });
  const completion = Promise.allSettled([
    queue.enqueueMany(Array.from({ length: 100_101 }, (_, index) => index)), queue.enqueueMany([100_101]),
  ]);
  expect(queue.pendingCount).toBe(100_000);
  expect(sends).toBe(1);
  const stopped = new Error("Session stopped");
  await queue.close(stopped);
  for (const result of await completion) expect(result).toEqual({ status: "rejected", reason: stopped });
  expect(queue.pendingCount).toBe(0);
});

test("calls larger than queue capacity drain without deadlock or interleaving later calls", async () => {
  const gate = Promise.withResolvers<void>();
  const batches: number[][] = [];
  const queue = new CcrBatchUploader<number>(async (batch) => {
    batches.push(batch); if (batches.length === 1) await gate.promise;
  }, () => {}, undefined, { maxBatchSize: 2, maxQueueSize: 3 });
  const pending = Promise.all([queue.enqueueMany([0, 1, 2, 3, 4, 5, 6]), queue.enqueueMany([7, 8])]);
  expect(queue.pendingCount).toBe(3);
  gate.resolve(); await pending;
  expect(batches.flat()).toEqual([0, 1, 2, 3, 4, 5, 6, 7, 8]);
  expect(batches.every((batch) => batch.length <= 2)).toBe(true);
});

test("cancelling a backpressured bulk call removes its remaining events and releases later calls", async () => {
  const owner = new AbortController();
  const batches: number[][] = [];
  const queue = new CcrBatchUploader<number>(async (batch, signal) => {
    batches.push(batch);
    if (batches.length === 1) await new Promise<void>((_resolve, reject) => {
      signal.addEventListener("abort", () => reject(signal.reason), { once: true });
    });
  }, () => {}, undefined, { maxBatchSize: 2, maxQueueSize: 3 });
  const bulk = queue.enqueueMany([0, 1, 2, 3, 4, 5, 6], owner.signal);
  const cancelled = bulk.catch((error: unknown) => error);
  const later = queue.enqueueMany([7, 8]);
  expect(queue.pendingCount).toBe(3);
  const stopped = new Error("Call cancelled");
  owner.abort(stopped);
  expect(await cancelled).toBe(stopped);
  await later;
  expect(batches).toEqual([[0, 1], [7, 8]]);
  expect(queue.pendingCount).toBe(0);
});

test("output retries retain snapshots and UUIDs, refresh tokens, log safe fields, and accept failed 2xx cleanup", async () => {
  const waiting = Promise.withResolvers<void>();
  const resume = Promise.withResolvers<void>();
  const requests: CcrHttpRequest[] = [];
  const logs: string[] = [];
  const warn = spyOn(console, "warn").mockImplementation((value) => { logs.push(String(value)); });
  let cleanups = 0;
  const { client, credentials, waits } = fixture(async (request) => {
    requests.push(request);
    credentials.token = `private-token-${requests.length}`;
    if (requests.length === 1) return new Response("private-response", { status: 503 });
    if (requests.length === 2) throw Object.assign(new Error("private-error"), { code: "ECONNRESET" });
    if (requests.length === 3) return new Response(null, { status: 429, headers: { "Retry-After": "2" } });
    return new Response(new ReadableStream({ cancel() { cleanups++; throw new Error("cleanup failed"); } }), { status: 202 });
  });
  const detach = client.bindLease("private-session").subscribe!(() => {});
  client.config.sleep = async (ms) => { waits.push(ms); if (waits.length === 1) { waiting.resolve(); await resume.promise; } };
  const payloads = [{ type: "agent.message", content: { text: "private-original" } }, { type: "agent.thinking", uuid: "private-fixed" }];
  let confirmed = false;
  const pending = client.events("private-session", payloads).then(() => { confirmed = true; });
  try {
    await waiting.promise;
    expect(confirmed).toBe(false);
    payloads[0]!.content!.text = "private-mutated";
    resume.resolve(); await pending;
    expect(confirmed).toBe(true);
    expect(requests).toHaveLength(4);
    for (const request of requests) expect(request.body).toEqual(requests[0]!.body);
    expect(body(requests[0]!).events[0]!.payload.content).toEqual({ text: "private-original" });
    expect(typeof body(requests[0]!).events[0]!.payload.uuid).toBe("string");
    expect(requests.map((request) => request.token)).toEqual(["private-token", "private-token-1", "private-token-2", "private-token-3"]);
    expect(waits).toEqual([500, 1000, 2000]);
    expect(cleanups).toBe(1);
    const fields = { method: "post", path: "/worker/events", session_ref: "7ae99811b2c7c696275f6aca5adcedd71a90d8ab3ced71cd81d7e2c8020628fd", worker_epoch: 7, batch_size: 2 };
    expect(logs.map((log) => JSON.parse(log))).toEqual([
      { event: "cli_worker_request_failed", ...fields, status: 503, failure_reason: "server_error" },
      { event: "cli_worker_request_error", ...fields, failure_reason: "transport_error", error_code: "ECONNRESET" },
      { event: "cli_worker_request_failed", ...fields, status: 429, failure_reason: "rate_limited" },
    ]);
    expect(logs.join()).not.toContain("private-");
    await client.events("private-session", [{ type: "control_response" }]);
    expect(requests).toHaveLength(5);
    expect(logs).toHaveLength(3);
  } finally { resume.resolve(); detach(); await pending.catch(() => {}); warn.mockRestore(); }
});

test("cancelling a queued keepalive leaves surrounding output calls in order", async () => {
  const first = Promise.withResolvers<void>();
  const requests: CcrHttpRequest[] = [];
  const { client } = fixture(async (request) => {
    requests.push(request); if (requests.length === 1) await first.promise; return new Response(null, { status: 204 });
  });
  const keepAlive = new AbortController();
  const pending = Promise.allSettled([
    client.events("s", [{ type: "agent.message", uuid: "first" }]),
    client.events("s", [{ type: "keep_alive", uuid: "keep" }], keepAlive.signal),
    client.events("s", [{ type: "control_response", uuid: "last" }]),
  ]);
  const stopped = new Error("Activity ended");
  keepAlive.abort(stopped); first.resolve();
  const results = await pending;
  expect(results.map((result) => result.status)).toEqual(["fulfilled", "rejected", "fulfilled"]);
  expect(results[1]).toEqual({ status: "rejected", reason: stopped });
  expect(requests.flatMap((request) => body(request).events.map((event) => event.payload.uuid))).toEqual(["first", "last"]);
});

test("cancelling keepalive backoff releases queued output without stopping the session", async () => {
  for (const bound of [false, true]) {
    const waiting = Promise.withResolvers<void>();
    const requests: CcrHttpRequest[] = [];
    const { client } = fixture(async (request) => {
      requests.push(request); return new Response(null, { status: body(request).events[0]!.payload.type === "keep_alive" ? 503 : 204 });
    });
    client.config.sleep = async () => { waiting.resolve(); await new Promise<void>(() => {}); };
    const detach = bound ? client.bindLease("s").subscribe!(() => {}) : undefined;
    const owner = new AbortController();
    const keepAlive = client.events("s", [{ type: "keep_alive" }], owner.signal);
    const cancelled = keepAlive.catch((error: unknown) => error);
    try {
      await waiting.promise;
      const output = client.events("s", [{ type: "agent.message" }]);
      expect(requests).toHaveLength(1);
      const stopped = new Error("Activity ended");
      owner.abort(stopped);
      expect(await cancelled).toBe(stopped);
      await output;
      expect(requests).toHaveLength(2);
      expect(requests[0]!.signal?.aborted).toBe(true);
      await client.events("s", [{ type: "control_response" }]);
      expect(requests).toHaveLength(3);
    } finally { owner.abort(); detach?.(); }
  }
});

test("late output failures after call cancellation cannot log or stop later output", async () => {
  for (const networkError of [false, true]) {
    const response = Promise.withResolvers<Response>();
    const logs: string[] = [];
    const warn = spyOn(console, "warn").mockImplementation((value) => { logs.push(String(value)); });
    let attempts = 0;
    const { client } = fixture(async () => ++attempts === 1 ? response.promise : new Response(null, { status: 204 }));
    const owner = new AbortController();
    const pending = client.events("s", [{ type: "keep_alive" }], owner.signal);
    const cancelled = pending.catch((error: unknown) => error);
    try {
      const output = client.events("s", [{ type: "agent.message" }]);
      owner.abort(new Error("Activity ended"));
      expect(await cancelled).toBeInstanceOf(Error);
      await output;
      if (networkError) response.reject(Object.assign(new Error("private-error"), { code: "ECONNRESET" }));
      else response.resolve(new Response(null, { status: 409 }));
      await settle();
      await client.events("s", [{ type: "control_response" }]);
      expect(attempts).toBe(3);
      expect(logs).toEqual([]);
    } finally { response.resolve(new Response(null, { status: 204 })); warn.mockRestore(); }
  }
});

test("cancellation during successful output cleanup rejects only its call and keeps the queue usable", async () => {
  const cleaning = Promise.withResolvers<void>();
  const cleanup = Promise.withResolvers<void>();
  let requests = 0;
  const { client } = fixture(async () => {
    requests++;
    return requests === 1 ? new Response(new ReadableStream({
      cancel() { cleaning.resolve(); return cleanup.promise; },
    }), { status: 200 }) : new Response(null, { status: 204 });
  });
  const detach = client.bindLease("s").subscribe!(() => {});
  const owner = new AbortController();
  const completion = Promise.allSettled([
    client.events("s", [{ type: "keep_alive" }], owner.signal),
    client.events("s", [{ type: "agent.message" }]),
  ]);
  try {
    await cleaning.promise;
    const stopped = new Error("Activity ended");
    owner.abort(stopped);
    const results = await completion;
    expect(results[0]).toEqual({ status: "rejected", reason: stopped });
    expect(results[1].status).toBe("fulfilled");
    cleanup.reject(new Error("response cleanup failed")); await settle();
    await client.events("s", [{ type: "control_response" }]);
    expect(requests).toBe(3);
  } finally { owner.abort(); cleanup.resolve(); detach(); await completion; }
});

test("output lease rejection logs before cancellation and rejects unsent batches", async () => {
  for (const status of [409, 410, 401, 403]) {
    const logs: string[] = [];
    const warn = spyOn(console, "warn").mockImplementation((value) => { logs.push(String(value)); });
    let attempts = 0;
    const { client, credentials } = fixture(async () => { attempts++; return new Response(null, { status }); });
    if (status === 401) credentials.token = `header.${Buffer.from(JSON.stringify({ exp: 1 })).toString("base64url")}.signature`;
    const owner = new AbortController();
    const heartbeat = new CcrHeartbeat({ check: client.bindLease("s"), maxAuthFailures: 2,
      onLeaseLost: () => { owner.abort(); } }, owner.signal);
    try {
      await expect(client.events("s", Array.from({ length: 240 }, () => ({ type: "agent.message" }))))
        .rejects.toMatchObject({ status });
      expect(attempts).toBe(status === 403 ? 2 : 1);
      expect(logs).toHaveLength(attempts);
      for (const log of logs) expect(JSON.parse(log)).toMatchObject({ event: "cli_worker_request_failed", status,
        method: "post", path: "/worker/events", worker_epoch: 7, batch_size: 100 });
    } finally { await heartbeat.close(); warn.mockRestore(); }
  }
});

test("epoch and same-epoch registration replacements cancel old output without poisoning the new queue", async () => {
  for (const register of [false, true]) {
    const response = Promise.withResolvers<Response>();
    const requests: CcrHttpRequest[] = [];
    const { client, credentials } = fixture(async (request) => {
      if (request.path.endsWith("/register")) return Response.json({ worker_epoch: 7 });
      requests.push(request); return requests.length === 1 ? response.promise : new Response(null, { status: 204 });
    });
    const pending = Promise.allSettled([
      client.events("s", Array.from({ length: 240 }, () => ({ type: "agent.message" }))),
      client.events("s", [{ type: "control_response" }]),
    ]);
    try {
      if (register) await client.register("s", "replacement");
      else credentials.workerEpoch = 8;
      await client.events("s", [{ type: "agent.message", uuid: "new" }]);
      expect((await pending).every((result) => result.status === "rejected")).toBe(true);
      response.resolve(new Response(null, { status: 409 })); await settle();
      await client.events("s", [{ type: "agent.message", uuid: "next" }]);
      expect(requests).toHaveLength(3);
      expect(requests[0]!.signal?.aborted).toBe(true);
      expect(body(requests[1]!).worker_epoch).toBe(register ? 7 : 8);
    } finally { response.resolve(new Response(null, { status: 204 })); await pending; }
  }
});

test("empty, invalid and pre-cancelled output calls send nothing, and sessions remain independent", async () => {
  const first = Promise.withResolvers<void>();
  const requests: CcrHttpRequest[] = [];
  const { client } = fixture(async (request) => {
    requests.push(request); if (request.path.includes("/s/")) await first.promise; return new Response(null, { status: 204 });
  });
  await client.events("s", []);
  await expect(client.events("s", [{ type: "agent.message" }, { type: "agent.message", invalid: 1n }])).rejects.toThrow();
  await expect(client.events("s", [{ type: "agent.message" }], AbortSignal.abort())).rejects.toThrow();
  expect(requests).toEqual([]);
  const pending = client.events("s", [{ type: "agent.message" }]);
  try {
    await client.events("other", [{ type: "agent.message" }]);
    expect(requests).toHaveLength(2);
  } finally { first.resolve(); await pending; }
});

test("output logging failures cannot change retry results or terminal HTTP errors", async () => {
  const warn = spyOn(console, "warn").mockImplementation(() => { throw new Error("logging failed"); });
  let attempts = 0;
  const { client } = fixture(async () => {
    if (++attempts === 1) return new Response(null, { status: 503 });
    if (attempts === 2) throw new Error("network failed");
    return new Response(null, { status: 204 });
  });
  const { client: rejected } = fixture(async () => new Response(new ReadableStream({
    cancel() { throw new Error("cleanup failed"); },
  }), { status: 409 }));
  try {
    await client.events("s", [{ type: "agent.message" }]);
    expect(attempts).toBe(3);
    await expect(rejected.events("s", [{ type: "agent.message" }])).rejects.toMatchObject({ status: 409 });
    expect(warn).toHaveBeenCalledTimes(3);
  } finally { warn.mockRestore(); }
});
