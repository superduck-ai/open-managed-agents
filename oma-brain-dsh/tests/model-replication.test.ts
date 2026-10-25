import { afterEach, expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { LlmAttemptId } from "@deepseek-ai/dsh-llm";
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

test("completed tools publish confirmed results while the next model request is still running", async () => {
  const { backend, session } = await fixture();
  const releaseModel = Promise.withResolvers<ReturnType<typeof textEvents>>();
  const releaseResult = Promise.withResolvers<void>();
  const persist = session.backend.persist.bind(session.backend);
  let held = false;
  session.backend.persist = async (...args) => {
    const data = args[2];
    if ("seq" in data && data.type === "tool/result") { held = true; await releaseResult.promise; }
    await persist(...args);
  };
  backend.onModel = async () => backend.requests.length === 1 ? toolEvents() : releaseModel.promise;
  backend.onApproval = payload => session.control({ type: "control_response", response: {
    subtype: "success", request_id: payload.request_id, response: { behavior: "allow" },
  } }).then(() => {});
  const publishedResult = () => [...backend.publicLog.values()].some(event => event.type === "user"
    && event.message?.content?.some((block: { type: string; tool_use_id?: string }) => block.type === "tool_result" && block.tool_use_id === "call_1"));
  const turn = session.receive("live-tool-result", { content: "Run a tool and explain" });
  turn.catch(() => {});
  try {
    await eventually(() => held && backend.requests.length === 2);
    expect(backend.executions).toHaveLength(1);
    expect(publishedResult()).toBe(false);
    releaseResult.resolve();
    await eventually(publishedResult);
    expect(backend.processed).toEqual([]);
    expect([...backend.publicLog.values()].some(event => event.type === "result")).toBe(false);
  } finally {
    releaseResult.resolve();
    releaseModel.resolve(textEvents("done"));
    await turn;
    session.backend.persist = persist;
  }
});


test("durable publication cannot overtake an earlier unconfirmed stream preview", async () => {
  const { session } = await fixture();
  const release = Promise.withResolvers<void>();
  const checkpointed = Promise.withResolvers<void>();
  const events = session.backend.events.bind(session.backend);
  const checkpoint = session.checkpoint.bind(session);
  const order: string[] = [];
  let held = false;
  session.backend.events = async (...args) => {
    const preview = args[1].some(event => event.type === "event_start");
    if (preview) { held = true; await release.promise; }
    else order.push("durable");
    await events(...args);
    if (preview) order.push("preview");
  };
  session.inputId = "preview-order";
  const attemptId = LlmAttemptId("preview-order");
  session.stream({ type: "start", attemptId, revision: 1, turn: 1, step: 1 });
  session.stream({ type: "chunk", attemptId, revision: 1, index: 0, time: 0,
    chunk: { type: "text-delta", index: 0, text: "partial" } });
  await eventually(() => held);
  session.checkpoint = async () => { await checkpoint(); checkpointed.resolve(); };
  session.pendingEvents.push({ type: "agent.message", uuid: "durable-after-preview", content: [{ type: "text", text: "partial" }] });
  const publication = session.publish();
  publication.catch(() => {});
  try {
    await checkpointed.promise;
    await new Promise<void>(resolve => setImmediate(resolve));
    expect(order).toEqual([]);
    release.resolve();
    await publication;
    expect(order).toEqual(["preview", "durable"]);
  } finally {
    release.resolve();
    await publication;
    session.backend.events = events;
    session.checkpoint = checkpoint;
  }
});
