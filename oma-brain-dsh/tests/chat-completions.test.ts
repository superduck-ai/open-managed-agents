import { expect, test } from "bun:test";
import { BlockAssembler, MessageId, ToolCallId, type GenerateOptions, type StreamChunk } from "@deepseek-ai/dsh-llm";
import { SessionId } from "@deepseek-ai/dsh-session";
import { OmaApi } from "../src/api.ts";
import { AiSdkAdapter } from "../src/llm-ai-sdk/index.ts";
import { omaModelOptions } from "../src/model.ts";
import { assistantMessages } from "../src/ccrv2/assistant.ts";
import { assignment, textEvents } from "./backend.ts";
import type { Payload } from "../src/json.ts";

function response(chunks: (Payload | string)[]) {
  return new Response(chunks.map((chunk) => `data: ${typeof chunk === "string" ? chunk : JSON.stringify(chunk)}\r\n\r\n`).join(""),
    { headers: { "Content-Type": "text/event-stream", "Request-Id": "span_chat" } });
}
function options(id: string, model = "chat-model"): GenerateOptions {
  return { provider: "oma", model, sessionId: SessionId(id), messages: [{ role: "user", content: [{ type: "text", text: "Hello" }] }] };
}
async function collect(stream: AsyncIterable<StreamChunk>) {
  const chunks: StreamChunk[] = [];
  for await (const chunk of stream) chunks.push(chunk);
  return chunks;
}
function setup(chunks: (Payload | string)[]) {
  const requests: Request[] = [];
  const api = new OmaApi("http://model.test", (async (input, init) => {
    requests.push(new Request(input, init));
    return response(chunks);
  }) as typeof fetch);
  const a = assignment(); a.launch.model_backends = { "chat-model": "chat_completions" };
  api.assignments.set(a.session_id, a);
  return { api, a, requests, adapter: new AiSdkAdapter(omaModelOptions(api)) };
}
function textChunk(text = "answer", finish: string | null = "stop"): Payload {
  return { id: "chat_1", choices: [{ index: 0, delta: { content: text }, finish_reason: finish }] };
}

test("Chat adapter reports HTTP errors without retrying the OMA model request", async () => {
  for (const [status, code] of [[401, "AUTH"], [403, "AUTH"], [429, "RATE_LIMIT"], [503, "SERVER"]] as const) {
    let requests = 0;
    const api = new OmaApi("http://model.test", (async (_input, _init) => { requests++; return Response.json({ error: { message: "denied" } }, { status }); }) as typeof fetch);
    const a = assignment(); a.launch.model_backends = { "chat-model": "chat_completions" }; api.assignments.set(a.session_id, a);
    await expect(collect(new AiSdkAdapter(omaModelOptions(api)).stream(options(a.session_id)))).rejects.toMatchObject({ code, message: "denied" });
    expect(requests).toBe(1);
  }
});

test("Chat adapter rejects incomplete streams and honors stop sequences", async () => {
  for (const chunks of [[{ id: "chat_1", choices: [{ index: 0, delta: { content: "partial" } }] }], ["{broken"],
    [{ id: "chat_1", choices: [{ index: 0, delta: {}, finish_reason: "stop" }] }, "[DONE]"]]) {
    const { a, adapter } = setup(chunks);
    await expect(collect(adapter.stream(options(a.session_id)))).rejects.toMatchObject({ code: expect.stringMatching(/^(TRANSPORT|EMPTY_RESPONSE)$/) });
  }
  const { a, adapter, requests } = setup([textChunk(), "[DONE]"]);
  await collect(adapter.stream({ ...options(a.session_id), stop: ["END"] }));
  expect((await requests[0]!.json()).stop).toEqual(["END"]);
  await expect(collect(adapter.stream({ ...options(a.session_id), signal: AbortSignal.abort() }))).rejects.toThrow();
  expect(requests).toHaveLength(1);
});

test("Chat adapter cancellation cancels the OMA HTTP stream", async () => {
  let cancelled = false;
  const api = new OmaApi("http://model.test", (async (_input, _init) => new Response(new ReadableStream<Uint8Array>({
    start(controller) { controller.enqueue(new TextEncoder().encode(`data: ${JSON.stringify({ id: "chat_cancel", choices: [{ index: 0, delta: { content: "partial" } }] })}\n\n`)); },
    cancel() { cancelled = true; },
  }))) as typeof fetch);
  const a = assignment(); a.launch.model_backends = { "chat-model": "chat_completions" }; api.assignments.set(a.session_id, a);
  const controller = new AbortController();
  const iterator = new AiSdkAdapter(omaModelOptions(api)).stream({ ...options(a.session_id), signal: controller.signal })[Symbol.asyncIterator]();
  expect((await iterator.next()).value).toMatchObject({ type: "block-start" });
  expect((await iterator.next()).value).toMatchObject({ type: "text-delta", text: "partial" });
  controller.abort();
  await expect(iterator.next()).rejects.toThrow();
  expect(cancelled).toBe(true);
});

test("Chat adapter parses large streamed tool arguments in linear time", async () => {
  const json = JSON.stringify({ content: "x".repeat(400_000) });
  const deltas = Array.from({ length: Math.ceil(json.length / 100) }, (_, index) => json.slice(index * 100, (index + 1) * 100));
  const { a, adapter } = setup([
    { id: "chat_1", choices: [{ index: 0, delta: { tool_calls: [{ index: 0, id: "call_1", type: "function", function: { name: "Write", arguments: "" } }] } }] },
    ...deltas.map((delta) => ({ id: "chat_1", choices: [{ index: 0, delta: { tool_calls: [{ index: 0, function: { arguments: delta } }] } }] })),
    { id: "chat_1", choices: [{ index: 0, delta: {}, finish_reason: "tool_calls" }] }, "[DONE]",
  ]);
  const started = performance.now();
  const chunks = await collect(adapter.stream(options(a.session_id)));
  expect(performance.now() - started).toBeLessThan(2000);
  expect(chunks).toContainEqual(expect.objectContaining({ type: "block-end", block: expect.objectContaining({ arguments: json }) }));
});

test("Chat adapter assembles reasoning, fragmented parallel tools and usage", async () => {
  const { a, api, adapter, requests } = setup([
    { id: "chat_1", choices: [{ index: 0, delta: { reasoning_content: "thinking" } }] },
    { id: "chat_1", choices: [{ index: 0, delta: { content: "running", tool_calls: [
      { index: 1, id: "call_b", type: "function", function: { name: "Read", arguments: "{" } },
      { index: 0, id: "call_a", type: "function", function: { name: "Bash", arguments: '{"command":' } },
    ] } }] },
    { id: "chat_1", choices: [{ index: 0, delta: { tool_calls: [
      { index: 0, function: { arguments: '"pwd"}' } }, { index: 1, function: { arguments: "}" } },
    ] }, finish_reason: "tool_calls" }] },
    { id: "chat_1", choices: [], usage: { prompt_tokens: 20, completion_tokens: 5, total_tokens: 25, prompt_tokens_details: { cached_tokens: 12 } } }, "[DONE]",
  ]);
  const result = await collect(adapter.stream(options(a.session_id)));
  const assembler = new BlockAssembler(); for (const chunk of result) assembler.push(chunk);
  expect(assembler.message({ provider: "oma", model: "chat-model" }).content).toEqual([
    { type: "reasoning", text: "thinking" }, { type: "text", text: "running" },
    { type: "tool-call", id: ToolCallId("call_b"), name: "Read", arguments: "{}" },
    { type: "tool-call", id: ToolCallId("call_a"), name: "Bash", arguments: '{"command":"pwd"}' },
  ]);
  expect(result).toContainEqual({ type: "usage", usage: { inputTokens: 8, outputTokens: 5, cacheReadTokens: 12 } });
  expect(result.at(-1)).toMatchObject({ type: "finish", reason: { kind: "tool-calls" }, replayState: {
    response: { format: "oma-chat-completions-v1", messageID: "chat_1", requestID: "span_chat", stopReason: "tool_calls" },
    blocks: [{ index: 0 }, { index: 1 }, { index: 2 }, { index: 3 }],
  } });
  expect(api.modelResponses.get(a.session_id)).toBe("chat_1");
  expect(requests[0]!.url).toBe("http://model.test/v1/chat/completions");
  expect(requests[0]!.headers.get("authorization")).toBe(`Bearer ${a.model_token}`);
  expect(requests[0]!.headers.has("x-api-key")).toBe(false);
  expect(requests[0]!.headers.has("anthropic-version")).toBe(false);
});

test("Chat adapter sends tool history and retains OMA event identity after replay", async () => {
  const { a, adapter, requests } = setup([
    textChunk("done", null),
    { id: "chat_1", choices: [{ index: 0, delta: { tool_calls: [{ index: 0, id: "call_1", type: "function", function: { name: "Bash", arguments: "{}" } }] }, finish_reason: "tool_calls" }] },
    "[DONE]",
  ]);
  const request = options(a.session_id); request.maxTokens = 100; request.temperature = 0.5;
  request.tools = [{ name: "Bash", description: "Run", parameters: { type: "object" } }];
  request.messages.push({ id: MessageId("old"), role: "assistant", content: [
    { type: "text", text: "running" }, { type: "tool-call", id: ToolCallId("old_call"), name: "Bash", arguments: '{"command":"pwd"}' },
  ], source: { kind: "model", provider: "oma", model: "chat-model", replayState: { response: { format: "oma-chat-completions-v1" } } } },
  { id: MessageId("tool"), role: "tool", content: [{ type: "text", text: "stdout" }], source: { kind: "tool", callId: ToolCallId("old_call") }, toolCallId: ToolCallId("old_call") });
  const assembler = new BlockAssembler(); for (const chunk of await collect(adapter.stream(request))) assembler.push(chunk);
  const saved = assembler.message({ provider: "oma", model: "chat-model", replayState: assembler.replayState });
  expect(saved.source.replayState).toMatchObject({ blocks: [{ index: 0 }, { index: 1 }] });
  const first = assistantMessages(a.session_id, "fallback", saved)[0]!;
  expect(first.model_request_start_id).toBe("span_chat");
  expect(assistantMessages(a.session_id, "other", saved)[0]!.id).toBe(first.id);
  const body = await requests[0]!.json();
  expect(body).toMatchObject({ model: "chat-model", stream: true, max_completion_tokens: 100, temperature: 0.5,
    tools: [{ type: "function", function: { name: "Bash" } }] });
  expect(body.messages.at(-1)).toMatchObject({ role: "tool", tool_call_id: "old_call", content: "stdout" });
  expect(body.messages[1].tool_calls[0].function.arguments).toBe('{"command":"pwd"}');
});

test("OMA selects backend by actual model while isolating concurrent session credentials", async () => {
  const requests: Request[] = [];
  const api = new OmaApi("http://model.test", (async (input, init) => {
    const request = new Request(input, init); requests.push(request);
    if (new URL(request.url).pathname === "/v1/messages") return new Response(textEvents("anthropic").map((event) => `event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`).join(""), { headers: { "Content-Type": "text/event-stream" } });
    return response([textChunk(), "[DONE]"]);
  }) as typeof fetch);
  const a = assignment(); a.launch.model_backends = { "anthropic-model": "messages", "chat-model": "chat_completions" }; a.model_token = "root-token";
  const b = assignment(); b.launch.model_backends = a.launch.model_backends; b.model_token = "other-token";
  api.assignments.set(a.session_id, a); api.assignments.set(b.session_id, b); api.modelSessions.set("child", a.session_id);
  const adapter = new AiSdkAdapter(omaModelOptions(api));
  await Promise.all([collect(adapter.stream(options(a.session_id, "anthropic-model"))), collect(adapter.stream(options("child"))), collect(adapter.stream(options(b.session_id)))]);
  expect(requests.map((request) => [new URL(request.url).pathname, request.headers.get("authorization"), request.headers.get("x-claude-code-agent-id")]).sort()).toEqual([
    ["/v1/messages", "Bearer root-token", null], ["/v1/chat/completions", "Bearer root-token", "child"], ["/v1/chat/completions", "Bearer other-token", null],
  ].sort());
  a.model_token = "rotated"; await collect(adapter.stream(options("child")));
  expect(requests.at(-1)!.headers.get("authorization")).toBe("Bearer rotated");
  await expect(collect(adapter.stream(options(a.session_id, "missing")))).rejects.toMatchObject({ code: "INVALID_ARGS" });
});
