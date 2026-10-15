import { expect, spyOn, test } from "bun:test";
import { CcrClient, type CcrHttpRequest } from "../src/ccrv2/client.ts";
import { CcrHeartbeat } from "../src/ccrv2/heartbeat.ts";

const settle = () => new Promise<void>((resolve) => setImmediate(resolve));

function fixture(request: (request: CcrHttpRequest) => Promise<Response>) {
  const credentials = { token: "private-token", workerEpoch: 7 };
  const waits: number[] = [];
  const client = new CcrClient({ request, credentials: () => credentials, sessionIds: () => ["s"],
    sleep: async (ms) => { waits.push(ms); }, random: () => 0 });
  return { client, credentials, waits };
}

test("internal-events logs each failed batch attempt with safe fields and preserves retry confirmation", async () => {
  const first = Promise.withResolvers<void>();
  const logs: string[] = [];
  const warn = spyOn(console, "warn").mockImplementation((value) => { logs.push(String(value)); });
  const requests: CcrHttpRequest[] = [];
  const { client, waits } = fixture(async (request) => {
    requests.push(request);
    if (requests.length === 1) { await first.promise; return new Response(null, { status: 204 }); }
    if (requests.length === 2) return new Response("private-response", { status: 503 });
    if (requests.length === 3)
      throw Object.assign(new Error("https://private-host/private-token"), { code: "ECONNRESET" });
    if (requests.length === 4) return new Response(null, { status: 429, headers: { "Retry-After": "2" } });
    return new Response(null, { status: 204 });
  });
  const detach = client.bindLease("private-session").subscribe!(() => {});
  let confirmed = false;
  const pending = Promise.all([
    client.persist("private-session", "private-subtype", { text: "private-seed" }),
    client.persist("private-session", "private-subtype", { text: "private-body" }),
    client.persist("private-session", "private-subtype", { text: "private-child-body" }, "private-agent"),
  ]).then(() => { confirmed = true; });
  try {
    expect(confirmed).toBe(false);
    first.resolve();
    await pending;
    expect(confirmed).toBe(true);
    expect(requests).toHaveLength(5);
    expect(requests.slice(2).map((request) => request.body)).toEqual(Array(3).fill(requests[1]!.body));
    expect(waits).toEqual([500, 1000, 2000]);
    const fields = { method: "post", path: "/worker/internal-events", worker_epoch: 7, batch_size: 2 };
    expect(logs.map((log) => JSON.parse(log))).toEqual([
      { event: "cli_worker_request_failed", ...fields, status: 503, failure_reason: "server_error" },
      { event: "cli_worker_request_error", ...fields, failure_reason: "transport_error", error_code: "ECONNRESET" },
      { event: "cli_worker_request_failed", ...fields, status: 429, failure_reason: "rate_limited" },
    ]);
    expect(logs.join()).not.toContain("private-");
  } finally { first.resolve(); detach(); await pending.catch(() => {}); warn.mockRestore(); }
});

test("internal-events logs terminal HTTP failures before claim cancellation, including auth exhaustion", async () => {
  for (const status of [409, 410, 401, 403]) {
    const logs: string[] = [];
    const warn = spyOn(console, "warn").mockImplementation((value) => { logs.push(String(value)); });
    let attempts = 0;
    const { client, credentials } = fixture(async () => {
      attempts++; return new Response(null, { status });
    });
    if (status === 401)
      credentials.token = `header.${Buffer.from(JSON.stringify({ exp: 1 })).toString("base64url")}.signature`;
    const owner = new AbortController();
    let losses = 0;
    const heartbeat = new CcrHeartbeat({ check: client.bindLease("s"), maxAuthFailures: 2,
      onLeaseLost: () => { losses++; owner.abort(); } }, owner.signal);
    try {
      await expect(client.persist("s", "private-subtype", { text: "private-body" })).rejects.toMatchObject({ status });
      expect(attempts).toBe(status === 403 ? 2 : 1);
      expect(losses).toBe(1);
      expect(logs).toHaveLength(attempts);
      for (const log of logs) expect(JSON.parse(log)).toMatchObject({ event: "cli_worker_request_failed", status,
        method: "post", path: "/worker/internal-events", worker_epoch: 7, batch_size: 1 });
      expect(logs.join()).not.toContain("private-");
      expect(logs.join()).not.toContain(credentials.token);
    } finally { await heartbeat.close(); warn.mockRestore(); }
  }
});

test("cancelled internal-events requests do not log late HTTP or network failures against a replacement", async () => {
  for (const networkError of [false, true]) {
    const response = Promise.withResolvers<Response>();
    const logs: string[] = [];
    const warn = spyOn(console, "warn").mockImplementation((value) => { logs.push(String(value)); });
    let attempts = 0;
    const { client } = fixture(async () => ++attempts === 1 ? response.promise : new Response(null, { status: 204 }));
    let detach = client.bindLease("s").subscribe!(() => {});
    const pending = client.persist("s", "dsh_log", { old: true });
    const cancelled = pending.catch((error: unknown) => error);
    try {
      expect(attempts).toBe(1);
      detach();
      expect(await cancelled).toBeInstanceOf(Error);
      detach = client.bindLease("s").subscribe!(() => {});
      await client.persist("s", "dsh_log", { replacement: true });
      if (networkError) response.reject(Object.assign(new Error("private-error"), { code: "ECONNRESET" }));
      else response.resolve(new Response(null, { status: 409 }));
      await settle();
      await client.persist("s", "dsh_log", { next: true });
      expect(attempts).toBe(3);
      expect(logs).toEqual([]);
    } finally { response.resolve(new Response(null, { status: 204 })); detach(); warn.mockRestore(); }
  }
});

test("internal-events validation and credential waits do not invent HTTP failures", async () => {
  const warn = spyOn(console, "warn").mockImplementation(() => {});
  try {
    for (const changeEpoch of [false, true]) {
      let attempts = 0;
      const { client, credentials } = fixture(async () => { attempts++; return new Response(null, { status: 204 }); });
      await expect(client.persist("s", "dsh_log", { invalid: 1n })).rejects.toThrow();
      credentials.token = "";
      client.config.sleep = async () => {
        credentials.token = "new-private-token";
        if (changeEpoch) credentials.workerEpoch++;
      };
      const pending = client.persist("s", "dsh_log", {});
      if (changeEpoch) { await expect(pending).rejects.toThrow(); expect(attempts).toBe(0); }
      else { await pending; expect(attempts).toBe(1); }
      await client.persist("s", "dsh_log", { next: true });
    }
    expect(warn).not.toHaveBeenCalled();
  } finally { warn.mockRestore(); }
});

test("internal-events network diagnostics classify timeouts and omit untrusted error codes", async () => {
  const logs: string[] = [];
  const warn = spyOn(console, "warn").mockImplementation((value) => { logs.push(String(value)); });
  const errors = [Object.assign(new Error("private-error"), { code: "ETIMEDOUT" }),
    Object.assign(new Error("private-error"), { code: "private-code" })];
  const { client } = fixture(async () => {
    const error = errors.shift();
    if (error) throw error;
    return new Response(null, { status: 204 });
  });
  try {
    await client.persist("s", "dsh_log", {});
    const fields = { event: "cli_worker_request_error", method: "post", path: "/worker/internal-events",
      worker_epoch: 7, batch_size: 1 };
    expect(logs.map((log) => JSON.parse(log))).toEqual([
      { ...fields, failure_reason: "timeout", error_code: "ETIMEDOUT" },
      { ...fields, failure_reason: "transport_error" },
    ]);
    expect(logs.join()).not.toContain("private-");
  } finally { warn.mockRestore(); }
});

test("internal-events logging failures cannot change retries or mask a terminal HTTP error", async () => {
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
    await client.persist("s", "dsh_log", {});
    expect(attempts).toBe(3);
    await expect(rejected.persist("s", "dsh_log", {})).rejects.toMatchObject({ status: 409 });
    expect(warn).toHaveBeenCalledTimes(3);
  } finally { warn.mockRestore(); }
});
