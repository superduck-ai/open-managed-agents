import { nativeOutcomes } from "./session-log.ts";
import { modelSystemText } from "./backend.ts";
import { afterEach, expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { SessionId, SESSION_FORMAT_VERSION } from "@deepseek-ai/dsh-session";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { Backend, textEvents, toolEvents, eventually } from "./backend.ts";
import type { Payload } from "../src/json.ts";
import { Subagents } from "../src/runtime/subagents.ts";
import { CcrSubagentBridge } from "../src/ccrv2/subagents.ts";
import type { SubagentEvent, SubagentThread, ToolApproval, ToolExecutionBridge } from "../src/runtime/contracts.ts";

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
    if (!["list_agents", "send_to_agent", "archive_agent"].includes(payload.request.tool_name)) return;
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

test("runtime accepts an independent event sink and a class-based execution bridge", async () => {
  const f = await fixture();
  const storage = new CcrSubagentBridge(f.session);
  const threads: SubagentThread[] = [];
  const events: SubagentEvent[] = [];
  const approvals: string[] = [];
  // Prototype methods and receiver state must survive runtime's flush wrapper.
  class Execution implements ToolExecutionBridge {
    constructor(readonly delegate: ToolExecutionBridge) {}
    async approve(request: ToolApproval) {
      approvals.push(request.callId);
      return { allowed: true };
    }
    runTool<T>(callId: string, execute: () => Promise<T>, readOnly: boolean) { return this.delegate.runTool(callId, execute, readOnly); }
    approvalChanged(name: string, callId: string, effective: Payload) { this.delegate.approvalChanged(name, callId, effective); }
  }
  const runtime = new Subagents(f.session.runtime, {
    threads: () => threads,
    bindPersistence: (id) => storage.bindPersistence(id),
    emit: async (event) => { events.push(structuredClone(event)); await runtime.flush(); },
    onEvent: () => {},
    onStream: () => {},
    toolExecution: (id) => new Execution(storage.toolExecution(id)),
  });
  f.b.modelScript = [toolEvents("pwd", "injected_call"), textEvents("independent report")];
  try {
    const result = JSON.parse(await runtime.send({
      name: "independent", system_prompt: "Inspect", message: "Inspect the workspace",
    }, new AbortController().signal));
    expect(result.report).toBe("independent report");
    expect(threads[0]!.status).toBe("idle");
    expect(approvals).toEqual(["injected_call"]);
    expect(f.b.executions).toHaveLength(1);
    expect(events.map((event) => event.kind)).toEqual([
      "created", "message-sent", "running", "report-received", "idle",
    ]);
    expect([...f.b.publicLog.values()].some((event) => event.type.startsWith("session.thread_"))).toBe(false);
    expect(f.session.subagents.threads ?? []).toHaveLength(0);
  } finally {
    runtime.stop();
    await runtime.close();
  }
});

test("delegates with isolated history, no nested delegation, shared approvals, and retained follow-up history after restart", async () => {
  const f = await fixture();
  f.b.modelScript = [
    call("send_to_agent", { name: "researcher", system_prompt: "Inspect files", message: "Find a fact" }, "delegate"),
    toolEvents("pwd", "child_bash"), textEvents("child report"), textEvents("parent synthesis"),
  ];
  f.b.onApproval = async (payload) => {
    if (payload.request.tool_name === "Bash") expect(payload.session_thread_id).toStartWith("sthr_");
    await f.session.control({ type: "control_response", response: {
      subtype: "success", request_id: payload.request_id, response: { behavior: "allow" },
    } });
  };
  await f.session.receive("first", { content: "Delegate this work" });
  expect(f.b.requests).toHaveLength(4);
  const created = [...f.b.publicLog.values()].find((event) => event.type === "session.thread_created");
  expect(created?.agent).toEqual({ type: "inline", name: "researcher", system: "Inspect files" });
  expect(f.session.subagents.threads?.[0]?.inline).toBe(true);
  const parentTools = f.b.requests[0]!.tools.map((tool: Payload) => tool.name);
  expect(parentTools).toContain("send_to_agent");
  const childTools = f.b.requests[1]!.tools.map((tool: Payload) => tool.name);
  expect(childTools).toContain("Bash");
  expect(childTools).not.toContain("send_to_agent");
  expect(JSON.stringify(f.b.requests[1]!.messages)).not.toContain("Delegate this work");
  expect(JSON.stringify(f.b.requests[3]!.messages)).toContain("child report");
  const thread = f.session.subagents.threads![0]!;
  expect(thread.status).toBe("idle");
  expect(f.b.executions).toHaveLength(1);
  expect([...f.b.privateLog.values()].some((payload) => payload.type === "dsh_log" && f.b.privateAgentIds.get(payload.uuid) === thread.id && "seq" in payload.data)).toBe(true);
  expect([...f.b.publicLog.values()].some((payload) => payload.type === "agent.thread_message_received" && payload.from_session_thread_id === thread.id)).toBe(true);
  expect([...f.b.publicLog.values()].find((payload) => payload.type === "agent.message" && payload.session_thread_id === thread.id)?.content[0].text).toBe("child report");
  await f.close();
  cleanups.pop();
  expect(f.api.modelSessions.size).toBe(0);
  const restored = await fixture(f.b);
  f.b.modelScript = [call("send_to_agent", { thread_id: thread.id, message: "Explain that fact" }, "followup"), textEvents("more details"), textEvents("followup synthesis")];
  await restored.session.receive("second", { content: "Follow up" });
  const childRequest = f.b.requests.at(-2)!;
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
  b.modelScript = [call("send_to_agent", { agent_name: "reviewer", message: "Review the code" }, "delegate"), textEvents("review report"), textEvents("done")];
  await f.session.receive("specialist", { content: "Review" });
  const child = b.requests[1]!;
  expect(child.model).toBe("review-model");
  const created = [...b.publicLog.values()].find((event) => event.type === "session.thread_created");
  expect(created?.agent).toEqual(agentSnapshot);
  expect(f.session.subagents.threads?.[0]?.inline).toBe(false);
  expect(f.session.subagents.threads?.[0]?.agentSnapshot?.id).toBe("agent_reviewer");
  expect(modelSystemText(child)).toContain("Review only");
  expect(child.tools.map((tool: Payload) => tool.name).sort()).toEqual(["Grep", "Read"]);
  await expect(f.session.subagents.send({ name: "inline", system_prompt: "test", message: "test" }, new AbortController().signal)).rejects.toThrow("Inline agents");
});

test("25 retained threads count toward capacity, archival frees capacity and refuses follow-up", async () => {
  const f = await fixture();
  f.session.subagents.threads.push(...Array.from({ length: 25 }, (_, index) => ({
    id: `sthr_test_${index}`, name: "test", systemPrompt: "test", model: "test-model", status: "idle" as const,
  })));
  for (const thread of f.session.subagents.threads) f.b.publicLog.set(thread.id, { type: "session.thread_created", session_thread_id: thread.id });
  await expect(f.session.subagents.send({ name: "new", system_prompt: "test", message: "test" }, new AbortController().signal)).rejects.toThrow("25");
  f.b.modelScript = [call("archive_agent", { thread_id: "sthr_test_0" }, "archive"), textEvents("archived")];
  await f.session.receive("archive", { content: "Archive a thread" });
  expect(f.session.subagents.threads[0]!.status).toBe("terminated");
  await expect(f.session.subagents.send({ thread_id: "sthr_test_0", message: "test" }, new AbortController().signal)).rejects.toThrow("archived");
  f.b.modelScript = [call("send_to_agent", { name: "new", system_prompt: "test", message: "test" }, "new"), textEvents("report"), textEvents("done")];
  await f.session.receive("replacement", { content: "Delegate" });
  expect(f.session.subagents.threads.filter((thread) => thread.status !== "terminated")).toHaveLength(25);
});

test("approval count alone can identify a parent delegation wait before the child exists", async () => {
  const f = await fixture();
  f.b.onApproval = async () => {};
  f.b.modelScript = [call("send_to_agent", { name: "child", system_prompt: "test", message: "run" }, "delegate")];
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
  f.b.modelScript = [call("send_to_agent", { name: "child", system_prompt: "test", message: "run" }, "delegate"), toolEvents("pwd", "child_tool")];
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
  f.b.modelScript = [
    call("send_to_agent", { name: "child", system_prompt: "test", message: "inspect" }, "delegate"),
    call("Read", { file_path: "/tmp/read.txt" }, "child_read"), textEvents("child report"), textEvents("done"),
  ];
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
    expect(nativeOutcomes(f.session, thread.id).child_read?.failed).toBe(false);
    expect(thread.status).toBe("idle");
  } finally { f.session.stop(); await settled; }
});

test("lost child tool outcome blocks parent recovery until new input and never replays the command", async () => {
  const f = await fixture();
  const lostResponse = Promise.withResolvers<Payload>();
  f.b.onTool = () => lostResponse.promise;
  f.b.onCancel = async () => { lostResponse.reject(new Error("Tool response lost after cancellation")); };
  f.b.modelScript = [call("send_to_agent", { name: "child", system_prompt: "test", message: "run" }, "delegate"), toolEvents("side effect", "child_tool")];
  f.b.onApproval = async (payload) => {
    await f.session.control({ type: "control_response", response: {
      subtype: "success", request_id: payload.request_id, response: { behavior: "allow" },
    } });
  };
  const turn = f.session.receive("unknown", { content: "Delegate" }).catch(() => {});
  await eventually(() => f.b.executions.length === 1);
  f.session.stop();
  await turn;
  expect(nativeOutcomes(f.session, f.session.subagents.threads[0]!.id).child_tool?.failed).toBe(true);
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

test.each(["user.interrupt", "control_request"])("%s targeted child interruption leaves parent quiet until user input while primary interruption stops the turn", async (type) => {
  const f = await fixture();
  f.b.modelScript = [call("send_to_agent", { name: "child", system_prompt: "test", message: "run" }, "delegate"), toolEvents("pwd", "child_tool"), textEvents("RESUMED")];
  const turn = f.session.receive("target", { content: "Delegate" });
  await eventually(() => f.session.subagents.threads?.length === 1 && f.session.approvals.size === 1);
  const thread = f.session.subagents.threads![0]!;
  await f.session.control({ type, session_thread_id: thread.id, ...(type === "control_request" ? { request_id: "child", request: { subtype: "interrupt" } } : {}) });
  await turn;
  expect(f.b.executions).toHaveLength(0);
  expect(f.b.requests).toHaveLength(2);
  expect([...f.b.publicLog.values()].some((event) => event.type === "agent.message" && !event.session_thread_id)).toBe(false);
  await f.session.receive("resume-after-child-interrupt", {content:"Reply RESUMED, do not restart cancelled work."});
  expect(f.b.requests).toHaveLength(3);
  expect([...f.b.publicLog.values()].some((event) => event.type === "agent.message" && !event.session_thread_id && event.content[0].text === "RESUMED")).toBe(true);
  f.b.modelScript = [toolEvents("pwd", "primary_tool")];
  const primaryTurn = f.session.receive("primary", { content: "Run" });
  await eventually(() => f.session.approvals.size === 1);
  await f.session.control({ type, session_thread_id: f.session.id, ...(type === "control_request" ? { request_id: "primary", request: { subtype: "interrupt" } } : {}) });
  await primaryTurn;
  expect(f.b.executions).toHaveLength(0);
});

test("backend launch roster is listed and delegated by agent_name without inline fallback", async () => {
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
  b.modelScript = [
    call("list_agents", {}, "roster"),
    call("send_to_agent", { agent_name: "fast_probe", message: "Report your assigned task" }, "delegate"),
    textEvents("fast report"), textEvents("done"),
  ];
  await f.session.receive("configured-roster", { content: "Use the configured fast_probe" });
  const listing = b.requests[1]!.messages.flatMap((message: Payload) => message.content)
    .find((block: Payload) => block.type === "tool_result" && block.tool_use_id === "roster");
  const catalog = JSON.parse(typeof listing.content === "string" ? listing.content : listing.content[0].text);
  expect(catalog.agents).toEqual([
    { name: "slow_probe", description: "Slow probe" },
    { name: "fast_probe", description: "Fast probe" },
  ]);
  expect(catalog.inline_agents).toBe(false);
  const child = b.requests[2]!;
  expect(child.model).toBe("fast-model");
  expect(modelSystemText(child)).toContain("Pinned fast instructions");
  expect(child.tools ?? []).toEqual([]);
  expect(f.session.subagents.threads!.map((thread) => thread.name)).toEqual(["fast_probe"]);
  await expect(f.session.subagents.send({ agent_name: "missing", message: "Run" }, new AbortController().signal))
    .rejects.toThrow("Unknown predefined agent");
});

test("a retained followup checks authoritative archive before creating child work", async () => {
  const f = await fixture();
  f.b.modelScript = [call("send_to_agent", {name:"external_archive",system_prompt:"Reply READY",message:"Reply READY"}, "create_before_archive"),textEvents("READY"),textEvents("done")];
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
  f.b.modelScript = [call("send_to_agent",{thread_id:child.id,message:"Reply READY"},"followup_after_external_archive"),textEvents("rejected")];
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
  b.modelScript = [call("send_to_agent", { name: "inline", system_prompt: "Inspect only", message: "Inspect" }, "delegate"), textEvents("inline report"), textEvents("done")];
  await f.session.receive("restricted", { content: "Inspect" });
  const root = b.requests[0]!;
  expect(root.tools.map((tool: Payload) => tool.name).sort()).toEqual(["Grep", "Read", "archive_agent", "list_agents", "send_to_agent"]);
  expect(b.requests[1]!.tools.map((tool: Payload) => tool.name).sort()).toEqual(["Grep", "Read"]);
});

test("predefined specialist tools are limited to the tools enabled for the parent", async () => {
  const b = new Backend();
  cleanups.push(async () => b.stop());
  b.a.launch.tools = ["Read", "Grep", "Glob"];
  b.a.launch.subagents = { inlineAgents: false, agents: [{
    name: "writer", description: "Writes files", systemPrompt: "Write things", tools: ["Read", "Write", "Edit", "Grep"],
  }] };
  const f = await fixture(b);
  b.modelScript = [call("send_to_agent", { agent_name: "writer", message: "Write" }, "delegate"), textEvents("writer report"), textEvents("done")];
  await f.session.receive("intersect", { content: "Write" });
  expect(b.requests[1]!.tools.map((tool: Payload) => tool.name).sort()).toEqual(["Grep", "Read"]);
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
  const result = JSON.parse(await f.session.subagents.send({ name: "replacement", system_prompt: "test", message: "report" }, new AbortController().signal));
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
  await expect(f.session.subagents.send({ name: "new", system_prompt: "test", message: "test" }, new AbortController().signal)).rejects.toThrow();
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
  const results = await Promise.allSettled(["a", "b"].map((name) => f.session.subagents.send({name, system_prompt:"test", message:"report"}, new AbortController().signal)));
  expect(results.filter((result) => result.status === "fulfilled")).toHaveLength(1);
  expect(results.filter((result) => result.status === "rejected")).toHaveLength(1);
  expect(f.session.subagents.threads).toHaveLength(26);
  expect(f.session.subagents.threads.filter((thread) => thread.status !== "terminated")).toHaveLength(25);
});
