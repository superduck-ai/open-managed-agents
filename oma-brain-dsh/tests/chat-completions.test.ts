import { expect, test } from "bun:test";
import { BlockAssembler, MessageId, ToolCallId, type GenerateOptions, type StreamChunk } from "@deepseek-ai/dsh-llm";
import { SessionId } from "@deepseek-ai/dsh-session";
import { OmaApi } from "../src/api.ts";
import { ChatCompletionsAdapter } from "../src/chat-model.ts";
import { OmaModelAdapter } from "../src/model-router.ts";
import { assistantMessages } from "../src/ccrv2/assistant.ts";
import { assignment, textEvents } from "./backend.ts";
import type { ChatChunk } from "../src/chat-stream.ts";

function response(chunks: (ChatChunk | string)[]) {
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
function setup(chunks: (ChatChunk | string)[]) {
  const requests: Request[] = [];
  const api = new OmaApi("http://model.test", (async (input, init) => {
    requests.push(new Request(input, init));
    return response(chunks);
  }) as typeof fetch);
  const a = assignment(); api.assignments.set(a.session_id, a);
  return { api, a, requests, adapter: new ChatCompletionsAdapter(api) };
}
function textChunk(text = "answer", finish = "stop"): ChatChunk {
  return { id: "chat_1", choices: [{ index: 0, delta: { content: text }, finish_reason: finish }] };
}

test("Chat Completions rejects truncated, filtered, invalid tool and unsupported streams", async () => {
  for (const chunks of [
    [textChunk()],
    [{ id: "chat_1", choices: [{ index: 0, delta: { content: "partial" } }] }, "[DONE]"],
    [textChunk("refused", "content_filter"), "[DONE]"],
    [{ id: "chat_1", choices: [{ index: 0, delta: { tool_calls: [{ index: -1 }] } }] }, "[DONE]"],
    [{ id: "chat_1", choices: [{ index: 0, delta: { tool_calls: [{ index: 0, function: { arguments: "{}" } }] }, finish_reason: "tool_calls" }] }, "[DONE]"],
    [{ id: "chat_1", choices: [{ index: 0, delta: { tool_calls: [{ index: 0, id: "call", function: { name: "Bash", arguments: "[]" } }] }, finish_reason: "tool_calls" }] }, "[DONE]"],
    [{ id: "chat_1", choices: [{ index: 0, delta: { reasoning_content: "thinking" } }] }, "[DONE]"],
    ["{broken"],
  ] satisfies (ChatChunk | string)[][]) {
    const { a, adapter } = setup(chunks);
    await expect(collect(adapter.stream(options(a.session_id)))).rejects.toThrow();
  }
});

test("Chat Completions maps HTTP and stream errors and honors preflight abort", async () => {
  for (const [status, code] of [[401, "AUTH"], [403, "AUTH"], [429, "RATE_LIMIT"], [503, "SERVER"]] as const) {
    const api = new OmaApi("http://model.test", (async (_input, _init) => Response.json({ error: { message: "denied" } }, { status })) as typeof fetch);
    const a = assignment(); api.assignments.set(a.session_id, a);
    await expect(collect(new ChatCompletionsAdapter(api).stream(options(a.session_id)))).rejects.toMatchObject({ code });
  }
  const { a, adapter, requests } = setup([{ error: { message: "quota exceeded" } }]);
  await expect(collect(adapter.stream(options(a.session_id)))).rejects.toThrow("quota exceeded");
  await expect(collect(adapter.stream({ ...options(a.session_id), signal: AbortSignal.abort() }))).rejects.toThrow();
  expect(requests).toHaveLength(1);
});

test("Chat Completions cancellation closes an active response body", async () => {
  let cancelled = false;
  const api = new OmaApi("http://model.test", (async (_input, _init) => new Response(new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(new TextEncoder().encode(`data: ${JSON.stringify({ id: "chat_cancel", choices: [{ index: 0, delta: { content: "partial" } }] })}\n\n`));
    },
    cancel() { cancelled = true; },
  }))) as typeof fetch);
  const a = assignment(); api.assignments.set(a.session_id, a);
  const controller = new AbortController();
  const iterator = new ChatCompletionsAdapter(api).stream({ ...options(a.session_id), signal: controller.signal })[Symbol.asyncIterator]();
  expect((await iterator.next()).value).toMatchObject({ type: "block-start" });
  expect((await iterator.next()).value).toMatchObject({ type: "text-delta", text: "partial" });
  controller.abort();
  await expect(iterator.next()).rejects.toThrow();
  expect(cancelled).toBe(true);
});


test("Chat Completions preserves fragmented parallel tools and consumes the usage tail", async () => {
  const { a, api, adapter, requests } = setup([
    { id: "chat_1", choices: [{ index: 0, delta: { content: "running", tool_calls: [
      { index: 1, id: "call_b", type: "function", function: { name: "Read", arguments: "{" } },
      { index: 0, id: "call_a", type: "function", function: { name: "Bash", arguments: '{"command":' } },
    ] } }] },
    { id: "chat_1", choices: [{ index: 0, delta: { tool_calls: [
      { index: 0, function: { arguments: '"pwd"}' } }, { index: 1, function: { arguments: "}" } },
    ] }, finish_reason: "tool_calls" }] },
    { id: "chat_1", choices: [], usage: { prompt_tokens: 20, completion_tokens: 5, prompt_tokens_details: { cached_tokens: 12 } } },
    "[DONE]",
  ]);
  const result = await collect(adapter.stream(options(a.session_id)));
  expect(result.filter((chunk) => chunk.type === "block-end")).toEqual([
    { type: "block-end", index: 0, block: { type: "text", text: "running" } },
    { type: "block-end", index: 1, block: { type: "tool-call", id: ToolCallId("call_a"), name: "Bash", arguments: '{"command":"pwd"}' } },
    { type: "block-end", index: 2, block: { type: "tool-call", id: ToolCallId("call_b"), name: "Read", arguments: "{}" } },
  ]);
  expect(result).toContainEqual({ type: "usage", usage: { inputTokens: 8, outputTokens: 5, cacheReadTokens: 12 } });
  expect(result.at(-1)).toMatchObject({ type: "finish", reason: { kind: "tool-calls" }, replayState: {
    response: { format: "oma-chat-completions-v1", messageID: "chat_1", requestID: "span_chat" }, blocks: [{ index: 0 }, { index: 2 }, { index: 1 }],
  } });
  expect(api.modelResponses.get(a.session_id)).toBe("chat_1");
  expect(new URL(requests[0]!.url).pathname).toBe("/v1/chat/completions");
  expect(requests[0]!.headers.has("anthropic-version")).toBe(false);
});

test("Chat history round-trips tool results and stable assistant identity", async () => {
  const { a, adapter, requests } = setup([textChunk(), "[DONE]"]);
  const request = options(a.session_id);
  request.system = "instructions"; request.maxTokens = 100; request.temperature = 0.5; request.stop = ["END"];
  request.tools = [{ name: "Bash", description: "Run", parameters: { type: "object" } }];
  request.messages.push({ id: MessageId("saved"), role: "assistant", content: [
    { type: "tool-call", id: ToolCallId("call_a"), name: "Bash", arguments: '{"command":"pwd"}' },
  ], source: { kind: "model", provider: "oma", model: "chat-model", replayState: { response: { format: "oma-chat-completions-v1" } } } },
  { id: MessageId("result"), role: "tool", toolCallId: ToolCallId("call_a"), source: { kind: "tool", callId: ToolCallId("call_a") },
    content: [{ type: "text", text: "permission denied" }], isError: true });
  const chunks = await collect(adapter.stream(request));
  const body = await requests[0]!.json();
  expect(body).toMatchObject({ model: "chat-model", stream: true, n: 1, max_completion_tokens: 100, temperature: 0.5, stop: ["END"],
    stream_options: { include_usage: true }, tools: [{ type: "function", function: { name: "Bash", parameters: { type: "object" } } }] });
  expect(body.messages).toEqual([
    { role: "system", content: "instructions" }, { role: "user", content: "Hello" },
    { role: "assistant", content: null, tool_calls: [{ id: "call_a", type: "function", function: { name: "Bash", arguments: '{"command":"pwd"}' } }] },
    { role: "tool", tool_call_id: "call_a", content: "permission denied" },
  ]);
  expect(chunks.some((chunk) => chunk.type === "usage")).toBe(false);
  const finish = chunks.find((chunk) => chunk.type === "finish")!;
  if (finish.type !== "finish") throw new Error("No finish");
  const events = assistantMessages(a.session_id, "fallback", {
    id: MessageId("saved"), role: "assistant", content: [{ type: "text", text: "answer" }],
    source: { kind: "model", provider: "oma", model: "chat-model", replayState: finish.replayState },
  });
  expect(events[0]?.model_request_start_id).toBe("span_chat");
  expect(events[0]?.id).not.toContain("fallback");
});

test("model router selects root and child backends and re-reads rotated credentials", async () => {
  const paths: string[] = [];
  const tokens: string[] = [];
  const agents: (string | null)[] = [];
  const api = new OmaApi("http://model.test", (async (input, init) => {
    const request = new Request(input, init); const path = new URL(request.url).pathname;
    paths.push(path); tokens.push(request.headers.get("Authorization")!);
    agents.push(request.headers.get("X-Claude-Code-Agent-Id"));
    if (path === "/v1/chat/completions") return response([textChunk(), "[DONE]"]);
    return new Response(textEvents("root").map((event) => `data: ${JSON.stringify(event)}\n\n`).join(""));
  }) as typeof fetch);
  const a = assignment(); a.launch.model_backends = { "anthropic-model": "messages", "chat-model": "chat_completions" };
  api.assignments.set(a.session_id, a); api.modelSessions.set("child", a.session_id);
  const adapter = new OmaModelAdapter(api);
  await collect(adapter.stream(options(a.session_id, "anthropic-model")));
  await collect(adapter.stream(options("child")));
  a.model_token = "rotated";
  await collect(adapter.stream(options(a.session_id)));
  await collect(adapter.stream(options("child", "anthropic-model")));
  expect(paths).toEqual(["/v1/messages", "/v1/chat/completions", "/v1/chat/completions", "/v1/messages"]);
  expect(tokens.at(-1)).toBe("Bearer rotated");
  expect(agents).toEqual([null, "child", null, "child"]);
  expect(() => adapter.stream(options("child", "removed"))).toThrow("reload");
});

test("replay indices follow native assembly when tools precede text", async () => {
  const { a, adapter } = setup([
    { id: "chat_1", choices: [{ index: 0, delta: { tool_calls: [
      { index: 1, id: "call_b", function: { name: "Read", arguments: "{}" } },
      { index: 0, id: "call_a", function: { name: "Bash", arguments: "{}" } },
    ] } }] },
    { id: "chat_1", choices: [{ index: 0, delta: { content: "running" }, finish_reason: "tool_calls" }] },
    "[DONE]",
  ]);
  const assembler = new BlockAssembler();
  for (const chunk of await collect(adapter.stream(options(a.session_id)))) assembler.push(chunk);
  const message = assembler.message({ provider: "oma", model: "chat-model", replayState: assembler.replayState });
  expect(message.content.map((block) => block.type)).toEqual(["tool-call", "tool-call", "text"]);
  expect(message.source.replayState).toMatchObject({ blocks: [{ index: 2 }, { index: 1 }, { index: 0 }] });
  const events = assistantMessages(a.session_id, "fallback", message);
  const textOnly = assistantMessages(a.session_id, "fallback", { ...message, content: [{ type: "text", text: "running" }],
    source: { ...message.source, replayState: { response: { format: "oma-chat-completions-v1", messageID: "chat_1", requestID: "span_chat" }, blocks: [{ index: 0 }] } } });
  expect(events[0]?.id).toBe(textOnly[0]?.id);
});
