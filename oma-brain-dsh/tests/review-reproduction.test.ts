import { afterEach, expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import SessionStore, { SESSION_FORMAT_VERSION, SessionId, SessionSeq, appendPluginRecord, pluginRecordOf, type SessionEvent, type SessionHeader } from "@deepseek-ai/dsh-session";
import { LlmAttemptId } from "@deepseek-ai/dsh-llm";
import JsonlPersistence from "@deepseek-ai/dsh-session-persistence-jsonl";
import { mkdtemp, rm } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { CcrReplication } from "../src/ccrv2/persistence.ts";
import type { CcrBackend } from "../src/ccrv2/contracts.ts";
import type { DshLogData } from "../src/ccrv2/persistence-contracts.ts";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { Backend, eventually } from "./backend.ts";

const cleanups: (() => Promise<unknown>)[] = [];
afterEach(async () => {
  for (const cleanup of cleanups.splice(0).reverse()) await cleanup();
});

function remoteStore() {
  const records: DshLogData[] = [];
  const backend: CcrBackend = {
    async privateEvents() { return structuredClone(records.map(data => ({ type: "dsh_log", data }))); },
    async persist(_id: string, _type: "dsh_log", data: DshLogData) { records.push(structuredClone(data)); },
    async getWorkerState() { return { metadata: null, durationMs: 0 }; },
    async isToolConfirmationPending() { return false; },
    async events() {},
    async acknowledge() {},
    async reportDelivery() {},
    async setStatus() {},
    async reportMetadata() {},
    sessionIds() { return ["returning-session"]; },
  };
  return { records, backend };
}

async function instance(backend: CcrBackend, root?: string) {
  if (!root) {
    root = await mkdtemp(join(tmpdir(), "oma-review-reproduction-"));
    const directory = root;
    cleanups.push(async () => {
      await rm(directory, { recursive: true, force: true });
    });
  }
  const ctx = new Context();
  cleanups.push(() => ctx.fiber.dispose());
  await ctx.plugin(JsonlPersistence, { root, compression: "none" });
  await ctx.plugin(SessionStore);
  await ctx.plugin(CcrReplication, { backend });
  return { ctx, root, local: ctx.sessionPersistence, replication: ctx.ccrReplication };
}

const id = SessionId("returning-session");
const header: SessionHeader = { id, version: SESSION_FORMAT_VERSION, createdAt: 1, cwd: "/home/user", isSeeded: false };
const started: SessionEvent = { type: "turn/start", seq: SessionSeq(0), time: 2, data: { turn: 1 } };
const completed: SessionEvent = { type: "turn/end", seq: SessionSeq(1), time: 3, data: { turn: 1, reason: { kind: "completed" } } };

test("A to B to A must reconcile a remote tail before returning a usable local session", async () => {
  const { backend, records } = remoteStore();
  const a = await instance(backend);
  const first = await a.local.create(header);
  await first.append([started]);
  await first.close();
  await a.replication.flush(id);
  await a.ctx.fiber.dispose();

  const b = await instance(backend);
  expect(await b.replication.ensure(id)).toBe(true);
  const second = await b.local.open(id, "write");
  await second.append([completed]);
  await second.close();
  await b.replication.flush(id);
  await b.ctx.fiber.dispose();

  const returning = await instance(backend, a.root);
  await returning.replication.ensure(id);
  const reader = await returning.local.open(id, "read");
  const localEvents = (await reader.read()).events;
  await reader.close();
  let checkpointError: string | undefined;
  try { await returning.replication.flush(id); }
  catch (error) { checkpointError = (error as Error).message; }
  const remoteEvents = records.filter(record => "seq" in record);
  expect({ localEvents, checkpointError }).toEqual({ localEvents: remoteEvents, checkpointError: undefined });
});

test.each([20, 200])("preview and background replication avoid full reads with %s historical events", async extraEvents => {
  const backend = new Backend();
  cleanups.push(async () => { backend.stop(); });
  await backend.ready;
  const ctx = new Context();
  cleanups.push(() => ctx.fiber.dispose());
  const api = new OmaApi(backend.url);
  await mountOma(ctx, api);
  const session = createOmaSession(ctx, api, backend.a);
  cleanups.push(() => session.close());
  await session.open();
  session.delivery.inputId = "preview-input";
  for (let index = 0; index < extraEvents; index++) {
    appendPluginRecord(session.agent.session, "plugin:oma/delivery", {
      version: 1, delivery: { ...session.delivery, revision: index + 1 },
    });
  }
  await session.checkpoint();
  await new Promise(resolve => setTimeout(resolve, 50));
  const historyLength = session.agent.session.snapshotEvents().length;
  const open = ctx.sessionPersistence.open.bind(ctx.sessionPersistence);
  const reads: number[] = [];
  ctx.sessionPersistence.open = async (...args) => {
    const handle = await open(...args);
    const read = handle.read.bind(handle);
    handle.read = async (...options) => {
      const result = await read(...options);
      reads.push(result.events.length);
      return result;
    };
    return handle;
  };
  const publish = backend.route.bind(backend);
  let previewFrames = 0;
  backend.route = async request => {
    if (request.method === "POST" && new URL(request.url).pathname.endsWith("/worker/events")) {
      const body = await request.clone().json();
      previewFrames += body.events.filter((event: { payload: { type: string } }) => event.payload.type === "event_delta").length;
    }
    return publish(request);
  };
  try {
    session.stream({ type: "start", attemptId: LlmAttemptId("review-preview"), revision: 1, turn: 1, step: 1 });
    for (let index = 0; index < 3; index++) {
      appendPluginRecord(session.agent.session, "plugin:oma/delivery", {
        version: 1, delivery: { ...session.delivery, revision: extraEvents + index + 1 },
      });
      session.stream({ type: "chunk", attemptId: LlmAttemptId("review-preview"), revision: 1, index, time: index,
        chunk: { type: "text-delta", index: 0, text: "piece" } });
      await eventually(() => previewFrames === index + 1);
    }
    expect(previewFrames).toBe(3);
    expect(session.agent.session.snapshotEvents()).toHaveLength(historyLength + 3);
    expect(reads).toEqual([]);
  } finally {
    ctx.sessionPersistence.open = open;
  }
});

test("delivery journal stores completed inputs once and skips unchanged checkpoints", async () => {
  const backend = new Backend();
  cleanups.push(async () => { backend.stop(); });
  await backend.ready;
  const ctx = new Context();
  cleanups.push(() => ctx.fiber.dispose());
  const api = new OmaApi(backend.url);
  await mountOma(ctx, api);
  const session = createOmaSession(ctx, api, backend.a);
  cleanups.push(() => session.close());
  await session.open();
  for (let index = 0; index < 10; index++) {
    session.delivery.completed.push(`input-${index}`);
    await session.saveDelivery();
  }
  const lengths = session.agent.session.snapshotEvents().flatMap(event => {
    const record = pluginRecordOf(event);
    if (record?.type !== "plugin:oma/delivery") return [];
    const data = record.data as { version: number; completedAdded: string[] };
    expect(data.version).toBe(2);
    return [data.completedAdded.length];
  });
  expect(lengths).toEqual(Array(10).fill(1));
  const count = session.agent.session.snapshotEvents().length;
  for (let index = 0; index < 10; index++) await session.saveDelivery();
  expect(session.agent.session.snapshotEvents()).toHaveLength(count);
});

test("existing divergent local history rejects resume and preserves both histories", async () => {
  const { backend, records } = remoteStore();
  const remote = await instance(backend);
  const writer = await remote.local.create(header);
  await writer.append([started]);
  await writer.close();
  await remote.replication.flush(id);
  await remote.ctx.fiber.dispose();
  const stale = await instance(backend);
  const fork = { ...started, time: 99 };
  const local = await stale.local.create(header);
  await local.append([fork]);
  await local.close();
  await expect(stale.replication.ensure(id)).rejects.toThrow("Local and remote log histories diverged");
  const reader = await stale.local.open(id, "read");
  expect((await reader.read()).events).toEqual([fork]);
  await reader.close();
  expect(records.filter(record => "seq" in record)).toEqual([started]);
});


async function liveFixture() {
  const backend = new Backend();
  cleanups.push(async () => { backend.stop(); });
  await backend.ready;
  const ctx = new Context();
  cleanups.push(() => ctx.fiber.dispose());
  const api = new OmaApi(backend.url);
  await mountOma(ctx, api);
  const session = createOmaSession(ctx, api, backend.a);
  cleanups.push(() => session.close());
  await session.open();
  return { backend, ctx, session };
}

test.each(["checkpoint", "background"])("%s replication survives another session's failed native writer", async mode => {
  const { backend, ctx, session } = await liveFixture();
  const broken = await ctx.sessionPersistence.create({ ...header, id: SessionId("broken-unrelated") });
  await broken.flush();
  const flush = broken.flush.bind(broken);
  broken.flush = async () => { throw new Error("unrelated disk failure"); };
  try {
    await expect(ctx.sessionPersistence.flush()).rejects.toThrow("flush failed");
    appendPluginRecord(session.agent.session, "plugin:oma/delivery", {
      version: 1, delivery: { ...session.delivery, revision: 1 },
    });
    const last = session.agent.session.snapshotEvents().at(-1)!;
    if (mode === "checkpoint") await session.checkpoint();
    await eventually(() => [...backend.privateLog.values()].some(payload => {
      const data = payload.data as { seq?: number };
      return payload.type === "dsh_log" && data.seq === last.seq;
    }));
    expect(session.abort.signal.aborted).toBe(false);
  } finally {
    broken.flush = flush;
    await broken.close();
  }
});

test("a session's own durability failure blocks background replication and checkpoints until recovery", async () => {
  const { backend, ctx, session } = await liveFixture();
  const dispose = ctx.on("session/flush", live => {
    if (live.id === session.agent.session.id) throw new Error("own disk failure");
  });
  appendPluginRecord(session.agent.session, "plugin:oma/delivery", {
    version: 1, delivery: { ...session.delivery, revision: 1 },
  });
  const last = session.agent.session.snapshotEvents().at(-1)!;
  const replicated = () => [...backend.privateLog.values()].some(payload => {
    const data = payload.data as { seq?: number };
    return payload.type === "dsh_log" && data.seq === last.seq;
  });
  await expect(session.checkpoint()).rejects.toThrow("own disk failure");
  await new Promise(resolve => setTimeout(resolve, 150));
  expect(replicated()).toBe(false);
  dispose();
  await session.checkpoint();
  expect(replicated()).toBe(true);
});


test("root checkpoints await related child durability and preserve the child's confirmation barrier", async () => {
  const { backend, ctx, session } = await liveFixture();
  const child = ctx.sessions.create(SessionId("related-child"), { meta: { cwd: "/workspace" } });
  ctx.ccrReplication.bindThread(child.id, session.id);
  const writer = await ctx.sessionPersistence.create(child.header);
  const flush = writer.flush.bind(writer);
  writer.flush = async () => { throw new Error("child disk failure"); };
  try {
    appendPluginRecord(child, "plugin:oma/delivery", {
      version: 1, delivery: { ...session.delivery, revision: 1 },
    });
    await expect(session.checkpoint()).rejects.toThrow("child disk failure");
    expect([...backend.privateLog.values()].some(payload => backend.privateAgentIds.get(payload.uuid) === child.id)).toBe(false);
  } finally { writer.flush = flush; }
  await session.checkpoint();
  expect([...backend.privateLog.values()].some(payload => backend.privateAgentIds.get(payload.uuid) === child.id && "seq" in payload.data)).toBe(true);
  await writer.close();
});
