import { expect, test } from "bun:test";
import { SessionId } from "@deepseek-ai/dsh-session";
import { MessageId, ToolCallId, type GenerateOptions, type StreamChunk } from "@deepseek-ai/dsh-llm";
import { OmaApi } from "../src/api.ts";
import { AiSdkAdapter } from "../src/llm-ai-sdk/index.ts";
import { omaModelOptions } from "../src/model.ts";
import { SandboxCalls } from "../src/oma-sandbox/client.ts";
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
  expect((await requests[0]!.json()).max_tokens).toBe(32768);
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
    [Response.json({ error: { message: "concurrency allocated quota exceeded. please try again later." } }, { status: 429 }), "RATE_LIMIT"],
    [Response.json({ error: { message: "insufficient_quota" } }, { status: 429 }), "QUOTA"],
    [Response.json({ error: { message: "balance exhausted" } }, { status: 429 }), "QUOTA"],
    [Response.json({ error: { message: "credits depleted" } }, { status: 429 }), "QUOTA"],
    [Response.json({ error: { message: "Invalid tool schema" } }, { status: 400 }), "HTTP_ERROR"],
    [Response.json({ error: { code: "InvalidParameter", message: "<400> InternalError.Algo.InvalidParameter: Range of input length should be [1, 260096]", param: null, type: null } }, { status: 400 }), "CONTEXT_WINDOW_EXCEEDED"],
    [Response.json({ error: { message: "Range of max_tokens should be [1, 32768]" } }, { status: 400 }), "HTTP_ERROR"],
    [eventsResponse([{ type: "error", error: { type: "invalid_request_error", message: "context window exceeded" } }]), "CONTEXT_WINDOW_EXCEEDED"],
    [eventsResponse(textEvents("empty").filter((event) => !event.type.startsWith("content_block_"))), "EMPTY_RESPONSE"],
  ] as const) {
    const api = new OmaApi("http://model.test", (async (_input, _init) => response) as typeof fetch);
    const a = assignment(); api.assignments.set(a.session_id, a);
    await expect(collect(new AiSdkAdapter(omaModelOptions(api)).stream(options(a.session_id)))).rejects.toMatchObject({ code });
  }
});

test("model context lookup isolates concurrent sessions and child sessions", async () => {
  const api = new OmaApi("http://model.test");
  const first = assignment();
  const second = assignment();
  first.launch.models = {"test-model": { ...first.launch.models!["test-model"]!,  max_output_tokens: 128000, input_modalities: ["text", "image"]  }};
  first.launch.models!["test-model"]!.context_window = 1000000;
  second.launch.models!["test-model"]!.context_window = 256000;
  api.assignments.set(first.session_id, first);
  api.assignments.set(second.session_id, second);
  api.modelSessions.set("child-context", first.session_id);
  const adapterFor = (id: string) => new AiSdkAdapter(omaModelOptions(api, undefined, () => id));
  const values = await Promise.all([
    adapterFor(first.session_id).resolveModel("oma", "test-model"),
    adapterFor(second.session_id).resolveModel("oma", "test-model"),
    adapterFor("child-context").resolveModel("oma", "test-model"),
  ]);
  expect(values.map((value) => value.context?.contextWindow)).toEqual([1000000, 256000, 1000000]);
  expect(values.map((value) => value.defaultMaxTokens)).toEqual([128000, undefined, 128000]);
  expect(values.map((value) => value.inputModalities)).toEqual([["text", "image"], ["text"], ["text", "image"]]);
});

test("configured output tokens reach both model API protocols and refresh between calls", async () => {
  for (const backend of ["messages", "chat_completions"] as const) {
    const requests: Payload[] = [];
    const api = new OmaApi("http://model.test", (async (_input, init) => {
      requests.push(JSON.parse(init!.body as string));
      return backend === "messages"
        ? eventsResponse(textEvents("ok"))
        : new Response(
            `data: ${JSON.stringify({ id: "chat", object: "chat.completion.chunk", created: 1, model: "test-model", choices: [{ index: 0, delta: { role: "assistant", content: "ok" }, finish_reason: null }] })}\n\ndata: ${JSON.stringify({ id: "chat", object: "chat.completion.chunk", created: 1, model: "test-model", choices: [{ index: 0, delta: {}, finish_reason: "stop" }] })}\n\ndata: [DONE]\n\n`,
            { headers: { "Content-Type": "text/event-stream" } },
          );
    }) as typeof fetch);
    const a = assignment();
    a.launch.models = { "test-model": { id: "test-model", api_backend: backend } };
    api.assignments.set(a.session_id, a);
    const adapter = new AiSdkAdapter(omaModelOptions(api));
    const key = backend === "messages" ? "max_tokens" : "max_completion_tokens";
    expect((await collect(adapter.stream(options(a.session_id)))).at(-1)?.type).toBe("finish");
    expect(requests[0][key]).toBe(32768);
    a.launch.models = {"test-model": { ...a.launch.models!["test-model"]!,  max_output_tokens: 128000  }};
    expect((await collect(adapter.stream(options(a.session_id)))).at(-1)?.type).toBe("finish");
    expect(requests[1][key]).toBe(128000);
    const explicit = { ...options(a.session_id), maxTokens: 4096 };
    expect((await collect(adapter.stream(explicit))).at(-1)?.type).toBe("finish");
    expect(requests[2][key]).toBe(4096);
    delete a.launch.models!["test-model"]!.max_output_tokens;
    expect((await collect(adapter.stream(options(a.session_id)))).at(-1)?.type).toBe("finish");
    expect(requests[3][key]).toBe(32768);
    a.launch.models = {"test-model": { ...a.launch.models!["test-model"]!,  max_output_tokens: 8000  }};
    expect((await collect(adapter.stream(options(a.session_id)))).at(-1)?.type).toBe("finish");
    expect(requests[4][key]).toBe(8000);
  }
});

test("unconfigured model capabilities use DeepSeek fallbacks without declaring an explicit request budget", async () => {
  const api = new OmaApi("http://model.test");
  const a = assignment();
  api.assignments.set(a.session_id, a);
  const adapter = new AiSdkAdapter(omaModelOptions(api, undefined, () => a.session_id));
  const info = await adapter.resolveModel("oma", "test-model");
  expect(info.context?.contextWindow).toBe(262144);
  expect(info.inputModalities).toEqual(["text"]);
  expect(info.defaultMaxTokens).toBeUndefined();
  expect(a.launch.models!["test-model"]!.context_window).toBeUndefined();
  expect(a.launch.models!["test-model"]!.max_output_tokens).toBeUndefined();
});
