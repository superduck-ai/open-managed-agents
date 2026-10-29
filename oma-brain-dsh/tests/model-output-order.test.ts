import { expect, test } from "bun:test";
import { pluginRecordOf } from "@deepseek-ai/dsh-session";
import { Context } from "@deepseek-ai/cordis";
import type { LanguageModelV4StreamPart } from "@ai-sdk/provider";
import { AiSdkStream } from "../src/llm-ai-sdk/stream.ts";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { Backend, eventually } from "./backend.ts";

const usage = { inputTokens: { total: 1, noCache: 1, cacheRead: 0, cacheWrite: 0 }, outputTokens: { total: 1, text: 1, reasoning: 0 } };

test("a stream with an unclosed model block fails instead of fabricating its end", () => {
  const stream = new AiSdkStream("oma-messages-v1");
  stream.accept({ type: "text-start", id: "answer" });
  stream.accept({ type: "text-delta", id: "answer", delta: "partial" });
  stream.accept({ type: "finish", finishReason: { unified: "stop", raw: "end_turn" }, usage });
  expect(() => stream.finish()).toThrow("unclosed");
});

test("block ends advance at upstream boundaries, including interleaved text, thinking and tools", () => {
  const stream = new AiSdkStream("oma-messages-v1");
  const parts: LanguageModelV4StreamPart[] = [
    { type: "reasoning-start", id: "reason" },
    { type: "reasoning-delta", id: "reason", delta: "private" },
    { type: "reasoning-end", id: "reason" },
    { type: "text-start", id: "first" },
    { type: "text-delta", id: "first", delta: "first" },
    { type: "text-end", id: "first" },
    { type: "tool-input-start", id: "tool", toolName: "Read" },
    { type: "tool-input-delta", id: "tool", delta: "{}" },
    { type: "tool-input-end", id: "tool" },
    { type: "text-start", id: "second" },
    { type: "text-delta", id: "second", delta: "second" },
    { type: "tool-call", toolCallId: "tool", toolName: "Read", input: "{}" },
    { type: "text-end", id: "second" },
  ];
  expect(parts.map(part => stream.accept(part).map(chunk => chunk.type))).toEqual([
    ["block-start"], ["reasoning-delta"], ["block-end"],
    ["block-start"], ["text-delta"], ["block-end"],
    ["block-start"], ["tool-call-delta"], [],
    ["block-start"], ["text-delta"], ["block-end"], ["block-end"],
  ]);
  stream.accept({ type: "finish", finishReason: { unified: "tool-calls", raw: "tool_use" }, usage });
  expect(stream.finish().map(chunk => chunk.type)).toEqual(["usage", "finish"]);
});

test.each([false, true])("completed thinking is durable and public before the next model block (child=%s)", async child => {
  const backend = new Backend();
  await backend.ready;
  const ctx = new Context();
  const api = new OmaApi(backend.url);
  await mountOma(ctx, api);
  const session = createOmaSession(ctx, api, backend.a);
  const release = Promise.withResolvers<void>();
  const sentReasoning = Promise.withResolvers<void>();
  let turn: Promise<unknown> | undefined;
  let confirmed = false;
  const persist = session.backend.persist.bind(session.backend);
  session.backend.persist = async (...args) => {
    await persist(...args);
    if ("type" in args[2] && "seq" in args[2] && pluginRecordOf(args[2])?.type === "plugin:oma/assistant-block") confirmed = true;
  };
  backend.onModel = async () => new Response(new ReadableStream<Uint8Array>({ async start(controller) {
    const emit = (event: Record<string, unknown>) => controller.enqueue(new TextEncoder().encode(`event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`));
    emit({ type: "message_start", message: { id: "msg_ordered", type: "message", role: "assistant", model: "test-model", content: [], usage: { input_tokens: 1, output_tokens: 0 } } });
    emit({ type: "content_block_start", index: 0, content_block: { type: "thinking", thinking: "" } });
    emit({ type: "content_block_delta", index: 0, delta: { type: "thinking_delta", thinking: "private reasoning" } });
    emit({ type: "content_block_stop", index: 0 });
    sentReasoning.resolve();
    await release.promise;
    emit({ type: "content_block_start", index: 1, content_block: { type: "text", text: "" } });
    emit({ type: "content_block_delta", index: 1, delta: { type: "text_delta", text: "ordered answer" } });
    emit({ type: "content_block_stop", index: 1 });
    emit({ type: "message_delta", delta: { stop_reason: "end_turn" }, usage: { output_tokens: 2 } });
    emit({ type: "message_stop" });
    controller.close();
  } }), { headers: { "content-type": "text/event-stream", "request-id": "request_ordered" } });
  try {
    await session.open();
    turn = child
      ? session.subagents.send({ name: "specialist", system_prompt: "Inspect", message: "Reply" }, new AbortController().signal)
      : session.receive("ordered-input", { content: "Reply" });
    turn.catch(() => {});
    await sentReasoning.promise;
    await eventually(() => [...backend.publicLog.values()].some(event => event.type === "agent.thinking"));
    expect(confirmed).toBe(true);
    expect([...backend.publicLog.values()].filter(event => event.type === "agent.thinking")).toHaveLength(1);
    expect([...backend.publicLog.values()].find(event => event.type === "agent.thinking")?.model_request_start_id).toBe("request_ordered");
    expect(backend.timeline.some(event => event.type === "event_delta")).toBe(false);
    release.resolve();
    await turn;
    const types = backend.timeline.map(event => event.type);
    expect(types.indexOf("agent.thinking")).toBeLessThan(types.indexOf("event_delta"));
    const finals = [...backend.publicLog.values()].filter(event => ["agent.thinking", "agent.message"].includes(event.type));
    expect(finals.map(event => event.type)).toEqual(["agent.thinking", "agent.message"]);
    expect(finals.every(event => event.uuid === event.id)).toBe(true);
    expect(JSON.stringify(finals)).not.toContain("private reasoning");
    expect(finals[1]?.session_thread_id).toBe(child ? session.subagents.threads[0]!.id : undefined);
  } finally {
    release.resolve();
    await turn?.catch(() => {});
    await session.close();
    await ctx.fiber.dispose();
    await backend.stop();
  }
});
