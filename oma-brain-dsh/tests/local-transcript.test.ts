import { afterEach, expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { SESSION_FORMAT_VERSION, SessionId, SessionSeq, type SessionEvent, type SessionHeader } from "@deepseek-ai/dsh-session";
import { mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { CcrPersistence } from "../src/ccrv2/persistence.ts";
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
  await ctx.plugin(CcrPersistence, { backend: store, localRoot: root });
  return { ctx, root, persistence: ctx.sessionPersistence as CcrPersistence };
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

test.each(["append", "flush"] as const)("remote failure retains the local event and retries %s without a duplicate", async (operation) => {
  const { store } = backend();
  const { persistence, root } = await setup(store);
  const handle = await persistence.create(header());
  const persist = store.persist;
  store.persist = async () => { throw new Error("remote unavailable"); };
  if (operation === "flush") persistence.writers.get(handle.id)!.enqueue(event());
  const write = () => operation === "append" ? handle.append([event()]) : handle.flush();
  await expect(write()).rejects.toThrow("remote unavailable");
  expect((await handle.read()).events).toEqual([]);
  expect((await transcripts(root))[0]!.lines.slice(1)).toEqual([event()]);
  store.persist = persist;
  await write();
  expect((await handle.read()).events).toEqual([event()]);
  expect((await transcripts(root))[0]!.lines.slice(1)).toEqual([event()]);
  await handle.close();
});

test("local creation failure does not publish a remote header or retain ownership", async () => {
  const { store, logs } = backend();
  const { persistence, root } = await setup(store);
  const invalidRoot = join(root, "file");
  await writeFile(invalidRoot, "occupied");
  persistence.config.localRoot = invalidRoot;
  await expect(persistence.create(header())).rejects.toThrow();
  expect(logs.size).toBe(0);
  expect(persistence.writers.size).toBe(0);
  expect(await persistence.stat(header().id)).toBeUndefined();
  persistence.config.localRoot = root;
  await (await persistence.create(header())).close();
});

test("failed close releases ownership and recovery excludes the unconfirmed local tail while retaining its file", async () => {
  const { store } = backend();
  const { persistence, root } = await setup(store);
  const handle = await persistence.create(header());
  const persist = store.persist;
  store.persist = async () => { throw new Error("lease lost"); };
  persistence.writers.get(handle.id)!.enqueue(event());
  await expect(handle.close()).rejects.toThrow("lease lost");
  persistence.forget(handle.id);
  const original = (await transcripts(root))[0]!;
  store.persist = persist;
  const restored = await persistence.open(handle.id, "write");
  expect((await restored.read()).events).toEqual([]);
  expect(await transcripts(root)).toHaveLength(2);
  expect(await readFile(original.path, "utf8")).toContain('"turn/start"');
  await restored.close();
});

test("remote read failure refuses restoration despite an existing local transcript", async () => {
  const { store } = backend();
  const { persistence, root } = await setup(store);
  const handle = await persistence.create(header());
  await handle.append([event()]);
  await handle.close();
  persistence.forget(handle.id);
  store.privateEvents = async () => { throw new Error("remote read failed"); };
  await expect(persistence.open(handle.id, "write")).rejects.toThrow("remote read failed");
  expect(await transcripts(root)).toHaveLength(1);
});

test("concurrent acquisition permits only one writer while local hydration is pending", async () => {
  const { persistence } = await setup();
  const creating = persistence.create(header());
  await expect(persistence.create(header())).rejects.toThrow("already");
  const handle = await creating;
  await handle.close();
  const opening = persistence.open(handle.id, "write");
  await expect(persistence.open(handle.id, "write")).rejects.toThrow("already");
  await (await opening).close();
});

test("closing a remote reader leaves the native JSONL writer open", async () => {
  const { persistence, root } = await setup();
  const writer = await persistence.create(header());
  const reader = await persistence.open(writer.id, "read");
  await reader.close();
  await writer.append([event()]);
  await writer.close();
  expect((await transcripts(root))[0]!.lines.slice(1)).toEqual([event()]);
});

test("takeover hydrates complete foreground and child logs before appending and disposal closes both stores", async () => {
  const { store, logs } = backend();
  const first = await setup(store);
  first.persistence.bindThread("child", "main");
  for (const id of ["main", "child"]) {
    const handle = await first.persistence.create(header(id));
    await handle.append([event()]);
    await handle.close();
  }
  const second = await setup(store, first.root);
  second.persistence.bindThread("child", "main");
  for (const id of ["main", "child"]) {
    const reader = await second.persistence.open(SessionId(id), "read");
    expect((await reader.read()).events).toEqual([event()]);
    await reader.close();
    const handle = await second.persistence.open(SessionId(id), "write");
    expect((await handle.read()).events).toEqual([event()]);
    second.persistence.writers.get(handle.id)!.enqueue(event(1));
  }
  await second.ctx.fiber.dispose();
  expect(second.persistence.writers.size).toBe(0);
  const files = await transcripts(first.root);
  expect(files).toHaveLength(4);
  expect(files.filter((file) => file.lines.length === 3)).toHaveLength(2);
  expect(logs.get("main")!.filter((entry) => entry.agent_id === "child").at(-1)!.data.events).toEqual([event(1)]);
});
