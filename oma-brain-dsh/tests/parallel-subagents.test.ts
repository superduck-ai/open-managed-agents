import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { SessionId } from "@deepseek-ai/dsh-session";
import { modelSystemText } from "./backend.ts";
import { afterEach, expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { Backend, textEvents, toolEvents, eventually } from "./backend.ts";
import type { Payload } from "../src/json.ts";

const cleanups: (() => Promise<void>)[] = [];
afterEach(async () => { for (const cleanup of cleanups.splice(0).reverse()) await cleanup(); });
async function fixture(b = new Backend()) {
  await b.ready;
  const ctx = new Context();
  const api = new OmaApi(b.url);
  await mountOma(ctx, api);
  const session = createOmaSession(ctx, api, b.a);
  b.onDelegationApproval = async (payload) => {
    await session.control({ type: "control_response", response: {
      subtype: "success", request_id: payload.request_id, response: { behavior: "allow" },
    } });
  };
  let closed = false;
  const close = async () => { if (closed) return; closed = true; await session.close(); await ctx.fiber.dispose(); };
  cleanups.push(async () => { await close(); b.stop(); });
  await session.open();
  return { b, ctx, api, session, close };
}
export function parallelDelegations(names = ["alpha", "beta"]): Payload[] {
  const calls = names.flatMap((name, index) => toolEvents("", `delegate_${name}`).slice(1, 4).map((event) => ({
    ...event, index,
    ...(event.type === "content_block_start" ? { content_block: { ...event.content_block, name: "send_to_agent" } } : {}),
    ...(event.type === "content_block_delta" ? { delta: { type: "input_json_delta", partial_json: JSON.stringify({ name, system_prompt: `${name} specialist`, message: `Task for ${name}` }) } } : {}),
  })));
  return [toolEvents()[0]!, ...calls, ...toolEvents().slice(4)];
}
async function decide(session: Awaited<ReturnType<typeof fixture>>["session"], request: Payload, allow: boolean, command?: string) {
  await session.control({ type: "control_response", response: {
    subtype: "success", request_id: request.request_id,
    response: { behavior: allow ? "allow" : "deny", ...(command ? { updatedInput: { command } } : {}) },
  } });
}
function scriptedChildren(b: Backend, alpha?: Promise<void>, beta?: Promise<void>) {
  const counts = new Map<string, number>();
  b.onModel = async (body) => {
    const name = modelSystemText(body).includes("You are subagent alpha.") ? "alpha" : modelSystemText(body).includes("You are subagent beta.") ? "beta" : "primary";
    const count = counts.get(name) ?? 0; counts.set(name, count + 1);
    if (name === "primary") return count === 0 ? parallelDelegations() : textEvents("parent synthesis");
    if (count === 0) return toolEvents(name, "same_call_id");
    await (name === "alpha" ? alpha : beta);
    return textEvents(`${name} report`);
  };
}

for (const policy of ["ask", "deny"] as const) {
  test(`a retained child applies an allow to ${policy} update on its next turn`, async () => {
    const backend = new Backend();
    backend.a.launch.tool_permissions = { bash: "allow" };
    const f = await fixture(backend);
    f.b.modelScript = [parallelDelegations(["alpha"]), toolEvents("echo first"), textEvents("child done"), textEvents("parent done")];
    await f.session.receive("first-turn", { content: "Delegate first" });
    expect(f.b.executions).toHaveLength(1);
    const threadId = f.session.state.threads![0]!.id;
    f.b.a.launch.tool_permissions = { bash: policy };
    const followup = parallelDelegations(["alpha"]);
    const delta = followup.find((event) => event.type === "content_block_delta")!;
    delta.delta.partial_json = JSON.stringify({ thread_id: threadId, message: "Run second" });
    f.b.modelScript = [followup, toolEvents("echo second"), textEvents("child done"), textEvents("parent done")];
    const approvals: Payload[] = [];
    f.b.onApproval = async (request) => { approvals.push(request); };
    const turn = f.session.receive("second-turn", { content: "Follow up" });
    const settled = turn.catch(() => {});
    try {
      await eventually(() => approvals.length === 1 || f.b.executions.length > 1);
      expect(approvals).toHaveLength(1);
      expect(approvals[0]!.session_thread_id).toBe(threadId);
      expect(f.b.executions).toHaveLength(1);
      await decide(f.session, approvals[0]!, policy === "ask");
      await turn;
      expect(f.b.executions).toHaveLength(policy === "ask" ? 2 : 1);
      expect(f.session.blocked()).toBe(false);
    } finally { f.session.stop(); await settled; }
  });
}

test("native tool batch starts two children concurrently, isolates identical call IDs, and aggregates approvals", async () => {
  const f = await fixture();
  const betaResult = Promise.withResolvers<Payload>();
  const alphaResult = Promise.withResolvers<Payload>();
  scriptedChildren(f.b);
  f.b.onTool = (params) => params.arguments.command === "approved-alpha" ? alphaResult.promise : betaResult.promise;
  const requests: Payload[] = [];
  f.b.onApproval = async (request) => { requests.push(request); };
  const turn = f.session.receive("parallel", { content: "Delegate independent tasks" });
  const settled = turn.catch(() => {});
  try {
    await eventually(() => requests.length === 2
      && f.b.timeline.filter(event => event.type === "worker.status").at(-1)?.external_metadata?.pending_action?.pending_actions?.length === 2);
    expect(f.session.approvals.size).toBe(2);
    const alpha = f.session.state.threads!.find((thread) => thread.name === "alpha")!;
    const beta = f.session.state.threads!.find((thread) => thread.name === "beta")!;
    expect(alpha.status).toBe("running"); expect(beta.status).toBe("running");
    expect(f.b.statuses.at(-1)).toBe("requires_action");
    const status = f.b.timeline.filter((event) => event.type === "worker.status").at(-1)!;
    expect(status.external_metadata.pending_action.pending_actions).toHaveLength(2);
    const aRequest = requests.find((request) => request.session_thread_id === alpha.id)!;
    const bRequest = requests.find((request) => request.session_thread_id === beta.id)!;
    expect(aRequest.request.tool_use_id).toBe(bRequest.request.tool_use_id);
    expect(aRequest.request_id).not.toBe(bRequest.request_id);
    await decide(f.session, bRequest, true, "approved-beta");
    await eventually(() => f.b.executions.length === 1);
    expect(f.b.statuses.at(-1)).toBe("running");
    expect(f.session.approvals.size).toBe(1);
    expect(f.session.state.executions![beta.id]!.phase).toBe("dispatch");
    await decide(f.session, aRequest, true, "approved-alpha");
    await eventually(() => f.b.executions.length === 2);
    expect(f.session.state.executions![alpha.id]!.phase).toBe("dispatch");
    betaResult.resolve({ content: [{ type: "text", text: "beta output" }] });
    await eventually(() => beta.status === "idle");
    expect(f.session.state.phase).toBe("dispatch");
    expect(f.b.statuses.at(-1)).toBe("running");
    expect(f.session.state.executions![beta.id]!.toolOutcomes.same_call_id!.text).toBe("beta output");
    alphaResult.resolve({ content: [{ type: "text", text: "alpha output" }] });
    await turn;
    expect(f.session.state.phase).toBe("completed");
    expect(f.b.statuses.at(-1)).toBe("idle");
    expect(f.session.state.executions![alpha.id]!.toolOutcomes.same_call_id!.text).toBe("alpha output");
    expect(f.session.state.executions![beta.id]!.approvedCalls.same_call_id!.effective).toEqual({ command: "approved-beta" });
    const synthesis = f.b.requests.at(-1)!;
    expect(JSON.stringify(synthesis.messages)).toContain("alpha report");
    expect(JSON.stringify(synthesis.messages)).toContain("beta report");
    const published = [...f.b.publicLog.values()].filter((event) => event.type === "agent.thread_message_received");
    expect(published).toHaveLength(2);
  } finally {
    alphaResult.resolve({ content: [{ type: "text", text: "alpha output" }] });
    betaResult.resolve({ content: [{ type: "text", text: "beta output" }] });
    f.session.stop(); await settled;
  }
});

test.each(["user.interrupt", "control_request"])("%s targeted interruption cancels only one concurrent approval and denial does not stop a sibling", async (type) => {
  const f = await fixture();
  scriptedChildren(f.b);
  const requests: Payload[] = [];
  f.b.onApproval = async (request) => { requests.push(request); };
  const turn = f.session.receive("interrupt-one", { content: "Delegate" });
  const settled = turn.catch(() => {});
  try {
    await eventually(() => requests.length === 2);
    const alpha = f.session.state.threads!.find((thread) => thread.name === "alpha")!;
    await f.session.control({ type, session_thread_id: alpha.id, ...(type === "control_request" ? { request_id: "targeted", request: { subtype: "interrupt" } } : {}) });
    await eventually(() => f.session.approvals.size === 1 && alpha.status === "idle"
      && f.b.statuses.at(-1) === "requires_action");
    expect(f.b.statuses.at(-1)).toBe("requires_action");
    const alphaIdle = [...f.b.publicLog.values()].filter((event) => event.type === "session.thread_status_idle" && event.session_thread_id === alpha.id).at(-1);
    expect(alphaIdle?.stop_reason?.type).toBe("end_turn");
    const alphaError = [...f.b.publicLog.values()].find((event) => event.type === "user" && event.session_thread_id === alpha.id && event.message?.content?.some((block: Payload) => block.type === "tool_result" && block.is_error));
    expect(JSON.stringify(alphaError?.message?.content)).toContain("Tool execution was interrupted before completion. Please retry.");
    const betaRequest = requests.find((request) => request.session_thread_id !== alpha.id)!;
    await decide(f.session, betaRequest, false);
    await turn;
    expect(f.b.executions).toHaveLength(0);
    expect(f.session.state.executions![alpha.id]!.phase).toBe("interrupted");
    expect(f.b.statuses.at(-1)).toBe("idle");
    expect(JSON.stringify(f.b.requests.at(-1)!.messages)).toContain("beta report");
  } finally { f.session.stop(); await settled; }
});

test("a retained thread rejects simultaneous follow-ups before creating a second turn", async () => {
  const f = await fixture();
  const response = Promise.withResolvers<void>();
  f.b.onModel = async () => { await response.promise; return textEvents("report"); };
  const first = f.session.subagents.send({ name: "alpha", system_prompt: "test", message: "first" }, new AbortController().signal);
  const settled = first.catch(() => {});
  try {
    await eventually(() => f.b.requests.length === 1);
    const id = f.session.state.threads![0]!.id;
    await expect(f.session.subagents.send({ thread_id: id, message: "second" }, new AbortController().signal)).rejects.toThrow("running");
    expect(f.b.requests).toHaveLength(1);
    response.resolve(); await first;
  } finally { response.resolve(); f.session.stop(); await settled; }
});

test("an unknown child tool outcome blocks the session and cancels sibling approvals", async () => {
  const f = await fixture();
  scriptedChildren(f.b);
  const requests: Payload[] = [];
  f.b.onApproval = async (request) => { requests.push(request); };
  const baseRoute = f.b.route.bind(f.b);
  f.b.route = async (request) => {
    if (request.method === "POST" && new URL(request.url).pathname.endsWith("/mcp")) {
      const body = await request.clone().json();
      if (body.method === "tools/call") return new Response(null, { status: 500 });
    }
    return baseRoute(request);
  };
  const turn = f.session.receive("unknown-parallel", { content: "Delegate" });
  const settled = turn.catch(() => {});
  try {
    await eventually(() => requests.length === 2);
    const alpha = f.session.state.threads!.find((thread) => thread.name === "alpha")!;
    await decide(f.session, requests.find((request) => request.session_thread_id === alpha.id)!, true);
    await settled;
    expect(f.session.blocked()).toBe(true);
    expect(f.session.state.executions![alpha.id]!.phase).toBe("blocked");
    expect(f.session.approvals.size).toBe(0);
    expect(f.session.abort.signal.aborted).toBe(true);
    expect(f.b.requests).toHaveLength(3);
  } finally { f.session.stop(); await settled; }
});

test("legacy remote state requires explicit migration before missing-local resume", async () => {
  const b = new Backend();
  await b.ready;
  const localRoot = await mkdtemp(join(tmpdir(), "oma-legacy-transcript-"));
  const retainedPath = join(localRoot, "retained-legacy.jsonl");
  const retainedContent = '{"legacy":"preserve this transcript"}\n';
  await writeFile(retainedPath, retainedContent);
  const legacy = {
    uuid: "legacy-state", subtype: "dsh_brain_state", schema_version: 1,
    data: { version: 1, revision: 1, inputId: "legacy", phase: "dispatch", completed: [], outbox: [],
      approvedCalls: { legacy_call: { name: "Bash", original: { command: "write" }, effective: { command: "write" } } },
      toolOutcomes: {} },
  };
  b.privateLog.set(legacy.uuid, structuredClone(legacy));
  const api = new OmaApi(b.url);
  const fresh = new Context();
  let resumed: ReturnType<typeof createOmaSession> | undefined;
  try {
    await mountOma(fresh, api, {}, localRoot);
    expect(await fresh.sessionPersistence.stat(SessionId(b.a.session_id))).toBeUndefined();
    resumed = createOmaSession(fresh, api, b.a);
    await expect(resumed.open()).rejects.toThrow("requires migration");
    expect(b.requests).toHaveLength(0);
    expect(b.executions).toHaveLength(0);
    expect(b.processed).toHaveLength(0);
    expect(await fresh.sessionPersistence.stat(SessionId(b.a.session_id))).toBeUndefined();
    expect(b.privateLog.get(legacy.uuid)).toEqual(legacy);
    expect(await readFile(retainedPath, "utf8")).toBe(retainedContent);
  } finally {
    try { await resumed?.close(); }
    finally {
      await fresh.fiber.dispose();
      b.stop();
      await rm(localRoot, { recursive: true, force: true });
    }
  }
});

for (const mode of ["unique", "ambiguous", "scoped"] as const) {
  test(`operator reconciliation isolates blocked children in ${mode} mode`, async () => {
    const sameCallId = mode !== "unique";
    const f = await fixture();
    const modelCalls = new Map<string, number>();
    f.b.onModel = async body => {
      const system = modelSystemText(body);
      const name = system.includes("You are subagent alpha.") ? "alpha"
        : system.includes("You are subagent beta.") ? "beta" : "primary";
      const count = modelCalls.get(name) ?? 0;
      modelCalls.set(name, count + 1);
      if (name === "primary") return count === 0 ? parallelDelegations() : textEvents("parent report");
      return toolEvents(name, sameCallId ? "shared_unknown" : `${name}_unknown`);
    };
    const dispatched = new Set<string>();
    const bothDispatched = Promise.withResolvers<void>();
    const route = f.b.route.bind(f.b);
    f.b.route = async request => {
      if (request.method === "POST" && new URL(request.url).pathname.endsWith("/mcp")) {
        const body = await request.clone().json();
        if (body.method === "tools/call") {
          dispatched.add(body.params.arguments.command);
          if (dispatched.size === 2) bothDispatched.resolve();
          await bothDispatched.promise;
          return new Response(null, { status: 503 });
        }
      }
      return route(request);
    };
    f.b.onApproval = async request => { await decide(f.session, request, true); };
    await f.session.receive("two_unknown_children", { content: "Delegate independent tasks" }).catch(() => {});
    expect([...dispatched].sort()).toEqual(["alpha", "beta"]);
    expect(f.session.unknownOutcomes()).toHaveLength(2);
    const alpha = f.session.state.threads!.find(thread => thread.name === "alpha")!;
    await f.close();
    f.b.workerMetadata.tool_outcome_reconciliation = {
      tool_use_ids: [sameCallId ? "shared_unknown" : "alpha_unknown"],
      ...(mode === "scoped" ? { session_thread_id: alpha.id } : {}),
    };
    const resumed = await fixture(f.b);
    expect(resumed.session.blocked()).toBe(true);
    const remaining = resumed.session.unknownOutcomes()!;
    expect(remaining.map(call => call.tool_use_id)).toEqual(mode === "ambiguous"
      ? ["shared_unknown", "shared_unknown"] : [sameCallId ? "shared_unknown" : "beta_unknown"]);
    const notices = () => [...f.b.publicLog.values()].filter(event => event.type === "system.message"
      && JSON.stringify(event).includes("acknowledged by an operator"));
    if (mode === "ambiguous") {
      expect(notices()).toEqual([]);
      return;
    }
    expect(notices()).toHaveLength(1);
    expect(JSON.stringify(notices()[0])).toContain("session remains blocked");
    expect(JSON.stringify(notices()[0])).not.toContain("session accepts new input");
    if (mode === "scoped") {
      const beta = resumed.session.state.threads!.find(thread => thread.name === "beta")!;
      await resumed.close();
      f.b.workerMetadata.tool_outcome_reconciliation = { tool_use_ids: ["shared_unknown"], session_thread_id: beta.id };
      const completed = await fixture(f.b);
      expect(completed.session.blocked()).toBe(false);
      expect(notices()).toHaveLength(2);
      expect(JSON.stringify(notices().at(-1))).toContain("session accepts new input");
      expect([...dispatched].sort()).toEqual(["alpha", "beta"]);
    }
  });
}
