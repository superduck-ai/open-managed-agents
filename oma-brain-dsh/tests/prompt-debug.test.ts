import { afterEach, expect, spyOn, test } from "bun:test";
import { logSystemPrompt } from "../src/prompt/debug.ts";

const original = process.env.OMA_DEBUG_SYSTEM_PROMPT;
afterEach(() => {
  if (original === undefined) delete process.env.OMA_DEBUG_SYSTEM_PROMPT;
  else process.env.OMA_DEBUG_SYSTEM_PROMPT = original;
});

function capture(run: () => void): Record<string, unknown>[] {
  const info = spyOn(console, "info").mockImplementation(() => {});
  try {
    run();
    return info.mock.calls.map(([line]) => JSON.parse(String(line)));
  } finally {
    info.mockRestore();
  }
}

test("system prompt is not logged unless enabled", () => {
  delete process.env.OMA_DEBUG_SYSTEM_PROMPT;
  expect(capture(() => logSystemPrompt("disabled", { system: "secret instructions" }))).toEqual([]);
  process.env.OMA_DEBUG_SYSTEM_PROMPT = "true";
  expect(capture(() => logSystemPrompt("disabled", { system: "secret instructions" }))).toEqual([]);
});

test("system prompt is logged once per session until it changes", () => {
  process.env.OMA_DEBUG_SYSTEM_PROMPT = "1";
  const request = { system: "# OMA agent\n\nRules.", tools: [{ name: "Read" }, { name: "Grep" }] };
  const records = capture(() => {
    logSystemPrompt("session-a", request);
    logSystemPrompt("session-a", request);
    logSystemPrompt("thread-b", request);
    logSystemPrompt("session-a", { ...request, tools: [{ name: "Read" }] });
    logSystemPrompt("session-a", { ...request, system: "# OMA agent\n\nChanged." });
  });
  expect(records.map((record) => [record.session_id, record.system, record.tools])).toEqual([
    ["session-a", "# OMA agent\n\nRules.", ["Read", "Grep"]],
    ["thread-b", "# OMA agent\n\nRules.", ["Read", "Grep"]],
    ["session-a", "# OMA agent\n\nRules.", ["Read"]],
    ["session-a", "# OMA agent\n\nChanged.", ["Read", "Grep"]],
  ]);
  expect(records.every((record) => record.event === "oma_system_prompt" && /^[0-9a-f]{64}$/.test(String(record.sha256)))).toBe(true);
});


test("official model payloads keep system prompt diagnostics for both protocols", () => {
  process.env.OMA_DEBUG_SYSTEM_PROMPT = "1";
  const records = capture(() => {
    logSystemPrompt("official-messages", { system: [{ type: "text", text: "instructions" }], tools: [{ name: "Read" }] });
    logSystemPrompt("official-chat", { messages: [{ role: "system", content: "instructions" }, { role: "user", content: "private input" }], tools: [{ type: "function", function: { name: "Read" } }] });
  });
  expect(records.map((record) => [record.system, record.tools])).toEqual([["instructions", ["Read"]], ["instructions", ["Read"]]]);
});
