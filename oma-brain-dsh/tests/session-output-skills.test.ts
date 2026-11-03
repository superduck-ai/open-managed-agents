import { expect, test } from "bun:test";
import { SessionSeq, type SessionEvent } from "@deepseek-ai/dsh-session";
import { ToolCallId, createToolResultMessage } from "@deepseek-ai/dsh-llm";
import { CcrToolResults } from "../src/ccrv2/tool-results.ts";
import { sessionEventOutput } from "../src/ccrv2/session-output.ts";

test.each([undefined, "child"])("native skill loading stays in the journal for %s", threadId => {
  for (const isError of [false, true]) {
    const call = { type: "tool/call", data: { callId: "native", name: "skill", arguments: '{"name":"pdf"}' } } as SessionEvent;
    const result: SessionEvent = { type: "tool/result", surfaceOp: "append", seq: SessionSeq(2), time: 2,
      data: { turn: 1, step: 1, message: createToolResultMessage({ callId: ToolCallId("native"), isError, content: [{ type: "text", text: "skill body" }] }) } };
    for (const restored of [false, true]) {
      const names = new CcrToolResults();
      names.record(restored ? structuredClone(call) : call, threadId ?? "root");
      const event = restored ? structuredClone(result) : result;
      const name = names.record(event, threadId ?? "root");
      expect(sessionEventOutput("session", "input", event, name, threadId)).toEqual([]);
      expect(event.data).toEqual(result.data);
    }
    for (const name of [undefined, "Read", "Bash", "mcp__peer__fetch", "AskUserQuestion"]) {
      const output = sessionEventOutput("session", "input", result, name, threadId);
      expect(output).toHaveLength(1);
      expect(output[0]!.message.content[0]).toMatchObject({ tool_use_id: "native", is_error: isError });
    }
  }
});
