import { afterEach, beforeEach, expect, spyOn, test } from "bun:test";
import { CcrClient, CcrHttpError, type CcrHttpRequest } from "../src/ccrv2/index.ts";
import { CcrHeartbeat, type CcrLeaseFailure, type CcrLeaseResult } from "../src/ccrv2/heartbeat.ts";

const logs: { level: string; line: string }[] = [];
const restoreLogs: (() => void)[] = [];
beforeEach(() => {
  logs.length = 0;
  for (const level of ["warn", "error", "info"] as const) {
    const mock = spyOn(console, level).mockImplementation((line: unknown) => {
      logs.push({ level, line: String(line) });
    });
    restoreLogs.push(() => mock.mockRestore());
  }
});
afterEach(() => { for (const restore of restoreLogs.splice(0)) restore(); });

function clientWith(request: (request: CcrHttpRequest) => Promise<Response>, token = "worker-token") {
  return new CcrClient({
    request,
    credentials: () => ({ token, workerEpoch: 7 }),
    sessionIds: () => ["session/a"],
    sleep: async () => {},
    random: () => 0,
  });
}

test("GET worker reads external metadata without a body or a lease notification", async () => {
  const requests: CcrHttpRequest[] = [];
  const metadata = { model: "model-a", permission_mode: "plan", custom: { enabled: true } };
  const client = clientWith(async (request) => {
    requests.push(request);
    return Response.json({ worker: { external_metadata: metadata } });
  });
  let notifications = 0;
  const unsubscribe = client.bindLease("session/a").subscribe!(() => { notifications++; });
  try {
    const result = await client.getWorkerState("session/a");
    expect(result.metadata).toEqual(metadata);
    expect(result.durationMs).toBeGreaterThanOrEqual(0);
    expect(requests).toHaveLength(1);
    expect(requests[0]!.path).toBe("/v1/code/sessions/session%2Fa/worker");
    expect(requests[0]!.method).toBe("GET");
    expect(requests[0]!.body).toBeUndefined();
    expect(requests[0]!.token).toBe("worker-token");
    expect(requests[0]!.timeout).toBe(30_000);
    expect(requests[0]!.headers).toEqual({ "anthropic-version": "2023-06-01", "User-Agent": "oma-brain-dsh" });
    expect(notifications).toBe(0);
    expect(JSON.parse(logs.at(-1)!.line)).toEqual({
      event: "ccrv2_worker_state_read", context: "worker_state", session_id: "session/a",
      worker_epoch: 7, duration_ms: result.durationMs, had_state: true,
    });
    expect(JSON.stringify(logs)).not.toContain("model-a");
    expect(JSON.stringify(logs)).not.toContain("worker-token");
  } finally {
    unsubscribe();
  }
});

test("GET worker accepts omitted metadata and preserves an explicitly empty object", async () => {
  for (const body of [{}, { worker: {} }, { worker: null }, { worker: { external_metadata: null } }]) {
    const client = clientWith(async () => Response.json(body));
    expect((await client.getWorkerState("session/a")).metadata).toBeNull();
  }
  const client = clientWith(async () => Response.json({ worker: { external_metadata: {} } }));
  expect((await client.getWorkerState("session/a")).metadata).toEqual({});
});

test("GET worker skips requests without authentication and reports zero duration", async () => {
  let requests = 0;
  const client = clientWith(async () => { requests++; return Response.json({}); }, "");
  expect(await client.getWorkerState("session/a")).toEqual({ metadata: null, durationMs: 0 });
  expect(requests).toBe(0);
  expect(logs).toEqual([]);
});

test("GET worker rejects malformed responses instead of treating them as missing metadata", async () => {
  for (const body of [null, [], { worker: [] }, { worker: "invalid" },
    { worker: { external_metadata: [] } }, { worker: { external_metadata: 1 } }]) {
    const client = clientWith(async () => Response.json(body));
    await expect(client.getWorkerState("session/a")).rejects.toThrow("Expected a JSON object");
  }
  for (const body of ["", "{invalid"]) {
    const client = clientWith(async () => new Response(body));
    await expect(client.getWorkerState("session/a")).rejects.toThrow();
  }
});

test("GET worker can check the current epoch and rejects invalid or replaced epochs before sending", async () => {
  const requests: CcrHttpRequest[] = [];
  const client = clientWith(async (request) => {
    requests.push(request);
    return Response.json({ worker: {} });
  });
  expect((await client.getWorkerState("session/a", 7)).metadata).toBeNull();
  expect(requests[0]!.path).toBe("/v1/code/sessions/session%2Fa/worker?worker_epoch=7");
  expect(requests[0]!.body).toBeUndefined();
  for (const epoch of [0, -1, 1.5, NaN, Infinity, Number.MAX_SAFE_INTEGER + 1]) {
    await expect(client.getWorkerState("session/a", epoch)).rejects.toThrow("Invalid worker epoch");
  }
  await expect(client.getWorkerState("session/a", 6)).rejects.toThrow("OMA HTTP 409");
  expect(requests).toHaveLength(1);
});

test("GET worker retries transient failures with current credentials and honors Retry-After", async () => {
  const requests: CcrHttpRequest[] = [];
  const waits: number[] = [];
  let token = "first-token";
  const client = new CcrClient({
    credentials: () => ({ token, workerEpoch: 7 }),
    sessionIds: () => ["session/a"],
    sleep: async (ms) => { waits.push(ms); },
    random: () => 0,
    request: async (request) => {
      requests.push(request);
      token = "refreshed-token";
      if (requests.length === 1) throw new Error("Connection lost");
      if (requests.length === 2)
        return new Response(null, { status: 429, headers: { "Retry-After": "3" } });
      return Response.json({ worker: { external_metadata: { model: "restored-model" } } });
    },
  });
  expect((await client.getWorkerState("session/a", 7)).metadata).toEqual({ model: "restored-model" });
  expect(requests.map((request) => request.token)).toEqual(["first-token", "refreshed-token", "refreshed-token"]);
  expect(requests.every((request) => request.path.endsWith("?worker_epoch=7") && request.body === undefined)).toBe(true);
  expect(waits).toEqual([500, 3000]);
});

test("GET worker retries ordinary HTTP failures ten times and returns null without changing lease accounting", async () => {
  for (const status of [400, 401, 403, 408, 429, 503]) {
    let requests = 0;
    let notifications = 0;
    const client = clientWith(async () => {
      requests++;
      return new Response(null, { status });
    });
    const unsubscribe = client.bindLease("session/a").subscribe!(() => { notifications++; });
    try {
      expect((await client.getWorkerState("session/a", 7)).metadata).toBeNull();
      expect(requests).toBe(10);
      expect(notifications).toBe(0);
      expect(logs.at(-1)!.line).toContain("ccrv2_worker_get_retries_exhausted");
    } finally {
      unsubscribe();
    }
  }
});

test("GET worker uses the reference backoff cap and jitter, reports elapsed time and redacts transport errors", async () => {
  let now = 100;
  const time = spyOn(Date, "now").mockImplementation(() => now);
  const waits: number[] = [];
  let requests = 0;
  const client = new CcrClient({
    credentials: () => ({ token: "private-token", workerEpoch: 7 }),
    sessionIds: () => ["session/a"],
    random: () => 0.5,
    sleep: async (ms) => { waits.push(ms); now += ms; },
    request: async () => { requests++; now += 5; throw new Error("private-error-body"); },
  });
  try {
    expect(await client.getWorkerState("session/a")).toEqual({ metadata: null, durationMs: 123800 });
    expect(requests).toBe(10);
    expect(waits).toEqual([750, 1250, 2250, 4250, 8250, 16250, 30250, 30250, 30250]);
    expect(logs.filter((log) => log.level === "warn").map((log) => JSON.parse(log.line).attempt))
      .toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9, 10]);
    expect(JSON.parse(logs.at(-1)!.line)).toEqual({
      event: "ccrv2_worker_get_retries_exhausted", context: "worker_state", session_id: "session/a",
      worker_epoch: 7, attempts: 10, duration_ms: 123800,
    });
    expect(JSON.stringify(logs)).not.toContain("private-");
  } finally { time.mockRestore(); }
});

test("terminal GET failures stop only the owning session and preserve the HTTP error", async () => {
  const expired = `header.${Buffer.from(JSON.stringify({ exp: Math.floor(Date.now() / 1000) - 60 })).toString("base64url")}.signature`;
  for (const [status, token, reason] of [
    [409, "token", "epoch_mismatch"], [410, "token", "lease_expired"],
    [401, expired, "token_expired"], [403, expired, "token_expired"],
  ] as const) {
    let requests = 0;
    const client = clientWith(async () => { requests++; return new Response(null, { status }); }, token);
    const owner = new AbortController();
    const losses: CcrLeaseFailure[] = [];
    const heartbeat = new CcrHeartbeat({
      check: client.bindLease("session/a"),
      onLeaseLost: (failure) => { losses.push(failure); owner.abort(); },
    }, owner.signal);
    const other: CcrLeaseResult[] = [];
    const unsubscribe = client.bindLease("other-session").subscribe!((result) => { other.push(result); });
    try {
      await expect(client.getWorkerState("session/a")).rejects.toBeInstanceOf(CcrHttpError);
      expect(requests).toBe(1);
      expect(owner.signal.aborted).toBe(true);
      expect(losses).toEqual([{
        status: "rejected", httpStatus: status, workerEpoch: 7, reason, consecutiveFailures: 1,
      }]);
      expect(other).toEqual([]);
    } finally { unsubscribe(); await heartbeat.close(); }
  }
});

test("successful and failed GETs neither reset nor increment the shared authentication counter", async () => {
  let getStatus = 401;
  const client = clientWith(async ({ method }) => method === "GET" && getStatus === 200
    ? Response.json({ worker: {} }) : new Response(null, { status: 401 }));
  const losses: CcrLeaseFailure[] = [];
  const heartbeat = new CcrHeartbeat({
    check: client.bindLease("session/a"), maxAuthFailures: 2,
    onLeaseLost: (failure) => { losses.push(failure); },
  }, new AbortController().signal);
  try {
    await expect(client.heartbeat("session/a")).rejects.toThrow("401");
    expect((await client.getWorkerState("session/a")).metadata).toBeNull();
    expect(losses).toEqual([]);
    getStatus = 200;
    expect((await client.getWorkerState("session/a")).metadata).toBeNull();
    await expect(client.heartbeat("session/a")).rejects.toThrow("401");
    expect(losses).toHaveLength(1);
    expect(losses[0]!.consecutiveFailures).toBe(2);
  } finally { await heartbeat.close(); }
});

test("GET worker never follows a replacement epoch during retry", async () => {
  let workerEpoch = 7;
  let requests = 0;
  const client = new CcrClient({
    credentials: () => ({ token: "worker-token", workerEpoch }),
    sessionIds: () => ["session/a"],
    sleep: async () => { workerEpoch = 8; },
    request: async () => {
      requests++;
      return new Response(null, { status: 503 });
    },
  });
  await expect(client.getWorkerState("session/a", 7)).rejects.toThrow("OMA HTTP 409");
  expect(requests).toBe(1);
});

test("a late GET rejection cannot stop a replacement claim even when the epoch is reused", async () => {
  const response = Promise.withResolvers<Response>();
  const client = clientWith(async () => response.promise);
  const old: CcrLeaseResult[] = [];
  const current: CcrLeaseResult[] = [];
  const detachOld = client.bindLease("session/a").subscribe!((result) => { old.push(result); });
  const pending = client.getWorkerState("session/a");
  detachOld();
  const detachCurrent = client.bindLease("session/a").subscribe!((result) => { current.push(result); });
  try {
    response.resolve(new Response(null, { status: 409 }));
    await expect(pending).rejects.toThrow("session stopped");
    expect(old).toEqual([]);
    expect(current).toEqual([]);
    expect(logs).toEqual([]);
  } finally { detachCurrent(); }
});

test("GET worker forwards cancellation to the transport and cancels retry waits", async () => {
  const aborted = new AbortController();
  aborted.abort(new Error("Already closed"));
  const neverSend = clientWith(async () => { throw new Error("Must not send"); });
  await expect(neverSend.getWorkerState("session/a", undefined, aborted.signal)).rejects.toThrow("Already closed");

  const started = Promise.withResolvers<void>();
  const abort = new AbortController();
  const client = clientWith(async ({ signal }) => {
    expect(signal).toBe(abort.signal);
    started.resolve();
    return new Promise<Response>((_, reject) => {
      signal!.addEventListener("abort", () => reject(signal!.reason), { once: true });
    });
  });
  const pending = client.getWorkerState("session/a", 7, abort.signal);
  await started.promise;
  abort.abort(new Error("Session closed"));
  await expect(pending).rejects.toThrow("Session closed");

  const sleeping = Promise.withResolvers<void>();
  const retryAbort = new AbortController();
  let requests = 0;
  const retrying = new CcrClient({
    credentials: () => ({ token: "worker-token", workerEpoch: 7 }),
    sessionIds: () => ["session/a"],
    request: async () => { requests++; return new Response(null, { status: 503 }); },
    sleep: async () => { sleeping.resolve(); await new Promise(() => {}); },
  });
  const waiting = retrying.getWorkerState("session/a", 7, retryAbort.signal);
  await sleeping.promise;
  retryAbort.abort(new Error("Retry cancelled"));
  await expect(waiting).rejects.toThrow("Retry cancelled");
  expect(requests).toBe(1);
});

test("claim shutdown cancels GET retry waits even without a caller signal", async () => {
  const sleeping = Promise.withResolvers<void>();
  let requests = 0;
  const client = new CcrClient({
    credentials: () => ({ token: "worker-token", workerEpoch: 7 }),
    sessionIds: () => ["session/a"],
    request: async () => { requests++; return new Response(null, { status: 503 }); },
    sleep: async () => { sleeping.resolve(); await new Promise(() => {}); },
  });
  const detach = client.bindLease("session/a").subscribe!(() => {});
  const pending = client.getWorkerState("session/a");
  await sleeping.promise;
  detach();
  await expect(pending).rejects.toThrow("session stopped");
  expect(requests).toBe(1);
  expect(logs.some((log) => log.line.includes("retries_exhausted"))).toBe(false);
});

test("a broken diagnostic sink does not change GET success or fallback", async () => {
  const mocks = ["info", "warn", "error"].map((level) => spyOn(console, level as "info" | "warn" | "error")
    .mockImplementation(() => { throw new Error("Broken diagnostic sink"); }));
  try {
    const success = clientWith(async () => Response.json({ worker: { external_metadata: { model: "m" } } }));
    expect((await success.getWorkerState("session/a")).metadata).toEqual({ model: "m" });
    const failed = clientWith(async () => new Response(null, { status: 503 }));
    expect((await failed.getWorkerState("session/a")).metadata).toBeNull();
  } finally { for (const mock of mocks) mock.mockRestore(); }
});

test("metadata-only PUT preserves patch keys and waits for confirmation with an unchanged retry payload", async () => {
  const requests: CcrHttpRequest[] = [];
  const retrying = Promise.withResolvers<void>();
  const resume = Promise.withResolvers<void>();
  const confirmation = Promise.withResolvers<Response>();
  const client = new CcrClient({
    credentials: () => ({ token: "worker-token", workerEpoch: 7 }), sessionIds: () => ["session/a"],
    random: () => 0,
    sleep: async () => { retrying.resolve(); await resume.promise; },
    request: async (request) => {
      requests.push(request);
      return requests.length === 1 ? new Response(null, { status: 503 }) : confirmation.promise;
    },
  });
  const detach = client.bindLease("session/a").subscribe!(() => {});
  const patch = { model: "new-model", task_summary: null, custom: { a: 1 } };
  let done = false;
  const pending = client.reportMetadata("session/a", patch).then(() => { done = true; });
  try {
    await retrying.promise;
    patch.custom.a = 99;
    expect(done).toBe(false);
    resume.resolve();
    await new Promise<void>((resolve) => setImmediate(resolve));
    expect(requests).toHaveLength(2);
    expect(done).toBe(false);
    expect(requests[0]!.method).toBe("PUT");
    expect(requests[0]!.path).toBe("/v1/code/sessions/session%2Fa/worker");
    expect(requests[0]!.body).toEqual({
      session_id: "session/a", worker_epoch: 7,
      external_metadata: { model: "new-model", task_summary: null, custom: { a: 1 } },
    });
    expect(requests[1]!.body).toEqual(requests[0]!.body);
    confirmation.resolve(new Response(null, { status: 204 }));
    await pending;
    expect(done).toBe(true);
  } finally { resume.resolve(); confirmation.resolve(new Response(null, { status: 204 })); detach(); }
});

test("idle clears task_summary in the status PUT while running and approval preserve it", async () => {
  const requests: CcrHttpRequest[] = [];
  const client = clientWith(async (request) => { requests.push(request); return new Response(null, { status: 204 }); });
  await client.setStatus("session/a", "idle");
  expect(requests[0]!.body).toEqual({
    session_id: "session/a", worker_epoch: 7, worker_status: "idle",
    requires_action_details: null, external_metadata: { pending_action: null, task_summary: null },
  });
  const details = { tool_name: "Bash", tool_use_id: "tool", request_id: "request", action_description: "run" };
  await client.setStatus("session/a", "requires_action", details);
  await client.setStatus("session/a", "running");
  expect(requests.slice(1).every((request) => !JSON.stringify(request.body).includes("task_summary"))).toBe(true);
  expect(requests).toHaveLength(3);
});

test("PUT diagnostics classify each failed attempt without disclosing tokens, metadata or error bodies", async () => {
  let requests = 0;
  const client = clientWith(async () => {
    if (++requests === 1) throw new Error("private-token-and-server-body");
    return new Response(null, { status: requests === 2 ? 503 : 204 });
  });
  await client.reportMetadata("session/a", { task_summary: "private-user-summary" });
  expect(logs.map((log) => JSON.parse(log.line))).toEqual([
    { event: "ccrv2_worker_put_failed", method: "PUT", path: "/worker", session_id: "session/a",
      worker_epoch: 7, http_status: null, failure_reason: "transport_error" },
    { event: "ccrv2_worker_put_failed", method: "PUT", path: "/worker", session_id: "session/a",
      worker_epoch: 7, http_status: 503, failure_reason: "server_error" },
  ]);
  expect(JSON.stringify(logs)).not.toContain("private-");
  expect(JSON.stringify(logs)).not.toContain("worker-token");
});

test("terminal PUT failures are diagnosed even when lease loss cancels the claim", async () => {
  const owner = new AbortController();
  const client = clientWith(async () => new Response(null, { status: 409 }));
  const heartbeat = new CcrHeartbeat({
    check: client.bindLease("session/a"), onLeaseLost: () => owner.abort(),
  }, owner.signal);
  try {
    await expect(client.reportMetadata("session/a", { model: "m" })).rejects.toThrow("409");
    expect(owner.signal.aborted).toBe(true);
    expect(logs).toHaveLength(1);
    expect(JSON.parse(logs[0]!.line).failure_reason).toBe("epoch_mismatch");
  } finally { await heartbeat.close(); }
});

test("cancelled PUTs do not report failure and a broken diagnostic sink cannot disrupt retries", async () => {
  const started = Promise.withResolvers<void>();
  const client = clientWith(async ({ signal }) => {
    started.resolve();
    return new Promise<Response>((_, reject) => signal!.addEventListener("abort", () => reject(signal!.reason), { once: true }));
  });
  const detach = client.bindLease("session/a").subscribe!(() => {});
  const pending = client.reportMetadata("session/a", { task_summary: null });
  await started.promise;
  detach();
  await expect(pending).rejects.toThrow("session stopped");
  expect(logs).toEqual([]);
  const broken = spyOn(console, "warn").mockImplementation(() => { throw new Error("Broken sink"); });
  try {
    let attempts = 0;
    const retrying = clientWith(async () => new Response(null, { status: ++attempts === 1 ? 503 : 204 }));
    await retrying.setStatus("session/a", "idle");
    expect(attempts).toBe(2);
  } finally { broken.mockRestore(); }
});

test("worker PUT FIFO preserves snapshots, retries and per-call confirmation without blocking other sessions", async () => {
  const requests: CcrHttpRequest[] = [];
  const retrying = Promise.withResolvers<void>();
  const retry = Promise.withResolvers<void>();
  const metadataStarted = Promise.withResolvers<void>();
  const metadataResponse = Promise.withResolvers<Response>();
  let firstAttempts = 0;
  const client = new CcrClient({
    credentials: () => ({ token: "token", workerEpoch: 7 }), sessionIds: () => [], random: () => 0,
    sleep: async () => { retrying.resolve(); await retry.promise; },
    request: async (request) => {
      requests.push(request);
      const body = request.body as Record<string, unknown>;
      if (body.session_id === "b") return new Response(null, { status: 204 });
      if (body.worker_status === "running" && ++firstAttempts === 1) return new Response(null, { status: 503 });
      if (body.worker_status === undefined) { metadataStarted.resolve(); return metadataResponse.promise; }
      return new Response(null, { status: 204 });
    },
  });
  const running = client.setStatus("a", "running");
  await retrying.promise;
  const patch = { task_summary: "progress", custom: { value: 1 } };
  let patched = false;
  let idled = false;
  const metadata = client.reportMetadata("a", patch).then(() => { patched = true; });
  const idle = client.setStatus("a", "idle").then(() => { idled = true; });
  patch.custom.value = 99;
  await client.setStatus("b", "running");
  expect(requests).toHaveLength(2);
  retry.resolve();
  await running;
  await metadataStarted.promise;
  expect(patched).toBe(false);
  expect(idled).toBe(false);
  expect(requests[2]!.body).toEqual(requests[0]!.body);
  expect(requests[3]!.body).toEqual({ session_id: "a", worker_epoch: 7,
    external_metadata: { task_summary: "progress", custom: { value: 1 } } });
  metadataResponse.resolve(new Response(null, { status: 204 }));
  await Promise.all([metadata, idle]);
  expect((requests[4]!.body as Record<string, unknown>).worker_status).toBe("idle");
  expect(requests.every((request) => request.headers?.["anthropic-version"] === "2023-06-01"
    && request.headers?.["User-Agent"] === "oma-brain-dsh")).toBe(true);
});

test("status deduplication waits for confirmation and metadata clearing cannot be skipped", async () => {
  const started = Promise.withResolvers<void>();
  const first = Promise.withResolvers<Response>();
  const requests: CcrHttpRequest[] = [];
  const client = clientWith(async (request) => {
    requests.push(request);
    if (requests.length === 1) { started.resolve(); return first.promise; }
    return new Response(null, { status: 204 });
  });
  const initial = client.setStatus("session/a", "idle");
  await started.promise;
  let duplicateDone = false;
  const duplicate = client.setStatus("session/a", "idle").then(() => { duplicateDone = true; });
  await new Promise<void>((resolve) => setImmediate(resolve));
  expect(duplicateDone).toBe(false);
  first.resolve(new Response(null, { status: 204 }));
  await Promise.all([initial, duplicate]);
  expect(requests).toHaveLength(1);
  await client.reportMetadata("session/a", { model: "m" });
  await client.setStatus("session/a", "idle");
  expect(requests).toHaveLength(2);
  await client.reportMetadata("session/a", { task_summary: "progress" });
  await client.setStatus("session/a", "idle");
  expect(requests).toHaveLength(4);
  expect((requests[3]!.body as Record<string, unknown>).external_metadata).toEqual({ pending_action: null, task_summary: null });
  const details = { tool_name: "Bash", tool_use_id: "tool", request_id: "request", action_description: "run" };
  await client.setStatus("session/a", "requires_action", details);
  await client.setStatus("session/a", "requires_action", details);
  expect(requests).toHaveLength(6);
});

test("failed PUT invalidates confirmed status and does not poison the queue", async () => {
  let calls = 0;
  const client = clientWith(async () => new Response(null, { status: ++calls === 2 ? 400 : 204 }));
  await client.setStatus("session/a", "idle");
  await expect(client.setStatus("session/a", "running")).rejects.toThrow("400");
  await client.setStatus("session/a", "idle");
  expect(calls).toBe(3);
  await client.setStatus("session/a", "idle");
  expect(calls).toBe(3);
});

test("queued PUTs cancel with their original claim and replacement claims cannot inherit deduplication", async () => {
  const started = Promise.withResolvers<void>();
  const release = Promise.withResolvers<Response>();
  const requests: CcrHttpRequest[] = [];
  const client = clientWith(async (request) => {
    requests.push(request);
    if (requests.length === 1) { started.resolve(); return release.promise; }
    return new Response(null, { status: 204 });
  });
  const detach = client.bindLease("session/a").subscribe!(() => {});
  const first = client.setStatus("session/a", "idle");
  const firstRejected = first.catch((error: unknown) => error);
  await started.promise;
  const queued = client.reportMetadata("session/a", { model: "old" });
  const queuedRejected = queued.catch((error: unknown) => error);
  detach();
  expect(String(await queuedRejected)).toContain("session stopped");
  const detachNew = client.bindLease("session/a").subscribe!(() => {});
  const replacement = client.setStatus("session/a", "idle");
  await new Promise<void>((resolve) => setImmediate(resolve));
  expect(requests).toHaveLength(1);
  release.resolve(new Response(null, { status: 204 }));
  expect(String(await firstRejected)).toContain("session stopped");
  await replacement;
  expect(requests).toHaveLength(2);
  detachNew();
  const detachAgain = client.bindLease("session/a").subscribe!(() => {});
  await client.setStatus("session/a", "idle");
  expect(requests).toHaveLength(3);
  detachAgain();
});

test("queued detached PUTs retain their original epoch", async () => {
  let epoch = 7;
  const started = Promise.withResolvers<void>();
  const release = Promise.withResolvers<Response>();
  let calls = 0;
  const client = new CcrClient({
    credentials: () => ({ token: "token", workerEpoch: epoch }), sessionIds: () => [],
    request: async () => { calls++; started.resolve(); return release.promise; },
  });
  const first = client.setStatus("a", "idle");
  await started.promise;
  const queued = client.reportMetadata("a", { model: "old" });
  const rejected = queued.catch((error: unknown) => error);
  epoch = 8;
  release.resolve(new Response(null, { status: 204 }));
  await first;
  expect(String(await rejected)).toContain("409");
  expect(calls).toBe(1);
});

test("GET worker and history reads stop at once when the code session is not found", async () => {
  let requests = 0;
  const client = clientWith(async () => { requests++; return new Response(null, { status: 404 }); });
  await expect(client.getWorkerState("session/a", 7)).rejects.toThrow("404");
  expect(requests).toBe(1);
  await expect(client.readInternalEvents("session/a")).rejects.toThrow("404");
  expect(requests).toBe(2);
});

test("history pages retry a failed body read but not malformed JSON", async () => {
  let requests = 0;
  const client = clientWith(async () => {
    requests++;
    if (requests === 1) return new Response(new ReadableStream({ start(controller) { controller.error(new TypeError("socket closed")); } }));
    return Response.json({ data: [{ event_id: "e", payload: { type: "system" } }] });
  });
  expect(await client.readInternalEvents("session/a")).toMatchObject([{ event_id: "e", payload: { type: "system" } }]);
  expect(requests).toBe(2);
  const malformed = clientWith(async () => new Response("{", { headers: { "Content-Type": "application/json" } }));
  await expect(malformed.readInternalEvents("session/a")).rejects.toThrow(SyntaxError);
});

test("private event reads honour the caller's cancellation", async () => {
  let requests = 0;
  const client = clientWith(async () => { requests++; return new Response(null, { status: 503 }); });
  client.config.sleep = (ms, signal) => new Promise((resolve, reject) => {
    const timer = setTimeout(resolve, ms * 1000);
    signal?.addEventListener("abort", () => { clearTimeout(timer); reject(signal.reason); }, { once: true });
  });
  const abort = new AbortController();
  const reading = client.privateEvents("session/a", { signal: abort.signal });
  await Bun.sleep(10);
  abort.abort(new Error("session closed"));
  await expect(reading).rejects.toThrow("session closed");
  expect(requests).toBe(1);
});
