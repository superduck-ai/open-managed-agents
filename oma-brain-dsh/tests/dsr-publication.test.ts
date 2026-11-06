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

test("invalid and future publication receipts cannot skip history", () => {
  for (const data of [
    { version: 2, through: 1 },
    { version: 1, through: 99 },
    { version: 1, through: -1 },
    { version: 1, through: 0.5 },
  ]) {
    const source = session("invalid");
    appendPluginRecord(source, "plugin:oma/dsr-publication", data as never);
    expect(() => new TranscriptPublication().prepare(source)).toThrow();
  }
});

test("regressing publication receipts are rejected", () => {
  const source = session("regressing");
  appendPluginRecord(source, "plugin:oma/dsr-publication", {
    version: 1,
    through: 2,
  });
  appendPluginRecord(source, "plugin:oma/dsr-publication", {
    version: 1,
    through: 1,
  });
  expect(() => new TranscriptPublication().prepare(source)).toThrow(
    "invalid_publication_receipt",
  );
});

test("publication cursors are independent for root and child transcripts", () => {
  const root = session("root"),
    child = session("child");
  appendPluginRecord(root, "plugin:oma/dsr-publication", {
    version: 1,
    through: 2,
  });
  const projection = new TranscriptPublication();
  projection.prepare(root);
  projection.prepare(child);
  expect(projection.confirmed(root, root.snapshotEvents()[1]!)).toBe(true);
  expect(projection.confirmed(child, child.snapshotEvents()[1]!)).toBe(false);
});

test("receipts and non-output tails do not create new publication targets", () => {
  const source = session("target");
  const projection = new TranscriptPublication();
  const event = source.snapshotEvents()[1]!;
  projection.read(event, source);
  projection.queued(source, event);
  const targets = projection.targets();
  projection.acknowledge(targets);
  projection.read(source.snapshotEvents().at(-1)!, source);
  expect(projection.targets()).toEqual([]);
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
