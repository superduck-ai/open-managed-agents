import { expect, test } from "bun:test";
import { SessionId } from "@deepseek-ai/dsh-session";
import { MessageId, ToolCallId, type GenerateOptions, type StreamChunk } from "@deepseek-ai/dsh-llm";
import { OmaApi } from "../src/api.ts";
import { AiSdkAdapter } from "../src/llm-ai-sdk/index.ts";
import { omaModelOptions } from "../src/model.ts";
import { SandboxMcp } from "../src/oma-sandbox/client.ts";
import { assignment, textEvents, toolEvents } from "./backend.ts";
import type { Payload } from "../src/json.ts";

function eventsResponse(events: Payload[]) {
  return new Response(events.map((event) => `event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`).join(""),
    { headers: { "Content-Type": "text/event-stream" } });
}
const options = (id: string): GenerateOptions => ({
  provider: "oma", model: "test-model", sessionId: SessionId(id),
  messages: [{ role: "user", content: [{ type: "text", text: "Hello" }] }],
});
async function collect(stream: AsyncIterable<StreamChunk>) {
  const chunks: StreamChunk[] = [];
  for await (const chunk of stream) chunks.push(chunk);
  return chunks;
}

test("OMA Messages adapter binds concurrent root/child requests to their OMA tokens and re-reads rotated credentials", async () => {
  const requests: Request[] = [];
  const api = new OmaApi("http://model.test", (async (input, init) => {
    const request = new Request(input, init);
    requests.push(request);
    expect(new URL(request.url).pathname).toBe("/v1/messages");
    expect(request.headers.has("x-api-key")).toBe(false);
    return eventsResponse(textEvents(request.headers.get("Authorization")!));
  }) as typeof fetch);
  const a = assignment(); const b = assignment();
  a.model_token = "root-a"; b.model_token = "root-b";
  a.trace_parent = "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01";
  api.assignments.set(a.session_id, a); api.assignments.set(b.session_id, b);
  api.modelSessions.set("child-a", a.session_id);
  const adapter = new AiSdkAdapter(omaModelOptions(api));
  const streams = await Promise.all([collect(adapter.stream(options(a.session_id))), collect(adapter.stream(options(b.session_id))),
    collect(adapter.stream(options("child-a")))]);
  expect(requests.map((request) => request.headers.get("Authorization")).sort()).toEqual(["Bearer root-a", "Bearer root-a", "Bearer root-b"]);
  expect(requests.map((request) => request.headers.get("X-Claude-Code-Agent-Id"))).toEqual([null, null, "child-a"]);
  expect(requests.map((request) => [request.headers.get("Authorization"), request.headers.get("traceparent")]).sort()).toEqual([
    ["Bearer root-a", a.trace_parent], ["Bearer root-a", a.trace_parent], ["Bearer root-b", null],
  ]);
  expect(requests.every((request) => request.headers.get("user-agent")?.startsWith("deepseek-harness/"))).toBe(true);
  for (const [index, chunks] of streams.entries()) {
    expect(chunks).toContainEqual({ type: "text-delta", index: 0, text: index === 1 ? "Bearer root-b" : "Bearer root-a" });
    expect(chunks.at(-1)?.type).toBe("finish");
  }
  a.model_token = "rotated";
  await collect(adapter.stream(options(a.session_id)));
  expect(requests.at(-1)!.headers.get("Authorization")).toBe("Bearer rotated");
  expect((await requests[0]!.json()).max_tokens).toBe(8192);
});

test("OMA Messages adapter streams tools and preserves signed history", async () => {
  let body: Payload = {};
  const api = new OmaApi("http://model.test", (async (_input, init) => {
    body = JSON.parse(init!.body as string);
    return eventsResponse(toolEvents("pwd", "tool_1"));
  }) as typeof fetch);
  const a = assignment(); api.assignments.set(a.session_id, a);
  const request = options(a.session_id);
  request.messages = [request.messages[0]!, {
    id: MessageId("legacy-assistant"), role: "assistant", content: [{ type: "reasoning", text: "saved thinking" }, { type: "text", text: "saved answer" }],
    source: { kind: "model", provider: "oma", model: "test-model", replayState: {
      response: { format: "oma-messages-v1" }, blocks: [{ signature: "opaque-signature" }, { signature: "" }],
    } },
  }, { role: "user", content: [{ type: "text", text: "continue" }] }];
  const chunks = await collect(new AiSdkAdapter(omaModelOptions(api)).stream(request));
  expect(body.messages.flatMap((message: Payload) => message.content)).toContainEqual({ type: "thinking", thinking: "saved thinking", signature: "opaque-signature" });
  expect(chunks.some((chunk) => chunk.type === "block-end" && chunk.block.type === "tool-call" && chunk.block.arguments === '{"command":"pwd"}')).toBe(true);
  const finish = chunks.find((chunk) => chunk.type === "finish");
  expect(finish).toMatchObject({
    type: "finish", reason: { kind: "tool-calls" },
    replayState: {
      response: { format: "oma-messages-v1", stopReason: "tool_use" },
      blocks: [{ index: 0 }],
    },
  });
  const saved = request.messages[1]!;
  if (saved.role !== "assistant" || saved.source.kind !== "model") throw new Error("Missing saved assistant");
  expect((saved.source.replayState as Payload).response.format).toBe("oma-messages-v1");
  saved.source.replayState = {
    response: { kind: "pi-ai", version: 2, api: "anthropic-messages", provider: "oma", model: "test-model", stopReason: "stop" },
    blocks: [{ type: "reasoning", thinkingSignature: "pi-signature" }, { type: "text" }],
  };
  await collect(new AiSdkAdapter(omaModelOptions(api)).stream(request));
  expect(body.messages.flatMap((message: Payload) => message.content)).toContainEqual({ type: "thinking", thinking: "saved thinking", signature: "pi-signature" });
});

test("OMA Messages adapter rejects incomplete streams and honors cancellation", async () => {
  let requests = 0;
  const api = new OmaApi("http://model.test", (async (_input, init) => {
    requests++;
    init!.signal?.throwIfAborted();
    return eventsResponse(textEvents("partial").slice(0, -1));
  }) as typeof fetch);
  const a = assignment(); api.assignments.set(a.session_id, a);
  const adapter = new AiSdkAdapter(omaModelOptions(api));
  await expect(collect(adapter.stream(options(a.session_id)))).rejects.toMatchObject({ code: "TRANSPORT" });
  expect(requests).toBe(1);
  await expect(collect(adapter.stream({ ...options(a.session_id), signal: AbortSignal.abort() }))).rejects.toThrow();
  expect(requests).toBe(1);
});

test("OMA Messages adapter maps Anthropic controls, tools, results and cache usage directly", async () => {
  let body: Payload = {};
  const events = textEvents("done");
  events[0]!.message.usage = { input_tokens: 10, cache_read_input_tokens: 12, cache_creation_input_tokens: 3 };
  const api = new OmaApi("http://model.test", (async (_input, init) => {
    expect(new Headers(init!.headers).get("anthropic-version")).toBe("2023-06-01");
    body = JSON.parse(init!.body as string);
    return eventsResponse(events);
  }) as typeof fetch);
  const a = assignment(); api.assignments.set(a.session_id, a);
  const request = options(a.session_id);
  request.system = "instructions";
  request.maxTokens = 1234; request.temperature = 0.2;
  request.tools = [{ name: "Bash", description: "Run a command", parameters: { type: "object" } }];
  request.messages.push({
    id: MessageId("tool-call"), role: "assistant", source: { kind: "model", provider: "oma", model: "test-model" },
    content: [{ type: "tool-call", id: ToolCallId("call"), name: "Bash", arguments: '{"command":"pwd"}' }],
  }, {
    id: MessageId("tool-result"), role: "tool", source: { kind: "tool", callId: ToolCallId("call") },
    toolCallId: ToolCallId("call"),
    content: [{ type: "text", text: "permission denied" }], isError: true,
  });
  const chunks = await collect(new AiSdkAdapter(omaModelOptions(api)).stream(request));
  expect(body).toMatchObject({ model: "test-model", stream: true, system: [{ type: "text", text: "instructions" }], max_tokens: 1234,
    temperature: 0.2, tools: [{ name: "Bash", input_schema: { type: "object" } }] });
  expect(body).not.toHaveProperty("thinking");
  expect(body.tools[0]).not.toHaveProperty("eager_input_streaming");
  expect(body.messages[1].content).toEqual([{ type: "tool_use", id: "call", name: "Bash", input: { command: "pwd" } }]);
  expect(body.messages[2].content).toEqual([{ type: "tool_result", tool_use_id: "call", is_error: true,
    content: "permission denied" }]);
  expect(chunks).toContainEqual({ type: "usage", usage: {
    inputTokens: 10, outputTokens: 5, cacheReadTokens: 12, cacheWriteTokens: 3,
  } });
  expect(chunks.at(-1)).toMatchObject({ type: "finish", reason: { kind: "stop" } });
});

test("OMA Messages adapter rejects HTTP, in-stream and empty response errors", async () => {
  for (const [response, code] of [
    [Response.json({ error: { message: "Service unavailable" } }, { status: 503 }), "SERVER"],
    [Response.json({ error: { message: "Rate limited" } }, { status: 429 }), "RATE_LIMIT"],
    [eventsResponse([{ type: "error", error: { type: "invalid_request_error", message: "context window exceeded" } }]), "CONTEXT_WINDOW_EXCEEDED"],
    [eventsResponse(textEvents("empty").filter((event) => !event.type.startsWith("content_block_"))), "EMPTY_RESPONSE"],
  ] as const) {
    const api = new OmaApi("http://model.test", (async (_input, _init) => response) as typeof fetch);
    const a = assignment(); api.assignments.set(a.session_id, a);
    await expect(collect(new AiSdkAdapter(omaModelOptions(api)).stream(options(a.session_id)))).rejects.toMatchObject({ code });
  }
});

test("MCP SDK correlates SSE responses, negotiates sessions,", async () => {
  const calls: Payload[] = [];
  let dispatched = 0;
  const api = new OmaApi("http://sandbox.test", (async (input, init) => {
    const request = new Request(input, init);
    if (request.method === "GET") return new Response(null, { status: 405 });
    expect(request.headers.get("Authorization")).toBe("Bearer test-mcp");
    expect(request.headers.has("X-Brain-Epoch")).toBe(false);
    const body = await request.json() as Payload; calls.push(body);
    if (body.method === "server/discover") return Response.json({ jsonrpc: "2.0", id: body.id, error: { code: -32601, message: "Method not found" } }, { status: 404 });
    if (body.method.startsWith("notifications/")) return new Response(null, { status: 202 });
    if (body.method === "initialize") return Response.json({ jsonrpc: "2.0", id: body.id, result: {
      protocolVersion: "2024-11-05", capabilities: { tools: {} }, serverInfo: { name: "sandbox", version: "1" },
    } }, { headers: { "Mcp-Session-Id": "session-1" } });
    dispatched++;
    expect(request.headers.get("Mcp-Session-Id")).toBe("session-1");
    if (body.params.arguments.command === "hang") return new Promise<Response>((_resolve, reject) => {
      init!.signal!.addEventListener("abort", () => reject(init!.signal!.reason), { once: true });
    });
    return new Response(`data: ${JSON.stringify({ jsonrpc: "2.0", id: body.id, result: { content: [{ type: "text", text: "stdout" }] } })}\n\n`,
      { headers: { "Content-Type": "text/event-stream" } });
  }) as typeof fetch);
  const a = assignment(); api.assignments.set(a.session_id, a);
  const executor = new SandboxMcp(api, a.session_id);
  const tool = { name: "Bash", remoteName: "Bash", description: "", inputSchema: {} };
  try {
    expect(calls).toHaveLength(0);
    await executor.provision(new AbortController().signal);
    expect(await executor.call(tool, { command: "pwd" }, new AbortController().signal)).toEqual({ content: [{ type: "text", text: "stdout" }] });
    expect(dispatched).toBe(1);
    expect(calls.filter((call) => call.method === "initialize")).toHaveLength(1);
  } finally { await executor.close(); }
});

test("tool interruption retains the correlated remote cancellation result", async () => {
  const started = Promise.withResolvers<void>();
  const result = Promise.withResolvers<Response>();
  let requestId: string | number;
  let cancellations = 0;
  const api = new OmaApi("http://sandbox.test", (async (input, init) => {
    const request = new Request(input, init);
    if (request.method === "GET") return new Response(null, { status: 405 });
    const body = await request.json() as Payload;
    if (body.method === "server/discover") return Response.json({ jsonrpc: "2.0", id: body.id, error: { code: -32601, message: "Method not found" } }, { status: 404 });
    if (body.method === "initialize") return Response.json({ jsonrpc: "2.0", id: body.id, result: {
      protocolVersion: "2025-03-26", capabilities: { tools: {} }, serverInfo: { name: "sandbox", version: "1" },
    } });
    if (body.method === "notifications/cancelled") {
      expect(body.params.requestId).toBe(requestId);
      cancellations++;
      result.resolve(Response.json({ jsonrpc: "2.0", id: requestId, result: {
        isError: true, content: [{ type: "text", text: "cancelled" }], structuredContent: { status: "cancelled", cancelled: true },
      } }));
      return new Response(null, { status: 202 });
    }
    if (body.method !== "tools/call") return new Response(null, { status: 202 });
    requestId = body.id;
    started.resolve();
    return result.promise;
  }) as typeof fetch);
  const a = assignment(); api.assignments.set(a.session_id, a);
  const executor = new SandboxMcp(api, a.session_id);
  const abort = new AbortController();
  const pending = executor.call({ name: "Bash", remoteName: "bash", description: "", inputSchema: {} }, { command: "sleep 120" }, abort.signal);
  const outcome = pending.then((value) => value, (error) => error);
  try {
    await started.promise;
    abort.abort(new Error("user interrupt"));
    expect(await outcome).toEqual({ isError: true, content: [{ type: "text", text: "cancelled" }], structuredContent: { status: "cancelled", cancelled: true } });
    expect(cancellations).toBe(1);
  } finally { result.resolve(new Response(null, { status: 500 })); await executor.close(); }
});

test("modern MCP tool interruption closes the original request", async () => {
  const started = Promise.withResolvers<void>();
  const cancelled = Promise.withResolvers<void>();
  let cancellationNotifications = 0;
  let toolCalls = 0;
  const api = new OmaApi("http://sandbox.test", (async (input, init) => {
    const request = new Request(input, init);
    const body = await request.json() as Payload;
    if (body.method === "server/discover") return Response.json({ jsonrpc: "2.0", id: body.id, result: {
      resultType: "complete", ttlMs: 0, cacheScope: "private", supportedVersions: ["2026-07-28"],
      capabilities: { tools: {} }, _meta: { "io.modelcontextprotocol/serverInfo": { name: "sandbox", version: "1" } },
    } });
    if (body.method === "notifications/cancelled") {
      cancellationNotifications++;
      return new Response(null, { status: 202 });
    }
    if (body.method === "tools/list") return Response.json({ jsonrpc: "2.0", id: body.id, result: {
      resultType: "complete", ttlMs: 0, cacheScope: "private", tools: [{ name: "bash", inputSchema: { type: "object" } }],
    } });
    if (body.method === "tools/call") {
      expect(request.headers.get("Mcp-Method")).toBe("tools/call");
      expect(request.headers.get("Mcp-Name")).toBe("bash");
      if (++toolCalls === 1) return Response.json({ jsonrpc: "2.0", id: body.id, result: {
        resultType: "complete", content: [{ type: "text", text: "stdout" }],
      } });
      started.resolve();
      return new Promise<Response>((_resolve, reject) => {
        request.signal.addEventListener("abort", () => {
          cancelled.resolve();
          reject(request.signal.reason);
        }, { once: true });
      });
    }
    throw new Error(`Unexpected MCP method ${body.method}`);
  }) as typeof fetch);
  const a = assignment(); api.assignments.set(a.session_id, a);
  const executor = new SandboxMcp(api, a.session_id);
  const tool = { name: "Bash", remoteName: "bash", description: "", inputSchema: {} };
  const abort = new AbortController();
  try {
    expect(await executor.call(tool, { command: "pwd" }, new AbortController().signal)).toEqual({ content: [{ type: "text", text: "stdout" }] });
    const pending = executor.call(tool, { command: "sleep 120" }, abort.signal);
    const outcome = pending.then((value) => value, (error) => error);
    await Promise.race([started.promise, outcome.then((value) => { throw new Error(`MCP call ended before dispatch: ${value}`); })]);
    abort.abort(new Error("user interrupt"));
    await Promise.race([cancelled.promise, Bun.sleep(1000).then(() => { throw new Error("Original MCP request remained open"); })]);
    expect(await outcome).toBeInstanceOf(Error);
    expect(cancellationNotifications).toBe(0);
  } finally { await executor.close(); }
});
