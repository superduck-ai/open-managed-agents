import { expect, test } from "bun:test";
import type { SessionEvent } from "@deepseek-ai/dsh-session";
import { CcrToolResults } from "../src/ccrv2/tool-results.ts";

function call(name: string): SessionEvent {
  return {
    type: "tool/call",
    data: { callId: "same", name, arguments: "{}" },
  } as SessionEvent;
}

function result(isError = false): SessionEvent {
  return {
    type: "tool/result",
    data: { message: { toolCallId: "same", isError } },
  } as SessionEvent;
}

test("missing calls never infer MCP identity from result content", () => {
  const results = new CcrToolResults();
  expect(results.record(result(true), "root")).toBeUndefined();
});

test("failed and successful calls retain their names with thread isolation and consumed identities", () => {
  const results = new CcrToolResults();
  results.record(call("mcp__peer__fetch"), "root");
  results.record(call("Bash"), "child");
  expect(results.record(result(true), "root")).toBe("mcp__peer__fetch");
  expect(results.record(result(), "child")).toBe("Bash");
  expect(results.record(result(), "root")).toBeUndefined();
  results.record(call("mcp__other__fetch"), "root");
  results.record({ type: "turn/end", data: {} } as SessionEvent, "root");
  expect(results.record(result(), "root")).toBeUndefined();
});
