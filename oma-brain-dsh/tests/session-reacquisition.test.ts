import { afterEach, expect, test } from "bun:test";
import { mkdtemp, readdir, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Context } from "@deepseek-ai/cordis";
import { SessionId, Session, appendPluginRecord } from "@deepseek-ai/dsh-session";
import JsonlPersistence from "@deepseek-ai/dsh-session-persistence-jsonl";
import { OmaApi } from "../src/api.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { mountOma } from "../src/plugin.ts";
import { Backend, textEvents } from "./backend.ts";

declare module "@deepseek-ai/dsh-session" {
  interface PluginRecordMap {
    "plugin:oma/test-local-tail": { marker: string };
  }
}

const cleanups: (() => Promise<void>)[] = [];
afterEach(async () => {
  for (const cleanup of cleanups.splice(0).reverse()) await cleanup();
});

async function fixture() {
  const backend = new Backend();
  await backend.ready;
  const directory = await mkdtemp(join(tmpdir(), "oma-session-reacquisition-"));
  cleanups.push(async () => { backend.stop(); await rm(directory, { recursive: true, force: true }); });
  return { backend, rootA: join(directory, "brain-a"), rootB: join(directory, "brain-b") };
}

async function mount(backend: Backend, localRoot: string) {
  backend.a.worker_epoch++;
  const ctx = new Context();
  const api = new OmaApi(backend.url);
  await mountOma(ctx, api, {}, localRoot);
  const session = createOmaSession(ctx, api, backend.a);
  let closed = false;
  const close = async () => {
    if (closed) return;
    closed = true;
    try { await session.close(); }
    finally { await ctx.fiber.dispose(); }
  };
  cleanups.push(close);
  try { await session.open(); }
  catch (error) { await close().catch(() => {}); throw error; }
  return { session, close };
}

async function files(directory: string): Promise<string[]> {
  const result: string[] = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) result.push(...await files(path));
    else result.push(path);
  }
  return result;
}

async function appendUnreplicatedTurn(root: string, id: string) {
  const ctx = new Context();
  await ctx.plugin(JsonlPersistence, { root, compression: "none" });
  try {
    const handle = await ctx.sessionPersistence.open(SessionId(id), "write");
    try {
      const { events } = await handle.read();
      const detached = Session.create(SessionId(id), events, handle.header);
      appendPluginRecord(detached, "plugin:oma/test-local-tail", { marker: "local-unconfirmed" });
      await handle.append(detached.snapshotEvents().slice(events.length));
      await handle.flush();
    } finally { await handle.close(); }
  } finally { await ctx.fiber.dispose(); }
  const transcript = (await files(root)).find(path => path.endsWith(".jsonl"));
  if (!transcript) throw new Error("Missing native transcript fixture");
  return readFile(transcript);
}

test("A to B to A reacquisition restores B's confirmed input receipt before redelivery", async () => {
  const { backend, rootA, rootB } = await fixture();
  const first = await mount(backend, rootA);
  backend.modelScript = [textEvents("reply on A")];
  await first.session.receive("input-a", { content: "First input" });
  await first.close();
  const second = await mount(backend, rootB);
  backend.modelScript = [textEvents("reply on B")];
  await second.session.receive("input-b", { content: "Second input" });
  await second.close();
  const requests = backend.requests.length;
  const third = await mount(backend, rootA);
  expect(third.session.delivery.completed).toContain("input-b");
  expect(third.session.restoredFromRemote).toBe(true);
  await third.session.receive("input-b", { content: "Second input" });
  expect(backend.requests).toHaveLength(requests);
  expect(backend.processed.filter(id => id === "input-b")).toHaveLength(2);
});

test("remote extension on reacquisition discovers a child created by the other Brain", async () => {
  const { backend, rootA, rootB } = await fixture();
  const first = await mount(backend, rootA);
  backend.modelScript = [textEvents("initial reply")];
  await first.session.receive("input-a", { content: "First input" });
  await first.close();
  const second = await mount(backend, rootB);
  backend.modelScript = [textEvents("child reply on B")];
  await second.session.subagents.send({ name: "remote-specialist", system_prompt: "Inspect remote work", message: "Inspect files" }, new AbortController().signal);
  const childId = second.session.subagents.threads[0]!.id;
  await second.session.saveDelivery();
  await second.close();
  const requests = backend.requests.length;
  const third = await mount(backend, rootA);
  expect(third.session.restoredFromRemote).toBe(true);
  expect(third.session.subagents.threads).toEqual([expect.objectContaining({ id: childId, name: "remote-specialist", systemPrompt: "Inspect remote work", status: "idle" })]);
  expect(backend.requests).toHaveLength(requests);
});

test("divergent reacquisition quarantines A's original transcript before using B's remote history", async () => {
  const { backend, rootA, rootB } = await fixture();
  const first = await mount(backend, rootA);
  backend.modelScript = [textEvents("initial reply")];
  await first.session.receive("input-a", { content: "First input" });
  await first.close();
  const original = await appendUnreplicatedTurn(rootA, backend.a.session_id);
  const second = await mount(backend, rootB);
  backend.modelScript = [textEvents("authoritative reply on B")];
  await second.session.receive("input-b", { content: "Second input" });
  await second.close();
  const requests = backend.requests.length;
  const publicEvents = [...backend.publicLog.values()];
  const third = await mount(backend, rootA);
  expect(third.session.delivery.completed).toContain("input-b");
  expect(third.session.agent.session.snapshotEvents().some(event => String(event.type) === "plugin:oma/test-local-tail")).toBe(false);
  const quarantine = (await files(`${rootA}.quarantine`)).filter(path => path.endsWith(".jsonl"));
  expect(quarantine).toHaveLength(1);
  expect(await readFile(quarantine[0]!)).toEqual(original);
  expect(backend.requests).toHaveLength(requests);
  expect([...backend.publicLog.values()]).toEqual(publicEvents);
});

test("invalid remote history fails reacquisition without changing the existing local transcript", async () => {
  const { backend, rootA } = await fixture();
  const first = await mount(backend, rootA);
  backend.modelScript = [textEvents("initial reply")];
  await first.session.receive("input-a", { content: "First input" });
  await first.close();
  const transcript = (await files(rootA)).find(path => path.endsWith(".jsonl"));
  if (!transcript) throw new Error("Missing native transcript fixture");
  const original = await readFile(transcript);
  backend.privateLog.set("invalid-remote-tail", {
    type: "dsh_log", uuid: "invalid-remote-tail",
    data: { seq: 999999, time: 0, type: "turn/start", data: { turn: 1 } },
  });
  const requests = backend.requests.length;
  const publicEvents = [...backend.publicLog.values()];
  await expect(mount(backend, rootA)).rejects.toThrow();
  expect(await readFile(transcript)).toEqual(original);
  expect(backend.requests).toHaveLength(requests);
  expect([...backend.publicLog.values()]).toEqual(publicEvents);
});
