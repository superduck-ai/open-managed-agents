import { stateData, logData } from "./persistence-fixtures.ts";
import { expect, test } from "bun:test";
import { CcrClient, CcrHttpError, type CcrHttpRequest } from "../src/ccrv2/index.ts";

function clientWith(request: (request: CcrHttpRequest) => Promise<Response>) {
  return new CcrClient({
    request,
    credentials: () => ({ token: "worker-token", workerEpoch: 7 }),
    sessionIds: () => ["session/a"],
    sleep: async () => {},
    random: () => 0,
  });
}

test("lost persistence confirmation retries the same durable envelope with current credentials", async () => {
  const requests: CcrHttpRequest[] = [];
  let tokenVersion = 7;
  const client = new CcrClient({
    credentials: () => ({ token: `token-${tokenVersion}`, workerEpoch: 7 }),
    sessionIds: () => ["session/a"],
    sleep: async () => {},
    random: () => 0,
    async request(request) {
      requests.push({ ...request, body: structuredClone(request.body) });
      tokenVersion++;
      if (requests.length === 1) throw new Error("Connection lost after write");
      if (requests.length === 2) return new Response(null, { status: 503 });
      return new Response(null, { status: 204 });
    },
  });
  await client.persist("session/a", "dsh_brain_state", stateData({ revision: 1 }));
  expect(requests).toHaveLength(3);
  const envelope = {
    type: "system",
    uuid: "ddb8dd2411772033980a632b0c014f9f77300318c9bfc8484c7fb5c81ca2a53e",
    subtype: "dsh_brain_state",
    schema_version: 1,
    data: stateData({ revision: 1 }),
  };
  for (const [index, request] of requests.entries()) {
    expect(request.path).toBe("/v1/code/sessions/session%2Fa/worker/internal-events");
    expect(request.method).toBe("POST");
    expect(request.timeout).toBe(10_000);
    expect(request.token).toBe(`token-${7 + index}`);
    expect(request.body).toEqual({
      events: [{ payload: envelope, is_compaction: false }],
      session_id: "session/a",
      worker_epoch: 7,
    });
  }
});

test("unbound persistence uses a bounded retry budget and refuses stale epochs immediately", async () => {
  for (const [status, expectedAttempts] of [[409, 1], [401, 1], [408, 3], [429, 3], [500, 3]] as const) {
    let attempts = 0;
    const client = clientWith(async () => {
      attempts++;
      return new Response(null, { status });
    });
    try {
      await client.persist("session/a", "dsh_log", logData({}));
      throw new Error("Persistence should fail");
    } catch (error) {
      expect(error).toBeInstanceOf(CcrHttpError);
      expect((error as CcrHttpError).status).toBe(status);
    }
    expect(attempts).toBe(expectedAttempts);
  }
});

test("private event pagination preserves payload order and detects stuck or malformed pages", async () => {
  const requests: CcrHttpRequest[] = [];
  const client = clientWith(async (request) => {
    requests.push(request);
    return Response.json(requests.length === 1
      ? { data: [{ payload: { sequence: 1 } }], next_cursor: "a/b ?" }
      : { data: [{ payload: { sequence: 2 } }], next_cursor: null });
  });
  expect(await client.privateEvents("session/a")).toEqual([{ sequence: 1 }, { sequence: 2 }]);
  expect(requests[0]!.path).toBe("/v1/code/sessions/session%2Fa/worker/internal-events");
  expect(requests[1]!.path).toEndWith("?cursor=a%2Fb+%3F");
  expect(requests.every((r) => r.method === "GET" && r.body === undefined)).toBe(true);
  expect(requests.every((r) => r.timeout === 30_000)).toBe(true);
  let calls = 0;
  const stuck = clientWith(async () => {
    calls++;
    return Response.json({ data: [], next_cursor: "same" });
  });
  await expect(stuck.privateEvents("session/a")).rejects.toThrow("cursor did not advance");
  expect(calls).toBe(2);
  const malformed = clientWith(async () => Response.json({ data: {} }));
  await expect(malformed.privateEvents("session/a")).rejects.toThrow("Invalid private event page");
});

test("subagent history keeps its scope on every page and writes agent_id outside payload", async () => {
  const requests: CcrHttpRequest[] = [];
  const client = clientWith(async (request) => {
    requests.push(request);
    if (request.method === "POST") return Response.json({});
    return Response.json(requests.length === 1
      ? { data: [{ agent_id: "child", payload: { sequence: 1 } }], next_cursor: "501" }
      : { data: [{ agent_id: "child", payload: { sequence: 2 } }], next_cursor: null });
  });
  expect(await client.privateEvents("session/a", { subagents: true })).toEqual([{ sequence: 1 }, { sequence: 2 }]);
  expect(requests[0]!.path).toEndWith("/internal-events?subagents=true");
  expect(requests[1]!.path).toEndWith("/internal-events?subagents=true&cursor=501");
  await client.persist("session/a", "dsh_log", logData({}, "child"), "child");
  const body = requests[2]!.body as { events: { agent_id: string; payload: Record<string, unknown>; is_compaction: boolean }[] };
  expect(body.events[0]!.agent_id).toBe("child");
  expect(body.events[0]!.payload.agent_id).toBeUndefined();
  expect(body.events[0]!.is_compaction).toBe(false);
});

test("internal event readers preserve envelopes and retry each page independently", async () => {
  const requests: CcrHttpRequest[] = [];
  const event = {
    event_id: "event-1", event_type: "transcript", payload: { content: "history" },
    event_metadata: { source: "child" }, is_compaction: true,
    created_at: "2026-10-13T00:00:00Z", agent_id: "child-1",
  };
  const client = clientWith(async (request) => {
    requests.push(request);
    if (requests.length === 1) return Response.json({ data: [event], next_cursor: "500" });
    if (requests.length === 2) return new Response(null, { status: 503 });
    return Response.json({ data: [{ ...event, event_id: "event-2" }], next_cursor: null });
  });
  expect(await client.readSubagentInternalEvents("session/a")).toEqual([event, { ...event, event_id: "event-2" }]);
  expect(requests[1]!.path).toBe(requests[2]!.path);
  expect(requests[2]!.path).toEndWith("?subagents=true&cursor=500");
  expect(requests.every((request) => request.timeout === 30_000 && request.body === undefined)).toBe(true);
  expect(requests[0]!.headers).toEqual({ "anthropic-version": "2023-06-01", "User-Agent": "oma-brain-dsh" });
});

test("ordinary history GET errors retry ten times and never return a partial history", async () => {
  for (const status of [400, 401, 403, 429, 503]) {
    let attempts = 0;
    const waits: number[] = [];
    const client = new CcrClient({
      credentials: () => ({ token: "worker-token", workerEpoch: 7 }),
      sessionIds: () => ["session/a"], random: () => 0,
      sleep: async (ms) => { waits.push(ms); },
      request: async (request) => {
        if (!request.path.includes("cursor="))
          return Response.json({ data: [{ payload: { content: "partial" } }], next_cursor: "500" });
        attempts++;
        return new Response(null, { status });
      },
    });
    expect(await client.readInternalEvents("session/a")).toBeNull();
    expect(attempts).toBe(10);
    expect(waits).toEqual([500, 1000, 2000, 4000, 8000, 16000, 30000, 30000, 30000]);
    // The dsh adapter must reject unavailable history rather than create an empty session.
    await expect(client.privateEvents("session/a")).rejects.toThrow("history unavailable");
  }
});

test("history reads distinguish empty history from missing credentials and lost ownership", async () => {
  const empty = clientWith(async () => Response.json({}));
  expect(await empty.readInternalEvents("session/a")).toEqual([]);
  expect(await empty.privateEvents("session/a")).toEqual([]);
  const missing = new CcrClient({
    credentials: () => ({ token: "", workerEpoch: 7 }), sessionIds: () => [],
    request: async () => { throw new Error("must not send unauthenticated GET"); },
  });
  expect(await missing.readInternalEvents("session/a")).toBeNull();
  for (const status of [409, 410]) {
    let attempts = 0;
    const lost = clientWith(async () => { attempts++; return new Response(null, { status }); });
    await expect(lost.readInternalEvents("session/a")).rejects.toThrow(String(status));
    expect(attempts).toBe(1);
  }
});

test("history read cancellation interrupts retry waits and keeps the original epoch", async () => {
  const abort = new AbortController();
  const sleeping = Promise.withResolvers<void>();
  let attempts = 0;
  const client = new CcrClient({
    credentials: () => ({ token: "worker-token", workerEpoch: 7 }), sessionIds: () => [],
    request: async () => { attempts++; return new Response(null, { status: 503 }); },
    sleep: async () => { sleeping.resolve(); await new Promise<void>(() => {}); },
  });
  const read = client.readInternalEvents("session/a", abort.signal);
  await sleeping.promise;
  abort.abort(new Error("session closed"));
  await expect(read).rejects.toThrow("session closed");
  expect(attempts).toBe(1);
  let epoch = 7;
  const replaced = new CcrClient({
    credentials: () => ({ token: "worker-token", workerEpoch: epoch }), sessionIds: () => [],
    sleep: async () => {},
    request: async () => { epoch++; return new Response(null, { status: 503 }); },
  });
  await expect(replaced.readInternalEvents("session/a")).rejects.toThrow("409");
});

test("publication preserves ephemeral flags and delivery ACK accepts ignored updates", async () => {
  const requests: CcrHttpRequest[] = [];
  let applied = 1;
  const client = clientWith(async (request) => {
    requests.push(request);
    return Response.json({ ok: true, applied, ignored: 1 - applied });
  });
  const payloads = ["event_start", "event_delta", "agent.message", "control_request", "user"]
    .map((type) => ({ type, uuid: `event-${type}` }));
  await client.events("session/a", payloads);
  expect(requests[0]!.path).toEndWith("/worker/events");
  expect(requests[0]!.body).toEqual({
    events: payloads.map((payload, i) => ({ payload, ephemeral: i < 2 })),
    session_id: "session/a", worker_epoch: 7,
  });
  await client.acknowledge("session/a", "input");
  expect(requests[1]!.path).toEndWith("/worker/events/delivery");
  expect(requests[1]!.body).toEqual({
    updates: [{ event_id: "input", status: "processed" }],
    session_id: "session/a", worker_epoch: 7,
  });
  applied = 0;
  await client.acknowledge("session/a", "input");
  expect(requests).toHaveLength(3);
  expect(requests.every((r) => r.timeout === 10_000)).toBe(true);
});

test("worker status and heartbeat retain fencing and the five-second deadline", async () => {
  const requests: CcrHttpRequest[] = [];
  const client = clientWith(async (request) => {
    requests.push(request);
    return new Response(null, { status: 204 });
  });
  await client.setStatus("session/a", "running");
  await client.heartbeat("session/a");
  expect(requests[0]!.method).toBe("PUT");
  expect(requests[0]!.timeout).toBe(10_000);
  expect(requests[0]!.path).toBe("/v1/code/sessions/session%2Fa/worker");
  expect(requests[0]!.body).toEqual({
    worker_status: "running", session_id: "session/a", worker_epoch: 7,
    requires_action_details: null, external_metadata: { pending_action: null },
  });
  expect(requests[1]!.method).toBe("POST");
  expect(requests[1]!.path).toEndWith("/worker/heartbeat");
  expect(requests[1]!.body).toEqual({ session_id: "session/a", worker_epoch: 7 });
  expect(requests[1]!.timeout).toBe(5000);
  expect([...client.sessionIds()]).toEqual(["session/a"]);
});

test("lease checking distinguishes protocol rejection from transient failure without retrying", async () => {
  for (const [status, outcome] of [
    [200, "confirmed"], [204, "confirmed"],
    [401, "unavailable"], [403, "unavailable"], [409, "rejected"], [410, "rejected"],
    [400, "unavailable"], [404, "rejected"],
    [408, "unavailable"], [429, "unavailable"], [503, "unavailable"],
  ] as const) {
    let requests = 0;
    const client = clientWith(async () => {
      requests++;
      return status === 200
        ? Response.json({})
        : new Response(null, { status });
    });
    expect(await client.checkLease("session/a")).toBe(outcome);
    expect(requests).toBe(1);
  }
  const disconnected = clientWith(async () => { throw new Error("Connection lost"); });
  expect(await disconnected.checkLease("session/a")).toBe("unavailable");
});

test("successful empty write responses are accepted without retry or false lease failure", async () => {
  for (const status of [200, 202, 204]) {
    let requests = 0;
    const client = clientWith(async () => {
      requests++;
      return new Response(null, { status });
    });
    await client.persist("session/a", "dsh_log", logData({}));
    await client.events("session/a", [{ type: "agent.message", uuid: "message" }]);
    await client.setStatus("session/a", "idle");
    expect(await client.checkLease("session/a")).toBe("confirmed");
    expect(requests).toBe(4);
  }
});

test("bound heartbeats refresh tokens but never send a request for a replacement epoch", async () => {
  let token = "first-token";
  let workerEpoch = 7;
  const requests: CcrHttpRequest[] = [];
  const client = new CcrClient({
    credentials: () => ({ token, workerEpoch }),
    sessionIds: () => ["session/a"],
    request: async (request) => { requests.push(request); return new Response(null, { status: 204 }); },
  });
  const check = client.bindLease("session/a");
  const abort = new AbortController();
  expect(await check(abort.signal, 5000)).toEqual({ status: "confirmed" });
  token = "refreshed-token";
  expect(await check(abort.signal, 1500)).toEqual({ status: "confirmed" });
  expect(requests.map((request) => request.token)).toEqual(["first-token", "refreshed-token"]);
  expect(requests[1]!.signal).toBe(abort.signal);
  expect(requests[1]!.timeout).toBe(1500);
  expect(requests[1]!.body).toEqual({ session_id: "session/a", worker_epoch: 7 });
  workerEpoch = 8;
  expect(await check(abort.signal, 5000)).toEqual({
    status: "rejected", httpStatus: 409, reason: "epoch_mismatch", workerEpoch: 7,
  });
  expect(requests).toHaveLength(2);
  const replacement = client.bindLease("session/a");
  expect(await replacement(abort.signal, 5000)).toEqual({ status: "confirmed" });
  expect(requests[2]!.body).toEqual({ session_id: "session/a", worker_epoch: 8 });
  abort.abort();
  await expect(replacement(abort.signal, 5000)).rejects.toThrow();
  expect(requests).toHaveLength(3);
});

test("bound heartbeat cancellation reaches the transport without becoming a lease rejection", async () => {
  const started = Promise.withResolvers<void>();
  let requests = 0;
  const client = clientWith((request) => new Promise<Response>((_resolve, reject) => {
    requests++;
    request.signal!.addEventListener("abort", () => reject(request.signal!.reason), { once: true });
    started.resolve();
  }));
  const abort = new AbortController();
  const check = client.bindLease("session/a")(abort.signal, 5000);
  await started.promise;
  abort.abort(new Error("Session closed"));
  await expect(check).rejects.toThrow("Session closed");
  expect(requests).toBe(1);
});

test("heartbeat diagnostics retain the bound epoch and classify HTTP and transport failures", async () => {
  for (const [status, reason] of [
    [400, "request_rejected"], [401, "authentication_rejected"], [403, "authentication_rejected"],
    [404, "worker_not_registered"], [408, "timeout"], [409, "epoch_mismatch"], [410, "lease_expired"],
    [429, "rate_limited"], [500, "server_error"], [503, "server_error"],
  ] as const) {
    let calls = 0;
    const client = clientWith(async () => { calls++; return new Response(null, { status }); });
    const result = await client.bindLease("session/a")(new AbortController().signal, 5000);
    expect(result).toEqual({
      status: status === 404 || status === 409 || status === 410 ? "rejected" : "unavailable",
      reason, httpStatus: status, workerEpoch: 7,
    });
    expect(calls).toBe(1);
  }
  for (const [error, reason] of [
    [new DOMException("private-timeout-message", "TimeoutError"), "timeout"],
    [Object.assign(new Error("private-network-message"), { code: "ETIMEDOUT" }), "timeout"],
    [new Error("private-network-message"), "transport_error"],
  ] as const) {
    const client = clientWith(async () => { throw error; });
    const result = await client.bindLease("session/a")(new AbortController().signal, 5000);
    expect(result).toEqual({ status: "unavailable", reason, workerEpoch: 7, httpStatus: undefined });
    expect(JSON.stringify(result)).not.toContain("private-");
  }
});

test("auth rejection checks the sent session token's expiry, including prefixed JWTs and token rotation", async () => {
  const jwt = (payload: unknown) => `header.${Buffer.from(JSON.stringify(payload)).toString("base64url")}.signature`;
  const expired = jwt({ exp: Math.floor(Date.now() / 1000) - 60 });
  const fresh = jwt({ exp: Math.floor(Date.now() / 1000) + 3600 });
  for (const [sentToken, expected] of [
    [expired, "rejected"], [`sk-ant-si-${expired}`, "rejected"],
    [fresh, "unavailable"], [jwt({}), "unavailable"],
    [jwt({ exp: "1" }), "unavailable"], [jwt(null), "unavailable"],
    ["not-a-jwt", "unavailable"], ["header.invalid.signature", "unavailable"],
  ] as const) {
    for (const status of [401, 403]) {
      let token = sentToken;
      const client = new CcrClient({
        credentials: () => ({ token, workerEpoch: 7 }), sessionIds: () => ["a"],
        request: async (request) => {
          expect(request.token).toBe(sentToken);
          // Changing credentials while a response is in flight must not change
          // which token that failed request is classified against.
          token = expected === "rejected" ? fresh : expired;
          return new Response(null, { status });
        },
      });
      expect(await client.bindLease("a")(new AbortController().signal, 5000)).toEqual({
        status: expected, httpStatus: status, workerEpoch: 7,
        reason: expected === "rejected" ? "token_expired" : "authentication_rejected",
      });
    }
  }
});

test("empty final cursors stop pagination and non-string cursors are rejected", async () => {
  for (const next_cursor of ["", null, undefined]) {
    let reads = 0;
    const client = clientWith(async () => {
      reads++;
      return Response.json({ data: [{ payload: { type: "system" } }], next_cursor });
    });
    expect(await client.privateEvents("session/a")).toEqual([{ type: "system" }]);
    expect(reads).toBe(1);
  }
  const invalid = clientWith(async () => Response.json({ data: [], next_cursor: 12 }));
  await expect(invalid.privateEvents("session/a")).rejects.toThrow("Invalid private event cursor");
});

test("rate-limited writes wait for Retry-After, keep their UUID and cap their retry budget", async () => {
  const requests: CcrHttpRequest[] = [];
  const waits: number[] = [];
  const client = new CcrClient({
    credentials: () => ({ token: "worker-token", workerEpoch: 7 }),
    sessionIds: () => ["session/a"],
    sleep: async (ms) => { waits.push(ms); },
    random: () => 0.5,
    request: async (request) => {
      requests.push({ ...request, body: structuredClone(request.body) });
      return new Response(null, { status: 429, headers: { "Retry-After": "30" } });
    },
  });
  await expect(client.events("session/a", [{ type: "agent.message" }])).rejects.toThrow("429");
  expect(waits).toEqual([30_250, 30_250]);
  expect(requests).toHaveLength(3);
  expect(requests[0]!.body).toEqual(requests[1]!.body);
  expect(requests[1]!.body).toEqual(requests[2]!.body);
});

test("transient read failures back off and malformed JSON is not treated as an empty page", async () => {
  let attempts = 0;
  const waits: number[] = [];
  const client = new CcrClient({
    credentials: () => ({ token: "worker-token", workerEpoch: 7 }),
    sessionIds: () => ["session/a"],
    sleep: async (ms) => { waits.push(ms); },
    random: () => 0,
    request: async () => {
      attempts++;
      if (attempts === 1) throw new Error("Connection lost");
      if (attempts === 2) return new Response(null, { status: 503 });
      return Response.json({ data: [{ payload: { revision: 1 } }], next_cursor: "" });
    },
  });
  expect(await client.privateEvents("session/a")).toEqual([{ revision: 1 }]);
  expect(waits).toEqual([500, 1000]);
  const malformed = clientWith(async () => new Response(null, { status: 200 }));
  await expect(malformed.privateEvents("session/a")).rejects.toThrow();
  // Delivery uses the HTTP result, matching superduck even with an empty body.
  await malformed.acknowledge("session/a", "input");
});

test("requires_action reports approval metadata and clears it when execution resumes", async () => {
  const requests: CcrHttpRequest[] = [];
  const client = clientWith(async (request) => {
    requests.push(request);
    return new Response(null, { status: 200 });
  });
  const details = {
    tool_name: "Bash", action_description: "Use Bash",
    tool_use_id: "call", request_id: "approval", input: { command: "pwd" },
  };
  await client.setStatus("session/a", "requires_action", details);
  await client.setStatus("session/a", "running");
  expect(requests[0]!.body).toEqual({
    worker_status: "requires_action", worker_epoch: 7, session_id: "session/a",
    requires_action_details: {
      tool_name: details.tool_name, action_description: details.action_description,
      request_id: details.request_id,
    },
    external_metadata: { pending_action: details },
  });
  expect(requests[1]!.body).toEqual({
    worker_status: "running", worker_epoch: 7, session_id: "session/a",
    requires_action_details: null, external_metadata: { pending_action: null },
  });
  await expect(client.setStatus("session/a", "requires_action")).rejects.toThrow("details");
  expect(requests).toHaveLength(2);
});

test("tool confirmation lookup matches durable backend requests rather than transient worker status", async () => {
  const request = { public_event_id: "tool", request_id: "request", provider_tool_use_id: "call" };
  for (const [metadata, expected] of [
    [{}, false],
    [{ pending_action: request }, false],
    [{ "managed_agent_tool_permission_request:other": request }, false],
    [{ "managed_agent_tool_permission_request:tool": { ...request, request_id: "other" } }, false],
    [{ "managed_agent_tool_permission_request:tool": { request_id: "request" } }, false],
    [{ "managed_agent_tool_permission_request:tool": null }, false],
    [{ "managed_agent_tool_permission_request:tool": request }, true],
    [{ managed_agent_tool_permission_request: request }, true],
  ] as const) {
    const client = clientWith(async () => Response.json({ worker: { external_metadata: metadata } }));
    expect(await client.isToolConfirmationPending("session/a", "request")).toBe(expected);
  }
});
