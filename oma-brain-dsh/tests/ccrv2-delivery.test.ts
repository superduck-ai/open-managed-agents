import { expect, spyOn, test } from "bun:test";
import { CcrClient, type CcrHttpRequest } from "../src/ccrv2/client.ts";
import { CcrHeartbeat, type CcrLeaseFailure, type CcrLeaseResult } from "../src/ccrv2/heartbeat.ts";
import type { DeliveryUpdate } from "../src/ccrv2/wire.ts";

function clientWith(request: (request: CcrHttpRequest) => Promise<Response>) {
  return new CcrClient({
    request, credentials: () => ({ token: "token", workerEpoch: 7 }),
    sessionIds: () => ["session/a"], sleep: async () => {}, random: () => 0,
  });
}

type DeliveryBody = {
  session_id: string;
  worker_epoch: number;
  updates: { event_id: string; status: DeliveryUpdate["status"] }[];
};

test("delivery batches preserve all three statuses, FIFO order and the 64-update limit", async () => {
  const release = Promise.withResolvers<void>();
  const requests: CcrHttpRequest[] = [];
  let active = 0;
  let maxActive = 0;
  const client = clientWith(async (request) => {
    requests.push(request);
    maxActive = Math.max(maxActive, ++active);
    if (requests.length === 1) await release.promise;
    active--;
    return Response.json({ ok: true, applied: 0, ignored: (request.body as DeliveryBody).updates.length });
  });
  const statuses = ["received", "processing", "processed"] as const;
  const updates = Array.from({ length: 130 }, (_, index) => ({
    event_id: `event-${index}`, status: statuses[index % statuses.length]!,
  }));
  let confirmed = false;
  const completion = Promise.all(updates.map((update) =>
    client.reportDelivery("session/a", update.event_id, update.status))).then(() => { confirmed = true; });
  expect(requests).toHaveLength(1);
  expect(confirmed).toBe(false);
  release.resolve();
  await completion;
  expect(maxActive).toBe(1);
  const bodies = requests.map((request) => request.body as DeliveryBody);
  expect(bodies.map((body) => body.updates.length)).toEqual([1, 64, 64, 1]);
  expect(bodies.flatMap((body) => body.updates)).toEqual(updates);
  for (const request of requests) {
    expect(request.path).toBe("/v1/code/sessions/session%2Fa/worker/events/delivery");
    expect(request.method).toBe("POST");
    expect(request.timeout).toBe(10_000);
    expect(request.token).toBe("token");
    expect(request.body).toMatchObject({ session_id: "session/a", worker_epoch: 7 });
  }
});

test("delivery retries beyond three failures and regroups queued updates without confirming them early", async () => {
  const requests: CcrHttpRequest[] = [];
  const waits: number[] = [];
  const paused = Promise.withResolvers<void>();
  const resume = Promise.withResolvers<void>();
  let token = 0;
  const client = new CcrClient({
    credentials: () => ({ token: `token-${token}`, workerEpoch: 7 }), sessionIds: () => ["s"], random: () => 0,
    request: async (request) => {
      requests.push(request);
      token++;
      if (requests.length === 1) throw new Error("Lost reply after server accepted the receipt");
      if (requests.length < 5) return new Response(null, { status: requests.length === 3 ? 429 : 503,
        headers: { "Retry-After": "3" } });
      return Response.json({ ok: true, applied: 0, ignored: 1 });
    },
    sleep: async (ms) => { waits.push(ms); if (waits.length === 4) { paused.resolve(); await resume.promise; } },
  });
  let confirmed = false;
  const first = client.reportDelivery("s", "event", "received").then(() => { confirmed = true; });
  let nextConfirmed = false;
  const next = client.reportDelivery("s", "event", "processing").then(() => { nextConfirmed = true; });
  await paused.promise;
  expect(confirmed).toBe(false);
  expect(nextConfirmed).toBe(false);
  expect(requests).toHaveLength(4);
  resume.resolve();
  await Promise.all([first, next]);
  expect(waits).toEqual([500, 1000, 3000, 4000]);
  expect(requests).toHaveLength(5);
  expect((requests[0]!.body as DeliveryBody).updates).toEqual([{ event_id: "event", status: "received" }]);
  for (const request of requests.slice(1)) {
    expect((request.body as DeliveryBody).worker_epoch).toBe(7);
    expect((request.body as DeliveryBody).updates).toEqual([
      { event_id: "event", status: "received" }, { event_id: "event", status: "processing" },
    ]);
  }
  expect(requests.slice(0, 5).map((request) => request.token)).toEqual(["token-0", "token-1", "token-2", "token-3", "token-4"]);
  expect(nextConfirmed).toBe(true);
});

test("regrouped retries preserve FIFO, batch limits and backpressure and reset backoff after success", async () => {
  const firstResponse = Promise.withResolvers<void>();
  const retryStarted = Promise.withResolvers<void>();
  const retryResponse = Promise.withResolvers<void>();
  const owner = new AbortController();
  const requests: DeliveryBody[] = [];
  const accepted: DeliveryBody["updates"] = [];
  const waits: number[] = [];
  let active = 0;
  let maxActive = 0;
  let confirmed = 0;
  const client = clientWith(async (request) => {
    const body = request.body as DeliveryBody;
    requests.push(body);
    const attempt = requests.length;
    maxActive = Math.max(maxActive, ++active);
    try {
      if (attempt === 1) await firstResponse.promise;
      if (attempt === 2) { retryStarted.resolve(); await retryResponse.promise; }
      if (attempt === 1 || attempt === 3) return new Response(null, { status: 503 });
      accepted.push(...body.updates);
      return new Response(null, { status: 204 });
    } finally { active--; }
  });
  client.config.sleep = async (ms) => { waits.push(ms); };
  const statuses = ["received", "processing", "processed"] as const;
  const updates = Array.from({ length: 130 }, (_, index) => ({
    event_id: `event-${index}`, status: statuses[index % statuses.length]!,
  }));
  const completion = Promise.allSettled(updates.map((update) =>
    client.reportDelivery("s", update.event_id, update.status, owner.signal).then(() => { confirmed++; })));
  try {
    expect(requests).toHaveLength(1);
    firstResponse.resolve();
    await retryStarted.promise;
    expect(requests).toHaveLength(2);
    expect(requests[1]!.updates).toEqual(updates.slice(0, 64));
    expect(confirmed).toBe(0);
    retryResponse.resolve();
    expect((await completion).every((result) => result.status === "fulfilled")).toBe(true);
    expect(confirmed).toBe(updates.length);
    expect(accepted).toEqual(updates);
    expect(requests.every((body) => body.updates.length <= 64)).toBe(true);
    expect(maxActive).toBe(1);
    expect(waits).toEqual([500, 500]);
  } finally {
    owner.abort(); firstResponse.resolve(); retryResponse.resolve(); await completion;
  }
});

test("delivery accepts any successful HTTP response, including empty and ignored responses", async () => {
  const responses = [new Response(null, { status: 204 }), new Response(null, { status: 200 }),
    Response.json({ ok: true, applied: 0, ignored: 1 }), Response.json({ ok: true, applied: 1, ignored: 0 })];
  const client = clientWith(async () => responses.shift()!);
  for (let index = 0; index < 4; index++) await client.acknowledge("session/a", "same-event");
  expect(responses).toEqual([]);
});

test("successful delivery cleanup failures neither retry nor log a failed request", async () => {
  const warn = spyOn(console, "warn").mockImplementation(() => {});
  try {
    for (const status of [200, 202]) {
      let requests = 0;
      const waits: number[] = [];
      const client = clientWith(async () => {
        requests++;
        return requests === 1 ? new Response(new ReadableStream({
          cancel() { throw new Error("response cleanup failed"); },
        }), { status }) : new Response(null, { status: 204 });
      });
      client.config.sleep = async (ms) => { waits.push(ms); };
      await client.acknowledge("s", "first");
      expect(requests).toBe(1);
      expect(waits).toEqual([]);
      await client.acknowledge("s", "next");
      expect(requests).toBe(2);
    }
    expect(warn).not.toHaveBeenCalled();
  } finally { warn.mockRestore(); }
});

test("cancellation during successful response cleanup still rejects in-flight and queued delivery", async () => {
  const cleaning = Promise.withResolvers<void>();
  const cleanup = Promise.withResolvers<void>();
  const owner = new AbortController();
  const stopped = new Error("Session closed during cleanup");
  let requests = 0;
  const client = clientWith(async () => {
    requests++;
    return new Response(new ReadableStream({
      cancel() { cleaning.resolve(); return cleanup.promise; },
    }), { status: 200 });
  });
  const completion = Promise.allSettled([
    client.acknowledge("s", "first", owner.signal), client.acknowledge("s", "next", owner.signal),
  ]);
  try {
    await cleaning.promise;
    owner.abort(stopped);
    cleanup.reject(new Error("response cleanup failed"));
    expect(await completion).toEqual([
      { status: "rejected", reason: stopped }, { status: "rejected", reason: stopped },
    ]);
    expect(requests).toBe(1);
    await expect(client.acknowledge("s", "later")).rejects.toThrow(stopped.message);
  } finally { owner.abort(stopped); cleanup.resolve(); await completion; }
});

test("standalone delivery stops at ten auth failures and shares resets with successful worker writes", async () => {
  for (const status of [401, 403]) {
    let attempts = 0;
    let reset = false;
    const client = clientWith(async (request) => {
      if (request.path.endsWith("/heartbeat")) return new Response(null, { status: 204 });
      attempts++;
      return new Response(null, { status });
    });
    client.config.sleep = async () => {
      if (!reset && attempts === 5) { reset = true; await client.heartbeat("s"); }
    };
    const results = await Promise.allSettled(Array.from({ length: 70 }, (_, index) =>
      client.reportDelivery("s", `event-${index}`, "received")));
    expect(results.every((result) => result.status === "rejected")).toBe(true);
    expect(attempts).toBe(15);
    expect(results[0]).toMatchObject({ status: "rejected", reason: { status, authExhausted: true } });
  }
});

test("standalone auth counters isolate sessions and replacement epochs", async () => {
  let epoch = 7;
  const attempts = new Map<string, number>();
  const client = clientWith(async (request) => {
    const key = `${request.path}:${(request.body as DeliveryBody).worker_epoch}`;
    attempts.set(key, (attempts.get(key) ?? 0) + 1);
    return new Response(null, { status: 401 });
  });
  client.config.credentials = () => ({ token: "token", workerEpoch: epoch });
  await Promise.allSettled([client.reportDelivery("a", "event", "received"), client.reportDelivery("b", "event", "received")]);
  epoch++;
  await expect(client.reportDelivery("a", "event", "received")).rejects.toThrow("401");
  expect([...attempts.values()]).toEqual([10, 10, 10]);
});

test("delivery reports sanitized HTTP and transport failures without changing retries", async () => {
  const logs: string[] = [];
  const warn = spyOn(console, "warn").mockImplementation((value) => { logs.push(String(value)); });
  let attempts = 0;
  const client = clientWith(async () => {
    attempts++;
    if (attempts === 1) return new Response(null, { status: 503 });
    if (attempts === 2) throw Object.assign(new Error("secret-token in exception"), { code: "ECONNRESET" });
    return new Response(null, { status: 204 });
  });
  try {
    await client.reportDelivery("private-session", "private-event", "received");
    expect(attempts).toBe(3);
    expect(logs.map((value) => JSON.parse(value))).toEqual([
      { event: "cli_worker_request_failed", method: "post", path: "/worker/events/delivery", worker_epoch: 7,
        batch_size: 1, status: 503, failure_reason: "server_error" },
      { event: "cli_worker_request_error", method: "post", path: "/worker/events/delivery", worker_epoch: 7,
        batch_size: 1, failure_reason: "transport_error", error_code: "ECONNRESET" },
    ]);
    for (const secret of ["secret-token", "private-session", "private-event"])
      expect(logs.join("\n")).not.toContain(secret);
    warn.mockImplementation(() => { throw new Error("log sink unavailable"); });
    attempts = 0;
    await client.reportDelivery("s", "event", "received");
    expect(attempts).toBe(3);
  } finally { warn.mockRestore(); }
});

test("terminal delivery failures stop the owning claim and reject its queued confirmations", async () => {
  for (const status of [409, 410, 401]) {
    let requests = 0;
    const client = clientWith(async () => { requests++; return new Response(null, { status }); });
    const owner = new AbortController();
    const losses: CcrLeaseFailure[] = [];
    const heartbeat = new CcrHeartbeat({
      check: client.bindLease("session/a"),
      onLeaseLost: (failure) => { losses.push(failure); owner.abort(); },
    }, owner.signal);
    try {
      const results = await Promise.allSettled(Array.from({ length: 80 }, (_, index) =>
        client.reportDelivery("session/a", `event-${index}`, "received")));
      expect(results.every((result) => result.status === "rejected")).toBe(true);
      expect(requests).toBe(status === 401 ? 10 : 1);
      expect(losses).toHaveLength(1);
      expect(losses[0]!.httpStatus).toBe(status);
      expect(owner.signal.aborted).toBe(true);
    } finally { await heartbeat.close(); }
  }
});

test("delivery cancellation releases pending updates, backpressure waiters and retry sleeps", async () => {
  const sleeping = Promise.withResolvers<void>();
  const owner = new AbortController();
  let requests = 0;
  const client = new CcrClient({
    credentials: () => ({ token: "token", workerEpoch: 7 }), sessionIds: () => ["s"],
    request: async () => { requests++; return new Response(null, { status: 503 }); },
    sleep: async () => { sleeping.resolve(); await new Promise(() => {}); },
  });
  const pending = Promise.allSettled(Array.from({ length: 150 }, (_, index) =>
    client.reportDelivery("s", `event-${index}`, "received", owner.signal)));
  await sleeping.promise;
  owner.abort(new Error("Session closed"));
  const results = await pending;
  expect(requests).toBe(1);
  expect(results.every((result) => result.status === "rejected")).toBe(true);
});

test("delivery cannot retry or notify a replacement claim with old queued work", async () => {
  let epoch = 7;
  const oldResponse = Promise.withResolvers<Response>();
  const requests: CcrHttpRequest[] = [];
  const client = new CcrClient({
    credentials: () => ({ token: `token-${epoch}`, workerEpoch: epoch }), sessionIds: () => ["s"],
    request: async (request) => {
      requests.push(request);
      return requests.length === 1 ? oldResponse.promise : new Response(null, { status: 204 });
    },
  });
  const old: CcrLeaseResult[] = [];
  const current: CcrLeaseResult[] = [];
  const detach = client.bindLease("s").subscribe!((result) => { old.push(result); });
  const pending = Promise.allSettled([
    client.reportDelivery("s", "old-1", "received"), client.reportDelivery("s", "old-2", "processed"),
  ]);
  detach();
  epoch = 8;
  const detachCurrent = client.bindLease("s").subscribe!((result) => { current.push(result); });
  try {
    await client.reportDelivery("s", "new", "received");
    oldResponse.resolve(new Response(null, { status: 401 }));
    expect((await pending).every((result) => result.status === "rejected")).toBe(true);
    await new Promise<void>((resolve) => setImmediate(resolve));
    expect(requests).toHaveLength(2);
    expect(requests[1]!.token).toBe("token-8");
    expect((requests[1]!.body as DeliveryBody).updates).toEqual([{ event_id: "new", status: "received" }]);
    expect(old).toEqual([]);
    expect(current).toEqual([{ status: "confirmed" }]);
  } finally { detachCurrent(); }
});

test("one session's delivery backlog does not block another session", async () => {
  const release = Promise.withResolvers<void>();
  const client = clientWith(async ({ path }) => {
    if (path.includes("/a/")) await release.promise;
    return new Response(null, { status: 204 });
  });
  let firstConfirmed = false;
  const first = client.reportDelivery("a", "event", "received").then(() => { firstConfirmed = true; });
  await client.reportDelivery("b", "event", "received");
  expect(firstConfirmed).toBe(false);
  release.resolve();
  await first;
});

test("invalid delivery IDs and statuses fail before entering the retry queue", async () => {
  let requests = 0;
  const client = clientWith(async () => { requests++; return new Response(null, { status: 204 }); });
  for (const eventId of ["", "  "]) await expect(client.reportDelivery("s", eventId, "received")).rejects.toThrow("event ID");
  await expect(client.reportDelivery("s", "event", "sent" as DeliveryUpdate["status"])).rejects.toThrow("status");
  expect(requests).toBe(0);
});
