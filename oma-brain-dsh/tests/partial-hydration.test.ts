import { afterEach, expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { SessionId } from "@deepseek-ai/dsh-session";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { Backend, textEvents } from "./backend.ts";

const cleanups: (() => Promise<unknown>)[] = [];
afterEach(async () => {
  for (const cleanup of cleanups.splice(0).reverse()) await cleanup();
});

async function mount(backend: Backend, localRoot?: string) {
  await backend.ready;
  const ctx = new Context();
  const api = new OmaApi(backend.url);
  const client = await mountOma(ctx, api, {}, localRoot);
  const session = createOmaSession(ctx, api, backend.a);
  let closed = false;
  const close = async () => {
    if (closed) return;
    closed = true;
    try { await session.close(); }
    finally { await ctx.fiber.dispose(); }
  };
  cleanups.push(close);
  return { ctx, client, session, close };
}

test("restart between root and child hydration recovers children from native delivery history", async () => {
  const backend = new Backend();
  cleanups.push(async () => { backend.stop(); });
  const root = await mkdtemp(join(tmpdir(), "brain-partial-hydration-"));
  cleanups.push(() => rm(root, { recursive: true, force: true }));
  const first = await mount(backend);
  await first.session.open();
  backend.modelScript = [textEvents("child completed")];
  await first.session.subagents.send({ name: "specialist", system_prompt: "Inspect", message: "Inspect files" }, new AbortController().signal);
  const childId = first.session.subagents.threads[0]!.id;
  await first.session.checkpoint();
  expect(first.session.pendingEvents).toEqual([]);
  await first.close();

  const partialCtx = new Context();
  const partialApi = new OmaApi(backend.url);
  partialApi.assignments.set(backend.a.session_id, backend.a);
  await mountOma(partialCtx, partialApi, {}, root);
  try {
    expect(await partialCtx.ccrReplication.ensure(backend.a.session_id)).toBe(true);
    expect(await partialCtx.sessionPersistence.stat(SessionId(childId))).toBeUndefined();
  } finally { await partialCtx.fiber.dispose(); }

  const resumed = await mount(backend, root);
  const privateEvents = resumed.client.privateEvents.bind(resumed.client);
  let inventoryRequests = 0;
  resumed.client.privateEvents = async (id, options) => {
    if (options?.subagents && options.agentId === undefined) {
      inventoryRequests++;

    }
    return privateEvents(id, options);
  };
  const requests = backend.requests.length;
  await resumed.session.open();
  expect(resumed.session.restoredFromRemote).toBe(false);
  expect(resumed.session.subagents.threads.map(thread => ({ id: thread.id, name: thread.name, status: thread.status })))
    .toEqual([{ id: childId, name: "specialist", status: "idle" }]);
  expect(await resumed.ctx.sessionPersistence.stat(SessionId(childId))).toBeDefined();
  expect(backend.requests).toHaveLength(requests);
  expect(inventoryRequests).toBe(1);
});
