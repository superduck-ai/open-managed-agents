import { expect, spyOn, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { CcrClient, consumeInbound, type CcrHttpRequest } from "../src/ccrv2/index.ts";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { Backend, eventually, textEvents, toolEvents, deliveryOf } from "./backend.ts";
import { sse } from "../src/sse.ts";

function client(request: (request: CcrHttpRequest) => Promise<Response>) {
  return new CcrClient({ request, credentials: () => ({ token: "ingress", workerEpoch: 7 }), sessionIds: () => ["session"] });
}

test("registration validates epochs and never retries ambiguous claims", async () => {
  let count = 0;
  await expect(client(async () => { count++; throw new Error("lost reply"); }).register("session", "token")).rejects.toThrow();
  expect(count).toBe(1);
  for (const epoch of [0, -1, "1junk", "9007199254740992", null])
    await expect(client(async () => Response.json({ worker_epoch: epoch })).register("session", "token")).rejects.toThrow("epoch");
  const c = client(async (request) => {
    expect(request.path).toBe("/v1/code/sessions/session/worker/register");
    expect(request.token).toBe("task-token");
    expect(request.method).toBe("POST");
    expect(request.body).toEqual({});
    return Response.json({ worker_epoch: "7" });
  });
  expect(await c.register("session", "task-token")).toBe(7);
});

test("SSE rejects cross-session input and permanent HTTP errors without reconnecting", async () => {
  for (const response of [...[401, 403, 404, 409, 410].map((status) => new Response(null, { status })), new Response(
    `event: client_event\ndata: ${JSON.stringify({ session_id: "other", event_id: "id", payload: { type: "user" } })}\n\n`,
    { headers: { "Content-Type": "text/event-stream" } },
  )]) {
    let calls = 0;
    const c = client(async () => { calls++; return response; });
    await expect((async () => { for await (const _ of c.inbound("session", new AbortController().signal)) {} })()).rejects.toThrow();
    expect(calls).toBe(1);
  }
});

test("direct client SSE intake reports envelope IDs without waiting for delivery and cancels receipts on return", async () => {
  const requests: CcrHttpRequest[] = [];
  let deliverySignal: AbortSignal | undefined;
  const c = client(async (request) => {
    requests.push(request);
    if (request.method === "POST") {
      deliverySignal = request.signal;
      return new Promise<Response>(() => {});
    }
    return new Response(["one", "two"].map((event_id) =>
      `id: 42\nevent: client_event\ndata: ${JSON.stringify({ session_id: "session", event_id, payload: { type: "user" } })}\n\n`).join(""),
    { headers: { "Content-Type": "text/event-stream" } });
  });
  const iterator = c.inbound("session", new AbortController().signal)[Symbol.asyncIterator]();
  expect((await iterator.next()).value?.id).toBe("one");
  expect((await iterator.next()).value?.id).toBe("two");
  expect(requests.filter((r) => r.method === "POST")).toHaveLength(1);
  expect(requests[1]!.body).toMatchObject({ updates: [{ event_id: "one", status: "received" }] });
  await iterator.return!();
  expect(deliverySignal?.aborted).toBe(true);
});

test("SSE reconnect sends the highest complete frame sequence in both resume fields", async () => {
  const streams: CcrHttpRequest[] = [];
  const frame = (id: string | undefined, eventId: string) =>
    `${id === undefined ? "" : `id: ${id}\r\n`}event: client_event\r\ndata: ${JSON.stringify({
      session_id: "session", event_id: eventId, payload: { type: "user" },
    })}\r\n\r\n`;
  const c = new CcrClient({
    credentials: () => ({ token: "ingress", workerEpoch: 7 }),
    sessionIds: () => ["session"], sleep: async () => {},
    request: async (request) => {
      if (request.method === "POST") return new Response(null, { status: 204 });
      streams.push(request);
      if (streams.length === 2) return new Response(null, { status: 503 });
      const connection = streams.length;
      const body = connection === 1
        ? frame("42", "first") + frame("40", "older") + frame("junk", "invalid")
          + frame("-1", "negative") + frame(undefined, "no-id")
          + frame("99", "incomplete").slice(0, -2)
        : frame("43", connection === 4 ? "resumed-again" : "resumed");
      const bytes = new TextEncoder().encode(body);
      let offset = 0;
      return new Response(new ReadableStream<Uint8Array>({
        pull(controller) {
          if (offset === bytes.length) {
            if (connection === 1) controller.error(new Error("connection lost"));
            else controller.close();
            return;
          }
          controller.enqueue(bytes.slice(offset, offset + 3));
          offset = Math.min(offset + 3, bytes.length);
        },
      }), { headers: { "Content-Type": "text/event-stream" } });
    },
  });
  const iterator = c.inbound("session", new AbortController().signal)[Symbol.asyncIterator]();
  try {
    for (const id of ["first", "older", "invalid", "negative", "no-id", "resumed", "resumed-again"])
      expect((await iterator.next()).value?.id).toBe(id);
    expect(streams).toHaveLength(4);
    const first = streams[0]!;
    expect(first.path).toBe("/v1/code/sessions/session/worker/events/stream?worker_epoch=7");
    expect(new Headers(first.headers).get("Accept")).toBe("text/event-stream");
    expect(new Headers(first.headers).has("Last-Event-ID")).toBe(false);
    for (const request of streams.slice(1, 3)) {
      expect(request.path).toBe("/v1/code/sessions/session/worker/events/stream?worker_epoch=7&from_sequence_num=42");
      expect(new Headers(request.headers).get("Last-Event-ID")).toBe("42");
    }
    expect(streams[3]!.path).toContain("from_sequence_num=43");
    expect(new Headers(streams[3]!.headers).get("Last-Event-ID")).toBe("43");
  } finally {
    await iterator.return!();
  }
  const fresh = c.inbound("session", new AbortController().signal)[Symbol.asyncIterator]();
  try {
    expect((await fresh.next()).value?.id).toBe("resumed");
    expect(streams[4]!.path).toContain("from_sequence_num=43");
    expect(new Headers(streams[4]!.headers).get("Last-Event-ID")).toBe("43");
  } finally {
    await fresh.return!();
  }
});

test("a terminal received failure closes direct SSE intake instead of leaving it hung", async () => {
  const c = client(async (request) => request.method === "POST"
    ? new Response(null, { status: 409 })
    : new Response(new ReadableStream({ start(controller) {
      controller.enqueue(new TextEncoder().encode(`event: client_event\ndata: ${JSON.stringify({ session_id: "session", event_id: "one", payload: { type: "user" } })}\n\n`));
    } }), { headers: { "Content-Type": "text/event-stream" } }));
  await expect((async () => { for await (const _ of c.inbound("session", new AbortController().signal)) {} })())
    .rejects.toThrow("409");
});

test("SSE skips unsupported and malformed events while retaining their frame cursor", async () => {
  const requests: CcrHttpRequest[] = [];
  const c = new CcrClient({
    credentials: () => ({ token: "ingress", workerEpoch: 7 }), sessionIds: () => ["session"],
    sleep: async () => {},
    request: async (request) => {
      if (request.method === "POST") return new Response(null, { status: 204 });
      requests.push(request);
      if (requests.length > 1) return new Response(null, { status: 404 });
      return new Response(
        'id: 10\ndata: {}\n\nid: 20\nevent: other\ndata: {}\n\n'
        + 'id: 30\nevent: client_event\ndata: broken JSON\n\n'
        + 'id: 40\nevent: client_event\ndata: {"payload":{}}\n\n'
        + 'id: 50suffix\nevent: client_event\ndata: {"event_id":"valid","payload":{"type":"user"}}\n\n',
      );
    },
  });
  const seen: string[] = [];
  await expect((async () => {
    for await (const event of c.inbound("session", new AbortController().signal)) seen.push(event.id);
  })()).rejects.toThrow("404");
  expect(seen).toEqual(["valid"]);
  expect(requests[1]!.path).toContain("from_sequence_num=50");
  expect(new Headers(requests[1]!.headers).get("Last-Event-ID")).toBe("50");
  expect(c.inboundSequenceNum("session")).toBe(50);
});

test("SSE retries nonpermanent HTTP responses with Superduck backoff and a ten minute budget", async () => {
  let now = 0;
  const delays: number[] = [];
  const statuses = [400, 408, 429, 500];
  const c = new CcrClient({
    credentials: () => ({ token: "ingress", workerEpoch: 7 }), sessionIds: () => ["session"],
    random: () => 0.5, now: () => now,
    sleep: async (delay) => { delays.push(delay); now += delay; },
    request: async () => new Response(null, { status: statuses.shift() ?? 503, headers: { "Retry-After": "100" } }),
  });
  await expect((async () => {
    for await (const _ of c.inbound("session", new AbortController().signal)) {}
  })()).rejects.toThrow("reconnection budget exhausted");
  expect(delays.slice(0, 7)).toEqual([1000, 2000, 4000, 8000, 16000, 30000, 30000]);
  expect(now).toBeGreaterThanOrEqual(600_000);
  expect(Math.max(...delays)).toBe(30_000);
});

test("SSE accepts a transferred cursor but never transfers it to a replacement epoch", async () => {
  let epoch = 7;
  const requests: CcrHttpRequest[] = [];
  const c = new CcrClient({
    credentials: () => ({ token: "ingress", workerEpoch: epoch }), sessionIds: () => ["session"],
    request: async (request) => {
      if (request.method === "POST") return new Response(null, { status: 204 });
      requests.push(request);
      return new Response('event: client_event\ndata: {"event_id":"input","payload":{"type":"user"}}\n\n');
    },
  });
  const first = c.inbound("session", new AbortController().signal, 88)[Symbol.asyncIterator]();
  try { expect((await first.next()).value?.id).toBe("input"); }
  finally { await first.return!(); }
  expect(requests[0]!.path).toContain("from_sequence_num=88");
  expect(c.inboundSequenceNum("session")).toBe(88);
  epoch = 8;
  const replacement = c.inbound("session", new AbortController().signal)[Symbol.asyncIterator]();
  try { expect((await replacement.next()).value?.id).toBe("input"); }
  finally { await replacement.return!(); }
  expect(requests[1]!.path).not.toContain("from_sequence_num");
  expect(c.inboundSequenceNum("session")).toBe(0);
});

test("SSE refuses reconnect with credentials for a replacement worker epoch", async () => {
  let epoch = 7;
  let requests = 0;
  const c = new CcrClient({
    credentials: () => ({ token: "ingress", workerEpoch: epoch }), sessionIds: () => ["session"],
    sleep: async () => { epoch = 8; },
    request: async () => { requests++; return new Response(null, { status: 503 }); },
  });
  await expect((async () => {
    for await (const _ of c.inbound("session", new AbortController().signal)) {}
  })()).rejects.toThrow("409");
  expect(requests).toBe(1);
});

test("SSE liveness advances only for complete frames, including keepalive comments", async () => {
  let controller!: ReadableStreamDefaultController<Uint8Array>;
  const body = new ReadableStream<Uint8Array>({ start(value) { controller = value; } });
  let frames = 0;
  const iterator = sse(body, undefined, () => { frames++; })[Symbol.asyncIterator]();
  const next = iterator.next();
  controller.enqueue(new TextEncoder().encode(":keepalive\r\n"));
  await Bun.sleep(0);
  expect(frames).toBe(0);
  controller.enqueue(new TextEncoder().encode("\r\n"));
  expect((await next).value?.data).toBeUndefined();
  expect(frames).toBe(1);
  controller.enqueue(new TextEncoder().encode("id: 11\nevent: client_event\ndata: first\ndata: second\n\n"));
  expect((await iterator.next()).value).toEqual({ id: "11", event: "client_event", data: "first\nsecond" });
  expect(frames).toBe(2);
  controller.close();
  expect((await iterator.next()).done).toBe(true);
});

test("delivery processing gates model work without blocking controls", async () => {
  const b = new Backend(); await b.ready;
  const ctx = new Context();
  const api = new OmaApi(b.url);
  const c = await mountOma(ctx, api);
  const session = createOmaSession(ctx, api, b.a);
  const release = Promise.withResolvers<void>();
  const request = c.config.request;
  const deliveries: CcrHttpRequest[] = [];
  c.config.request = async (options) => {
    if (options.path.endsWith("/events/delivery")) {
      deliveries.push(options);
      await release.promise;
      if (deliveries.length === 1) return new Response(null, { status: 503 });
    }
    return request(options);
  };
  let stream: Promise<void> | undefined;
  let cancel: ReturnType<typeof spyOn> | undefined;
  try {
    await session.open();
    cancel = spyOn(session.agent, "cancel");
    b.modelScript = [toolEvents(), textEvents("done")];
    let approvalId: string | undefined;
    b.onApproval = async (request) => { approvalId = request.request_id; };
    stream = consumeInbound(c, session);
    await eventually(() => b.connections === 1);
    b.deliver("input", { type: "user", content: "run" });
    await eventually(() => deliveries.length === 1);
    expect(approvalId).toBeUndefined();
    expect(b.requests).toHaveLength(0);
    b.deliver("orphan", { type: "control_response", response: { subtype: "success", request_id: "missing" } });
    await Bun.sleep(20);
    release.resolve();
    await eventually(() => Boolean(approvalId));
    b.deliver("decision", { type: "control_response", response: {
      subtype: "success", request_id: approvalId,
      response: { behavior: "allow", updatedInput: { command: "approved" } },
    } });
    await eventually(() => session.delivery.completed.includes("input") && session.delivery.outbox.length === 0);
    expect(b.executions).toEqual([{ name: "bash", arguments: { command: "approved" } }]);
    b.deliver("interrupt", { type: "interrupt" });
    await eventually(() => cancel!.mock.calls.length > 0);
    release.resolve();
    await eventually(() => ["input", "orphan", "decision", "interrupt"].every((id) => b.processed.includes(id)));
    expect(deliveries.slice(1).flatMap((delivery) => (delivery.body as { updates: unknown[] }).updates))
      .toEqual(expect.arrayContaining([
        { event_id: "input", status: "received" },
        { event_id: "input", status: "processing" },
      ]));
    for (const inputId of ["orphan", "decision", "interrupt"])
      expect(b.timeline.filter((entry) => entry.type === "input.delivery" && entry.inputId === inputId)
        .map((entry) => entry.status)).toEqual(["received", "processed"]);
    expect(b.timeline.filter((entry) => entry.type === "input.delivery" && entry.inputId === "input")
      .map((entry) => entry.status)).toEqual(["received", "processing", "processed"]);
    const completed = b.timeline.findIndex((entry) => entry.type === "private.persist"
      && !!deliveryOf(entry.payload) && deliveryOf(entry.payload)!.completed.includes("input")
      && deliveryOf(entry.payload)!.outbox.length > 0);
    const published = b.timeline.findIndex((entry) => entry.type === "result");
    const cleared = b.timeline.findIndex((entry) => entry.type === "private.persist"
      && !!deliveryOf(entry.payload) && deliveryOf(entry.payload)!.completed.includes("input")
      && deliveryOf(entry.payload)!.outbox.length === 0);
    const acknowledged = b.timeline.findIndex((entry) => entry.type === "input.ack" && entry.inputId === "input");
    expect(completed).toBeGreaterThan(-1);
    expect(published).toBeGreaterThan(completed);
    expect(cleared).toBeGreaterThan(published);
    expect(acknowledged).toBeGreaterThan(cleared);
  } finally {
    release.resolve(); cancel?.mockRestore(); session.stop();
    await stream; await session.close(); await ctx.fiber.dispose(); b.stop();
  }
});

test("SSE reconnect services approvals during an active turn and preserves journal barriers", async () => {
  const b = new Backend(); await b.ready;
  const ctx = new Context();
  const api = new OmaApi(b.url);
  const c = await mountOma(ctx, api);
  const session = createOmaSession(ctx, api, b.a);
  let stream: Promise<void> | undefined;
  try {
    expect(await c.register(session.id, b.a.ingress_token)).toBe(1);
    await session.open();
    stream = consumeInbound(c, session);
    b.modelScript = [toolEvents(), textEvents("done")];
    let approval: any;
    b.onApproval = async (request) => { approval = request; };
    await eventually(() => b.connections === 1);
    b.deliver("input", { type: "user", content: "run" });
    await eventually(() => Boolean(approval));
    expect(b.timeline.filter((entry) => entry.type === "input.delivery" && entry.inputId === "input")
      .map((entry) => entry.status)).toEqual(["received", "processing"]);
    b.disconnect();
    await eventually(() => b.connections === 2);
    expect(session.busy).toBe(true);
    b.deliver("decision", { type: "control_response", response: {
      subtype: "success", request_id: approval.request_id,
      response: { behavior: "allow", updatedInput: { command: "approved" } },
    } });
    await eventually(() => session.delivery.completed.includes("input") && b.processed.includes("input"));
    expect(b.registrations).toBe(1);
    expect(b.executions).toEqual([{ name: "bash", arguments: { command: "approved" } }]);
    expect(b.processed).toContain("decision");
    expect(b.processed).not.toContain("42");
    expect(b.timeline.filter((entry) => entry.type === "input.delivery" && entry.inputId === "decision")
      .map((entry) => entry.status)).toEqual(["received", "processed"]);
    const first = (fn: (entry: any) => boolean) => b.timeline.findIndex(fn);
    expect(first((e) => e.type === "private.persist" && e.payload.data?.phase === "accepted"))
      .toBeLessThan(first((e) => e.type === "input.ack"));
    expect(first((e) => e.type === "private.persist" && e.payload.data?.phase === "approved"))
      .toBeLessThan(first((e) => e.type === "sandbox.prepare"));
    expect(first((e) => e.type === "private.persist" && e.payload.data?.phase === "dispatch"))
      .toBeLessThan(first((e) => e.type === "tool.dispatch"));
    // The reconnect may already have redelivered this still-processing input.
    await eventually(() => b.processed.filter((id) => id === "input").length === 2);
    const acknowledged = b.processed.filter((id) => id === "input").length;
    b.deliver("input", { type: "user", content: "duplicate" });
    await eventually(() => b.processed.filter((id) => id === "input").length > acknowledged);
    expect(b.requests).toHaveLength(2);
  } finally { session.stop(); await stream; await session.close(); await ctx.fiber.dispose(); b.stop(); }
});

test("queued input waits for the current turn while interrupt bypasses a pending processing receipt", async () => {
  const b = new Backend(); await b.ready;
  const ctx = new Context();
  const api = new OmaApi(b.url);
  const c = await mountOma(ctx, api);
  const session = createOmaSession(ctx, api, b.a);
  const receive = spyOn(session, "receive");
  const release = Promise.withResolvers<void>();
  const request = c.config.request;
  let waiting = false;
  c.config.request = async (options) => {
    if (options.path.endsWith("/events/delivery") && JSON.stringify(options.body).includes('"processing"') && !waiting) {
      waiting = true;
      await release.promise;
    }
    return request(options);
  };
  let stream: Promise<void> | undefined;
  try {
    await session.open();
    b.modelScript = [textEvents("second done")];
    stream = consumeInbound(c, session);
    await eventually(() => b.connections === 1);
    b.deliver("first", { type: "user", content: "first" });
    await eventually(() => waiting);
    b.deliver("second", { type: "user", content: "second" });
    b.deliver("second", { type: "user", content: "second" });
    const cancel = spyOn(session.agent, "cancel");
    b.deliver("cancel-first", { type: "interrupt" });
    await eventually(() => cancel.mock.calls.length > 0);
    expect(session.delivery.inputId).toBe("first");
    expect(b.requests).toHaveLength(0);
    expect(b.processed).not.toContain("second");
    release.resolve();
    await eventually(() => b.processed.includes("first") && b.processed.filter((id) => id === "second").length === 2);
    await Promise.all(receive.mock.results.map((result) => result.value));
    expect(session.delivery.completed).toContain("first");
    expect(session.delivery.completed).toContain("second");
    expect(b.requests).toHaveLength(1);
    expect(b.timeline.filter((entry) => entry.type === "input.delivery" && entry.status === "processing")
      .map((entry) => entry.inputId)).toEqual(["first", "second"]);
  } finally {
    release.resolve();
    session.stop();
    await stream;
    await session.close();
    await ctx.fiber.dispose();
    b.stop();
  }
});

test("active input waits for its own turn and duplicate delivery cannot overtake queued input", async () => {
  const b = new Backend(); await b.ready;
  const ctx = new Context();
  const api = new OmaApi(b.url);
  const c = await mountOma(ctx, api);
  const session = createOmaSession(ctx, api, b.a);
  const tool = Promise.withResolvers<void>();
  const processing = Promise.withResolvers<void>();
  const request = c.config.request;
  let waiting = false;
  c.config.request = async (options) => {
    if (options.path.endsWith("/events/delivery") && JSON.stringify(options.body).includes('"second"')
      && JSON.stringify(options.body).includes('"processing"')) {
      waiting = true;
      await processing.promise;
    }
    return request(options);
  };
  let started = false;
  let stream: Promise<void> | undefined;
  b.modelScript = [toolEvents(), textEvents("first answer"), textEvents("second answer"), textEvents("third answer")];
  b.onTool = async () => {
    started = true;
    await tool.promise;
    return { content: [{ type: "text", text: "first answer" }] };
  };
  b.onApproval = async (request) => {
    await session.control({ type: "control_response", response: {
      subtype: "success", request_id: request.request_id, response: { behavior: "allow" },
    } });
  };
  try {
    await session.open();
    stream = consumeInbound(c, session);
    await eventually(() => b.connections === 1);
    b.deliver("first", { type: "user", content: "first user" });
    await eventually(() => started);
    b.deliver("second", { type: "user", content: "second user" });
    b.deliver("second", { type: "user", content: "second user" });
    b.deliver("third", { type: "user", content: "third user" });
    await eventually(() => b.timeline.some((entry) => entry.type === "input.delivery"
      && entry.inputId === "third" && entry.status === "received"));
    expect(waiting).toBe(false);
    expect(session.delivery.inputId).toBe("first");
    expect(b.requests).toHaveLength(1);
    tool.resolve();
    await eventually(() => waiting);
    expect(b.processed).toContain("first");
    expect(b.processed).not.toContain("second");
    expect(session.delivery.completed).toEqual(["first"]);
    expect(b.requests).toHaveLength(2);
    const firstHistory = JSON.stringify(b.requests[1]!.messages);
    expect(firstHistory).not.toContain("second user");
    expect(firstHistory).not.toContain("third user");
    processing.resolve();
    await eventually(() => b.processed.includes("third") && !session.busy);
    const secondHistory = JSON.stringify(b.requests[2]!.messages);
    expect(secondHistory).toContain("first answer");
    expect(secondHistory).toContain("second user");
    expect(secondHistory).not.toContain("third user");
    const thirdHistory = JSON.stringify(b.requests[3]!.messages);
    expect(thirdHistory.indexOf("third user")).toBeGreaterThan(thirdHistory.indexOf("second answer"));
    expect(thirdHistory.split("second user")).toHaveLength(2);
    expect(b.requests).toHaveLength(4);
    expect(b.executions).toHaveLength(1);
    expect(session.delivery.completed).toEqual(["first", "second", "third"]);
    expect(b.processed.filter((id) => id === "second")).toHaveLength(2);
    expect(b.timeline.filter((e) => e.type === "result")).toHaveLength(3);
    expect(b.timeline.filter((e) => e.type === "input.delivery" && e.status === "processing")
      .map((e) => e.inputId)).toEqual(["first", "second", "third"]);
  } finally {
    tool.resolve(); processing.resolve(); session.stop(); await stream;
    await session.close(); await ctx.fiber.dispose(); b.stop();
  }
});
