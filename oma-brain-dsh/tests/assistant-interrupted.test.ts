import { expect, test } from "bun:test";
import type { AssistantStreamFrame } from "@deepseek-ai/dsh-agent";
import { createAssistantMessage } from "@deepseek-ai/dsh-llm";
import {
  AssistantStream,
  interruptedAssistantMessages,
} from "../src/runtime/assistant.ts";

function frame(stream: AssistantStream, chunk: unknown, thread?: string) {
  return stream.record(
    {
      type: "chunk",
      attemptId: "attempt",
      revision: 2,
      index: 0,
      time: 0,
      chunk,
    } as AssistantStreamFrame,
    "input",
    thread,
  );
}
function begin(stream: AssistantStream, thread?: string) {
  stream.record(
    {
      type: "start",
      attemptId: "attempt",
      revision: 1,
      turn: 1,
      step: 1,
    } as AssistantStreamFrame,
    "input",
    thread,
  );
}
function message(
  content: Parameters<typeof createAssistantMessage>[0]["content"],
) {
  return createAssistantMessage({
    source: { provider: "test", model: "test" },
    content,
  });
}

test("interrupted mixed blocks retain only durable matching prefixes with stable stream identities", () => {
  const stream = new AssistantStream(
    "root",
    (id) => "response-" + id,
    (id) => "request-" + id,
  );
  begin(stream);
  frame(stream, { type: "block-start", index: 0, blockType: "reasoning" });
  frame(stream, {
    type: "reasoning-delta",
    index: 0,
    text: "actual reasoning",
  });
  const completed = frame(stream, {
    type: "block-end",
    index: 0,
    block: { type: "reasoning", text: "actual reasoning" },
  });
  frame(stream, { type: "block-start", index: 1, blockType: "tool-call" });
  const started = frame(stream, {
    type: "block-start",
    index: 2,
    blockType: "text",
  });
  frame(stream, { type: "text-delta", index: 2, text: "actual prefix" });
  const partial = message([
    { type: "reasoning", text: "actual reasoning" },
    { type: "text", text: "actual prefix" },
  ]);
  const outputs = stream.interrupted(partial);
  expect(outputs).toHaveLength(1);
  expect(outputs[0]).toMatchObject({
    type: "agent.message",
    id: started[0]!.event.id,
    content: [{ type: "text", text: "actual prefix" }],
    model_request_start_id: "request-root",
  });
  expect(outputs[0]!.id).not.toBe(completed[0]!.id);
  expect(() =>
    stream.interrupted(
      message([
        { type: "reasoning", text: "actual reasoning" },
        { type: "text", text: "invented tail" },
      ]),
    ),
  ).toThrow("differs from streamed prefix");
  expect(() =>
    stream.interrupted(message([{ type: "text", text: "actual prefix" }])),
  ).toThrow("differs from streamed prefix");
});

test("empty or whitespace interruption emits no fabricated final and cannot acquire another thread's prefix", () => {
  const stream = new AssistantStream(
    "root",
    (id) => "response-" + id,
    (id) => "request-" + id,
  );
  begin(stream);
  begin(stream, "child");
  frame(stream, { type: "block-start", index: 0, blockType: "text" });
  frame(stream, { type: "text-delta", index: 0, text: " \n\t" });
  frame(stream, { type: "block-start", index: 0, blockType: "text" }, "child");
  frame(
    stream,
    { type: "text-delta", index: 0, text: "child prefix" },
    "child",
  );
  expect(stream.interrupted(message([]))).toEqual([]);
  expect(() =>
    stream.interrupted(message([{ type: "text", text: "child prefix" }])),
  ).toThrow();
  const child = stream.interrupted(
    message([{ type: "text", text: "child prefix" }]),
    "child",
  );
  expect(child[0]).toMatchObject({
    session_thread_id: "child",
    model_request_start_id: "request-child",
  });
  expect(() => stream.interrupted(message([]), "unknown")).toThrow(
    "unavailable",
  );
});

test("cold interrupted projection requires exact durable chunks and preserves nonzero block identity", () => {
  const records = [
    {
      type: "chunk" as const,
      time: 0,
      chunk: {
        type: "block-start" as const,
        index: 2,
        blockType: "text" as const,
      },
    },
    {
      type: "text-chunks" as const,
      time0: 1,
      index: 2,
      dt: [],
      texts: ["prefix"],
    },
  ];
  const identity = {
    turn: 1,
    step: 1,
    messageID: "response",
    requestID: "request",
  };
  const stream = new AssistantStream(
    "root",
    () => "response",
    () => "request",
  );
  begin(stream);
  const preview = frame(stream, {
    type: "block-start",
    index: 2,
    blockType: "text",
  });
  const recovered = interruptedAssistantMessages(
    "root",
    "input",
    message([{ type: "text", text: "prefix" }]),
    records,
    identity,
  );
  expect(recovered).toHaveLength(1);
  expect(recovered[0]).toMatchObject({
    id: preview[0]!.event.id,
    model_request_start_id: "request",
    content: [{ type: "text", text: "prefix" }],
  });
  expect(() =>
    interruptedAssistantMessages(
      "root",
      "input",
      message([{ type: "text", text: "unseen tail" }]),
      records,
      identity,
    ),
  ).toThrow("differs from streamed prefix");
  expect(() =>
    interruptedAssistantMessages(
      "root",
      "input",
      message([{ type: "text", text: "prefix" }]),
      [],
      identity,
    ),
  ).toThrow("differs from streamed prefix");
});
