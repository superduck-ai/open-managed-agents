import { afterEach, expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import SessionStore, { SESSION_FORMAT_VERSION, Session, SessionId, SessionSeq, type SessionEvent, type SessionHeader } from "@deepseek-ai/dsh-session";
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
      return structuredClone((logs.get(id) ?? []).filter(entry => options?.agentId !== undefined ? entry.agent_id === options.agentId : options?.subagents || !entry.agent_id));
    },
    async persist(id, type, data, agentId) {
      logs.set(id, [...(logs.get(id) ?? []), structuredClone({ type, data, agent_id: agentId })]);
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

function nativeRecords(header: SessionHeader, events: SessionEvent[]): Payload[] {
  return [header, ...events].map(data => ({ type: "dsh_log", data }));
}

function remoteEvents(entries: Payload[]): SessionEvent[] {
  return entries.filter(entry => entry.type === "dsh_log" && "seq" in entry.data).map(entry => entry.data);
}

async function setup(store = backend().store, localRoot?: string) {
  const root = localRoot ?? await mkdtemp(join(tmpdir(), "brain-transcript-"));
  if (!localRoot) cleanups.push(async () => { await rm(root, { recursive: true, force: true }); });
  const ctx = new Context();
  cleanups.push(() => ctx.fiber.dispose());
  await ctx.plugin(JsonlPersistence, { root, compression: "none" });
  await ctx.plugin(SessionStore);
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
  expect(await store.privateEvents(handle.id)).toHaveLength(2);
  await handle.close();
});

test("divergent history refuses confirmation without altering the local file", async () => {
  const { store, logs } = backend();
  logs.set("main", nativeRecords(header(), [{ ...event(), time: 99 }]));
  const { persistence, replication, root } = await setup(store);
  const handle = await persistence.create(header());
  await handle.append([event()]);
  await expect(replication.flush(handle.id)).rejects.toThrow("diverged");
  expect((await transcripts(root))[0]!.lines.slice(1)).toEqual([event()]);
  expect(logs.get("main")).toHaveLength(2);
  await handle.close();
});

test("remote reconciliation failure preserves the existing local transcript at its path", async () => {
  const { store } = backend();
  const { persistence, replication, root } = await setup(store);
  const handle = await persistence.create(header());
  await handle.append([event()]);
  await handle.close();
  const original = (await transcripts(root))[0]!.path;
  replication.forget(handle.id);
  store.privateEvents = async () => { throw new Error("remote read failed"); };
  await expect(replication.ensure(handle.id)).rejects.toThrow("remote read failed");
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
  expect(remoteEvents(logs.get("main")!)).toEqual([]);
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
  const live = ctx.sessions.create(SessionId("main"), { meta: header() });
  const handle = await persistence.create(live.header);
  live.append("turn/start", { turn: 1 });
  await new Promise(resolve => setTimeout(resolve, 250));
  expect(attempts).toBeGreaterThanOrEqual(2);
  expect((await handle.read()).events).toEqual([...live.snapshotEvents()]);
  expect(remoteEvents(await store.privateEvents(handle.id))).toEqual([...live.snapshotEvents()]);
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
    expect(await second.replication.ensure(id)).toBe(true);
    expect(await second.replication.ensure(id)).toBe(false);
    const handle = await second.persistence.open(SessionId(id), "write");
    expect((await handle.read()).events).toEqual([event()]);
    await handle.append([event(1)]);
    await handle.close();
  }
  await second.replication.flush("main");
  expect(await transcripts(second.root)).toHaveLength(2);
  expect(logs.get("main")!.filter(entry => entry.agent_id === "child").at(-1)!.data).toEqual(event(1));
});

test("remote prefix comparison ignores object property order", async () => {
  const { store, logs } = backend();
  logs.set("main", nativeRecords(header(), [{ data: { turn: 1 }, time: 2, seq: SessionSeq(0), type: "turn/start" }]));
  const { persistence, replication } = await setup(store);
  const handle = await persistence.create(header());
  await handle.append([event()]);
  await replication.flush(handle.id);
  expect(logs.get("main")).toHaveLength(2);
  await handle.close();
});

test("legacy remote state requires migration before hydration", async () => {
  const { store, logs } = backend();
  logs.set("main", [{ subtype: "dsh_brain_state", schema_version: 1, data: {} }]);
  const { persistence, replication } = await setup(store);
  await expect(replication.ensure("main")).rejects.toThrow("Unsupported persistence format");
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
  expect(remoteEvents(await store.privateEvents(handle.id))).toEqual([event()]);
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

test.each([
  { type: "dsh_log", data: { header: header(), inherited: 0, events: [] } },
  { subtype: "dsh_log", schema_version: 1, data: header() },
  { type: "grok_checkpoint", data: {} },
])("native hydration rejects older envelope forms: %j", async payload => {
  const { store, logs } = backend();
  logs.set("main", [payload]);
  const { persistence, replication } = await setup(store);
  await expect(replication.ensure("main")).rejects.toThrow("Unsupported persistence format");
  expect(await persistence.stat(SessionId("main"))).toBeUndefined();
});

test("native event retry survives an ambiguous confirmation and preserves an extended local tail", async () => {
  const { store } = backend();
  const persist = store.persist;
  let failed = false;
  store.persist = async (...args) => {
    await persist(...args);
    if (!failed && "seq" in args[2]) {
      failed = true;
      throw new Error("confirmation lost");
    }
  };
  const first = await setup(store);
  const handle = await first.persistence.create(header());
  await handle.append([event()]);
  await expect(first.replication.flush(handle.id)).rejects.toThrow("confirmation lost");
  await handle.append([event(1)]);
  await first.replication.flush(handle.id);
  await handle.close();
  const recovered = await setup(store);
  expect(await recovered.replication.ensure(handle.id)).toBe(true);
  const reader = await recovered.persistence.open(handle.id, "read");
  expect((await reader.read()).events).toEqual([event(), event(1)]);
  await reader.close();
});

test("native hydration scopes each child header and event independently from siblings", async () => {
  const { store, logs } = backend();
  logs.set("main", [
    ...nativeRecords(header("main"), [event()]),
    ...nativeRecords(header("child"), [event()]).map(entry => ({ ...entry, agent_id: "child" })),
    ...nativeRecords(header("sibling"), [event(), event(1)]).map(entry => ({ ...entry, agent_id: "sibling" })),
  ]);
  const { persistence, replication } = await setup(store);
  replication.bindThread("child", "main");
  expect(await replication.ensure("child")).toBe(true);
  const reader = await persistence.open(SessionId("child"), "read");
  expect((await reader.read()).events).toEqual([event()]);
  await reader.close();
  expect(await persistence.stat(SessionId("sibling"))).toBeUndefined();
});

test("reacquisition refuses to repair a transcript while a native writer owns it", async () => {
  const { store, logs } = backend();
  logs.set("main", nativeRecords(header(), [event(), event(1)]));
  const { persistence, replication } = await setup(store);
  const writer = await persistence.create(header());
  await writer.append([event()]);
  await expect(replication.ensure("main")).rejects.toThrow("already");
  expect((await writer.read()).events).toEqual([event()]);
  await writer.close();
  expect(await replication.ensure("main")).toBe(true);
  const reader = await persistence.open(SessionId("main"), "read");
  expect((await reader.read()).events).toEqual([event(), event(1)]);
  await reader.close();
});

test("reacquisition preserves a longer local prefix and uploads only its unconfirmed tail", async () => {
  const { store, logs } = backend();
  logs.set("main", nativeRecords(header(), [event()]));
  const { persistence, replication, root } = await setup(store);
  const writer = await persistence.create(header());
  await writer.append([event(), event(1)]);
  await writer.close();
  const path = (await transcripts(root))[0]!.path;
  const before = await readFile(path);
  expect(await replication.ensure("main")).toBe(false);
  expect(await replication.ensure("main")).toBe(false);
  expect(await readFile(path)).toEqual(before);
  await replication.flush("main");
  expect(logs.get("main")).toHaveLength(3);
  expect(logs.get("main")!.at(-1)!.data).toEqual(event(1));
});

test("divergent acquisition preserves the original directory and releases native ownership", async () => {
  const { store, logs } = backend();
  logs.set("main", nativeRecords(header(), [{ ...event(), time: 99 }]));
  const { persistence, replication, root } = await setup(store);
  const original = await persistence.create(header());
  await original.append([event()]);
  await original.close();
  const old = (await transcripts(root))[0]!;
  const originalBytes = await readFile(old.path);
  await expect(replication.ensure("main")).rejects.toThrow("Local and remote log histories diverged");
  expect((await transcripts(root)).map(entry => entry.path)).toEqual([old.path]);
  expect(await readFile(old.path)).toEqual(originalBytes);
  const writer = await persistence.open(SessionId("main"), "write");
  expect((await writer.read()).events).toEqual([event()]);
  const other = await setup(store, root);
  await expect(other.persistence.open(SessionId("main"), "write")).rejects.toThrow("already");
  await writer.close();
  await (await other.persistence.open(SessionId("main"), "write")).close();
});

test("a late created session cannot restore an obsolete live cache after forget", async () => {
  const { store } = backend();
  const { ctx, persistence, replication } = await setup(store);
  const writer = await persistence.create(header());
  await writer.append([event()]);
  await writer.close();
  replication.forget("main");
  const obsolete = Session.create(SessionId("main"), [{ ...event(), time: 99 }], header());
  ctx.emit("session/created", obsolete);
  await replication.flush("main");
  expect(remoteEvents(await store.privateEvents("main"))).toEqual([event()]);
});

test("a local durability failure prevents uploading captured live events", async () => {
  const { store, logs } = backend();
  const { ctx, persistence, replication } = await setup(store);
  const live = ctx.sessions.create(SessionId("main"), { seed: [event()], meta: header() });
  const writer = await persistence.create(live.header);
  await writer.append(live.snapshotEvents());
  live.append("turn/end", { turn: 1, reason: { kind: "completed" } });
  const flush = writer.flush.bind(writer);
  writer.flush = async () => { throw new Error("local disk unavailable"); };
  try {
    await expect(replication.flush("main")).rejects.toThrow("local disk unavailable");
    expect(logs.size).toBe(0);
  } finally { writer.flush = flush; }
  await replication.flush("main");
  expect(remoteEvents(await store.privateEvents("main"))).toEqual([...live.snapshotEvents()]);
  await writer.close();
});

test.each(["ahead", "divergent"] as const)("invalid %s remote lifecycle cannot modify a valid local transcript", async mode => {
  const { store, logs } = backend();
  const invalid: SessionEvent[] = mode === "ahead"
    ? [event(), { ...event(1), data: { turn: 2, reason: { kind: "completed" } } } as SessionEvent]
    : [{ ...event(), data: { turn: 2 } } as SessionEvent];
  logs.set("main", nativeRecords(header(), invalid));
  const { persistence, replication, root } = await setup(store);
  const writer = await persistence.create(header());
  await writer.append([event()]);
  await writer.close();
  const path = (await transcripts(root))[0]!.path;
  const before = await readFile(path);
  await expect(replication.ensure("main")).rejects.toThrow();
  expect(await readFile(path)).toEqual(before);
  await expect(readdir(`${root}.quarantine`)).rejects.toMatchObject({ code: "ENOENT" });
});

test("any terminal event rejection stops a batch even when an earlier event reports a transport error", async () => {
  const { store } = backend();
  const persist = store.persist;
  const terminal = new CcrHttpError(409);
  let attempts = 0;
  store.persist = async (...args) => {
    attempts++;
    if ("seq" in args[2]) throw args[2].seq === 0 ? new Error("response lost") : terminal;
    await persist(...args);
  };
  const { persistence, replication } = await setup(store);
  const writer = await persistence.create(header());
  await writer.append([event(), event(1)]);
  await expect(replication.flush("main")).rejects.toThrow("response lost");
  const stopped = attempts;
  await expect(replication.flush("main")).rejects.toBe(terminal);
  expect(attempts).toBe(stopped);
  await writer.close();
});

test("native validation rejects an unknown required event before hydration", async () => {
  const { store, logs } = backend();
  logs.set("main", [
    { type: "dsh_log", data: header() },
    { type: "dsh_log", data: { type: "plugin:fixture/annotation", seq: 0, time: 2, data: { marker: "opaque" } } },
  ]);
  const { persistence, replication } = await setup(store);
  await expect(replication.ensure("main")).rejects.toThrow("not marked ignorable");
  expect(await persistence.stat(SessionId("main"))).toBeUndefined();
});

test("native hydration uses native header normalization and preserves opaque ignorable extensions", async () => {
  const { store, logs } = backend();
  const originalHeader = header();
  const extension = { type: "plugin:fixture/annotation", seq: 0, time: 2, ignorable: true, data: { marker: "opaque", nested: { unchanged: true } } };
  logs.set("main", [
    { type: "dsh_log", data: originalHeader },
    { type: "dsh_log", data: extension },
  ]);
  const reference = await setup();
  const writer = await reference.persistence.create(originalHeader);
  await writer.flush();
  await writer.close();
  const nativeReader = await reference.persistence.open(SessionId("main"), "read");
  const nativeHeader = nativeReader.header;
  await nativeReader.close();
  const { ctx, persistence, replication, root } = await setup(store);
  expect(await replication.ensure("main")).toBe(true);
  const reader = await persistence.open(SessionId("main"), "read");
  try {
    expect(reader.header).toEqual(nativeHeader);
    expect((await reader.read()).events).toMatchObject([extension]);
  } finally { await reader.close(); }
  const file = (await transcripts(root))[0]!;
  expect(file.lines[1]).toEqual(extension);
  const originalBytes = await readFile(file.path);
  replication.forget("main");
  expect(await replication.ensure("main")).toBe(false);
  expect(await readFile(file.path)).toEqual(originalBytes);
  await replication.flush("main");
  expect(logs.get("main")).toHaveLength(2);
  expect(logs.get("main")![0]!.data).toEqual(originalHeader);
  await ctx.fiber.dispose();
});
