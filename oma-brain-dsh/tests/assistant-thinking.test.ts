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


test("text preview explicitly indexes its public single-block message even for nonzero native blocks", () => {
  for (const index of [0, 3]) {
    const stream = new CcrAssistantStream("cse_preview", () => "msg_preview");
    stream.record(start(), "input");
    const first = stream.record(chunk({ type: "text-delta", index, text: "A" }), "input");
    const next = stream.record(chunk({ type: "text-delta", index, text: "B" }), "input");
    const id = first[0]!.event.id;
    expect(first[1]).toEqual({ type: "event_delta", event_id: id, delta: {
      type: "content_delta", index: 0, content: { type: "text", text: "A" },
    } });
    expect(Object.hasOwn(first[1]!.delta, "index")).toBe(true);
    expect(next).toEqual([{ type: "event_delta", event_id: id, delta: {
      type: "content_delta", index: 0, content: { type: "text", text: "B" },
    } }]);
    const message: AssistantMessage = { id: MessageId("buffered-preview"), role: "assistant", content: [{ type: "text", text: "AB" }], source: { kind: "model", provider: "oma", model: "test-model", replayState: {
      response: { format: "oma-messages-v1", messageID: "msg_preview" }, blocks: [{ index }],
    } } };
    const final = assistantMessages("cse_preview", "fallback", message);
    expect(final[0]!.id).toBe(id);
    expect(final[0]!.content).toEqual([{ type: "text", text: "AB" }]);
  }
});
