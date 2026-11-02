import { runSubagent, subagentScripts } from "./subagent-tools.ts";
import { nativeOutcomes } from "./session-log.ts";
import { modelSystemText } from "./backend.ts";
import { afterEach, expect, spyOn, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { SessionId, SESSION_FORMAT_VERSION } from "@deepseek-ai/dsh-session";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { Backend, textEvents, toolEvents, eventually } from "./backend.ts";
import type { Payload } from "../src/json.ts";
import { CcrSubagentBridge } from "../src/ccrv2/subagents.ts";

const cleanups: (() => Promise<void>)[] = [];
afterEach(async () => { for (const cleanup of cleanups.splice(0).reverse()) await cleanup(); });
async function fixture(b = new Backend()) {
  if (!cleanups.length) cleanups.push(async () => b.stop());
  await b.ready;
  const ctx = new Context();
  const api = new OmaApi(b.url);
  await mountOma(ctx, api);
  const session = createOmaSession(ctx, api, b.a);
  b.onApproval = async (payload) => {
    if (!["list_agents", "subagent", "send_message", "interrupt_agent"].includes(payload.request.tool_name)) return;
    await session.control({ type: "control_response", response: {
      subtype: "success", request_id: payload.request_id, response: { behavior: "allow" },
    } });
  };
  await session.open();
  const close = async () => { await session.close(); await ctx.fiber.dispose(); };
  cleanups.push(close);
  return { b, ctx, api, session, close };
}
function call(name: string, args: Payload, id: string): Payload[] {
  return toolEvents("", id).map((event) => event.type === "content_block_start"
    ? { ...event, content_block: { ...event.content_block, name } }
    : event.type === "content_block_delta"
      ? { ...event, delta: { type: "input_json_delta", partial_json: JSON.stringify(args) } } : event);
}

test("disabled subagents are absent from the model tools and reject direct dispatch", async () => {
  const b = new Backend();
  delete b.a.launch.subagents;
  const f = await fixture(b);
  expect(f.ctx.tools.get("subagent", f.session.agent)).toBeUndefined();
  b.modelScript = [textEvents("done")];
  await f.session.receive("disabled", { content: "Inspect locally" });
  const request = b.requests[0]!;
  const tools = request.tools.map((tool: Payload) => tool.name);
  for (const name of ["list_agents", "subagent", "send_message", "interrupt_agent", "subagent_specialist_1"]) expect(tools).not.toContain(name);
  expect(modelSystemText(request)).toContain("Subagents are disabled for this session.");
  expect(f.session.subagents.threads).toHaveLength(0);
});

for (const status of ["idle", "terminated"] as const) {
  test(`restore handles an incomplete initialization header with ${status} authority`, async () => {
    const f = await fixture();
    const id = "sthr_failed_initialization";
    await f.ctx.ccrv2.config.backend.persist(f.session.id, "dsh_log", {
      version: SESSION_FORMAT_VERSION, id: SessionId(id), createdAt: 0, isSeeded: false,
      origin: "subagent", parentSession: SessionId(f.session.id), delegationDepth: 1,
    }, id);
    expect([...f.b.privateLog.values()].some(payload => payload.data?.id === id)).toBe(true);
    f.b.publicLog.set(`${id}_created`, { type: "session.thread_created", session_thread_id: id });
    if (status === "terminated") f.b.publicLog.set(`${id}_terminated`, { type: "session.thread_status_terminated", session_thread_id: id });
    const storage = new CcrSubagentBridge(f.session);
    if (status === "idle") {
      await expect(storage.restoreThreads()).rejects.toThrow("Subagent is missing its native descriptor");
    } else {
      await storage.restoreThreads();
      expect(storage.threads()).toEqual([]);
    }
    expect(f.b.requests).toHaveLength(0);
  });
}

test("publishes the child snapshot before requesting its scoped tool context", async () => {
  const f = await fixture();
  const route = f.b.route.bind(f.b);
  const contexts: string[] = [];
  f.b.route = async (request) => {
    const id = request.headers.get("X-OMA-Session-Thread-ID");
    if (id && new URL(request.url).pathname === `/v2/sessions/${f.b.a.session_id}`) {
      contexts.push(id);
      const created = [...f.b.publicLog.values()].some((event) => event.type === "session.thread_created" && event.session_thread_id === id);
      if (!created) return Response.json({ error: "Thread snapshot does not exist" }, { status: 500 });
    }
    return route(request);
  };
  f.b.modelScript = [textEvents("child report")];
  const result = await runSubagent(f.session, "subagent", { description: "python_agent", prompt: ["Write Python", "Return hello world"].filter(Boolean).join("\n") });
  expect(result.report).toBe("child report");
  expect(contexts).toContain(result.subagentId);
  expect(f.b.requests).toHaveLength(1);
});

test("restore ignores an unpublished child header left by interrupted initialization", async () => {
  const f = await fixture();
  const storage = new CcrSubagentBridge(f.session);
  const id = "sthr_unpublished";
  storage.bindPersistence(id);
  const handle = await f.ctx.sessionPersistence.create({
    version: SESSION_FORMAT_VERSION, id: SessionId(id), createdAt: 0, isSeeded: false,
    origin: "subagent", parentSession: SessionId(f.session.id), delegationDepth: 1,
  });
  await handle.close();
  await f.close();
  cleanups.pop();
  const restored = await fixture(f.b);
  expect(restored.session.subagents.threads).toEqual([]);
  expect(restored.b.requests).toHaveLength(0);
});

test("delegates with isolated history, no nested delegation, shared approvals, and retained follow-up history after restart", async () => {
  const f = await fixture();
  subagentScripts(f.b, [call("subagent", { description: "researcher", prompt: ["Inspect files", "Find a fact"].filter(Boolean).join("\n") }, "delegate"), textEvents("parent synthesis")], [toolEvents("pwd", "child_bash"), textEvents("child report")]);
  f.b.onApproval = async (payload) => {
    if (payload.request.tool_name === "Bash") expect(payload.session_thread_id).toMatch(/^[0-9a-f-]{36}$/);
    await f.session.control({ type: "control_response", response: {
      subtype: "success", request_id: payload.request_id, response: { behavior: "allow" },
    } });
  };
  await f.session.receive("first", { content: "Delegate this work" });
  expect(f.b.requests.filter(request => modelSystemText(request).includes("# Subagent rules"))).toHaveLength(2);
  const created = [...f.b.publicLog.values()].find((event) => event.type === "session.thread_created");
  expect(created?.agent).toEqual({ type: "inline", name: "researcher", system: "Inspect files\nFind a fact" });
  expect(modelSystemText(f.b.requests[1]!)).toContain("# Specialist instructions\n\nInspect files\nFind a fact");
  expect(f.session.subagents.threads?.[0]?.inline).toBe(true);
  const parentTools = f.b.requests[0]!.tools.map((tool: Payload) => tool.name);
  expect(parentTools).toContain("subagent");
  const childTools = f.b.requests[1]!.tools.map((tool: Payload) => tool.name);
  expect(childTools).toContain("Bash");
  expect(childTools).not.toContain("subagent");
  expect(JSON.stringify(f.b.requests[1]!.messages)).not.toContain("Delegate this work");
  expect(JSON.stringify(f.b.requests.at(-1)!.messages)).toContain("child report");
  const thread = f.session.subagents.threads![0]!;
  expect(thread.status).toBe("idle");
  expect(f.b.executions).toHaveLength(1);
  expect([...f.b.privateLog.values()].some((payload) => payload.type === "dsh_log" && f.b.privateAgentIds.get(payload.uuid) === thread.id && "seq" in payload.data)).toBe(true);
  expect([...f.b.publicLog.values()].some((payload) => payload.type === "agent.thread_message_received" && payload.from_session_thread_id === thread.id)).toBe(true);
  const messages = [...f.b.publicLog.values()].filter((event) => event.type === "agent.thread_message_sent" || event.type === "agent.thread_message_received");
  expect(messages).toHaveLength(2);
  for (const event of messages) {
    expect(event.session_thread_id).toBeUndefined();
    expect(event.agent_name).toBeUndefined();
  }
  expect(messages[0]!.to_session_thread_id).toBe(thread.id);
  expect(messages[1]!.from_session_thread_id).toBe(thread.id);

  expect([...f.b.publicLog.values()].find((payload) => payload.type === "agent.message" && payload.session_thread_id === thread.id)?.content[0].text).toBe("child report");
  await f.close();
  cleanups.pop();
  expect(f.api.modelSessions.size).toBe(0);
  const restored = await fixture(f.b);
  subagentScripts(f.b, [call("send_message", { agent_id: thread.id, message: "Explain that fact" }, "followup"), textEvents("followup synthesis")], [textEvents("more details")]);
  await restored.session.receive("second", { content: "Follow up" });
  const childRequest = f.b.requests.findLast(request => modelSystemText(request).includes("# Subagent rules"))!;
  expect(JSON.stringify(childRequest.messages)).toContain("child report");
  expect(JSON.stringify(childRequest.messages)).toContain("Explain that fact");
  expect(JSON.stringify(childRequest.messages)).not.toContain("parent synthesis");
  expect(restored.session.subagents.threads).toHaveLength(1);
});

test("predefined specialist uses its own prompt, model and restricted tools", async () => {
  const b = new Backend();
  cleanups.push(async () => b.stop());
  b.a.launch.models!["review-model"] = { id: "review-model", api_backend: "messages" };
  const agentSnapshot = {
    type: "agent", id: "agent_reviewer", version: 3, name: "reviewer", system: "Review only", model: "review-model",
    tools: [{ type: "agent_toolset_20260401", default_config: { enabled: false }, configs: [
      { name: "read", enabled: true, permission_policy: { type: "always_ask" } },
      { name: "grep", enabled: true, permission_policy: { type: "always_allow" } },
    ] }], mcp_servers: [], skills: [],
  };
  b.a.launch.subagents = { inlineAgents: false, agents: [{
    name: "reviewer", description: "Read-only reviewer", systemPrompt: "Review only", model: "review-model", tools: ["Read", "Grep"],
    agentSnapshot,
  }] };
  const f = await fixture(b);
  subagentScripts(b, [call("subagent", { agent_preset: `${f.session.id}/reviewer`, description: "reviewer", prompt: "Review the code" }, "delegate"), textEvents("done")], [textEvents("review report")]);
  await f.session.receive("specialist", { content: "Review" });
  expect(f.session.runtime.config.subagents?.agents).toEqual([{
    name: "reviewer", description: "Read-only reviewer", systemPrompt: "Review only", model: "review-model", tools: ["Read", "Grep"],
  }]);
  expect(f.session.subagents.threads?.[0]?.id).toMatch(/^[0-9a-f-]{36}$/);
  const child = b.requests[1]!;
  expect(child.model).toBe("review-model");
  const created = [...b.publicLog.values()].find((event) => event.type === "session.thread_created");
  expect(created?.agent).toEqual(agentSnapshot);
  expect(f.session.subagents.threads?.[0]?.inline).toBe(false);
  expect(modelSystemText(child)).toContain("Review only");
  expect(child.tools.map((tool: Payload) => tool.name).sort()).toEqual(["Grep", "Read"]);
  await expect(runSubagent(f.session, "subagent", { description: "inline", prompt: ["test", "test"].filter(Boolean).join("\n") })).rejects.toThrow();
});

test("approval count alone can identify a parent delegation wait before the child exists", async () => {
  const f = await fixture();
  f.b.onApproval = async () => {};
  subagentScripts(f.b, [call("subagent", { description: "child", prompt: ["test", "run"].filter(Boolean).join("\n") }, "delegate"), textEvents("Waiting for child")], []);
  const turn = f.session.receive("parent-wait", { content: "Delegate" });
  const settled = turn.catch(() => {});
  try {
    await eventually(() => f.session.approvals.size === 1);
    expect([...f.session.approvals.values()][0]!.threadId).toBe(f.session.id);
    expect(f.session.subagents.threads).toHaveLength(0);
    await f.session.control({ type: "user.interrupt" });
    await turn;
    expect(f.session.approvals.size).toBe(0);
    expect(f.b.executions).toHaveLength(0);
  } finally { f.session.stop(); await settled; }
});

test.each(["user.interrupt", "control_request"])("%s interrupting the whole session cancels child approval before sandbox dispatch", async (type) => {
  const f = await fixture();
  subagentScripts(f.b, [call("subagent", { description: "child", prompt: ["test", "run"].filter(Boolean).join("\n") }, "delegate"), textEvents("Waiting for child")], [toolEvents("pwd", "child_tool")]);
  const turn = f.session.receive("interrupt", { content: "Delegate" });
  const settled = turn.catch(() => {});
  try {
    await eventually(() => [...f.session.approvals.values()].some((approval) =>
      approval.threadId !== f.session.id && approval.details.tool_use_id === "child_tool"));
    const thread = f.session.subagents.threads[0]!;
    expect(thread.status).toBe("running");
    expect([...f.session.approvals.values()].some((approval) => approval.threadId === thread.id)).toBe(true);
    expect(f.b.executions).toHaveLength(0);
    await f.session.control({ type, ...(type === "control_request" ? { request_id: "global", request: { subtype: "interrupt" } } : {}) });
    await turn;
    expect(f.session.approvals.size).toBe(0);
    expect(f.b.executions).toHaveLength(0);
    expect(thread.status).toBe("idle");
  } finally { f.session.stop(); await settled; }
});

test("a child read approved during drain still executes and persists its result", async () => {
  const f = await fixture();
  subagentScripts(f.b, [call("subagent", { description: "child", prompt: ["test", "inspect"].filter(Boolean).join("\n") }, "delegate"), textEvents("done")], [call("Read", { file_path: "/tmp/read.txt" }, "child_read"), textEvents("child report")]);
  const turn = f.session.receive("draining-child", { content: "Delegate" });
  const settled = turn.catch(() => {});
  try {
    await eventually(() => [...f.session.approvals.values()].some((approval) => approval.details.tool_use_id === "child_read"));
    expect(await f.session.drain(0)).toBe(true);
    const request = [...f.session.approvals].find(([, approval]) => approval.details.tool_use_id === "child_read")!;
    await f.session.control({ type: "control_response", response: {
      subtype: "success", request_id: request[0], response: { behavior: "allow" },
    } });
    await turn;
    expect(f.b.executions.map((execution) => execution.name)).toEqual(["read"]);
    const thread = f.session.subagents.threads[0]!;
    expect((await nativeOutcomes(f.session, thread.id)).child_read?.failed).toBe(false);
    expect(thread.status).toBe("idle");
  } finally { f.session.stop(); await settled; }
});

test("lost child tool outcome blocks parent recovery until new input and never replays the command", async () => {
  const f = await fixture();
  const lostResponse = Promise.withResolvers<Payload>();
  f.b.onTool = () => lostResponse.promise;
  f.b.onCancel = async () => { lostResponse.reject(new Error("Tool response lost after cancellation")); };
  subagentScripts(f.b, [call("subagent", { description: "child", prompt: ["test", "run"].filter(Boolean).join("\n") }, "delegate"), textEvents("Waiting for child")], [toolEvents("side effect", "child_tool")]);
  f.b.onApproval = async (payload) => {
    await f.session.control({ type: "control_response", response: {
      subtype: "success", request_id: payload.request_id, response: { behavior: "allow" },
    } });
  };
  const turn = f.session.receive("unknown", { content: "Delegate" }).catch(() => {});
  await eventually(() => f.b.executions.length === 1);
  f.session.stop();
  await turn;
  expect((await nativeOutcomes(f.session, f.session.subagents.threads[0]!.id)).child_tool?.failed).toBe(true);
  await f.close();
  cleanups.pop();
  f.b.onTool = undefined;
  f.b.onCancel = undefined;
  const restored = await fixture(f.b);
  expect(restored.session.abort.signal.aborted).toBe(false);
  const requests = f.b.requests.length;
  f.b.modelScript = [textEvents("checked the child state")];
  await restored.session.receive("retry", { content: "try again" });
  expect(restored.session.abort.signal.aborted).toBe(false);
  expect(f.b.executions).toHaveLength(1);
  expect(f.b.requests).toHaveLength(requests + 1);
  expect(JSON.stringify(f.b.requests.at(-1)!.messages)).toContain("delegate");
});

test.each(["user.interrupt", "control_request"])("%s targeted child interruption stops child work and allows a new parent input", async (type) => {
  const f = await fixture();
  subagentScripts(f.b, [call("subagent", { description: "child", prompt: ["test", "run"].filter(Boolean).join("\n") }, "delegate"), textEvents("RESUMED")], [toolEvents("pwd", "child_tool")]);
  const turn = f.session.receive("target", { content: "Delegate" });
  await eventually(() => f.session.subagents.threads?.length === 1 && f.session.approvals.size === 1);
  const thread = f.session.subagents.threads![0]!;
  await f.session.control({ type, session_thread_id: thread.id, ...(type === "control_request" ? { request_id: "child", request: { subtype: "interrupt" } } : {}) });
  await turn;
  expect(f.b.executions).toHaveLength(0);
  const beforeResume = f.b.requests.length;
  f.b.onModel = undefined;
  f.b.modelScript = [textEvents("RESUMED")];
  await f.session.receive("resume-after-child-interrupt", {content:"Reply RESUMED, do not restart cancelled work."});
  expect(f.b.requests).toHaveLength(beforeResume + 1);
  expect([...f.b.publicLog.values()].some((event) => event.type === "agent.message" && !event.session_thread_id && event.content[0].text === "RESUMED")).toBe(true);
  f.b.modelScript = [toolEvents("pwd", "primary_tool")];
  const primaryTurn = f.session.receive("primary", { content: "Run" });
  await eventually(() => f.session.approvals.size === 1);
  await f.session.control({ type, session_thread_id: f.session.id, ...(type === "control_request" ? { request_id: "primary", request: { subtype: "interrupt" } } : {}) });
  await primaryTurn;
  expect(f.b.executions).toHaveLength(0);
});

test("configured presets are advertised by the official delegation tool without inline fallback", async () => {
  const b = new Backend();
  cleanups.push(async () => b.stop());
  b.a.launch.models!["fast-model"] = { id: "fast-model", api_backend: "messages" };
  b.a.launch.models!["slow-model"] = { id: "slow-model", api_backend: "messages" };
  b.a.launch.subagents = JSON.parse(`{
    "inlineAgents": false,
    "agents": [
      {"name":"slow_probe","description":"Slow probe","systemPrompt":"Pinned slow instructions","model":"slow-model","tools":["Bash"]},
      {"name":"fast_probe","description":"Fast probe","systemPrompt":"Pinned fast instructions","model":"fast-model","tools":[]}
    ]
  }`);
  const f = await fixture(b);
  subagentScripts(b, [call("list_agents", {}, "roster"), call("subagent", { agent_preset: `${f.session.id}/fast_probe`, description: "fast_probe", prompt: "Report your assigned task" }, "delegate"), textEvents("done")], [textEvents("fast report")]);
  await f.session.receive("configured-roster", { content: "Use the configured fast_probe" });
  const listing = b.requests[1]!.messages.flatMap((message: Payload) => message.content)
    .find((block: Payload) => block.type === "tool_result" && block.tool_use_id === "roster");
  expect(JSON.stringify(listing.content)).toContain("no subagents");
  expect(JSON.stringify(b.requests[0]!.tools)).toContain("fast_probe");
  const child = b.requests[2]!;
  expect(child.model).toBe("fast-model");
  expect(modelSystemText(child)).toContain("Pinned fast instructions");
  expect(child.tools ?? []).toEqual([]);
  expect(f.session.subagents.threads!.map((thread) => thread.name)).toEqual(["fast_probe"]);
  expect(f.ctx.tools.get("subagent", f.session.agent)).toBeDefined();
});

test("a retained followup checks authoritative archive before creating child work", async () => {
  const f = await fixture();
  subagentScripts(f.b, [call("subagent", { description: "external_archive", prompt: ["Reply READY", "Reply READY"].filter(Boolean).join("\n") }, "create_before_archive"), textEvents("done")], [textEvents("READY")]);
  await f.session.receive("before_external_archive",{content:"Delegate once"});
  const child = f.session.subagents.threads![0]!;
  expect(child.status).toBe("idle");
  const before = f.b.requests.length;
  const baseRoute = f.b.route.bind(f.b);
  let checks = 0;
  f.b.route = async (request) => {
    const url = new URL(request.url);
    if (request.method === "GET" && url.pathname.endsWith(`/worker/subagent-threads/${child.id}`)) {
      checks++;
      expect(url.searchParams.get("worker_epoch")).toBe(String(f.b.a.worker_epoch));
      return Response.json({id:child.id,status:"terminated",archived:true});
    }
    return baseRoute(request);
  };
  subagentScripts(f.b, [call("send_message", { agent_id: child.id, message: "Reply READY" }, "followup_after_external_archive"), textEvents("Waiting for child")], [textEvents("rejected")]);
  await f.session.receive("after_external_archive",{content:"Attempt the same existing child once and report real error, do not create another."});
  expect(checks).toBe(1);
  expect(f.b.requests.length).toBe(before+2);
  expect(f.session.subagents.threads).toHaveLength(1);
  expect(child.status).toBe("terminated");
  const error = [...f.b.publicLog.values()].filter((event) => event.type === "user").flatMap((event) => event.message.content).find((block:Payload) => block.type === "tool_result" && block.tool_use_id === "followup_after_external_archive");
  expect(error?.is_error).toBe(true);
  expect(JSON.stringify(error?.content)).toContain("archived");
});

test("root registers only enabled sandbox tools and inline agents inherit them", async () => {
  const b = new Backend();
  cleanups.push(async () => b.stop());
  b.a.launch.tools = ["Read", "Grep"];
  b.a.launch.subagents = {};
  const f = await fixture(b);
  subagentScripts(b, [call("subagent", { description: "inline", prompt: ["Inspect only", "Inspect"].filter(Boolean).join("\n") }, "delegate"), textEvents("done")], [textEvents("inline report")]);
  await f.session.receive("restricted", { content: "Inspect" });
  const root = b.requests[0]!;
  expect(root.tools.map((tool: Payload) => tool.name).sort()).toEqual(["Grep", "Read", "interrupt_agent", "list_agents", "send_message", "subagent"]);
  expect(b.requests[1]!.tools.map((tool: Payload) => tool.name).sort()).toEqual(["Grep", "Read"]);
});

test("predefined specialist uses its own enabled tools independently of the parent", async () => {
  const b = new Backend();
  cleanups.push(async () => b.stop());
  b.a.launch.tools = ["Read", "Grep", "Glob"];
  b.a.launch.subagents = { inlineAgents: false, agents: [{
    name: "writer", description: "Writes files", systemPrompt: "Write things", tools: ["Read", "Write", "Edit", "Grep"],
  }] };
  const f = await fixture(b);
  subagentScripts(b, [call("subagent", { agent_preset: `${f.session.id}/writer`, description: "writer", prompt: "Write" }, "delegate"), textEvents("done")], [textEvents("writer report")]);
  await f.session.receive("intersect", { content: "Write" });
  expect(b.requests[0]!.tools.map((tool: Payload) => tool.name)).not.toContain("Write");
  expect(b.requests[1]!.tools.map((tool: Payload) => tool.name).sort()).toEqual(["Edit", "Grep", "Read", "Write"]);
  expect(b.requests[1]!.tools.map((tool: Payload) => tool.name)).not.toContain("Glob");
});


test("public API archive releases exactly one retained capacity slot before new delegation", async () => {
  const f = await fixture();
  f.session.subagents.threads.push(...Array.from({ length: 25 }, (_, index) => ({
    id: `sthr_external_${index}`, name: "test", systemPrompt: "test", model: "test-model", status: "idle" as const,
  })));
  const originalRoute = f.b.route.bind(f.b);
  const checked: string[] = [];
  f.b.route = async (request) => {
    const path = new URL(request.url).pathname;
    if (request.method === "GET" && path.includes("/worker/subagent-threads/")) {
      const id = path.split("/worker/subagent-threads/")[1]!;
      checked.push(id);
      return Response.json({ id, status: id === "sthr_external_0" ? "terminated" : "idle", archived: id === "sthr_external_0" });
    }
    return originalRoute(request);
  };
  f.b.modelScript = [textEvents("replacement report")];
  const result = await runSubagent(f.session, "subagent", { description: "replacement", prompt: ["test", "report"].filter(Boolean).join("\n") });
  expect(result.report).toBe("replacement report");
  expect(checked).toHaveLength(25);
  expect(f.session.subagents.threads).toHaveLength(26);
  expect(f.session.subagents.threads[0]!.status).toBe("terminated");
  expect(f.session.subagents.threads.filter((thread) => thread.status !== "terminated")).toHaveLength(25);
  expect([...f.b.publicLog.values()].filter((event) => event.type === "session.thread_status_terminated")).toHaveLength(0);
});


test("capacity refresh rejects unknown authority without freeing an idle slot", async () => {
  const f = await fixture();
  f.session.subagents.threads.push(...Array.from({ length: 25 }, (_, index) => ({
    id: `sthr_unknown_${index}`, name: "test", systemPrompt: "test", model: "test-model", status: "idle" as const,
  })));
  const before = f.b.requests.length;
  await expect(runSubagent(f.session, "subagent", { description: "new", prompt: ["test", "test"].filter(Boolean).join("\n") })).rejects.toThrow();
  expect(f.b.requests.length).toBe(before);
  expect(f.session.subagents.threads.filter((thread) => thread.status !== "terminated")).toHaveLength(25);
});

test("capacity refresh shares admission and never creates two replacements for one archived slot", async () => {
  const f = await fixture();
  f.session.subagents.threads.push(...Array.from({ length: 25 }, (_, index) => ({
    id: `sthr_race_${index}`, name: "test", systemPrompt: "test", model: "test-model", status: "idle" as const,
  })));
  const originalRoute = f.b.route.bind(f.b);
  f.b.route = async (request) => {
    const path = new URL(request.url).pathname;
    if (request.method === "GET" && path.includes("/worker/subagent-threads/")) {
      const id = path.split("/worker/subagent-threads/")[1]!;
      await Bun.sleep(1);
      return Response.json({ id, status: id === "sthr_race_0" ? "terminated" : "idle", archived: id === "sthr_race_0" });
    }
    return originalRoute(request);
  };
  f.b.modelScript = [textEvents("one replacement")];
  const results = await Promise.allSettled(["a", "b"].map((name) => runSubagent(f.session, "subagent", { description: name, prompt: ["test", "report"].filter(Boolean).join("\n") })));
  expect(results.filter((result) => result.status === "fulfilled")).toHaveLength(1);
  expect(results.filter((result) => result.status === "rejected")).toHaveLength(1);
  expect(f.session.subagents.threads).toHaveLength(26);
  expect(f.session.subagents.threads.filter((thread) => thread.status !== "terminated")).toHaveLength(25);
});

test.each([false, true])("predefined specialist without enabled tools does not inherit parent tools (explicit=%s)", async explicit => {
  const b = new Backend();
  cleanups.push(async () => b.stop());
  b.a.launch.tools = ["Read", "Write"];
  b.a.launch.subagents = { inlineAgents: false, agents: [{
    name: "text-only", description: "No builtin tools", systemPrompt: "Only answer", tools: explicit ? [] : undefined,
  }] };
  const f = await fixture(b);
  subagentScripts(b, [call("subagent", { agent_preset: `${f.session.id}/text-only`, description: "text-only", prompt: "Answer" }, "delegate"), textEvents("done")], [textEvents("report")]);
  await f.session.receive("empty-specialist", { content: "Delegate" });
  expect(b.requests[0]!.tools.map((tool: Payload) => tool.name)).toContain("Write");
  expect(b.requests[1]!.tools ?? []).toEqual([]);
  expect(f.session.subagents.threads[0]?.tools).toEqual([]);
});

test("specialist sandbox calls carry child identity and root calls remain unscoped", async () => {
  const b = new Backend();
  cleanups.push(async () => b.stop());
  b.a.launch.tools = ["Read"];
  b.a.launch.subagents = { inlineAgents: false, agents: [{
    name: "writer", description: "Writes owned files", systemPrompt: "Write the assigned file", tools: ["Write"],
  }] };
  const f = await fixture(b);
  const route = b.route.bind(b);
  const calls: { tool: string; thread: string | null }[] = [];
  b.route = async request => {
    if (request.method === "POST") {
      const body = await request.clone().json();
      if (body.method === "tools/call") {
        const thread = request.headers.get("X-OMA-Session-Thread-ID");
        calls.push({ tool: body.params.name, thread });
        const permitted = body.params.name === "write" ? thread === f.session.subagents.threads[0]?.id : thread === null;
        if (!permitted) return Response.json({ error: { message: "wrong tool execution scope" } },
          { status: 403, headers: { "X-Oma-Sandbox-Not-Forwarded": "true" } });
      }
    }
    return route(request);
  };
  spyOn(f.session, "approve").mockResolvedValue({ allowed: true });
  subagentScripts(b, [call("subagent", { agent_preset: `${f.session.id}/writer`, description: "writer", prompt: "Write assigned file" }, "delegate"), call("Read", { file_path: "/workspace/owned.txt" }, "root-read"), textEvents("done")], [call("Write", { file_path: "/workspace/owned.txt", content: "owned" }, "child-write"), textEvents("written")]);
  await f.session.receive("thread-boundary", { content: "Delegate then read" });
  expect(calls).toHaveLength(2);
  expect(calls).toEqual(expect.arrayContaining([{ tool: "write", thread: f.session.subagents.threads[0]!.id }, { tool: "read", thread: null }]));
  expect(b.executions.map(call => call.name).sort()).toEqual(["read", "write"]);
  expect(b.requests[0]!.tools.map((tool: Payload) => tool.name)).not.toContain("Write");
});


test("concurrent inline children retain their own task-defined system with identical display names", async () => {
 const b = new Backend();
 cleanups.push(async () => b.stop());
 b.a.launch.system = "PARENT-ONLY-SYSTEM-CANARY";
 const f = await fixture(b);
 b.onModel = async request => textEvents(modelSystemText(request).includes("INLINE-A-INSTRUCTIONS") ? "REPORT-A" : "REPORT-B");
 const [a,other] = await Promise.all([
  runSubagent(f.session,"subagent",{description:"same display",prompt:"INLINE-A-INSTRUCTIONS"}),
  runSubagent(f.session,"subagent",{description:"same display",prompt:"INLINE-B-INSTRUCTIONS"}),
 ]);
 expect(a.subagentId).not.toBe(other.subagentId);
 expect(a.report).toBe("REPORT-A");
 expect(other.report).toBe("REPORT-B");
 for (const [id,task,foreign] of [[a.subagentId,"INLINE-A-INSTRUCTIONS","INLINE-B-INSTRUCTIONS"],[other.subagentId,"INLINE-B-INSTRUCTIONS","INLINE-A-INSTRUCTIONS"]]) {
  const thread=f.session.subagents.threads.find(thread=>thread.id===id)!;
  expect(thread.systemPrompt).toBe(task);
  const created=[...b.publicLog.values()].find(event=>event.type==="session.thread_created" && event.session_thread_id===id);
  expect(created?.agent).toEqual({type:"inline",name:"same display",system:task});
  const requests=b.requests.filter(request=>modelSystemText(request).includes(task));
  expect(requests.length).toBeGreaterThan(0);
  for (const request of requests) {
   expect(modelSystemText(request)).not.toContain(foreign!);
   expect(modelSystemText(request)).not.toContain("PARENT-ONLY-SYSTEM-CANARY");
  }
 }
});
