import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { TraceProducer, type TraceOwner } from "../src/observability/trace-producer.ts";
import { otlpSpan, SpanExporter, type SpanRecord } from "../src/observability/span-exporter.ts";
import { currentToolTrace, parseTraceParent, traceHooks } from "../src/observability/trace-context.ts";

const PARENT_TRACE = "4bf92f3577b34da6a3ce929d0e0e4736";
const PARENT_SPAN = "00f067aa0ba902b7";

type OtlpSpan = { traceId: string; spanId: string; parentSpanId?: string; name: string; attributes: { key: string; value: Record<string, unknown> }[]; status?: { code: number } };

const hex = (value: string) => Buffer.from(value, "base64").toString("hex");
const attribute = (span: OtlpSpan, key: string) => {
  const value = span.attributes.find((item) => item.key === key)?.value;
  return value?.stringValue ?? value?.boolValue ?? (value?.intValue !== undefined ? Number(value.intValue) : undefined);
};

function producer(content = true): { traces: TraceProducer; spans: SpanRecord[] } {
  const spans: SpanRecord[] = [];
  const traces = new TraceProducer();
  const owner: TraceOwner = {
    exporter: { add: (span: SpanRecord) => spans.push(span), flush: async () => {}, close: async () => {} } as unknown as TraceOwner["exporter"],
    content, model: "provider/model", systemPrompt: "agent system", liveSince: 1_000,
  };
  traces.attach("cse_root", owner);
  return { traces, spans };
}

test("trace context parsing ignores invalid and unsampled headers safely", () => {
  expect(parseTraceParent({ traceparent: `00-${PARENT_TRACE}-${PARENT_SPAN}-01`, tracestate: "a=1" })).toEqual({ traceId: PARENT_TRACE, spanId: PARENT_SPAN, sampled: true, traceState: "a=1" });
  expect(parseTraceParent({ traceparent: `00-${PARENT_TRACE}-${PARENT_SPAN}-00` })?.sampled).toBe(false);
  for (const traceparent of ["", `ff-${PARENT_TRACE}-${PARENT_SPAN}-01`, `00-${"0".repeat(32)}-${PARENT_SPAN}-01`, `00-${PARENT_TRACE}-${PARENT_SPAN}-01-extra`, 7])
    expect(parseTraceParent({ traceparent })).toBeUndefined();
  expect(otlpSpan({ traceId: PARENT_TRACE, spanId: PARENT_SPAN, name: "x", startMs: 2, endMs: 1, attributes: { a: 1.5, b: "", c: undefined } }))
    .toMatchObject({ startTimeUnixNano: "2000000", endTimeUnixNano: "2000000", attributes: [{ key: "a", value: { doubleValue: 1.5 } }] });
});

test("span batches stay below the ingress body limit", async () => {
  const bodies: string[] = [];
  const fetcher = (async (_url: string, init: RequestInit) => {
    bodies.push(String(init.body));
    return new Response(null, { status: 200 });
  }) as unknown as typeof fetch;
  const exporter = new SpanExporter("http://oma/traces", () => "token", fetcher, "cse_root");
  const content = "内".repeat(1_000_000);
  for (let index = 0; index < 4; index++)
    exporter.add({ traceId: PARENT_TRACE, spanId: PARENT_SPAN, name: "oma.model_request", startMs: 1, endMs: 2, attributes: { "gen_ai.output.messages": content } });
  await exporter.close();
  expect(bodies.length).toBeGreaterThan(1);
  for (const body of bodies) expect(Buffer.byteLength(body)).toBeLessThan(9 << 20);
  expect(bodies.flatMap((body) => JSON.parse(body).resourceSpans[0].scopeSpans[0].spans)).toHaveLength(4);
});

test("history replay, unsampled turns and skipped tools produce no spans", () => {
  const { traces, spans } = producer();
  traces.observe("cse_root", "cse_root", undefined, "turn/start", 500, { turn: 1 }, {});
  traces.observe("cse_root", "cse_root", undefined, "turn/end", 600, { turn: 1, reason: { kind: "completed" } }, {});
  expect(spans).toEqual([]);
  traces.inputReceived("cse_root", { traceparent: `00-${PARENT_TRACE}-${PARENT_SPAN}-00` });
  traces.observe("cse_root", "cse_root", undefined, "turn/start", 2_000, { turn: 2 }, {});
  traces.observe("cse_root", "cse_root", undefined, "assistant/message", 2_100, { step: 1, message: { content: [] } }, {});
  traces.observe("cse_root", "cse_root", undefined, "turn/end", 2_200, { turn: 2, reason: { kind: "completed" } }, {});
  expect(spans).toEqual([]);
  traces.observe("cse_root", "cse_root", undefined, "turn/start", 3_000, { turn: 3 }, {});
  for (const [callId, name] of [["c1", "send_to_agent"], ["c2", "Write"], ["c3", "Custom"]] as const)
    traces.observe("cse_root", "cse_root", undefined, "tool/call", 3_010, { callId, name, step: 1, arguments: "{}" }, {});
  traces.approvalResolved("cse_root", "c2", "denied");
  traces.approvalResolved("cse_root", "c3", "external");
  expect(traces.toolDispatched("cse_root", "c2")).toBeUndefined();
  for (const callId of ["c1", "c2", "c3"])
    traces.observe("cse_root", "cse_root", undefined, "tool/result", 3_020, { message: { toolCallId: callId, isError: false, content: [] } }, {});
  traces.observe("cse_root", "cse_root", undefined, "turn/end", 3_030, { turn: 3, reason: { kind: "completed" } }, {});
  expect(spans.map((span) => span.name)).toEqual(["oma.session.turn"]);
  expect(spans[0]!.parentSpanId).toBeUndefined();
});

test("approval waits split turns and subagent turns nest under the parent turn", () => {
  const { traces, spans } = producer(false);
  traces.inputReceived("cse_root", { traceparent: `00-${PARENT_TRACE}-${PARENT_SPAN}-01` });
  traces.observe("cse_root", "cse_root", undefined, "turn/start", 2_000, { turn: 1 }, {});
  traces.subagent("sthr_child", { name: "model-named", inline: true });
  traces.observe("sthr_child", "cse_root", "cse_root", "turn/start", 2_100, { turn: 1 }, {});
  traces.observe("sthr_child", "cse_root", "cse_root", "tool/call", 2_150, { callId: "mcp1", name: "mcp__docs__search", step: 1, arguments: "{\"q\":1}" }, {});
  traceHooks.registerMcpServer("docs", "Docs Server");
  traces.approvalPending("sthr_child", "mcp1");
  traces.approvalResolved("sthr_child", "mcp1", "allowed");
  const scope = traces.toolDispatched("sthr_child", "mcp1")!;
  traces.observe("sthr_child", "cse_root", "cse_root", "tool/result", 2_300, { message: { toolCallId: "mcp1", isError: true, content: [{ type: "text", text: "secret" }] }, error: { code: "MCP_AUTH" } }, {});
  traces.observe("sthr_child", "cse_root", "cse_root", "turn/end", 2_400, { turn: 1, reason: { kind: "completed" } }, {});
  traces.observe("cse_root", "cse_root", undefined, "turn/end", 2_500, { turn: 1, reason: { kind: "error", error: { code: "RATE_LIMIT" } } }, {});
  const turns = spans.filter((span) => span.name === "oma.session.turn");
  const [root] = turns.filter((span) => span.attributes["gen_ai.conversation.id"] === "cse_root");
  const child = turns.filter((span) => span.attributes["gen_ai.conversation.id"] === "sthr_child");
  expect(root!.traceId).toBe(PARENT_TRACE);
  expect(root!.parentSpanId).toBe(PARENT_SPAN);
  expect(root!.status).toBe("error");
  expect(root!.attributes["error.type"]).toBe("rate_limit_error");
  expect(child.map((span) => span.attributes["oma.session.turn.stop_reason"])).toEqual(["requires_action", "end_turn"]);
  expect(child.every((span) => span.parentSpanId === root!.spanId && span.traceId === PARENT_TRACE)).toBe(true);
  expect(child[0]!.status).toBeUndefined();
  expect(child[0]!.attributes["gen_ai.agent.name"]).toBe("model-named");
  expect(child[0]!.attributes["oma.agent.inline"]).toBe(true);
  const tool = spans.find((span) => span.name === "oma.mcp_tool_use search")!;
  expect(tool.parentSpanId).toBe(child[1]!.spanId);
  expect(scope.traceparent).toBe(`00-${PARENT_TRACE}-${tool.spanId}-01`);
  expect(tool.attributes).toMatchObject({ "oma.mcp.server.name": "Docs Server", "oma.tool.evaluated_permission": "ask", "error.type": "mcp_authentication_failed_error", "gen_ai.tool.call.id": "mcp1" });
  expect(tool.attributes["gen_ai.tool.call.arguments"]).toBeUndefined();
  expect(tool.attributes["oma.tool_result.content"]).toBeUndefined();
  expect(root!.attributes["gen_ai.input.messages"]).toBeUndefined();
});

test("the real loop exports turn, model and tool spans under the caller's trace", async () => {
  const { Backend, eventually, toolEvents, textEvents } = await import("./backend.ts");
  const { mountOma } = await import("../src/plugin.ts");
  const { createOmaSession } = await import("../src/oma-session.ts");
  const backend = new Backend();
  await backend.ready;
  backend.a.trace_export = { content: true };
  backend.a.registered_at = Date.now();
  backend.a.launch.tool_permissions = { bash: "allow" };
  backend.a.launch.system = "Configured agent instructions";
  backend.modelScript = [toolEvents("echo traced"), textEvents("traced reply")];
  const ctx = new Context();
  const api = new OmaApi(backend.url);
  let session: ReturnType<typeof createOmaSession> | undefined;
  try {
    await mountOma(ctx, api);
    await ctx.plugin({
      inject: ["ccrv2"],
      apply: async (scope: Context) => {
        session = createOmaSession(scope, api, backend.a);
        await session.open();
      },
    });
    await session!.receive("traced-input", { content: "run the traced tool", trace_context: { traceparent: `00-${PARENT_TRACE}-${PARENT_SPAN}-01` } });
    await eventually(() => (backend.traceSpans as OtlpSpan[]).some((span) => span.name === "oma.session.turn"), 10_000);
    const spans = backend.traceSpans as OtlpSpan[];
    const turn = spans.find((span) => span.name === "oma.session.turn")!;
    const requests = spans.filter((span) => span.name === "oma.model_request");
    const tool = spans.find((span) => span.name === "oma.tool_use Bash")!;
    expect(hex(turn.traceId)).toBe(PARENT_TRACE);
    expect(hex(turn.parentSpanId!)).toBe(PARENT_SPAN);
    expect(turn.status?.code).toBe(1);
    expect(attribute(turn, "gen_ai.operation.name")).toBe("invoke_agent");
    expect(attribute(turn, "oma.session.turn.stop_reason")).toBe("end_turn");
    expect(JSON.parse(String(attribute(turn, "gen_ai.input.messages")))[0].parts[0].content).toContain("run the traced tool");
    expect(String(attribute(turn, "gen_ai.output.messages"))).toContain("traced reply");
    expect(String(attribute(turn, "gen_ai.system_instructions"))).toContain("Configured agent instructions");
    expect(requests.length).toBe(2);
    for (const request of [...requests, tool]) {
      expect(hex(request.traceId)).toBe(PARENT_TRACE);
      expect(request.parentSpanId).toBe(turn.spanId);
    }
    expect(attribute(requests[0]!, "gen_ai.operation.name")).toBe("chat");
    expect(Number(attribute(requests[0]!, "oma.model_request.ttft_ms"))).toBeGreaterThanOrEqual(0);
    expect(attribute(tool, "gen_ai.tool.call.id")).toBe("call_1");
    expect(attribute(tool, "oma.tool.evaluated_permission")).toBe("allow");
    expect(String(attribute(tool, "gen_ai.tool.call.arguments"))).toContain("echo traced");
    expect(currentToolTrace()).toBeUndefined();
  } finally {
    await session?.close();
    await api.telemetry?.close(backend.a.session_id);
    await ctx.fiber.dispose();
    backend.stop();
  }
});

test("a turn lost with its worker is closed by the next worker as the parent of its exported children", () => {
  const lost = producer();
  lost.traces.inputReceived("cse_root", { traceparent: `00-${PARENT_TRACE}-${PARENT_SPAN}-01`, tracestate: "a=1" });
  const pending = lost.traces.openTurns("cse_root");
  expect(pending).toHaveLength(1);
  lost.traces.observe("cse_root", "cse_root", undefined, "turn/start", 2_000, { turn: 1 }, {});
  lost.traces.observe("cse_root", "cse_root", undefined, "assistant/message", 2_100, { step: 1, message: { content: [] } }, {});
  lost.traces.observe("cse_root", "cse_root", undefined, "tool/call", 2_110, { callId: "c1", name: "Write", step: 1, arguments: "{}" }, {});
  const open = lost.traces.openTurns("cse_root");
  expect(open).toEqual([{ sessionId: "cse_root", traceId: PARENT_TRACE, spanId: pending[0]!.spanId, parentSpanId: PARENT_SPAN, traceState: "a=1", startMs: 2_000 }]);
  expect(lost.spans.map((span) => span.parentSpanId)).toEqual([open[0]!.spanId]);
  expect(producer().traces.openTurns("cse_root")).toEqual([]);

  const next = producer();
  next.traces.turnsInterrupted("cse_root", open);
  expect(next.spans).toHaveLength(1);
  expect(next.spans[0]).toMatchObject({
    name: "oma.session.turn", traceId: PARENT_TRACE, spanId: open[0]!.spanId, parentSpanId: PARENT_SPAN, traceState: "a=1", startMs: 2_000, status: "error",
  });
  expect(next.spans[0]!.attributes).toMatchObject({ "oma.session.turn.stop_reason": "interrupted", "error.type": "worker_lost", "gen_ai.conversation.id": "cse_root" });
});

test("a turn that fails after model retries is exported as an error parenting every attempt", async () => {
  const { Backend, eventually } = await import("./backend.ts");
  const { mountOma } = await import("../src/plugin.ts");
  const { createOmaSession } = await import("../src/oma-session.ts");
  const backend = new Backend();
  await backend.ready;
  backend.a.trace_export = { content: false };
  backend.a.registered_at = Date.now();
  const responses = [
    () => Response.json({ type: "error", error: { type: "rate_limit_error", message: "Rate limited" } }, { status: 429, headers: { "retry-after": "0.01" } }),
    () => Response.json({ type: "error", error: { type: "invalid_request_error", message: "Model request is invalid" } }, { status: 400 }),
  ];
  backend.onModel = async () => responses.shift()!();
  const ctx = new Context();
  const api = new OmaApi(backend.url);
  let session: ReturnType<typeof createOmaSession> | undefined;
  try {
    await mountOma(ctx, api);
    session = createOmaSession(ctx, api, backend.a);
    await session.open();
    await session.receive("failing-input", { content: "fail", trace_context: { traceparent: `00-${PARENT_TRACE}-${PARENT_SPAN}-01` } }).catch(() => {});
    await eventually(() => (backend.traceSpans as OtlpSpan[]).some((span) => span.name === "oma.session.turn"), 10_000);
    const spans = backend.traceSpans as OtlpSpan[];
    const turn = spans.find((span) => span.name === "oma.session.turn")!;
    const attempts = spans.filter((span) => span.name === "oma.model_request");
    expect(hex(turn.parentSpanId!)).toBe(PARENT_SPAN);
    expect(turn.status?.code).toBe(2);
    expect(attribute(turn, "oma.session.turn.stop_reason")).toBe("error");
    expect(attempts).toHaveLength(2);
    for (const attempt of attempts) {
      expect(attempt.parentSpanId).toBe(turn.spanId);
      expect(attempt.status?.code).toBe(2);
    }
    expect(attempts.map((attempt) => attribute(attempt, "error.type"))).toEqual(["rate_limit_error", "api_error"]);
  } finally {
    await session?.close();
    await api.telemetry?.close(backend.a.session_id);
    await ctx.fiber.dispose();
    backend.stop();
  }
});

test("a worker killed mid-turn leaves its turn to the next worker, which exports it as the children's parent", async () => {
  const { spawn } = await import("node:child_process");
  const { once } = await import("node:events");
  const { Backend, eventually, toolEvents } = await import("./backend.ts");
  const { mountOma } = await import("../src/plugin.ts");
  const { createOmaSession } = await import("../src/oma-session.ts");
  const backend = new Backend();
  await backend.ready;
  backend.modelScript = [toolEvents()];
  backend.hangTool = true;
  backend.a.trace_export = { content: false };
  backend.a.registered_at = Date.now();
  backend.a.launch.tool_permissions = { bash: "allow" };
  const child = spawn(process.execPath, ["tests/crash-worker.ts"], {
    env: { ...process.env, TEST_API_URL: backend.url, TEST_ASSIGNMENT: JSON.stringify(backend.a), TEST_TRACEPARENT: `00-${PARENT_TRACE}-${PARENT_SPAN}-01` },
    stdio: ["pipe", "pipe", "pipe"],
  });
  const exited = once(child, "exit");
  const ctx = new Context();
  const api = new OmaApi(backend.url);
  let resumed: ReturnType<typeof createOmaSession> | undefined;
  try {
    await eventually(() => backend.executions.length === 1 && (backend.traceSpans as OtlpSpan[]).some((span) => span.name === "oma.model_request"), 10_000);
    child.kill("SIGKILL");
    await exited;
    const request = (backend.traceSpans as OtlpSpan[]).find((span) => span.name === "oma.model_request")!;
    expect((backend.traceSpans as OtlpSpan[]).some((span) => span.name === "oma.session.turn")).toBe(false);
    backend.a.worker_epoch++;
    backend.a.registered_at = Date.now();
    await mountOma(ctx, api);
    resumed = createOmaSession(ctx, api, backend.a);
    await resumed.open();
    await eventually(() => (backend.traceSpans as OtlpSpan[]).some((span) => span.name === "oma.session.turn"), 10_000);
    const turns = (backend.traceSpans as OtlpSpan[]).filter((span) => span.name === "oma.session.turn");
    expect(turns).toHaveLength(1);
    expect(turns[0]!.spanId).toBe(request.parentSpanId!);
    expect(hex(turns[0]!.traceId)).toBe(PARENT_TRACE);
    expect(hex(turns[0]!.parentSpanId!)).toBe(PARENT_SPAN);
    expect(turns[0]!.status?.code).toBe(2);
    expect(attribute(turns[0]!, "error.type")).toBe("worker_lost");
    await eventually(() => backend.workerMetadata.oma_trace_turns === undefined, 10_000);
  } finally {
    child.kill("SIGKILL");
    await resumed?.close();
    await api.telemetry?.close(backend.a.session_id);
    await ctx.fiber.dispose();
    backend.stop();
  }
});

test("cancelled model attempts are exported without a status or error type", () => {
  const { traces, spans } = producer(false);
  traces.observe("cse_root", "cse_root", undefined, "turn/start", 2_000, { turn: 1 }, {});
  const aborted = { type: "chunk", chunk: { type: "finish", reason: { kind: "aborted", failure: { code: "ABORTED", message: "cancelled" } } } };
  traces.observe("cse_root", "cse_root", undefined, "assistant/attempt", 2_100, { step: 1, stream: [aborted] }, {});
  traces.observe("cse_root", "cse_root", undefined, "assistant/message", 2_200, { step: 2, interrupted: true, message: { content: [] } }, {});
  const requests = spans.filter((span) => span.name === "oma.model_request");
  expect(requests).toHaveLength(2);
  for (const request of requests) {
    expect(request.status).toBeUndefined();
    expect(request.attributes["error.type"]).toBeUndefined();
  }
});
