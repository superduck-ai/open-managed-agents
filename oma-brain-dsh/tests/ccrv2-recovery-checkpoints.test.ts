import { completedInputs } from "./session-log.ts";
import { afterEach, expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { createMessage, freezeMessage, MessageId, ToolCallId } from "@deepseek-ai/dsh-llm";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { CcrSubagentBridge } from "../src/ccrv2/subagents.ts";
import { Backend, textEvents } from "./backend.ts";

const cleanups: (() => Promise<void>)[] = [];
afterEach(async () => {
  for (const cleanup of cleanups.splice(0).reverse()) await cleanup();
});

async function mount(backend: Backend) {
  await backend.ready;
  const ctx = new Context();
  const api = new OmaApi(backend.url);
  await mountOma(ctx, api);
  const session = createOmaSession(ctx, api, backend.a);
  let closed = false;
  const close = async () => {
    if (closed) return;
    closed = true;
    try { await session.close(); }
    finally { await ctx.fiber.dispose(); }
  };
  cleanups.push(close);
  await session.open();
  return { session, close };
}

for (const phase of ["requested", "dispatch"] as const) {
  test(`native ${phase} tool recovery persists once across another restart`, async () => {
    const backend = new Backend();
    cleanups.push(async () => { backend.stop(); });
    const first = await mount(backend);
    const native = first.session.agent.session;
    const callId = ToolCallId("interrupted-call");
    first.session.inputId = "interrupted-input";
    await first.session.checkpoint();
    native.append("turn/start", { turn: 1 });
    native.append("step/start", { turn: 1, step: 1 });
    native.append("user/message", freezeMessage({ id: MessageId("interrupted-input"), role: "user", source: { kind: "user" }, content: [{ type: "text", text: "Interrupted input" }] }), { surfaceOp: "append" });
    native.append("assistant/message", {
      turn: 1, step: 1, stream: [],
      message: createMessage({ role: "assistant", source: { kind: "model", provider: "oma", model: "test-model" },
        content: [{ type: "tool-call", id: callId, name: "Bash", arguments: '{"command":"write"}' }] }),
    }, { surfaceOp: "append" });
    if (phase === "dispatch") native.append("tool/call", { turn: 1, step: 1, callId, name: "Bash", arguments: '{"command":"write"}' });
    await first.session.checkpoint();
    await first.close();
    const recovered = await mount(backend);
    const results = recovered.session.agent.session.snapshotEvents().filter(event => event.type === "tool/result");
    expect(results).toHaveLength(1);
    expect(results[0]?.data.error?.code).toBe(phase === "dispatch" ? "TOOL_OUTCOME_UNKNOWN" : "TOOL_NOT_STARTED");
    expect(completedInputs(recovered.session)).toContain("interrupted-input");
    const publicResult = [...backend.publicLog.values()].flatMap(event => event.message?.content ?? [])
      .find(block => block.type === "tool_result" && block.tool_use_id === callId);
    expect(publicResult?.tool_name).toBe("Bash");
    await recovered.close();
    const persisted = () => [...backend.privateLog.values()].filter(payload => payload.data?.type !== "session/end-seed");
    const stored = structuredClone(persisted());
    const reopened = await mount(backend);
    expect(reopened.session.agent.session.snapshotEvents().filter(event => event.type === "tool/result")).toHaveLength(1);
    await reopened.close();
    expect(persisted()).toEqual(stored);
    expect(backend.requests).toHaveLength(0);
    expect(backend.executions).toHaveLength(0);
  });
}

test("publication leaves events arriving during its checkpoint for the next confirmed batch", async () => {
  const backend = new Backend();
  cleanups.push(async () => { backend.stop(); });
  const { session } = await mount(backend);
  const started = Promise.withResolvers<void>();
  const release = Promise.withResolvers<void>();
  const save = session.checkpoint.bind(session);
  let first = true;
  session.checkpoint = async () => {
    if (first) {
      first = false;
      started.resolve();
      await release.promise;
    }
    await save();
  };
  session.pendingEvents.push({ type: "system.message", uuid: "before-checkpoint", content: [{ type: "text", text: "first" }] });
  const publishing = session.publish();
  try {
    await started.promise;
    session.pendingEvents.push({ type: "system.message", uuid: "during-checkpoint", content: [{ type: "text", text: "second" }] });
    expect(backend.publicLog.has("before-checkpoint")).toBe(false);
    expect(backend.publicLog.has("during-checkpoint")).toBe(false);
    release.resolve();
    await publishing;
    expect(backend.publicLog.has("before-checkpoint")).toBe(true);
    expect(backend.publicLog.has("during-checkpoint")).toBe(false);
    expect(session.pendingEvents.map(payload => payload.uuid)).toEqual(["during-checkpoint"]);
    await session.publish();
    expect(backend.publicLog.has("during-checkpoint")).toBe(true);
    expect(session.pendingEvents).toEqual([]);
  } finally {
    release.resolve();
    await publishing;
    session.checkpoint = save;
  }
});

test("completed native child history repairs a stale running status with end_turn", async () => {
  const backend = new Backend();
  cleanups.push(async () => { backend.stop(); });
  const first = await mount(backend);
  backend.modelScript = [textEvents("child completed")];
  await first.session.subagents.send({ name: "specialist", system_prompt: "Inspect", message: "Inspect files" }, new AbortController().signal);
  const childId = first.session.subagents.threads[0]!.id;
  await first.session.checkpoint();
  await first.close();
  const route = backend.route.bind(backend);
  backend.route = async request => {
    if (request.method === "GET" && new URL(request.url).pathname.endsWith(`/worker/subagent-threads/${childId}`))
      return Response.json({ id: childId, status: "running", archived: false });
    return route(request);
  };
  const requests = backend.requests.length;
  const restored = await mount(backend);
  expect(restored.session.subagents.threads[0]?.status).toBe("idle");
  const idle = [...backend.publicLog.values()].filter(event => event.type === "session.thread_status_idle" && event.session_thread_id === childId);
  expect(idle).toHaveLength(2);
  expect(idle.at(-1)?.stop_reason.type).toBe("end_turn");
  expect(backend.requests).toHaveLength(requests);
});

test("an unconfirmed in-memory archive does not override authoritative child state after restart", async () => {
  const backend = new Backend();
  cleanups.push(async () => { backend.stop(); });
  const first = await mount(backend);
  backend.modelScript = [textEvents("child completed")];
  await first.session.subagents.send({ name: "specialist", system_prompt: "Inspect", message: "Inspect files" }, new AbortController().signal);
  const childId = first.session.subagents.threads[0]!.id;
  first.session.pendingEvents.push({ type: "session.thread_status_terminated", uuid: "pending-child-archive", session_thread_id: childId });
  await first.session.checkpoint();
  await first.close();
  expect(backend.publicLog.has("pending-child-archive")).toBe(false);
  const restored = await mount(backend);
  expect(backend.publicLog.has("pending-child-archive")).toBe(false);
  expect(restored.session.subagents.threads[0]?.status).toBe("idle");
  backend.modelScript = [textEvents("follow-up")];
  await restored.session.subagents.send({ thread_id: childId, message: "Do more work" }, new AbortController().signal);
  expect(restored.session.subagents.threads[0]?.status).toBe("idle");
});

test("child discovery rejects incomplete remote inventory instead of trusting only local headers", async () => {
  const backend = new Backend();
  cleanups.push(async () => { backend.stop(); });
  const { session } = await mount(backend);
  backend.modelScript = [textEvents("child completed")];
  await session.subagents.send({ name: "specialist", system_prompt: "Inspect", message: "Inspect files" }, new AbortController().signal);
  const childId = session.subagents.threads[0]!.id;
  const privateEvents = session.backend.privateEvents.bind(session.backend);
  let inventoryRequests = 0;
  session.backend.privateEvents = async (id, options) => {
    if (options?.subagents && options.agentId === undefined) {
      inventoryRequests++;
      throw new Error("Remote inventory unavailable");
    }
    return privateEvents(id, options);
  };
  try {
    const bridge = new CcrSubagentBridge(session);
    await expect(bridge.restoreThreads()).rejects.toThrow("Remote inventory unavailable");
    expect(bridge.threads()).toEqual([]);
    expect(session.subagents.threads[0]?.id).toBe(childId);
    expect(inventoryRequests).toBe(1);
  } finally { session.backend.privateEvents = privateEvents; }
});
