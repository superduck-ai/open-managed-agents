import { expect, spyOn, test } from "bun:test";
import { CcrClient, CcrHttpError, type CcrHttpRequest } from "../src/ccrv2/client.ts";
import { CcrHeartbeat, type CcrLeaseFailure } from "../src/ccrv2/heartbeat.ts";

const settle = () => new Promise<void>((resolve) => setImmediate(resolve));
const expiredToken = () => `header.${Buffer.from(JSON.stringify({ exp: 1 })).toString("base64url")}.signature`;

function fixture(request: (options: CcrHttpRequest) => Promise<Response>) {
  const credentials = { token: "token", workerEpoch: 7 };
  const client = new CcrClient({
    request, credentials: () => credentials, sessionIds: () => ["s"], sleep: async () => {}, random: () => 0,
  });
  return { client, credentials };
}

test("empty credentials wait beyond the auth budget, then send the original queued receipts", async () => {
  const requests: CcrHttpRequest[] = [];
  const waiting = Promise.withResolvers<void>();
  const resume = Promise.withResolvers<void>();
  const { client, credentials } = fixture(async (request) => {
    requests.push(request); return new Response(null, { status: 204 });
  });
  credentials.token = "";
  const waits: number[] = [];
  client.config.sleep = async (ms) => {
    waits.push(ms);
    if (waits.length === 12) { waiting.resolve(); await resume.promise; }
  };
  let confirmed = false;
  const delivery = Promise.all([
    client.reportDelivery("s", "event", "received"), client.reportDelivery("s", "event", "processing"),
  ]).then(() => { confirmed = true; });
  await waiting.promise;
  expect(requests).toHaveLength(0);
  expect(confirmed).toBe(false);
  expect(waits).toEqual([500, 1000, 2000, 4000, 8000, 16000, 30000, 30000, 30000, 30000, 30000, 30000]);
  expect(await client.checkLease("s")).toBe("unavailable");
  expect(requests).toHaveLength(0);
  credentials.token = "refreshed";
  resume.resolve(); await delivery;
  expect(requests.map((request) => request.token)).toEqual(["refreshed"]);
  expect(requests.map((request) => request.body)).toEqual([
    { session_id: "s", worker_epoch: 7, updates: [
      { event_id: "event", status: "received" }, { event_id: "event", status: "processing" },
    ] },
  ]);
});

test("missing credentials neither consume nor reset bound or standalone authentication failures", async () => {
  for (const bound of [false, true]) {
    let attempts = 0;
    let waits = 0;
    const { client, credentials } = fixture(async () => {
      attempts++;
      if (attempts === 8) credentials.token = "";
      return new Response(null, { status: 401 });
    });
    client.config.sleep = async () => { if (++waits === 20) credentials.token = "restored"; };
    const owner = new AbortController();
    const losses: CcrLeaseFailure[] = [];
    const heartbeat = bound ? new CcrHeartbeat({ check: client.bindLease("s"), onLeaseLost: (failure) => {
      losses.push(failure); owner.abort();
    } }, owner.signal) : undefined;
    try {
      await expect(client.reportDelivery("s", "event", "received", owner.signal)).rejects.toThrow();
      expect(attempts).toBe(10);
      expect(waits).toBe(21);
      expect(losses).toHaveLength(bound ? 1 : 0);
    } finally { await heartbeat?.close(); }
  }
});

test("empty-credential waits cancel with backpressure and never log a request failure", async () => {
  let attempts = 0;
  const { client, credentials } = fixture(async () => { attempts++; return new Response(null, { status: 204 }); });
  credentials.token = "";
  const waiting = Promise.withResolvers<void>();
  client.config.sleep = async () => { waiting.resolve(); await new Promise<void>(() => {}); };
  const owner = new AbortController();
  const warn = spyOn(console, "warn").mockImplementation(() => {});
  try {
    const pending = Promise.allSettled(Array.from({ length: 150 }, (_, index) =>
      client.reportDelivery("s", `event-${index}`, "received", owner.signal)));
    await waiting.promise;
    owner.abort(new Error("Stopped by caller"));
    expect((await pending).every((result) => result.status === "rejected")).toBe(true);
    expect(attempts).toBe(0);
    expect(warn).not.toHaveBeenCalled();
  } finally { warn.mockRestore(); }
});

test("credential waits retain their old epoch and cannot send under a replacement claim", async () => {
  const requests: CcrHttpRequest[] = [];
  const { client, credentials } = fixture(async (request) => { requests.push(request); return new Response(null, { status: 204 }); });
  credentials.token = "";
  client.config.sleep = async () => { credentials.workerEpoch = 8; credentials.token = "new-token"; };
  await expect(client.reportDelivery("s", "old", "received")).rejects.toThrow();
  expect(requests).toEqual([]);
  await client.reportDelivery("s", "new", "received");
  expect(requests).toHaveLength(1);
  expect(requests[0]!.body).toMatchObject({ worker_epoch: 8, updates: [{ event_id: "new", status: "received" }] });
});

test("terminal delivery claims cannot reopen through a token refresh, repeated call or successful write", async () => {
  for (const status of [409, 410, 401, 403]) {
    let rejected = true;
    let attempts = 0;
    const { client, credentials } = fixture(async (request) => {
      if (request.path.endsWith("/heartbeat")) return new Response(null, { status: 204 });
      attempts++;
      return new Response(null, { status: rejected ? status : 204 });
    });
    if (status === 401) credentials.token = expiredToken();
    await expect(client.reportDelivery("s", "event", "received")).rejects.toThrow(String(status));
    const failedAttempts = attempts;
    expect(failedAttempts).toBe(status === 403 ? 10 : 1);
    rejected = false;
    credentials.token = "new-token";
    await client.heartbeat("s");
    await expect(client.reportDelivery("s", "event", "processed")).rejects.toThrow(String(status));
    expect(attempts).toBe(failedAttempts);
    await client.reportDelivery("other", "event", "received");
    credentials.workerEpoch++;
    await client.reportDelivery("s", "new", "received");
    expect(attempts).toBe(failedAttempts + 2);
  }
});

test("only successful registration or a fresh lease binding can reopen the same epoch", async () => {
  for (const reset of ["register", "bind"] as const) {
    let status = 409;
    let registered = false;
    let deliveries = 0;
    const { client } = fixture(async (request) => {
      if (request.path.endsWith("/register")) return registered
        ? Response.json({ worker_epoch: 7 }) : new Response(null, { status: 503 });
      deliveries++;
      return new Response(null, { status });
    });
    await expect(client.reportDelivery("s", "old", "received")).rejects.toThrow("409");
    await expect(client.register("s", "token")).rejects.toThrow("503");
    status = 204;
    await expect(client.reportDelivery("s", "old", "received")).rejects.toThrow("409");
    let detach: (() => void) | undefined;
    if (reset === "register") { registered = true; await client.register("s", "token"); }
    else detach = client.bindLease("s").subscribe!(() => {});
    try { await client.reportDelivery("s", "new", "received"); expect(deliveries).toBe(2); }
    finally { detach?.(); }
  }
});

test("terminal HTTP failures log before lease cancellation, including shared auth exhaustion", async () => {
  for (const status of [409, 410, 401, 403]) {
    const logs: string[] = [];
    const warn = spyOn(console, "warn").mockImplementation((value) => { logs.push(String(value)); });
    let attempts = 0;
    const { client, credentials } = fixture(async () => {
      attempts++; return new Response(null, { status });
    });
    if (status === 401) credentials.token = expiredToken();
    const owner = new AbortController();
    const losses: CcrLeaseFailure[] = [];
    const heartbeat = new CcrHeartbeat({ check: client.bindLease("s"), maxAuthFailures: 2, onLeaseLost: (failure) => {
      losses.push(failure); owner.abort();
    } }, owner.signal);
    try {
      await expect(client.reportDelivery("s", "secret-event", "received", owner.signal)).rejects.toThrow();
      expect(attempts).toBe(status === 403 ? 2 : 1);
      expect(losses).toHaveLength(1);
      expect(logs).toHaveLength(attempts);
      expect(JSON.parse(logs.at(-1)!)).toMatchObject({ event: "cli_worker_request_failed", status,
        method: "post", path: "/worker/events/delivery", worker_epoch: 7, batch_size: 1 });
      expect(logs.join()).not.toContain("secret-event");
      expect(logs.join()).not.toContain(credentials.token);
      await expect(client.reportDelivery("s", "another", "received")).rejects.toThrow();
      expect(logs).toHaveLength(attempts);
    } finally { await heartbeat.close(); warn.mockRestore(); }
  }
});

test("late HTTP failure after cancellation cannot diagnose or poison a same-epoch replacement", async () => {
  const response = Promise.withResolvers<Response>();
  const started = Promise.withResolvers<void>();
  let attempts = 0;
  const { client } = fixture(async () => {
    if (++attempts === 1) { started.resolve(); return response.promise; }
    return new Response(null, { status: 204 });
  });
  const logs: string[] = [];
  const warn = spyOn(console, "warn").mockImplementation((value) => { logs.push(String(value)); });
  const owner = new AbortController();
  try {
    const pending = client.reportDelivery("s", "old", "received", owner.signal);
    const cancelled = pending.catch((error: unknown) => error);
    await started.promise;
    owner.abort(new CcrHttpError(410));
    expect(await cancelled).toBeInstanceOf(CcrHttpError);
    const detach = client.bindLease("s").subscribe!(() => {});
    try {
      await client.reportDelivery("s", "new", "received");
      response.resolve(new Response(null, { status: 409 }));
      await settle();
      await client.reportDelivery("s", "new", "processed");
      expect(attempts).toBe(3);
      expect(logs).toEqual([]);
    } finally { detach(); }
  } finally { response.resolve(new Response(null, { status: 204 })); warn.mockRestore(); }
});

test("response cleanup and logging failures cannot erase the terminal HTTP error", async () => {
  const { client } = fixture(async () => new Response(new ReadableStream({
    cancel() { throw new Error("cleanup failed"); },
  }), { status: 409 }));
  const warn = spyOn(console, "warn").mockImplementation(() => { throw new Error("logging failed"); });
  try {
    await expect(client.reportDelivery("s", "event", "received")).rejects.toThrow("409");
    await expect(client.reportDelivery("s", "event", "received")).rejects.toThrow("409");
    expect(warn).toHaveBeenCalledTimes(1);
  } finally { warn.mockRestore(); }
});
