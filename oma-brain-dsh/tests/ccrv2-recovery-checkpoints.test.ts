import { runSubagent } from "./subagent-tools.ts";
import { completedInputs, seedInterruptedTool } from "./session-log.ts";
import { afterEach, expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { CcrSubagentBridge } from "../src/ccrv2/subagents.ts";
import { Backend, textEvents, toolEvents } from "./backend.ts";

const cleanups: (() => Promise<void>)[] = [];
afterEach(async () => {
  for (const cleanup of cleanups.splice(0).reverse()) await cleanup();
});

async function mount(backend: Backend, beforeOpen?: (session: ReturnType<typeof createOmaSession>) => void) {
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
  beforeOpen?.(session);
  await session.open();
  return { session, close };
}

for (const phase of ["requested", "dispatch", "friday-oauth"] as const) {
  test(`native ${phase} tool recovery persists once across another restart`, async () => {
    const backend = new Backend();
    cleanups.push(async () => { backend.stop(); });
    const friday = phase === "friday-oauth";
    const name = friday ? "Friday" : "Bash";
    const args = friday ? '{"args":["app","list"]}' : '{"command":"write"}';
    if (friday) {
      backend.modelScript = [toolEvents("echo check", "followup-call"), textEvents("恢复查询完成")];
      const route = backend.route.bind(backend);
      backend.route = request => request.url.endsWith("/interrupted-call/friday-oauth?resume=1")
        ? Promise.resolve(Response.json({ authorized: true, placeholder: "oma_ph_test" })) : route(request);
    }
    const first = await mount(backend);
    const callId = "interrupted-call";
    await seedInterruptedTool(first.session, "interrupted-input", callId, { name, arguments: args }, phase !== "requested");
    await first.session.checkpoint();
    await first.close();
    let decisions = 0;
    const recovered = await mount(backend, session => {
      if (!friday) return;
      backend.onApproval = async payload => {
        decisions++;
        expect(session.initializing).toBe(true);
        await session.receive("followup-decision", { type: "control_response", response: {
          subtype: "success", request_id: payload.request_id, response: { behavior: "allow" },
        } });
      };
    });
    expect(decisions).toBe(friday ? 1 : 0);
    const results = recovered.session.agent.session.snapshotEvents().filter(event => event.type === "tool/result");
    expect(results).toHaveLength(friday ? 2 : 1);
    expect(results[0]?.data.error?.code).toBe(friday ? undefined : "TOOL_NOT_STARTED");
    expect(completedInputs(recovered.session)).toContain("interrupted-input");
    const publicResult = [...backend.publicLog.values()].flatMap(event => event.message?.content ?? [])
      .find(block => block.type === "tool_result" && block.tool_use_id === callId);
    expect(publicResult?.tool_name).toBe(name);
    await recovered.close();
    const persisted = () => [...backend.privateLog.values()].filter(payload => payload.data?.type !== "session/end-seed");
    const stored = structuredClone(persisted());
    const reopened = await mount(backend);
    expect(reopened.session.agent.session.snapshotEvents().filter(event => event.type === "tool/result")).toHaveLength(friday ? 2 : 1);
    await reopened.close();
    expect(persisted()).toEqual(stored);
    expect(backend.requests).toHaveLength(friday ? 2 : 0);
    expect(backend.executions).toHaveLength(friday ? 2 : 0);
  });
}

test("Friday 恢复保留临时授权错误和已提交但无法确认的取消结果", async () => {
  const backend = new Backend();
  cleanups.push(async () => { backend.stop(); });
  const first = await mount(backend);
  await seedInterruptedTool(first.session, "input", "friday-call", { name: "Friday", arguments: '{"args":["app","list"]}' });
  await first.session.checkpoint();
  await first.close();

  const route = backend.route.bind(backend);
  let available = false;
  let submitted = false;
  let interrupt: () => void = () => {};
  backend.route = async request => {
    const url = new URL(request.url);
    if (url.pathname.endsWith("/friday-oauth"))
      return available ? Response.json({ authorized: true, placeholder: "oma_ph_test" }) : new Response(null, { status: 503 });
    if (url.pathname.endsWith("/tool-calls") && request.method === "POST") submitted = true;
    if (submitted && url.pathname.endsWith("/friday-call") && request.method === "GET") interrupt();
    if (url.pathname.endsWith("/friday-call/cancel")) return new Response(null, { status: 409 });
    return route(request);
  };
  await expect(mount(backend)).rejects.toThrow();
  expect(submitted).toBe(false);
  available = true;
  const recovered = await mount(backend, session => {
    interrupt = () => { void session.receive("stop", { type: "user.interrupt" }); };
  });
  const result = recovered.session.agent.session.snapshotEvents().find(event => event.type === "tool/result");
  expect(result?.data.error?.code).toBe("TOOL_OUTCOME_UNKNOWN");
  expect(backend.executions).toHaveLength(1);
});

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
  await runSubagent(first.session, "subagent", { description: "specialist", prompt: ["Inspect", "Inspect files"].filter(Boolean).join("\n") });
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
  await runSubagent(first.session, "subagent", { description: "specialist", prompt: ["Inspect", "Inspect files"].filter(Boolean).join("\n") });
  const childId = first.session.subagents.threads[0]!.id;
  first.session.pendingEvents.push({ type: "session.thread_status_terminated", uuid: "pending-child-archive", session_thread_id: childId });
  await first.session.checkpoint();
  await first.close();
  expect(backend.publicLog.has("pending-child-archive")).toBe(false);
  const restored = await mount(backend);
  expect(backend.publicLog.has("pending-child-archive")).toBe(false);
  expect(restored.session.subagents.threads[0]?.status).toBe("idle");
  backend.modelScript = [textEvents("follow-up")];
  await runSubagent(restored.session, "send_message", { agent_id: childId, message: "Do more work" });
  expect(restored.session.subagents.threads[0]?.status).toBe("idle");
});

test("child discovery rejects incomplete remote inventory instead of trusting only local headers", async () => {
  const backend = new Backend();
  cleanups.push(async () => { backend.stop(); });
  const { session } = await mount(backend);
  backend.modelScript = [textEvents("child completed")];
  await runSubagent(session, "subagent", { description: "specialist", prompt: ["Inspect", "Inspect files"].filter(Boolean).join("\n") });
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
