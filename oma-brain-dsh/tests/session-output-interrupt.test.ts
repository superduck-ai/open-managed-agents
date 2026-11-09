import { expect, test } from "bun:test";
import { MessageId } from "@deepseek-ai/dsh-llm";
import { SessionSeq, type SessionEvent } from "@deepseek-ai/dsh-session";
import { sessionEventOutput } from "../src/ccrv2/session-output.ts";

function message(interrupted: boolean): SessionEvent {
  return {
    type: "assistant/message", seq: SessionSeq(1), time: 1, surfaceOp: "append",
    data: {
      turn: 1, step: 3, stream: [], ...(interrupted ? { interrupted: true as const } : {}),
      message: { id: MessageId("partial"), role: "assistant", content: [{ type: "text", text: "Both subagents have been started" }],
        source: { kind: "model", provider: "oma", model: "kimi-k2.5" } },
    },
  };
}

test.each([undefined, "child"])("cancelled prefix is retained only in the native journal for %s", threadId => {
  expect(sessionEventOutput("session", "input", message(true), undefined, threadId)).toEqual([]);
});

test.each([undefined, "child"])("completed unproxied message remains public for %s", threadId => {
  const output = sessionEventOutput("session", "input", message(false), undefined, threadId);
  expect(output).toHaveLength(1);
  expect(output[0]).toMatchObject({ type: "agent.message", content: [{ type: "text", text: "Both subagents have been started" }] });
  if (threadId) expect(output[0]!.session_thread_id).toBe(threadId);
});
