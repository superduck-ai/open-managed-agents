import { expect, test } from "bun:test";
import { CcrClient } from "../src/ccrv2/client.ts";
import {
  CcrHeartbeat, type CcrHeartbeatClock, type CcrHeartbeatHealth, type CcrLeaseFailure, type CcrLeaseResult,
} from "../src/ccrv2/heartbeat.ts";

class ManualClock implements CcrHeartbeatClock {
  readonly pending = new Set<{ callback: () => void; delayMs: number }>();
  randomValue = 0.5;
  time = 0;
  now() { return this.time; }
  random() { return this.randomValue; }
  schedule(callback: () => void, delayMs: number) {
    const task = { callback, delayMs };
    this.pending.add(task);
    return () => { this.pending.delete(task); };
  }
  fire() {
    const task = [...this.pending][0];
    if (!task) throw new Error("No heartbeat scheduled");
    this.pending.delete(task);
    this.time += task.delayMs;
    task.callback();
    return task.delayMs;
  }
}

const settle = () => new Promise<void>((resolve) => setImmediate(resolve));

test("heartbeat health reports first failure, one-minute reminders and recovery only after a successful beat", async () => {
  const clock = new ManualClock();
  const health: CcrHeartbeatHealth[] = [];
  let result: CcrLeaseResult = { status: "unavailable", httpStatus: 503, reason: "server_error", workerEpoch: 7 };
  const heartbeat = new CcrHeartbeat({
    check: async () => result, onLeaseLost: () => { throw new Error("Unexpected loss"); },
    onHealth: (event) => { health.push(event); },
  }, new AbortController().signal, clock);
  try {
    for (let i = 0; i < 4; i++) { clock.fire(); await settle(); }
    expect(health).toEqual([
      { type: "failure", httpStatus: 503, reason: "server_error", workerEpoch: 7, consecutiveFailures: 1, durationMs: 0 },
      { type: "failure", httpStatus: 503, reason: "server_error", workerEpoch: 7, consecutiveFailures: 4, durationMs: 60000 },
    ]);
    result = { status: "confirmed" };
    clock.fire(); await settle();
    expect(health.at(-1)).toEqual({ type: "recovered", workerEpoch: 7, consecutiveFailures: 4, durationMs: 80000 });
    clock.fire(); await settle();
    expect(health).toHaveLength(3);
    result = { status: "unavailable", reason: "timeout", workerEpoch: 7 };
    clock.fire(); await settle();
    expect(health.at(-1)).toEqual({
      type: "failure", httpStatus: undefined, reason: "timeout", workerEpoch: 7, consecutiveFailures: 1, durationMs: 0,
    });
  } finally { await heartbeat.close(); }
});

test("health logging failures do not affect scheduling and cancellation emits no recovery or failure", async () => {
  const clock = new ManualClock();
  const pending = Promise.withResolvers<CcrLeaseResult>();
  let calls = 0;
  let reports = 0;
  const heartbeat = new CcrHeartbeat({
    check: async () => ++calls === 1 ? { status: "unavailable" } : calls === 2 ? { status: "confirmed" } : pending.promise,
    onLeaseLost: () => { throw new Error("Unexpected loss"); },
    onHealth: () => { reports++; throw new Error("Broken log sink"); },
  }, new AbortController().signal, clock);
  clock.fire(); await settle();
  clock.fire(); await settle();
  clock.fire(); await settle();
  const closing = heartbeat.close();
  pending.resolve({ status: "unavailable" });
  await closing;
  expect(reports).toBe(2);
  expect(calls).toBe(3);
  expect(clock.pending.size).toBe(0);
});

test("default jitter spans 18 to 22 seconds and temporary failures keep scheduling", async () => {
  const clock = new ManualClock();
  clock.randomValue = 0;
  let httpStatus = 503;
  const lost: CcrLeaseFailure[] = [];
  let calls = 0;
  const client = new CcrClient({
    credentials: () => ({ token: "token", workerEpoch: 7 }),
    sessionIds: () => ["a"],
    request: async () => {
      calls++;
      if (httpStatus === 0) throw new Error("Connection lost");
      return new Response(null, { status: httpStatus, headers: { "Retry-After": "120" } });
    },
  });
  const heartbeat = new CcrHeartbeat({
    check: client.bindLease("a"), onLeaseLost: (failure) => { lost.push(failure); },
  }, new AbortController().signal, clock);
  try {
    clock.randomValue = 1;
    for (const [i, status] of Array.from({ length: 4 }, () => [503, 500, 408, 429, 400, 404, 0]).flat().entries()) {
      httpStatus = status;
      expect(clock.fire()).toBe(i === 0 ? 18_000 : 22_000);
      await settle();
      expect(lost).toEqual([]);
      expect(clock.pending.size).toBe(1);
    }
    expect(calls).toBe(28);
    httpStatus = 410;
    clock.fire();
    await settle();
    expect(lost).toHaveLength(1);
    expect(lost[0]?.reason).toBe("lease_expired");
    expect(clock.pending.size).toBe(0);
  } finally {
    await heartbeat.close();
  }
});

test("auth failures share all writes, reset on successful writes and survive other failures and GETs", async () => {
  const clock = new ManualClock();
  const lost: CcrLeaseFailure[] = [];
  let status = 401;
  const wait = Promise.withResolvers<void>();
  const client = new CcrClient({
    credentials: () => ({ token: "token-without-expiry", workerEpoch: 7 }),
    sessionIds: () => ["a"], sleep: () => wait.promise,
    request: async (request) => request.method === "GET"
      ? Response.json({ data: [], next_cursor: null }) : new Response(null, { status }),
  });
  const heartbeat = new CcrHeartbeat({
    check: client.bindLease("a"), onLeaseLost: (failure) => { lost.push(failure); },
  }, new AbortController().signal, clock);
  const beats = async (count: number, httpStatus = 401) => {
    status = httpStatus;
    for (let i = 0; i < count; i++) { clock.fire(); await settle(); }
  };
  try {
    // Every kind of successful POST/PUT clears the same auth counter.
    for (const write of [
      () => client.persist("a", "dsh_log", {}),
      () => client.events("a", [{ type: "agent.message" }]),
      () => client.setStatus("a", "idle"),
      () => client.acknowledge("a", "input").catch(() => {}),
      () => client.heartbeat("a"),
      () => client.checkLease("a"),
    ]) {
      await beats(9);
      expect(lost).toEqual([]);
      status = 204;
      // A successful delivery response resets authentication even without a body.
      await write();
    }
    await beats(7, 401);
    status = 403;
    const writing = client.setStatus("a", "running");
    writing.catch(() => {});
    await settle();
    await beats(3, 503);
    await client.privateEvents("a");
    await beats(1, 403);
    expect(lost).toEqual([]);
    await beats(1, 401);
    await expect(writing).rejects.toThrow("CCRv2 session stopped");
    expect(lost).toEqual([{
      status: "unavailable", httpStatus: 401, reason: "authentication_rejected",
      workerEpoch: 7, consecutiveFailures: 10,
    }]);
    expect(clock.pending.size).toBe(0);
  } finally {
    await heartbeat.close();
  }
});

test("manual and scheduled heartbeats count each auth failure once toward the same budget", async () => {
  for (const method of ["heartbeat", "checkLease"] as const) {
    const clock = new ManualClock();
    const lost: CcrLeaseFailure[] = [];
    let calls = 0;
    const client = new CcrClient({
      credentials: () => ({ token: "token", workerEpoch: 7 }), sessionIds: () => ["a"],
      request: async () => { calls++; return new Response(null, { status: calls % 2 ? 401 : 403 }); },
    });
    const heartbeat = new CcrHeartbeat({
      check: client.bindLease("a"), onLeaseLost: (failure) => { lost.push(failure); },
    }, new AbortController().signal, clock);
    const manual = async () => {
      if (method === "heartbeat") await expect(client.heartbeat("a")).rejects.toBeInstanceOf(Error);
      else expect(await client.checkLease("a")).toBe("unavailable");
    };
    try {
      for (let i = 0; i < 9; i++) {
        if (i % 2) await manual();
        else { clock.fire(); await settle(); }
        expect(lost).toEqual([]);
        expect(calls).toBe(i + 1);
      }
      await manual();
      expect(lost).toEqual([{
        status: "unavailable", httpStatus: 403, reason: "authentication_rejected",
        workerEpoch: 7, consecutiveFailures: 10,
      }]);
      expect(clock.pending.size).toBe(0);
      await manual();
      expect(lost).toHaveLength(1);
    } finally { await heartbeat.close(); }
  }
});

test("successful heartbeats reset auth failures and the tenth failing write stops immediately", async () => {
  const clock = new ManualClock();
  const lost: CcrLeaseFailure[] = [];
  let status = 403;
  let writes = 0;
  const wait = Promise.withResolvers<void>();
  const waiting = Promise.withResolvers<void>();
  const client = new CcrClient({
    credentials: () => ({ token: "token", workerEpoch: 7 }), sessionIds: () => ["a"],
    request: async (request) => {
      if (!request.path.endsWith("/heartbeat")) writes++;
      return new Response(null, { status });
    },
    sleep: async () => { if (writes === 9) { waiting.resolve(); await wait.promise; } },
  });
  const heartbeat = new CcrHeartbeat({
    check: client.bindLease("a"), onLeaseLost: (failure) => { lost.push(failure); },
  }, new AbortController().signal, clock);
  try {
    const writing = client.setStatus("a", "idle");
    writing.catch(() => {});
    await waiting.promise;
    expect(lost).toEqual([]);
    status = 204;
    clock.fire();
    await settle();
    status = 401;
    wait.resolve();
    await expect(writing).rejects.toThrow("401");
    expect(writes).toBe(19);
    expect(lost).toHaveLength(1);
    expect(lost[0]?.consecutiveFailures).toBe(10);
    expect(clock.pending.size).toBe(0);
  } finally {
    await heartbeat.close();
  }
});

test("write observations are detached on close and cannot affect another session or replacement claim", async () => {
  for (const operation of ["status", "heartbeat", "checkLease"] as const) for (const lateStatus of [204, 401, 409, 410]) {
    const oldResponse = Promise.withResolvers<Response>();
    let status = 401;
    let delayed = false;
    const lost: CcrLeaseFailure[] = [];
    const client = new CcrClient({
      credentials: () => ({ token: "token", workerEpoch: 7 }), sessionIds: () => ["a", "b"],
      request: async () => delayed ? oldResponse.promise : new Response(null, { status }),
    });
    const old = new CcrHeartbeat({
      check: client.bindLease("a"), onLeaseLost: () => { throw new Error("Old claim must be detached"); },
    }, new AbortController().signal, new ManualClock());
    delayed = true;
    const writing = (operation === "status" ? client.setStatus("a", "idle") : client[operation]("a")).catch(() => {});
    await old.close();
    delayed = false;
    const clock = new ManualClock();
    const replacement = new CcrHeartbeat({
      check: client.bindLease("a"), maxAuthFailures: 2,
      onLeaseLost: (failure) => { lost.push(failure); },
    }, new AbortController().signal, clock);
    try {
      clock.fire();
      await settle();
      // Neither another session nor a late old generation can
      // reset or increment this generation's single authentication failure.
      status = 204;
      await client.setStatus("b", "idle");
      await client.heartbeat("b");
      await client.checkLease("b");
      oldResponse.resolve(new Response(null, { status: lateStatus }));
      await writing;
      expect(lost).toEqual([]);
      status = 403;
      clock.fire();
      await settle();
      expect(lost).toHaveLength(1);
      expect(lost[0]?.consecutiveFailures).toBe(2);
    } finally {
      oldResponse.resolve(new Response(null, { status: lateStatus }));
      await Promise.all([writing, replacement.close()]);
    }
  }
});

test("epoch and lease rejection on other writes stop the heartbeat without altering HTTP errors", async () => {
  for (const status of [409, 410]) {
    const clock = new ManualClock();
    const lost: CcrLeaseFailure[] = [];
    const client = new CcrClient({
      credentials: () => ({ token: "token", workerEpoch: 7 }), sessionIds: () => ["a"],
      request: async () => new Response(null, { status }),
    });
    const heartbeat = new CcrHeartbeat({
      check: client.bindLease("a"), onLeaseLost: (failure) => { lost.push(failure); },
    }, new AbortController().signal, clock);
    try {
      await expect(client.persist("a", "dsh_log", {})).rejects.toThrow(String(status));
      expect(lost).toHaveLength(1);
      expect(lost[0]?.httpStatus).toBe(status);
      expect(clock.pending.size).toBe(0);
    } finally {
      await heartbeat.close();
    }
  }
});

test("manual heartbeats report epoch, lease and expired-token rejection immediately", async () => {
  const expired = `header.${Buffer.from(JSON.stringify({ exp: 1 })).toString("base64url")}.signature`;
  for (const method of ["heartbeat", "checkLease"] as const) for (const status of [401, 403, 409, 410]) {
    const clock = new ManualClock();
    const lost: CcrLeaseFailure[] = [];
    const client = new CcrClient({
      credentials: () => ({ token: expired, workerEpoch: 7 }), sessionIds: () => ["a"],
      request: async () => new Response(null, { status }),
    });
    const heartbeat = new CcrHeartbeat({
      check: client.bindLease("a"), onLeaseLost: (failure) => { lost.push(failure); },
    }, new AbortController().signal, clock);
    try {
      if (method === "heartbeat") await expect(client.heartbeat("a")).rejects.toThrow(String(status));
      else expect(await client.checkLease("a")).toBe("rejected");
      expect(lost).toEqual([{
        status: "rejected", httpStatus: status, workerEpoch: 7, consecutiveFailures: 1,
        reason: status === 409 ? "epoch_mismatch" : status === 410 ? "lease_expired" : "token_expired",
      }]);
      expect(clock.pending.size).toBe(0);
    } finally { await heartbeat.close(); }
  }
});

test("a cancelled manual heartbeat cannot reset or increment the active auth counter", async () => {
  for (const lateStatus of [204, 401, 409, 410]) {
    const clock = new ManualClock();
    const lost: CcrLeaseFailure[] = [];
    const response = Promise.withResolvers<Response>();
    let delayed = false;
    const client = new CcrClient({
      credentials: () => ({ token: "token", workerEpoch: 7 }), sessionIds: () => ["a"],
      request: async () => delayed ? response.promise : new Response(null, { status: 401 }),
    });
    const heartbeat = new CcrHeartbeat({
      check: client.bindLease("a"), maxAuthFailures: 2,
      onLeaseLost: (failure) => { lost.push(failure); },
    }, new AbortController().signal, clock);
    try {
      clock.fire(); await settle();
      const abort = new AbortController();
      delayed = true;
      const manual = client.heartbeat("a", abort.signal);
      abort.abort(new Error("Manual check cancelled"));
      response.resolve(new Response(null, { status: lateStatus }));
      await expect(manual).rejects.toThrow("Manual check cancelled");
      expect(lost).toEqual([]);
      delayed = false;
      clock.fire(); await settle();
      expect(lost).toHaveLength(1);
      expect(lost[0]?.consecutiveFailures).toBe(2);
    } finally { await heartbeat.close(); }
  }
});

test("an explicit transient failure limit keeps the default cadence and resets on heartbeat success", async () => {
  const clock = new ManualClock();
  const lost: CcrLeaseFailure[] = [];
  const results: CcrLeaseResult[] = [
    { status: "unavailable", httpStatus: 503 },
    { status: "unavailable" },
    { status: "confirmed" },
    { status: "unavailable", httpStatus: 408 },
    { status: "unavailable", httpStatus: 429 },
    { status: "unavailable", httpStatus: 503 },
  ];
  const heartbeat = new CcrHeartbeat({
    maxFailures: 3,
    check: async (_signal, timeoutMs) => {
      expect(timeoutMs).toBe(5000);
      return results.shift()!;
    },
    onLeaseLost: (failure) => { lost.push(failure); },
  }, new AbortController().signal, clock);
  try {
    for (let i = 0; i < 6; i++) {
      expect(clock.fire()).toBe(20000);
      await settle();
      expect(lost).toHaveLength(i === 5 ? 1 : 0);
    }
    expect(lost).toEqual([{ status: "unavailable", httpStatus: 503, consecutiveFailures: 3 }]);
    expect(clock.pending.size).toBe(0);
  } finally {
    await heartbeat.close();
  }
});

test("epoch and expired lease rejection notify the host immediately with the HTTP status", async () => {
  for (const httpStatus of [409, 410]) {
    const clock = new ManualClock();
    const lost: CcrLeaseFailure[] = [];
    const heartbeat = new CcrHeartbeat({
      check: async () => ({ status: "rejected", httpStatus }),
      onLeaseLost: (failure) => { lost.push(failure); },
    }, new AbortController().signal, clock);
    clock.fire();
    await settle();
    expect(lost).toEqual([{ status: "rejected", httpStatus, consecutiveFailures: 1 }]);
    expect(clock.pending.size).toBe(0);
    await heartbeat.close();
  }
});

test("a slow heartbeat cannot overlap itself or delay another session", async () => {
  const aClock = new ManualClock();
  const bClock = new ManualClock();
  const pending = Promise.withResolvers<CcrLeaseResult>();
  let aCalls = 0;
  let bCalls = 0;
  const a = new CcrHeartbeat({
    check: () => { aCalls++; return pending.promise; },
    onLeaseLost: () => { throw new Error("Unexpected loss"); },
  }, new AbortController().signal, aClock);
  const b = new CcrHeartbeat({
    check: async () => { bCalls++; return { status: "confirmed" }; },
    onLeaseLost: () => { throw new Error("Unexpected loss"); },
  }, new AbortController().signal, bClock);
  try {
    for (let i = 0; i < 3; i++) {
      aClock.fire();
      bClock.fire();
      await settle();
    }
    expect(aCalls).toBe(1);
    expect(bCalls).toBe(3);
    pending.resolve({ status: "confirmed" });
    await settle();
    aClock.fire();
    await settle();
    expect(aCalls).toBe(2);
  } finally {
    pending.resolve({ status: "confirmed" });
    await Promise.all([a.close(), b.close()]);
  }
});

test("closing cancels the request, waits for it and isolates late results from a new claim", async () => {
  for (const lateResult of [
    { status: "confirmed" },
    { status: "rejected", httpStatus: 409 },
    { status: "unavailable" },
  ] satisfies CcrLeaseResult[]) {
    const oldClock = new ManualClock();
    const nextClock = new ManualClock();
    const parent = new AbortController();
    const pending = Promise.withResolvers<CcrLeaseResult>();
    const oldLost: CcrLeaseFailure[] = [];
    const nextLost: CcrLeaseFailure[] = [];
    let oldCalls = 0;
    let requestSignal: AbortSignal | undefined;
    const old = new CcrHeartbeat({
      maxFailures: 3,
      check: async (signal) => {
        requestSignal = signal;
        return ++oldCalls < 3 ? { status: "unavailable" } : pending.promise;
      },
      onLeaseLost: (failure) => { oldLost.push(failure); },
    }, parent.signal, oldClock);
    for (let i = 0; i < 3; i++) { oldClock.fire(); await settle(); }
    parent.abort();
    let closed = false;
    const closing = old.close().then(() => { closed = true; });
    await settle();
    expect(requestSignal?.aborted).toBe(true);
    expect(oldClock.pending.size).toBe(0);
    expect(closed).toBe(false);

    const next = new CcrHeartbeat({
      maxFailures: 3,
      check: async () => ({ status: "unavailable" }),
      onLeaseLost: (failure) => { nextLost.push(failure); },
    }, new AbortController().signal, nextClock);
    try {
      nextClock.fire();
      await settle();
      expect(nextLost).toEqual([]);
      pending.resolve(lateResult);
      await closing;
      expect(oldLost).toEqual([]);
      nextClock.fire();
      await settle();
      expect(nextLost).toEqual([]);
      nextClock.fire();
      await settle();
      expect(nextLost).toEqual([{ status: "unavailable", consecutiveFailures: 3 }]);
    } finally {
      pending.resolve(lateResult);
      await Promise.all([closing, next.close()]);
    }
  }
});

test("an aborted session never starts a heartbeat and closing before the first beat removes the timer", async () => {
  const clock = new ManualClock();
  const parent = new AbortController();
  parent.abort();
  let checks = 0;
  const config = {
    check: async (): Promise<CcrLeaseResult> => { checks++; return { status: "confirmed" }; },
    onLeaseLost: () => { throw new Error("Unexpected loss"); },
  };
  const stopped = new CcrHeartbeat(config, parent.signal, clock);
  expect(clock.pending.size).toBe(0);
  await stopped.close();
  const active = new CcrHeartbeat(config, new AbortController().signal, clock);
  expect(clock.pending.size).toBe(1);
  await active.close();
  expect(clock.pending.size).toBe(0);
  expect(checks).toBe(0);
});

test("configured cadence and jitter remain independent of request duration", async () => {
  const clock = new ManualClock();
  clock.randomValue = 0;
  const deadlines: number[] = [];
  const heartbeat = new CcrHeartbeat({
    intervalMs: 1000, timeoutMs: 200, jitterFraction: 0.1,
    check: async (_signal, timeoutMs) => { deadlines.push(timeoutMs); return { status: "confirmed" }; },
    onLeaseLost: () => { throw new Error("Unexpected loss"); },
  }, new AbortController().signal, clock);
  try {
    clock.randomValue = 1;
    expect(clock.fire()).toBe(900);
    await settle();
    expect(clock.fire()).toBe(1100);
    await settle();
    expect(deadlines).toEqual([200, 200]);
  } finally {
    await heartbeat.close();
  }
});
