import { afterEach, expect, spyOn, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { createUserMessage } from "@deepseek-ai/dsh-llm";
import { SessionId, SessionSeq, appendPluginRecord } from "@deepseek-ai/dsh-session";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { Backend, eventually, modelSystemText, textEvents, toolEvents } from "./backend.ts";
import { executeTool } from "./subagent-tools.ts";
import type { Payload } from "../src/json.ts";

const cleanups: (() => Promise<void>)[] = [];
afterEach(async () => { for (const close of cleanups.splice(0).reverse()) await close(); });

async function fixture(b = new Backend()) {
  await b.ready;
  const ctx = new Context();
  const api = new OmaApi(b.url);
  await ctx.plugin({ name: "dsh-subagent-test-host", apply: async (scope: Context) => { await mountOma(scope, api); } });
  const session = createOmaSession(ctx, api, b.a);
  let closed = false;
  const close = async () => {
    if (closed) return;
    closed = true;
    try { await session.close(); } finally { await ctx.fiber.dispose(); }
  };
  cleanups.push(async () => { try { await close(); } finally { b.stop(); } });
  b.onDelegationApproval = async payload => {
    await session.control({ type: "control_response", response: { subtype: "success", request_id: payload.request_id, response: { behavior: "allow" } } });
  };
  await session.open();
  return { b, ctx, api, session, close };
}

async function settle(f: Awaited<ReturnType<typeof fixture>>) {
  do { await f.session.agent.whenIdle(); } while (await f.session.subagents.wait());
}

function delegation(name: string): Payload[] {
  return toolEvents("", `delegate_${name}`).map(event => event.type === "content_block_start"
    ? { ...event, content_block: { ...event.content_block, name: "subagent" } }
    : event.type === "content_block_delta"
      ? { ...event, delta: { type: "input_json_delta", partial_json: JSON.stringify({ description: name, prompt: "Inspect the sandbox" }) } }
      : event);
}

test.each([false, true])("model prompt restores only official subagent guidance (enabled=%s)", async enabled => {
  const backend = new Backend();
  if (!enabled) backend.a.launch.subagents = undefined;
  backend.a.launch.system = "Keep literal {{ agent_role }} instructions.";
  const f = await fixture(backend);
  f.ctx.systemPrompt.section({ name: "unrelated-guidance", order: 50, text: "UNRELATED_GUIDANCE" });
  f.ctx.systemPrompt.section({ name: "tool:subagent_fork", order: 51, text: "UNREGISTERED_FORK_GUIDANCE" });
  backend.onModel = async () => textEvents("done");
  const guidance = "Start independent delegations with `subagent` together in one assistant message and continue useful work while they run. When only waiting for their results remains, end your turn instead of sleeping or polling; the next result starts a new turn.";
  for (const extra of ["", "Additional literal {{ session_rule }} instructions."]) {
    f.session.runtime.appendedSystem = extra;
    await f.session.receive(`prompt-${extra.length}`, { content: "Reply" });
    const request = backend.requests.at(-1)!;
    const existing = [f.session.runtime.config.systemPrompt, f.session.runtime.delegationPrompt, extra].filter(Boolean).join("\n\n");
    expect(modelSystemText(request)).toBe([existing, ...(enabled ? [guidance] : [])].join("\n\n"));
    expect(request.tools.some((tool: Payload) => tool.name === "subagent")).toBe(enabled);
  }
  if (!enabled) return;
  await executeTool(f.session, "subagent", { description: "child prompt", prompt: "Report done" });
  await settle(f);
  const childRequests = backend.requests.filter(request => modelSystemText(request).includes("# Subagent rules"));
  expect(childRequests.length).toBeGreaterThan(0);
  for (const request of childRequests) {
    expect(modelSystemText(request)).not.toContain(guidance);
    expect(modelSystemText(request)).not.toContain("UNRELATED_GUIDANCE");
    expect(modelSystemText(request)).not.toContain("UNREGISTERED_FORK_GUIDANCE");
    expect(request.tools.some((tool: Payload) => tool.name === "subagent")).toBe(false);
  }
});

for (const phase of ["context", "tools"] as const) test(`DSH ${phase} initialization failure releases the agent and credential binding`, async () => {
  const f = await fixture();
  if (phase === "context") f.session.runtime.config.threadMcpContext = async () => { throw new Error("context failed"); };
  else f.session.runtime.config.setupTools = async () => { throw new Error("tools failed"); };
  await expect(executeTool(f.session, "subagent", { description: "broken", prompt: "Work" })).rejects.toThrow();
  const thread = f.session.subagents.threads[0]!;
  expect(thread.status).toBe("terminated");
  expect(f.ctx.agents.get(SessionId(thread.id))).toBeUndefined();
  expect(f.api.modelSessions.has(thread.id)).toBe(false);
  expect(f.b.requests).toHaveLength(0);
  expect([...f.b.publicLog.values()].at(-1)?.type).toBe("session.thread_status_terminated");
});

test("official delegation rejects denied creation and foreign control targets", async () => {
  const f = await fixture();
  f.b.onDelegationApproval = async payload => {
    await f.session.control({ type: "control_response", response: { subtype: "success", request_id: payload.request_id, response: { behavior: "deny", message: "No delegation" } } });
  };
  await expect(executeTool(f.session, "subagent", { description: "worker", prompt: "Work" })).rejects.toThrow("No delegation");
  expect(f.session.subagents.threads).toHaveLength(0);
  expect(f.b.requests).toHaveLength(0);
  f.b.onDelegationApproval = async payload => {
    await f.session.control({ type: "control_response", response: { subtype: "success", request_id: payload.request_id, response: { behavior: "allow" } } });
  };
  await expect(executeTool(f.session, "send_message", { agent_id: "foreign", message: "Work" })).rejects.toThrow();
  expect(f.ctx.tools.get("send_to_agent", f.session.agent)).toBeUndefined();
  expect(f.ctx.tools.get("archive_agent", f.session.agent)).toBeUndefined();
});

test("predefined-only composition rejects missing and foreign presets", async () => {
  const b = new Backend();
  b.a.launch.subagents = { inlineAgents: false, agents: [{ name: "reader", description: "Read only", systemPrompt: "Only inspect", tools: ["Read"] }] };
  const f = await fixture(b);
  expect(f.ctx.tools.get("subagent", f.session.agent)).toBeDefined();
  await expect(executeTool(f.session, "subagent", { description: "missing", prompt: "Work" })).rejects.toThrow("agent_preset is required");
  await expect(executeTool(f.session, "subagent", { description: "foreign", prompt: "Work", agent_preset: "foreign/reader" })).rejects.toThrow();
  expect(f.ctx.tools.get("subagent_specialist_1", f.session.agent)).toBeUndefined();
  f.b.onModel = async () => textEvents("read");
  const child = await executeTool(f.session, "subagent", { agent_preset: `${f.session.id}/reader`, description: "Read files", prompt: "Inspect" });
  await settle(f);
  expect(f.session.subagents.threads[0]!.name).toBe("reader");
  expect(await executeTool(f.session, "list_agents", {})).toEqual([
    { kind: "child", id: child.subagentId, label: "Read files", status: "inactive" },
  ]);
  const request = f.b.requests.find(request => modelSystemText(request).includes("# Subagent rules"))!;
  expect(request.tools.map((tool: Payload) => tool.name)).toEqual(["Read"]);
});

test("DSH parallel children isolate identical call IDs and interrupt only the addressed child", async () => {
  const f = await fixture();
  const approvals: Payload[] = [];
  f.b.onApproval = async payload => { approvals.push(payload); };
  f.b.onModel = async request => modelSystemText(request).includes("# Subagent rules")
    ? JSON.stringify(request.messages).includes("tool_result") ? textEvents("child report") : toolEvents("pwd", "shared_call")
    : textEvents("parent synthesis");
  const receipts = await Promise.all(["alpha", "beta"].map(async name => await executeTool(f.session, "subagent", { description: name, prompt: "Work" })));
  await eventually(() => approvals.length === 2);
  expect(new Set(approvals.map(approval => approval.session_thread_id)).size).toBe(2);
  expect(await executeTool(f.session, "list_agents", {})).toHaveLength(2);
  expect(await executeTool(f.session, "interrupt_agent", { agent_id: receipts[0]!.subagentId })).toEqual({ accepted: true });
  const beta = approvals.find(approval => approval.session_thread_id === receipts[1]!.subagentId)!;
  await f.session.control({ type: "control_response", response: { subtype: "success", request_id: beta.request_id, response: { behavior: "allow" } } });
  await settle(f);
  expect(f.b.executions).toHaveLength(1);
  expect(f.session.subagents.threads.map(thread => thread.status)).toEqual(["idle", "idle"]);
  expect([...f.b.publicLog.values()].filter(event => event.type === "agent.thread_message_received").map(event => event.from_session_thread_id)).toEqual([receipts[1]!.subagentId]);
  for (const receipt of receipts) expect(f.ctx.agents.get(SessionId(receipt.subagentId))).toBeUndefined();
});

test("official send_message steers a running child without creating another thread", async () => {
  const f = await fixture();
  const approval = Promise.withResolvers<Payload>();
  f.b.onApproval = async payload => { approval.resolve(payload); };
  f.b.onModel = async request => modelSystemText(request).includes("# Subagent rules")
    ? JSON.stringify(request.messages).includes("tool_result") ? textEvents("steered report") : toolEvents("pwd", "steered_call")
    : textEvents("parent synthesis");
  const child = await executeTool(f.session, "subagent", { description: "worker", prompt: "Inspect" });
  const request = await approval.promise;
  const receipt = await executeTool(f.session, "send_message", { agent_id: child.subagentId, message: "Also report the working directory" });
  expect(receipt.messageId).toBeString();
  expect(f.session.subagents.threads).toHaveLength(1);
  expect(f.session.subagents.threads[0]!.status).toBe("running");
  await f.session.control({ type: "control_response", response: { subtype: "success", request_id: request.request_id, response: { behavior: "allow" } } });
  await settle(f);
  const last = f.b.requests.findLast(request => modelSystemText(request).includes("# Subagent rules"))!;
  expect(JSON.stringify(last.messages)).toContain("Also report the working directory");
  expect(f.b.executions).toHaveLength(1);
  expect([...f.b.publicLog.values()].filter(event => event.type === "session.thread_created")).toHaveLength(1);
  expect([...f.b.publicLog.values()].filter(event => event.type === "agent.thread_message_sent")).toHaveLength(2);
});

test("DSH predefined specialists retain their own tool set and model", async () => {
  const b = new Backend();
  b.a.launch.models!.specialist = { id: "specialist", api_backend: "messages" };
  b.a.launch.subagents = { agents: [{ name: "reader", description: "Read only", systemPrompt: "Only inspect", model: "specialist", tools: ["Read"] }] };
  const f = await fixture(b);
  f.b.onModel = async () => textEvents("done");
  const receipt = await executeTool(f.session, "subagent", { agent_preset: `${f.session.id}/reader`, description: "Read", prompt: "Read" });
  await settle(f);
  await executeTool(f.session, "send_message", { agent_id: receipt.subagentId, message: "Continue" });
  await settle(f);
  const presetId = `${f.session.id}/reader`;
  expect((await f.ctx.agentPresets.readDocument(presetId)).description).toBe("Read only");
  const childLog = await f.ctx.sessionPersistence.open(SessionId(receipt.subagentId), "read");
  try { expect(childLog.header.agentPreset).toBe(presetId); }
  finally { await childLog.close(); }
  await f.close();
  const restored = await fixture(b);
  await executeTool(restored.session, "send_message", { agent_id: receipt.subagentId, message: "Continue after restart" });
  await settle(restored);
  const children = f.b.requests.filter(request => modelSystemText(request).includes("# Subagent rules"));
  expect(children).toHaveLength(3);
  for (const request of children) {
    expect(request.model).toBe("specialist");
    expect(request.tools.map((tool: Payload) => tool.name)).toEqual(["Read"]);
    expect(modelSystemText(request)).toContain("Only inspect");
  }
});

test("DSH delegation returns acceptance before approval and restores tools and history on cold continuation", async () => {
  const f = await fixture();
  const approvals: Payload[] = [];
  f.b.onApproval = async payload => { approvals.push(payload); };
  f.b.onModel = async request => modelSystemText(request).includes("# Subagent rules")
    ? JSON.stringify(request.messages).includes("tool_result") ? textEvents("child report") : toolEvents("pwd", "child_bash")
    : textEvents("parent synthesis");
  const receipt = await executeTool(f.session, "subagent", { description: "worker", prompt: "Find a fact" });
  expect(receipt.kind).toBe("activation");
  expect(receipt.report).toBeUndefined();
  await eventually(() => approvals.length === 1);
  expect(f.b.executions).toHaveLength(0);
  expect(approvals[0]!.session_thread_id).toBe(receipt.subagentId);
  expect(f.session.subagents.threads[0]!.status).toBe("running");
  await f.session.control({ type: "control_response", response: {
    subtype: "success", request_id: approvals[0]!.request_id, response: { behavior: "allow" },
  } });
  await settle(f);
  expect(f.session.subagents.threads[0]!.status).toBe("idle");
  expect(f.ctx.agents.get(SessionId(receipt.subagentId))).toBeUndefined();
  expect(f.api.modelSessions.has(receipt.subagentId)).toBe(false);
  const catalog = await f.ctx.subagents.listChildren(f.session.agent.session.id);
  expect(catalog.map(child => child.id)).toContain(receipt.subagentId);
  const firstChild = f.b.requests.find(request => modelSystemText(request).includes("# Subagent rules"))!;
  expect(firstChild.tools.map((tool: Payload) => tool.name)).toContain("Bash");
  expect(firstChild.tools.map((tool: Payload) => tool.name)).not.toContain("send_to_agent");
  expect(f.b.executions).toHaveLength(1);
  const sources: string[] = [];
  f.ctx.on("agent/created", ({ agent, source }) => { if (agent.id === receipt.subagentId) sources.push(source); return undefined; });
  const followup = await executeTool(f.session, "send_message", { agent_id: receipt.subagentId, message: "Continue the investigation" });
  expect(followup.messageId).toBeString();
  await settle(f);
  expect(sources).toEqual(["resume"]);
  const lastChild = f.b.requests.findLast(request => modelSystemText(request).includes("# Subagent rules"))!;
  expect(JSON.stringify(lastChild.messages)).toContain("Find a fact");
  expect(JSON.stringify(lastChild.messages)).toContain("Continue the investigation");
  expect([...f.b.publicLog.values()].filter(event => event.type === "agent.thread_message_received")).toHaveLength(2);
  await f.close();
  const restored = await fixture(f.b);
  expect(restored.session.subagents.threads[0]?.tools).toEqual(f.b.a.launch.tools);
  await executeTool(restored.session, "send_message", { agent_id: receipt.subagentId, message: "After restart" });
  await settle(restored);
  expect(JSON.stringify(f.b.requests.findLast(request => modelSystemText(request).includes("# Subagent rules"))!.messages)).toContain("Continue the investigation");
});

test("DSH host waits for descendants and their parent synthesis before acknowledging input", async () => {
  const f = await fixture();
  const approval = Promise.withResolvers<Payload>();
  f.b.onDelegationApproval = async payload => {
    await f.session.control({ type: "control_response", response: { subtype: "success", request_id: payload.request_id, response: { behavior: "allow" } } });
  };
  f.b.onApproval = async payload => { approval.resolve(payload); };
  f.b.onModel = async request => {
    const history = JSON.stringify(request.messages);
    if (modelSystemText(request).includes("# Subagent rules")) return history.includes("tool_result") ? textEvents("child answer") : toolEvents("pwd");
    if (!history.includes("delegate_worker")) return delegation("worker");
    return textEvents(history.includes("child answer") ? "final synthesis" : "waiting for report");
  };
  const receiving = f.session.receive("delegation_input", { content: "Delegate" });
  void receiving.catch(() => {});
  const request = await approval.promise;
  await eventually(() => f.b.requests.some(request => JSON.stringify(request.messages).includes("started subagent")));
  await eventually(() => f.session.agent.status === "idle");
  await f.session.flushEvents();
  expect([...f.b.publicLog.values()].some(event => event.type === "result")).toBe(false);
  expect(f.b.processed).not.toContain("delegation_input");
  await eventually(() => f.b.statuses.at(-1) === "requires_action");
  await f.session.control({ type: "control_response", response: { subtype: "success", request_id: request.request_id, response: { behavior: "allow" } } });
  await receiving;
  expect(f.b.processed).toContain("delegation_input");
  expect(f.b.statuses.at(-1)).toBe("idle");
  const finalReply = [...f.b.publicLog.values()].find(event => event.type === "agent.message" && JSON.stringify(event.content).includes("final synthesis"));
  expect(finalReply).toBeDefined();
  expect([...f.b.publicLog.values()].filter(event => event.type === "result")).toHaveLength(1);
  const requests = f.b.requests.length;
  await f.close();
  const restored = await fixture(f.b);
  await restored.session.receive("delegation_input", { content: "Delegate" });
  expect(f.b.requests).toHaveLength(requests);
  expect([...f.b.publicLog.values()].filter(event => event.type === "result")).toHaveLength(1);
  expect([...f.b.publicLog.values()].filter(event => event.type === "agent.message" && JSON.stringify(event.content).includes("final synthesis")).map(event => event.uuid)).toEqual([finalReply!.uuid]);
});

test("archived DSH threads refuse follow-up before another activation", async () => {
  const f = await fixture();
  f.b.onModel = async () => textEvents("done");
  const receipt = await executeTool(f.session, "subagent", { description: "worker", prompt: "Work" });
  await settle(f);
  f.b.publicLog.set("archived", { type: "session.thread_status_terminated", session_thread_id: receipt.subagentId });
  const count = f.b.requests.length;
  await expect(executeTool(f.session, "send_message", { agent_id: receipt.subagentId, message: "Continue" })).rejects.toThrow("archived");
  expect(f.b.requests).toHaveLength(count);
});

test("persisted DSH threads continue using official tools after restart", async () => {
  const f = await fixture();
  f.b.onModel = async () => textEvents("done");
  const receipt = await executeTool(f.session, "subagent", { description: "worker", prompt: "Work" });
  await settle(f);
  await f.close();
  const restored = await fixture(f.b);
  expect(restored.ctx.tools.get("send_to_agent", restored.session.agent)).toBeUndefined();
  const continued = await executeTool(restored.session, "send_message", { agent_id: receipt.subagentId, message: "Continue" });
  expect(continued.messageId).toBeString();
  await settle(restored);
});

test("legacy OMA runtime records are rejected without registering compatibility tools", async () => {
  const b = new Backend();
  await b.ready;
  const ctx = new Context();
  cleanups.push(async () => { await ctx.fiber.dispose(); b.stop(); });
  const api = new OmaApi(b.url);
  api.assignments.set(b.a.session_id, b.a);
  await mountOma(ctx, api);
  const id = SessionId(b.a.session_id);
  const handle = await ctx.agents.create({ sessionId: id, meta: { cwd: "/workspace" }, agentOptions: { provider: "oma", model: "test-model" } });
  appendPluginRecord(handle.agent.session, "plugin:oma/subagent-runtime", { version: 1, runtime: "oma" });
  await handle.dispose();
  await ctx.ccrReplication.flush(id);
  await ctx.fiber.dispose();
  await expect(fixture(b)).rejects.toThrow("Legacy OMA delegation sessions are unsupported");
  expect(b.requests).toHaveLength(0);
});

test("root history without a runtime record opens with official DSH tools", async () => {
  const b = new Backend();
  await b.ready;
  const ctx = new Context();
  cleanups.push(async () => { await ctx.fiber.dispose(); b.stop(); });
  const api = new OmaApi(b.url);
  api.assignments.set(b.a.session_id, b.a);
  await mountOma(ctx, api);
  const id = SessionId(b.a.session_id);
  const handle = await ctx.agents.create({ sessionId: id, meta: { cwd: "/workspace" }, agentOptions: { provider: "oma", model: "test-model" } });
  handle.agent.inject(createUserMessage({ content: [{ type: "text", text: "legacy history" }], source: { kind: "user" } }));
  await handle.dispose();
  await ctx.ccrReplication.flush(id);
  await ctx.fiber.dispose();
  const restored = await fixture(b);
  expect(restored.ctx.tools.get("subagent", restored.session.agent)).toBeDefined();
  expect(JSON.stringify(restored.session.agent.session.snapshotEvents())).toContain("legacy history");
});

for (const publishDuringRead of [true, false]) test(`replication keeps contiguous events when publication overlaps a cold read: ${publishDuringRead}`, async () => {
  const f = await fixture();
  const id = SessionId(`sthr_${crypto.randomUUID()}`);
  const options = { parentAgent: f.session.agent, agentOptions: { provider: f.session.config.provider, model: f.session.config.model } };
  f.ctx.ccrReplication.bindThread(id, f.session.id);
  const child = await f.ctx.agents.create({ sessionId: id, meta: { origin: "subagent", parentSession: f.session.agent.session.id, cwd: "/workspace" }, ...options });
  child.agent.inject(createUserMessage({ content: [{ type: "text", text: "stored context" }], source: { kind: "user" } }));
  await child.dispose();
  const replication = f.ctx.ccrReplication;
  await replication.drain(id);
  replication.forget(id);
  replication.bindThread(id, f.session.id);
  const entered = Promise.withResolvers<void>();
  const release = Promise.withResolvers<void>();
  const persistence = f.ctx.sessionPersistence;
  const prototype: typeof persistence = Object.getPrototypeOf(persistence);
  const open = prototype.open;
  let intercepted = false;
  const opening = spyOn(prototype, "open").mockImplementation(async function(this: typeof persistence, sessionId, mode, options) {
    const handle = await open.call(this, sessionId, mode, options);
    if (sessionId === id && mode === "read" && !intercepted) {
      intercepted = true;
      const read = handle.read.bind(handle);
      handle.read = async (...args) => {
        const result = await read(...args);
        expect(result.events.length).toBeGreaterThan(0);
        entered.resolve();
        await release.promise;
        return result;
      };
    }
    return handle;
  });
  let resumed: Awaited<ReturnType<typeof f.ctx.agents.resume>> | undefined;
  const flushing = replication.flush(id);
  try {
    await entered.promise;
    if (!publishDuringRead) { release.resolve(); await flushing; }
    resumed = await f.ctx.agents.resume({ resumeSessionId: id, ...options });
    release.resolve();
    await flushing;
    opening.mockRestore();
    resumed.agent.inject(createUserMessage({ content: [{ type: "text", text: "new context" }], source: { kind: "user" } }));
    await replication.flush(id);
    const localEvents = resumed.agent.session.snapshotEvents();
    const remote = [...f.b.privateLog.values()].filter(payload => f.b.privateAgentIds.get(payload.uuid) === id && "seq" in payload.data).map(payload => payload.data);
    expect(remote.sort((a, b) => a.seq - b.seq)).toEqual([...localEvents]);
  } finally {
    release.resolve();
    opening.mockRestore();
    await flushing.catch(() => {});
    await resumed?.dispose();
  }
});


test("legacy child without a root runtime marker is rejected without appending the marker", async () => {
  const b = new Backend();
  await b.ready;
  const ctx = new Context();
  const api = new OmaApi(b.url);
  api.assignments.set(b.a.session_id, b.a);
  await mountOma(ctx, api);
  const session = createOmaSession(ctx, api, b.a);
  cleanups.push(async () => { try { await session.close(); } finally { await ctx.fiber.dispose(); b.stop(); } });
  const root = await ctx.agents.create({ sessionId: SessionId(session.id), meta: { cwd: "/workspace" }, agentOptions: { provider: "oma", model: "test-model" } });
  const childId = SessionId(crypto.randomUUID());
  const child = await ctx.agents.create({ sessionId: childId, parentAgent: root.agent,
    meta: { origin: "subagent", parentSession: root.agent.session.id, cwd: "/workspace" },
    seed: [{ seq: SessionSeq(0), time: Date.now(), type: "subagent/descriptor", data: { version: 3, mode: "continuable", provider: "oma-dsh-inline", label: "legacy" } }],
    agentOptions: { provider: "oma", model: "test-model" } });
  b.publicLog.set("legacy-created", { type: "session.thread_created", session_thread_id: childId });
  await child.dispose();
  await root.dispose();
  const beforeHandle = await ctx.sessionPersistence.open(SessionId(session.id), "read");
  const before = (await beforeHandle.read()).events;
  await beforeHandle.close();
  await expect(session.open()).rejects.toThrow("legacy OMA threads require a new session");
  await session.close();
  const afterHandle = await ctx.sessionPersistence.open(SessionId(session.id), "read");
  try { expect((await afterHandle.read()).events).toEqual(before); }
  finally { await afterHandle.close(); }
});


test("root announcement failure preserves its error and allows session cleanup", async () => {
  const b = new Backend();
  await b.ready;
  const ctx = new Context();
  const api = new OmaApi(b.url);
  api.assignments.set(b.a.session_id, b.a);
  await mountOma(ctx, api);
  const session = createOmaSession(ctx, api, b.a);
  cleanups.push(async () => { try { await session.close(); } finally { await ctx.fiber.dispose(); b.stop(); } });
  ctx.on("agent/created", ({ agent }) => { if (agent.id === session.id) throw new Error("announce failed"); return undefined; });
  await expect(session.open()).rejects.toThrow("announce failed");
  expect(session.runtime.handle).toBeUndefined();
  expect(session.abort.signal.aborted).toBe(true);
  expect(ctx.agents.get(SessionId(session.id))).toBeUndefined();
  await expect(session.close()).resolves.toBeUndefined();
});
