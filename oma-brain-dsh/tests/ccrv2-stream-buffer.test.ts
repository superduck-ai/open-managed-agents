import { expect, test } from "bun:test";
import { CcrClient, type CcrHttpRequest } from "../src/ccrv2/client.ts";
import type { ClientEvent } from "../src/ccrv2/wire.ts";

class ManualClock {
  now = 0;
  readonly tasks = new Set<{ at: number; callback: () => void }>();
  readonly schedule = (callback: () => void, delayMs: number) => {
    expect(delayMs).toBe(100);
    const task = { at: this.now + delayMs, callback };
    this.tasks.add(task);
    return () => { this.tasks.delete(task); };
  };
  advance(ms: number) {
    this.now += ms;
    for (const task of [...this.tasks]) {
      if (task.at <= this.now && this.tasks.delete(task)) task.callback();
    }
  }
}

const settle = () => new Promise<void>((resolve) => setImmediate(resolve));
const events = (request: CcrHttpRequest) => (request.body as { events: ClientEvent[] }).events;
const delta = (text: string, uuid = `delta-${text}`) => ({ type: "event_delta", uuid, event_id: "message",
  delta: { type: "content_delta", content: { type: "text", text } } });

function fixture(send: (request: CcrHttpRequest) => Promise<Response> = async () => new Response(null, { status: 204 })) {
  const clock = new ManualClock();
  const credentials = { token: "token-7", workerEpoch: 7 };
  const requests: CcrHttpRequest[] = [];
  const client = new CcrClient({ credentials: () => credentials, sessionIds: () => ["s"],
    request: async (request) => { requests.push(request); return send(request); },
    sleep: async () => {}, random: () => 0, schedule: clock.schedule });
  return { clock, client, requests, credentials };
}

test("a fixed 100ms window batches raw start/delta events and still waits for HTTP confirmation", async () => {
  const response = Promise.withResolvers<void>();
  const { client, clock, requests, credentials } = fixture(async () => { await response.promise; return new Response(null, { status: 204 }); });
  const first = [{ type: "event_start", uuid: "start", event: { type: "agent.message", id: "message" } }, delta("A")];
  const child = { ...delta("B"), session_thread_id: "child" };
  const end = delta("Z", "last");
  const calls = [client.events("s", first)];
  clock.advance(40); calls.push(client.events("s", [child]));
  clock.advance(40); calls.push(client.events("s", [end]));
  let confirmed = false;
  const pending = Promise.all(calls).then(() => { confirmed = true; });
  try {
    expect(clock.tasks.size).toBe(1);
    clock.advance(19); await settle();
    expect(requests).toHaveLength(0);
    expect(confirmed).toBe(false);
    credentials.token = "refreshed-token";
    clock.advance(1); await settle();
    expect(requests).toHaveLength(1);
    expect(requests[0]!.token).toBe("refreshed-token");
    expect(events(requests[0]!)).toEqual([...first, child, end].map((payload) => ({ payload, ephemeral: true })));
    expect(confirmed).toBe(false);
    response.resolve(); await pending;
    expect(confirmed).toBe(true);
    expect(clock.tasks.size).toBe(0);
    const next = client.events("s", [delta("C")]);
    clock.advance(100); await next;
    expect(events(requests[1]!)).toEqual([{ payload: delta("C"), ephemeral: true }]);
  } finally { response.resolve(); clock.advance(100); await pending; }
});

test("non-stream output flushes earlier deltas and a cancelled timer cannot flush the next window", async () => {
  const { client, clock, requests } = fixture();
  const preview = client.events("s", [delta("A")]);
  const stale = [...clock.tasks][0]!.callback;
  clock.advance(20);
  const terminal = client.events("s", [{ type: "agent.message", uuid: "terminal" }]);
  await Promise.all([preview, terminal]);
  expect(requests).toHaveLength(1);
  expect(events(requests[0]!).map((event) => event.payload.uuid)).toEqual(["delta-A", "terminal"]);
  expect(clock.tasks.size).toBe(0);
  const next = client.events("s", [delta("B")]);
  stale(); await settle();
  clock.advance(99); await settle();
  expect(requests).toHaveLength(1);
  clock.advance(1); await next;
  expect(requests).toHaveLength(2);
});

test("in-flight requests neither bypass a later stream window nor add another delay after it expires", async () => {
  for (const releaseEarly of [false, true]) {
    const firstResponse = Promise.withResolvers<void>();
    let sends = 0;
    const { client, clock, requests } = fixture(async () => {
      if (++sends === 1) await firstResponse.promise;
      return new Response(null, { status: 204 });
    });
    const first = client.events("s", [delta("A")]);
    clock.advance(100);
    const second = client.events("s", [delta("B")]);
    const pending = Promise.all([first, second]);
    try {
      clock.advance(50);
      if (releaseEarly) { firstResponse.resolve(); await first; await settle(); }
      expect(requests).toHaveLength(1);
      clock.advance(49); await settle();
      expect(requests).toHaveLength(1);
      clock.advance(1); await settle();
      if (!releaseEarly) { expect(requests).toHaveLength(1); firstResponse.resolve(); }
      await pending;
      expect(requests).toHaveLength(2);
      expect(clock.tasks.size).toBe(0);
    } finally { firstResponse.resolve(); clock.advance(100); await pending; }
  }
});

test("one stream window splits at 100 events without delaying each batch or changing the snapshot", async () => {
  const { client, clock, requests } = fixture();
  const payloads = Array.from({ length: 251 }, (_, index) => delta(String(index)));
  const expected = structuredClone(payloads);
  const pending = client.events("s", payloads);
  payloads[0]!.delta.content.text = "mutated";
  expect(requests).toEqual([]);
  clock.advance(100); await pending;
  expect(requests.map((request) => events(request).length)).toEqual([100, 100, 51]);
  expect(requests.flatMap((request) => events(request).map((event) => event.payload))).toEqual(expected);
  expect(clock.tasks.size).toBe(0);
});

test("individual and session cancellation discard buffered events and clear unused timers", async () => {
  const { client, clock, requests } = fixture();
  const detach = client.bindLease("s").subscribe!(() => {});
  const owner = new AbortController();
  const cancelled = client.events("s", [delta("cancelled")], owner.signal).catch((error: unknown) => error);
  const kept = client.events("s", [delta("kept")]);
  const stopped = new Error("Call cancelled");
  owner.abort(stopped);
  expect(await cancelled).toBe(stopped);
  expect(clock.tasks.size).toBe(1);
  clock.advance(100); await kept;
  expect(events(requests[0]!).map((event) => event.payload.uuid)).toEqual(["delta-kept"]);
  const pending = client.events("s", [delta("old-session")]);
  const rejected = pending.catch((error: unknown) => error);
  const stale = [...clock.tasks][0]!.callback;
  detach();
  expect(await rejected).toBeInstanceOf(Error);
  expect(clock.tasks.size).toBe(0);
  const detachReplacement = client.bindLease("s").subscribe!(() => {});
  try {
    const next = client.events("s", [delta("new-session")]);
    stale(); clock.advance(99); await settle();
    expect(requests).toHaveLength(1);
    clock.advance(1); await next;
    expect(requests).toHaveLength(2);
    expect(events(requests[1]!).map((event) => event.payload.uuid)).toEqual(["delta-new-session"]);
  } finally { detachReplacement(); }
});

test("cancelling the only buffered call starts a fresh window for subsequent output", async () => {
  const { client, clock, requests } = fixture();
  const owner = new AbortController();
  const cancelled = client.events("s", [delta("cancelled")], owner.signal).catch((error: unknown) => error);
  clock.advance(50); owner.abort(); await cancelled;
  expect(clock.tasks.size).toBe(0);
  const next = client.events("s", [delta("next")]);
  clock.advance(50); await settle();
  expect(requests).toHaveLength(0);
  clock.advance(50); await next;
  expect(requests).toHaveLength(1);
});

test("epoch changes while buffering cannot send old events with new credentials", async () => {
  const { client, clock, requests, credentials } = fixture();
  const pending = client.events("s", [delta("old")]);
  const cancelled = pending.catch((error: unknown) => error);
  credentials.workerEpoch = 8; credentials.token = "new-epoch-token";
  clock.advance(100);
  expect(await cancelled).toMatchObject({ status: 409 });
  expect(requests).toHaveLength(0);
  const next = client.events("s", [delta("new")]);
  clock.advance(100); await next;
  expect(requests).toHaveLength(1);
  expect(requests[0]!.body).toMatchObject({ worker_epoch: 8 });
  expect(events(requests[0]!).map((event) => event.payload.uuid)).toEqual(["delta-new"]);
});

test("stream windows and non-stream flushes are isolated between sessions", async () => {
  const { client, clock, requests } = fixture();
  const first = client.events("s", [delta("A")]);
  clock.advance(50);
  const second = client.events("other", [delta("B")]);
  await client.events("s", [{ type: "control_response" }]); await first;
  expect(requests).toHaveLength(1);
  expect(clock.tasks.size).toBe(1);
  clock.advance(99); await settle();
  expect(requests).toHaveLength(1);
  clock.advance(1); await second;
  expect(requests[1]!.path).toContain("/other/");
});
