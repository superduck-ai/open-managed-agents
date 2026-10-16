import { expect, test } from "bun:test";
import type { AssistantStreamFrame } from "@deepseek-ai/dsh-agent";
import { MessageId, type AssistantMessage, type StreamChunk } from "@deepseek-ai/dsh-llm";
import { assistantMessages, CcrAssistantStream } from "../src/ccrv2/assistant.ts";
import vectors from "../../internal/managedagentsevents/testdata/thinking-identity.json";

function start(): AssistantStreamFrame {
  return { type: "start", attemptId: "attempt", revision: 1, turn: 1, step: 1 } as AssistantStreamFrame;
}
function chunk(value: StreamChunk): AssistantStreamFrame {
  return { type: "chunk", attemptId: "attempt", revision: 1, index: 0, time: 0, chunk: value } as AssistantStreamFrame;
}

test("reasoning deltas without a block start do not publish content", () => {
  const stream = new CcrAssistantStream("cse_test", () => "msg_test");
  stream.record(start(), "input");
  expect(stream.record(chunk({ type: "reasoning-delta", index: 0, text: "private" }), "input")).toEqual([]);
});

test("empty thinking starts once before answer and shares backend identity", () => {
  for (const vector of vectors) {
    const stream = new CcrAssistantStream(vector.codeSessionID, () => vector.messageID);
    stream.record(start(), "input");
    const blockStart = chunk({ type: "block-start", index: vector.index, blockType: "reasoning" });
    expect(stream.record(blockStart, "input")).toEqual([
      { type: "event_start", event: { type: "agent.thinking", id: vector.eventID } },
    ]);
    expect(stream.record(blockStart, "input")).toEqual([]);
    expect(stream.record(chunk({ type: "reasoning-delta", index: vector.index, text: "private reasoning" }), "input")).toEqual([]);
    expect(stream.record(chunk({ type: "block-end", index: vector.index, block: { type: "reasoning", text: "private reasoning" } }), "input")).toEqual([
      { type: "event_stop", event_id: vector.eventID },
    ]);
    expect(stream.record(blockStart, "input")).toEqual([]);
    const answer = stream.record(chunk({ type: "text-delta", index: vector.index + 1, text: "answer" }), "input");
    expect(answer[0]?.event.type).toBe("agent.message");
    const message: AssistantMessage = { id: MessageId("fixture"), role: "assistant", content: [{ type: "reasoning", text: "private reasoning" }], source: { kind: "model", provider: "oma", model: "test-model", replayState: {
      response: { format: "oma-messages-v1", messageID: vector.messageID }, blocks: [{ index: vector.index }],
    } } };
    const final = assistantMessages(vector.codeSessionID, "fallback", message);
    expect(final).toEqual([{ type: "agent.thinking", id: vector.eventID, uuid: vector.eventID }]);
    expect(new Set([vector.eventID, final[0]!.id]).size).toBe(1);
  }
});
