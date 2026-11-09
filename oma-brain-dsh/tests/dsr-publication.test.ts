import {
  Session,
  SessionId,
  appendPluginRecord,
} from "@deepseek-ai/dsh-session";
import {
  createUserMessage,
  createAssistantMessage,
  ToolCallId,
} from "@deepseek-ai/dsh-llm";
import { expect, test } from "bun:test";
import { TranscriptPublication } from "../src/dsr/publication.ts";

function session(id: string) {
  const value = Session.create(SessionId(id));
  value.append("turn/start", { turn: 1 });
  value.append(
    "user/message",
    createUserMessage({
      content: [{ type: "text", text: "input" }],
      source: { kind: "user" },
    }),
    { surfaceOp: "append" },
  );
  return value;
}

test("legacy publication receipts never suppress history", () => {
  for (const data of [
    { version: 1, through: 2 },
    { version: 2, through: 1 },
    { version: 1, through: 99 },
    { version: 1, through: -1 },
    { version: 1, through: 0.5 },
  ]) {
    const source = session("legacy");
    appendPluginRecord(source, "plugin:oma/dsr-publication", data as never);
    const projection = new TranscriptPublication();
    projection.prepare(source);
    const events = source.snapshotEvents();
    expect(projection.read(events.at(-1)!, source)).toEqual(events);
    expect(projection.read(events.at(-1)!, source)).toEqual([]);
  }
});

test("in-memory projection cursors are independent for root and child", () => {
  const root = session("root"),
    child = session("child");
  const projection = new TranscriptPublication();
  for (const source of [root, child]) {
    projection.prepare(source);
    const events = source.snapshotEvents();
    expect(projection.read(events.at(-1)!, source)).toEqual(events);
    expect(projection.read(events.at(-1)!, source)).toEqual([]);
    expect(source.snapshotEvents()).toEqual(events);
  }
});

test("tool identity preserves explicit call priority over the assistant fallback", () => {
  const source = session("tools");
  source.append("step/start", { turn: 1, step: 1 });
  const callId = ToolCallId("call");
  source.append(
    "assistant/message",
    {
      turn: 1,
      step: 1,
      stream: [],
      message: createAssistantMessage({
        source: { provider: "test", model: "test" },
        content: [
          { type: "tool-call", id: callId, name: "Bash", arguments: "{}" },
        ],
      }),
    },
    { surfaceOp: "append" },
  );
  const fallback = new TranscriptPublication();
  fallback.prepare(source);
  expect(fallback.tool(source, callId)).toBe("Bash");
  source.append("tool/call", {
    turn: 1,
    step: 1,
    callId,
    name: "Read",
    arguments: "{}",
  });
  fallback.read(source.snapshotEvents().at(-1)!, source);
  expect(fallback.tool(source, callId)).toBe("Read");
});
