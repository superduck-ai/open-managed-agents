import { expect,test } from "bun:test";



import type { LanguageModelV4StreamPart } from "@ai-sdk/provider";

import { AiSdkStream } from "../src/llm-ai-sdk/stream.ts";



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
