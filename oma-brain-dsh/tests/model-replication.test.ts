import { afterEach, expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { Backend, eventually, textEvents, toolEvents } from "./backend.ts";

const cleanups: (() => Promise<unknown>)[] = [];
afterEach(async () => { for (const cleanup of cleanups.splice(0).reverse()) await cleanup(); });

async function fixture() {
  const backend = new Backend();
  await backend.ready;
  cleanups.push(async () => backend.stop());
  const ctx = new Context();
  const api = new OmaApi(backend.url);
  await mountOma(ctx, api);
  const session = createOmaSession(ctx, api, backend.a);
  cleanups.push(async () => { try { await session.close(); } finally { await ctx.fiber.dispose(); } });
  await session.open();
  return { backend, ctx, session };
}

test.each([false, true])("replication lag allows model requests but blocks publication (child=%s)", async child => {
  const { backend, session } = await fixture();
  let childId: string | undefined;
  if (child) {
    backend.modelScript = [textEvents("warm child")];
    await session.subagents.send({ name: "specialist", system_prompt: "Inspect", message: "Warm up" }, new AbortController().signal);
    childId = session.subagents.threads[0]!.id;
  }
  const requests = backend.requests.length;
  const release = Promise.withResolvers<void>();
  const persist = session.backend.persist.bind(session.backend);
  let held = false;
  session.backend.persist = async (...args) => {
    const data = args[2];
    if ("seq" in data && data.type === "turn/start" && args[3] === childId) {
      held = true;
      await release.promise;
    }
    await persist(...args);
  };
  backend.modelScript = [textEvents("answer despite replication lag")];
  const before = backend.publicLog.size;
  let finished = false;
  const turn = child
    ? session.subagents.send({ thread_id: childId, message: "Continue" }, new AbortController().signal)
    : session.receive("replication-lag", { content: "Continue" });
  turn.then(() => { finished = true; }, () => {});
  try {
    await eventually(() => held);
    await eventually(() => backend.requests.length === requests + 1);
    expect(finished).toBe(false);
    expect(backend.processed).toEqual([]);
    expect([...backend.publicLog.values()].slice(before).some(event => event.type === "agent.message" || event.type === "result")).toBe(false);
    release.resolve();
    await turn;
    expect(backend.requests).toHaveLength(requests + 1);
    expect([...backend.publicLog.values()].some(event => JSON.stringify(event.content ?? []).includes("answer despite replication lag"))).toBe(true);
    if (!child) expect(backend.processed).toEqual(["replication-lag"]);
  } finally {
    release.resolve();
    await turn.catch(() => {});
    session.backend.persist = persist;
  }
});

test("a native tool call must be remotely confirmed before execution", async () => {
  const { backend, session } = await fixture();
  backend.modelScript = [toolEvents(), textEvents("done")];
  backend.onApproval = payload => session.control({ type: "control_response", response: {
    subtype: "success", request_id: payload.request_id, response: { behavior: "allow" },
  } }).then(() => {});
  const release = Promise.withResolvers<void>();
  const persist = session.backend.persist.bind(session.backend);
  let held = false;
  session.backend.persist = async (...args) => {
    const data = args[2];
    if ("seq" in data && data.type === "tool/call") { held = true; await release.promise; }
    await persist(...args);
  };
  const turn = session.receive("tool-barrier", { content: "Run a tool" });
  turn.catch(() => {});
  try {
    await eventually(() => held);
    expect(backend.requests).toHaveLength(1);
    expect(backend.executions).toEqual([]);
    expect(backend.processed).toEqual([]);
    release.resolve();
    await turn;
    expect(backend.executions).toHaveLength(1);
    expect(backend.processed).toEqual(["tool-barrier"]);
  } finally { release.resolve(); await turn.catch(() => {}); session.backend.persist = persist; }
});

test("input ACK still waits for public event confirmation", async () => {
  const { backend, session } = await fixture();
  backend.modelScript = [textEvents("done")];
  const release = Promise.withResolvers<void>();
  const events = session.backend.events.bind(session.backend);
  let held = false;
  session.backend.events = async (...args) => {
    if (args[1].some(event => event.type === "result")) { held = true; await release.promise; }
    await events(...args);
  };
  let finished = false;
  const turn = session.receive("publication-barrier", { content: "Reply" });
  turn.then(() => { finished = true; }, () => {});
  try {
    await eventually(() => held);
    expect(backend.requests).toHaveLength(1);
    expect(backend.processed).toEqual([]);
    expect(finished).toBe(false);
    release.resolve();
    await turn;
    expect(backend.publicLog.has("publication-barrier_result")).toBe(true);
    expect(backend.processed).toEqual(["publication-barrier"]);
  } finally { release.resolve(); await turn.catch(() => {}); session.backend.events = events; }
});
