import { expect, test } from "bun:test";
import type { GenerateOptions } from "@deepseek-ai/dsh-llm";
import type { AiSdkRoute } from "../src/llm-ai-sdk/route.ts";
import { AiSdkStream } from "../src/llm-ai-sdk/stream.ts";
import { callOptions } from "../src/llm-ai-sdk/prompt.ts";

const usage = { inputTokens: { total: 1, noCache: 1, cacheRead: 0, cacheWrite: 0 }, outputTokens: { total: 1, text: 0, reasoning: 1 } };

test("Responses 加密推理状态经持久化后按协议恢复", async () => {
  const stream = new AiSdkStream("oma-responses-v1");
  stream.accept({ type: "reasoning-start", id: "reason", providerMetadata: { openai: { itemId: "rs_1", reasoningEncryptedContent: null } } });
  stream.accept({ type: "reasoning-end", id: "reason", providerMetadata: { openai: { reasoningEncryptedContent: "encrypted-final" } } });
  stream.accept({ type: "finish", finishReason: { unified: "stop", raw: "stop" }, usage });
  const finish = stream.finish().find(part => part.type === "finish")!;
  const replayState = JSON.parse(JSON.stringify(finish.replayState));
  expect(replayState.blocks).toEqual([{ index: 0, itemId: "rs_1", reasoningEncryptedContent: "encrypted-final" }]);
  const options = { model: "test-model", messages: [{ role: "assistant", content: [{ type: "reasoning", text: "" }], source: { replayState } }] } as unknown as GenerateOptions;
  for (const replayFormat of ["oma-responses-v1", "oma-chat-completions-v1"]) {
    const result = await callOptions(options, { replayFormat } as AiSdkRoute);
    const message = result.prompt[0]!;
    expect(message.role).toBe("assistant");
    expect(message.content).toEqual([replayFormat === "oma-responses-v1"
      ? { type: "reasoning", text: "", providerOptions: { openai: { itemId: "rs_1", reasoningEncryptedContent: "encrypted-final" } } }
      : { type: "reasoning", text: "" }]);
  }
});

test("其他协议忽略 OpenAI 推理元数据并保留 Anthropic 签名", () => {
  const stream = new AiSdkStream("oma-messages-v1");
  stream.accept({ type: "reasoning-start", id: "reason", providerMetadata: { openai: { itemId: "rs_other" }, anthropic: { signature: "first" } } });
  stream.accept({ type: "reasoning-end", id: "reason", providerMetadata: { openai: { reasoningEncryptedContent: "encrypted" }, anthropic: { signature: "second" } } });
  stream.accept({ type: "finish", finishReason: { unified: "stop", raw: "end_turn" }, usage });
  expect(stream.finish().find(part => part.type === "finish")!.replayState?.blocks).toEqual([{ index: 0, signature: "firstsecond" }]);
});
