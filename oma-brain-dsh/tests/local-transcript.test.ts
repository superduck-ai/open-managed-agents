import { afterEach, expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { SESSION_FORMAT_VERSION, SessionId, SessionSeq, type SessionEvent, type SessionHeader } from "@deepseek-ai/dsh-session";
import { mkdtemp, readFile, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import JsonlPersistence from "@deepseek-ai/dsh-session-persistence-jsonl";
import { CcrHttpError } from "../src/ccrv2/client.ts";
import { CcrReplication } from "../src/ccrv2/persistence.ts";
import type { CcrBackend } from "../src/ccrv2/contracts.ts";
import type { Payload } from "../src/ccrv2/protocol.ts";

const cleanups: (() => Promise<unknown>)[] = [];
afterEach(async () => {
  for (const cleanup of cleanups.splice(0).reverse()) await cleanup();
});

function backend() {
  const logs = new Map<string, Payload[]>();
  const store: CcrBackend = {
    async getWorkerState() { return { metadata: null, durationMs: 0 }; },
    async isToolConfirmationPending() { return false; },
    async privateEvents(id, options) {
      return structuredClone((logs.get(id) ?? []).filter((entry) => !!entry.agent_id === !!options?.subagents));
    },
    async persist(id, subtype, data, agentId) {
      logs.set(id, [...(logs.get(id) ?? []), structuredClone({ subtype, schema_version: 1, data, agent_id: agentId })]);
    },
    async events() {},
    async acknowledge() {},
    async reportDelivery() {},
    async setStatus() {},
    async reportMetadata() {},
    sessionIds() { return logs.keys(); },
  };
  return { store, logs };
}

function header(id = "main"): SessionHeader {
  return { id: SessionId(id), version: SESSION_FORMAT_VERSION, createdAt: 1, cwd: "/workspace", isSeeded: false };
}

function event(seq = 0): SessionEvent {
  return seq === 0
    ? { type: "turn/start", seq: SessionSeq(seq), time: 2, data: { turn: 1 } }
    : { type: "turn/end", seq: SessionSeq(seq), time: 3, data: { turn: 1, reason: { kind: "completed" } } };
}

async function setup(store = backend().store, localRoot?: string) {
  const root = localRoot ?? await mkdtemp(join(tmpdir(), "brain-transcript-"));
  if (!localRoot) cleanups.push(() => rm(root, { recursive: true, force: true }));
  const ctx = new Context();
  cleanups.push(() => ctx.fiber.dispose());
  await ctx.plugin(JsonlPersistence, { root, compression: "none" });
  await ctx.plugin(CcrReplication, { backend: store });
  return { ctx, root, persistence: ctx.sessionPersistence, replication: ctx.ccrReplication };
}

async function transcripts(root: string): Promise<{ path: string; lines: unknown[] }[]> {
  const entries = await readdir(root, { withFileTypes: true });
  const results = await Promise.all(entries.map(async (entry) => {
    const path = join(root, entry.name);
    if (entry.isDirectory()) return transcripts(path);
    if (!entry.name.endsWith(".jsonl")) return [];
    const content = await readFile(path, "utf8");
    return [{ path, lines: content.trimEnd().split("\n").map((line) => JSON.parse(line)) }];
  }));
  return results.flat();
}

test("remote failures do not block native writes but reject confirmation", async () => {
  const { store } = backend();
  const { persistence, replication, root } = await setup(store);
  const handle = await persistence.create(header());
  const persist = store.persist;
  store.persist = async () => { throw new Error("remote unavailable"); };
  await handle.append([event()]);
  await handle.flush();
  expect((await handle.read()).events).toEqual([event()]);
  await expect(replication.flush(handle.id)).rejects.toThrow("remote unavailable");
  expect((await transcripts(root))[0]!.lines.slice(1)).toEqual([event()]);
  store.persist = persist;
  await replication.flush(handle.id);
  await replication.flush(handle.id);
  expect(await store.privateEvents(handle.id)).toHaveLength(1);
  await handle.close();
});

test("divergent history refuses confirmation without altering the local file", async () => {
  const { store, logs } = backend();
  logs.set("main", [{ subtype: "dsh_log", schema_version: 1, data: {
    header: header(), inherited: 0, events: [{ ...event(), time: 99 }],
  } }]);
  const { persistence, replication, root } = await setup(store);
  const handle = await persistence.create(header());
  await handle.append([event()]);
  await expect(replication.flush(handle.id)).rejects.toThrow("diverged");
  expect((await transcripts(root))[0]!.lines.slice(1)).toEqual([event()]);
  expect(logs.get("main")).toHaveLength(1);
  await handle.close();
});

test("existing local history resumes despite remote read failure at the same path", async () => {
  const { store } = backend();
  const { persistence, replication, root } = await setup(store);
  const handle = await persistence.create(header());
  await handle.append([event()]);
  await handle.close();
  const original = (await transcripts(root))[0]!.path;
  replication.forget(handle.id);
  store.privateEvents = async () => { throw new Error("remote read failed"); };
  await replication.ensure(handle.id);
  const reopened = await persistence.open(handle.id, "write");
  expect((await reopened.read()).events).toEqual([event()]);
  await reopened.append([event(1)]);
  await reopened.close();
  expect((await transcripts(root)).map(file => file.path)).toEqual([original]);
});

test("failed local append never uploads the rejected event", async () => {
  const { store, logs } = backend();
  const { persistence, replication } = await setup(store);
  const handle = await persistence.create(header());
  await expect(handle.append([event(1)])).rejects.toThrow();
  await replication.flush(handle.id);
  expect(logs.get("main")!.flatMap(entry => entry.data.events)).toEqual([]);
  await handle.close();
});

test("confirmation waits for backend persistence while native flush remains independent", async () => {
  const { store } = backend();
  const gate = Promise.withResolvers<void>();
  const persist = store.persist;
  let uploaded = false;
  store.persist = async (...args) => { await gate.promise; await persist(...args); uploaded = true; };
  const { persistence, replication } = await setup(store);
  const handle = await persistence.create(header());
  await handle.append([event()]);
  await persistence.flush();
  let confirmed = false;
  const confirmation = replication.flush(handle.id).then(() => { confirmed = true; });
  await new Promise(resolve => setTimeout(resolve, 10));
  expect(confirmed).toBe(false);
  gate.resolve();
  await confirmation;
  expect(uploaded).toBe(true);
  await handle.close();
});

test("native session events replicate asynchronously and retry without local duplication", async () => {
  const { store } = backend();
  const persist = store.persist;
  let attempts = 0;
  store.persist = async (...args) => {
    if (++attempts === 1) throw new Error("temporary");
    await persist(...args);
  };
  const { ctx, persistence } = await setup(store);
  const handle = await persistence.create(header());
  ctx.emit("session/event", { id: handle.id } as never, event());
  await new Promise(resolve => setTimeout(resolve, 250));
  expect(attempts).toBeGreaterThanOrEqual(2);
  expect((await handle.read()).events).toEqual([event()]);
  expect((await store.privateEvents(handle.id)).flatMap(entry => entry.data.events)).toEqual([event()]);
  await handle.close();
});

test("missing local foreground and child logs hydrate only once and replicate child tails", async () => {
  const { store, logs } = backend();
  const first = await setup(store);
  first.replication.bindThread("child", "main");
  for (const id of ["main", "child"]) {
    const handle = await first.persistence.create(header(id));
    await handle.append([event()]);
    await handle.close();
  }
  await first.replication.flush("main");
  const second = await setup(store);
  second.replication.bindThread("child", "main");
  for (const id of ["main", "child"]) {
    await second.replication.ensure(id);
    await second.replication.ensure(id);
    const handle = await second.persistence.open(SessionId(id), "write");
    expect((await handle.read()).events).toEqual([event()]);
    await handle.append([event(1)]);
    await handle.close();
  }
  await second.replication.flush("main");
  expect(await transcripts(second.root)).toHaveLength(2);
  expect(logs.get("main")!.filter(entry => entry.agent_id === "child").at(-1)!.data.events).toEqual([event(1)]);
});

test("remote prefix comparison ignores object property order", async () => {
  const { store, logs } = backend();
  logs.set("main", [{ subtype: "dsh_log", schema_version: 1, data: {
    header: header(), inherited: 0, events: [{ data: { turn: 1 }, time: 2, seq: 0, type: "turn/start" }],
  } }]);
  const { persistence, replication } = await setup(store);
  const handle = await persistence.create(header());
  await handle.append([event()]);
  await replication.flush(handle.id);
  expect(logs.get("main")).toHaveLength(1);
  await handle.close();
});

test("legacy remote state requires migration before hydration", async () => {
  const { store, logs } = backend();
  logs.set("main", [{ subtype: "dsh_brain_state", schema_version: 1, data: {} }]);
  const { persistence, replication } = await setup(store);
  await expect(replication.ensure("main")).rejects.toThrow("requires migration");
  expect(await persistence.stat(SessionId("main"))).toBeUndefined();
});

test("native JSONL locks reject a second writer sharing the same root", async () => {
  const first = await setup();
  const handle = await first.persistence.create(header());
  await handle.flush();
  const second = await setup(backend().store, first.root);
  await expect(second.persistence.open(handle.id, "write")).rejects.toThrow();
  await handle.close();
  await (await second.persistence.open(handle.id, "write")).close();
});

test("disposing replication cancels background retry timers", async () => {
  const { store } = backend();
  let attempts = 0;
  store.persist = async () => { attempts++; throw new Error("offline"); };
  const { ctx, persistence } = await setup(store);
  const handle = await persistence.create(header());
  ctx.emit("session/event", { id: handle.id } as never, event());
  await new Promise(resolve => setTimeout(resolve, 25));
  expect(attempts).toBeGreaterThan(0);
  await ctx.fiber.dispose();
  const stoppedAttempts = attempts;
  await new Promise(resolve => setTimeout(resolve, 150));
  expect(attempts).toBe(stoppedAttempts);
});

test.each([
  new CcrHttpError(409), new CcrHttpError(410),
  new CcrHttpError(401, undefined, true), new CcrHttpError(403, undefined, false, true),
])("terminal lease rejection disables background retries and confirmation until forget: %s", async error => {
  const { store } = backend();
  let attempts = 0;
  const persist = store.persist;
  store.persist = async () => { attempts++; throw error; };
  const { ctx, persistence, replication } = await setup(store);
  const handle = await persistence.create(header());
  ctx.emit("session/event", { id: handle.id } as never, event());
  await new Promise(resolve => setTimeout(resolve, 150));
  expect(attempts).toBe(1);
  await expect(replication.flush(handle.id)).rejects.toBe(error);
  expect(attempts).toBe(1);
  await handle.close();
  replication.forget(handle.id);
  store.persist = persist;
  await replication.flush(handle.id);
  expect((await store.privateEvents(handle.id)).flatMap(entry => entry.data.events)).toEqual([event()]);
});

test("forget fences queued work before its remote write and cannot resurrect its retry", async () => {
  const { store, logs } = backend();
  const gate = Promise.withResolvers<void>();
  let reading = false;
  store.privateEvents = async () => { reading = true; await gate.promise; return []; };
  const { ctx, persistence, replication } = await setup(store);
  const handle = await persistence.create(header());
  await handle.append([event()]);
  const confirmation = replication.flush(handle.id);
  void confirmation.catch(() => {});
  while (!reading) await new Promise(resolve => setTimeout(resolve, 1));
  replication.forget(handle.id);
  gate.resolve();
  await expect(confirmation).rejects.toThrow("replication is stopped");
  expect(logs.size).toBe(0);
  ctx.emit("session/flush", { id: handle.id } as never);
  await new Promise(resolve => setTimeout(resolve, 150));
  expect(logs.size).toBe(0);
  await handle.close();
});

test("drain waits for an already sent remote write and blocks later retries", async () => {
  const { store } = backend();
  const gate = Promise.withResolvers<void>();
  let writing = false;
  let attempts = 0;
  store.persist = async () => { writing = true; attempts++; await gate.promise; };
  const { ctx, persistence, replication } = await setup(store);
  const handle = await persistence.create(header());
  ctx.emit("session/event", { id: handle.id } as never, event());
  while (!writing) await new Promise(resolve => setTimeout(resolve, 1));
  let drained = false;
  const draining = replication.drain(handle.id).then(() => { drained = true; });
  await new Promise(resolve => setTimeout(resolve, 10));
  expect(drained).toBe(false);
  gate.resolve();
  await draining;
  expect(drained).toBe(true);
  ctx.emit("session/flush", { id: handle.id } as never);
  await new Promise(resolve => setTimeout(resolve, 150));
  expect(attempts).toBe(1);
  await handle.close();
});
