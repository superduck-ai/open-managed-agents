import { modelSystemText } from "./backend.ts";
import { afterEach, expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { Backend, textEvents, toolEvents, eventually } from "./backend.ts";
import type { Payload } from "../src/json.ts";
import { Subagents } from "../src/runtime/subagents.ts";
import { CcrSubagentBridge } from "../src/ccrv2/subagents.ts";
import type { SubagentEvent, SubagentThread, ToolApproval, ToolExecutionBridge,
  ApprovedToolCall, ToolOutcome } from "../src/runtime/contracts.ts";

const cleanups: (() => Promise<void>)[] = [];
afterEach(async () => { for (const cleanup of cleanups.splice(0).reverse()) await cleanup(); });
async function fixture(b = new Backend()) {
  if (!cleanups.length) cleanups.push(async () => b.stop());
  await b.ready;
  const ctx = new Context();
  const api = new OmaApi(b.url);
  await mountOma(ctx, api);
  const session = createOmaSession(ctx, api, b.a);
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

test("runtime accepts an independent event sink and a class-based execution bridge", async () => {
  const f = await fixture();
  const storage = new CcrSubagentBridge(f.session);
  const threads: SubagentThread[] = [];
  const events: SubagentEvent[] = [];
  const approvals: string[] = [];
  // Prototype methods and receiver state must survive runtime's flush wrapper.
  class Execution implements ToolExecutionBridge {
    constructor(readonly delegate: ToolExecutionBridge) {}
    blocked() { return this.delegate.blocked(); }
    async approve(request: ToolApproval) {
      approvals.push(request.callId);
      return { allowed: true };
    }
    recordApproved(call: ApprovedToolCall) { return this.delegate.recordApproved(call); }
    flushLog() { return this.delegate.flushLog(); }
    recordDispatch(callId: string) { return this.delegate.recordDispatch(callId); }
    recordUnknown(callId: string) { return this.delegate.recordUnknown(callId); }
    recordResult(outcome: ToolOutcome) { return this.delegate.recordResult(outcome); }
    stop() { this.delegate.stop(); }
  }
  const runtime = new Subagents(f.session.runtime, {
    threads: () => threads,
    bindPersistence: (id) => storage.bindPersistence(id),
    emit: async (event) => { events.push(structuredClone(event)); await runtime.flush(); },
    beforeRequest: () => storage.beforeRequest(),
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
    expect(f.session.state.threads ?? []).toHaveLength(0);
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
    expect(payload.session_thread_id).toStartWith("sthr_");
    await f.session.control({ type: "control_response", response: {
      subtype: "success", request_id: payload.request_id, response: { behavior: "allow" },
    } });
  };
  await f.session.receive("first", { content: "Delegate this work" });
  expect(f.b.requests).toHaveLength(4);
  const parentTools = f.b.requests[0]!.tools.map((tool: Payload) => tool.name);
  expect(parentTools).toContain("send_to_agent");
  const childTools = f.b.requests[1]!.tools.map((tool: Payload) => tool.name);
  expect(childTools).toContain("Bash");
  expect(childTools).not.toContain("send_to_agent");
  expect(JSON.stringify(f.b.requests[1]!.messages)).not.toContain("Delegate this work");
  expect(JSON.stringify(f.b.requests[3]!.messages)).toContain("child report");
  const thread = f.session.state.threads![0]!;
  expect(thread.status).toBe("idle");
  expect(f.b.executions).toHaveLength(1);
  expect([...f.b.privateLog.values()].some((payload) => payload.subtype === "dsh_log" && payload.data.header.id === thread.id)).toBe(true);
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
  expect(restored.session.state.threads).toHaveLength(1);
});

test("predefined specialist uses its own prompt, model and restricted tools", async () => {
  const b = new Backend();
  cleanups.push(async () => b.stop());
  b.a.launch.subagents = { inlineAgents: false, agents: [{
    name: "reviewer", description: "Read-only reviewer", systemPrompt: "Review only", model: "review-model", tools: ["Read", "Grep"],
  }] };
  const f = await fixture(b);
  b.modelScript = [call("send_to_agent", { agent_name: "reviewer", message: "Review the code" }, "delegate"), textEvents("review report"), textEvents("done")];
  await f.session.receive("specialist", { content: "Review" });
  const child = b.requests[1]!;
  expect(child.model).toBe("review-model");
  expect(modelSystemText(child)).toContain("Review only");
  expect(child.tools.map((tool: Payload) => tool.name).sort()).toEqual(["Grep", "Read"]);
  await expect(f.session.subagents.send({ name: "inline", system_prompt: "test", message: "test" }, new AbortController().signal)).rejects.toThrow("Inline agents");
});

test("25 retained threads count toward capacity, archival frees capacity and refuses follow-up", async () => {
  const f = await fixture();
  f.session.state.threads = Array.from({ length: 25 }, (_, index) => ({
    id: `sthr_test_${index}`, name: "test", systemPrompt: "test", model: "test-model", status: "idle" as const,
  }));
  await expect(f.session.subagents.send({ name: "new", system_prompt: "test", message: "test" }, new AbortController().signal)).rejects.toThrow("25");
  f.b.modelScript = [call("archive_agent", { thread_id: "sthr_test_0" }, "archive"), textEvents("archived")];
  await f.session.receive("archive", { content: "Archive a thread" });
  expect(f.session.state.threads[0]!.status).toBe("terminated");
  await expect(f.session.subagents.send({ thread_id: "sthr_test_0", message: "test" }, new AbortController().signal)).rejects.toThrow("archived");
  f.b.modelScript = [call("send_to_agent", { name: "new", system_prompt: "test", message: "test" }, "new"), textEvents("report"), textEvents("done")];
  await f.session.receive("replacement", { content: "Delegate" });
  expect(f.session.state.threads.filter((thread) => thread.status !== "terminated")).toHaveLength(25);
});

test("interrupting the whole session cancels child approval before sandbox dispatch", async () => {
  const f = await fixture();
  f.b.modelScript = [call("send_to_agent", { name: "child", system_prompt: "test", message: "run" }, "delegate"), toolEvents("pwd", "child_tool")];
  const turn = f.session.receive("interrupt", { content: "Delegate" }).catch(() => {});
  await eventually(() => f.session.approvals.size === 1);
  await f.session.control({ type: "user.interrupt" });
  await turn;
  expect(f.session.approvals.size).toBe(0);
  expect(f.b.executions).toHaveLength(0);
  expect(f.session.state.threads![0]!.status).toBe("idle");
});

test("lost child tool outcome blocks parent recovery and never replays the command", async () => {
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
  expect(f.session.state.phase).toBe("blocked");
  await f.close();
  cleanups.pop();
  f.b.onTool = undefined;
  f.b.onCancel = undefined;
  const restored = await fixture(f.b);
  expect(restored.session.blocked()).toBe(true);
  await expect(restored.session.receive("retry", { content: "try again" })).rejects.toThrow("reconciliation");
  expect(f.b.executions).toHaveLength(1);
});

test("targeted child interruption lets the parent recover while primary interruption stops the turn", async () => {
  const f = await fixture();
  f.b.modelScript = [call("send_to_agent", { name: "child", system_prompt: "test", message: "run" }, "delegate"), toolEvents("pwd", "child_tool"), textEvents("Child was interrupted")];
  const turn = f.session.receive("target", { content: "Delegate" });
  await eventually(() => f.session.approvals.size === 1);
  const thread = f.session.state.threads![0]!;
  await f.session.control({ type: "user.interrupt", session_thread_id: thread.id });
  await turn;
  expect(f.b.executions).toHaveLength(0);
  expect(f.b.requests).toHaveLength(3);
  expect([...f.b.publicLog.values()].some((event) => event.type === "agent.message" && !event.session_thread_id && event.content[0].text === "Child was interrupted")).toBe(true);
  f.b.modelScript = [toolEvents("pwd", "primary_tool")];
  const primaryTurn = f.session.receive("primary", { content: "Run" });
  await eventually(() => f.session.approvals.size === 1);
  await f.session.control({ type: "user.interrupt", session_thread_id: f.session.id });
  await primaryTurn;
  expect(f.b.executions).toHaveLength(0);
});

test("backend launch roster is listed and delegated by agent_name without inline fallback", async () => {
  const b = new Backend();
  cleanups.push(async () => b.stop());
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
  expect(f.session.state.threads!.map((thread) => thread.name)).toEqual(["fast_probe"]);
  await expect(f.session.subagents.send({ agent_name: "missing", message: "Run" }, new AbortController().signal))
    .rejects.toThrow("Unknown predefined agent");
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
