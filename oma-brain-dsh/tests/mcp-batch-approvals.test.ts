import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { Backend, eventually, textEvents, toolEvents } from "./backend.ts";
import type { Payload } from "../src/json.ts";
import type { ToolExecutionBridge } from "../src/runtime/contracts.ts";

function pair(): Payload[] {
  const blocks = ["first", "second"].flatMap((name, index) => toolEvents("", `batch_${index}`).slice(1, 4).map(event => ({
    ...event, index,
    ...(event.type === "content_block_start" ? { content_block: { ...event.content_block, name: `mcp__peer__${name}` } } : {}),
    ...(event.type === "content_block_delta" ? { delta: { type: "input_json_delta", partial_json: "{}" } } : {}),
  })));
  return [toolEvents()[0]!, ...blocks, ...toolEvents().slice(4)];
}

async function fixture() {
  const calls: string[] = [];
  const firstRelease = Promise.withResolvers<void>();
  let holdFirst = false;
  const peer = Bun.serve({ port: 0, async fetch(req) {
    if (req.method === "DELETE") return new Response(null, { status: 204 });
    if (req.method !== "POST") return new Response(null, { status: 405 });
    const body = await req.json() as Payload;
    if (body.method.startsWith("notifications/")) return new Response(null, { status: 202 });
    const reply = (result: Payload) => Response.json({ jsonrpc: "2.0", id: body.id, result });
    if (body.method === "initialize") return reply({ protocolVersion: "2024-11-05", capabilities: { tools: {} }, serverInfo: { name: "peer", version: "1" } });
    if (body.method === "tools/list") return reply({ tools: ["first", "second"].map(name => ({ name, description: name, inputSchema: { type: "object", properties: {} } })) });
    if (body.method === "tools/call") {
      calls.push(body.params.name);
      if (holdFirst && body.params.name === "first") await firstRelease.promise;
      return reply({ content: [{ type: "text", text: body.params.name }] });
    }
    return new Response(null, { status: 404 });
  } });
  const b = new Backend(); await b.ready;
  const config = { mcpServers: { peer: { type: "http", url: peer.url.href } } };
  b.a.external_mcp_config = config;
  const route = b.route.bind(b);
  b.route = request => new URL(request.url).pathname === `/v2/sessions/${b.a.session_id}`
    ? Promise.resolve(Response.json({ session_context: { cwd: "/workspace", model: "test-model", runtime_tools: [], mcp_config: config } })) : route(request);
  const api = new OmaApi(b.url, (async (input, init) => {
    const request = new Request(input, init); const url = new URL(request.url);
    return url.pathname.endsWith("/mcp") ? fetch(new Request(url.searchParams.get("mcp_url")!, request)) : fetch(request);
  }) as typeof fetch);
  const ctx = new Context(); await mountOma(ctx, api); const session = createOmaSession(ctx, api, b.a);
  const approvals: Payload[] = []; b.onApproval = async request => { approvals.push(request); };
  await session.open(); b.modelScript = [pair(), textEvents("done")];
  const decide = async (index: number, behavior: string) => { const request = approvals[index]!; await session.control({ type: "control_response", response: { subtype: "success", request_id: request.request_id, response: { behavior, message: "denied" } } }); };
  return { b, session, approvals, calls, decide, hold: () => { holdFirst = true; }, release: firstRelease.resolve,
    async close() { firstRelease.resolve(); await session.close(); await ctx.fiber.dispose(); b.stop(); peer.stop(true); } };
}

test("all MCP asks from one model response are visible before any confirmation", async () => {
  const f = await fixture();
  try {
    const turn = f.session.receive("batch", { content: "Two independent tools" });
    turn.catch(() => {});
    await eventually(() => f.approvals.length === 2);
    expect(f.calls).toEqual([]);
    await f.decide(0, "allow"); await eventually(() => f.calls.length === 1);
    expect(f.calls).toEqual(["first"]);
    expect(f.session.approvals.size).toBe(1);
    await f.decide(1, "deny"); await turn;
    expect(f.calls).toEqual(["first"]);
    expect(f.approvals).toHaveLength(2);
    expect(f.session.approvals.size).toBe(0);
  } finally { await f.close(); }
});

test("batch approval never makes external MCP side effects execute concurrently", async () => {
  const f = await fixture(); f.hold();
  try {
    const turn = f.session.receive("batch", { content: "Two independent tools" });
    turn.catch(() => {});
    await eventually(() => f.approvals.length === 2);
    await f.decide(1, "allow"); expect(f.calls).toEqual([]);
    await f.decide(0, "allow"); await eventually(() => f.calls.length === 1);
    await Bun.sleep(20); expect(f.calls).toEqual(["first"]);
    f.release(); await turn; expect(f.calls).toEqual(["first", "second"]);
  } finally { await f.close(); }
});

test("interrupt aborts all prefetched MCP asks without remote execution", async () => {
  const f = await fixture();
  try {
    const turn = f.session.receive("batch", { content: "Two independent tools" });
    turn.catch(() => {});
    await eventually(() => f.approvals.length === 2);
    await f.session.control({ type: "user.interrupt" }); await turn;
    expect(f.calls).toEqual([]); expect(f.session.approvals.size).toBe(0);
  } finally { await f.close(); }
});

test("MCP approval prefetch stops at unregistered, builtin and malformed call barriers", async () => {
  const { prefetchMcpApprovals } = await import("../src/runtime/tool-approvals.ts");
  const { ToolCallId } = await import("@deepseek-ai/dsh-llm");
  for (const barrier of [
    { name: "Bash", arguments: "{}" },
    { name: "mcp__missing", arguments: "{}" },
    { name: "mcp__peer__second", arguments: "not JSON" },
  ]) {
    const requests: string[] = [];
    const bridge: ToolExecutionBridge = {
      approve: async request => { requests.push(request.callId); return { allowed: true }; },
      runTool: (_id, execute) => execute(), approvalChanged() {},
    };
    const calls = [
      { type: "tool-call" as const, id: ToolCallId("first"), name: "mcp__peer__first", arguments: "{}" },
      { type: "tool-call" as const, id: ToolCallId("barrier"), ...barrier },
      { type: "tool-call" as const, id: ToolCallId("last"), name: "mcp__peer__second", arguments: "{}" },
    ];
    const registered = (name: string) => ["mcp__peer__first", "mcp__peer__second"].includes(name);
    prefetchMcpApprovals(bridge, calls, "first", registered, new AbortController().signal);
    prefetchMcpApprovals(bridge, calls, "first", registered, new AbortController().signal);
    expect(requests).toEqual(["first"]);
  }
});
