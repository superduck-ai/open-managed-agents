import { expect, test } from "bun:test";
import { normalizeSandboxResult } from "../src/oma-sandbox/output.ts";
import { toolText } from "../src/runtime/tools.ts";

function bashResult(stdout = "output\n") {
  const structuredContent = { stdout, stderr: "", exit_code: 0, status: "completed",
    duration_ms: 1, timed_out: false, cancelled: false, pid: 63,
    stdout_bytes: stdout.length, stderr_bytes: 0, stdout_truncated: false, stderr_truncated: false };
  return { structuredContent, content: [{ type: "text", text: stdout || "(no output)" },
    { type: "text", text: JSON.stringify(structuredContent) }] };
}

test("Bash preserves diagnostics for failures, truncation, stderr and inconsistent metadata", () => {
  for (const patch of [{ isError: true }, { structuredContent: { ...bashResult().structuredContent, exit_code: 1 } },
    { structuredContent: { ...bashResult().structuredContent, stderr: "warning" } },
    { structuredContent: { ...bashResult().structuredContent, stdout_truncated: true } },
    { structuredContent: { ...bashResult().structuredContent, stdout_error: "read failed" } },
    { structuredContent: { stdout: "output\n" } }]) {
    const result = { ...bashResult(), ...patch };
    expect(toolText(normalizeSandboxResult(result, "Bash"))).toBe(result.content.map((c) => c.text).join("\n"));
  }
  const mismatch = bashResult();
  mismatch.content[1]!.text = "{}";
  expect(toolText(normalizeSandboxResult(mismatch, "Bash"))).toBe("output\n\n{}");
  expect(toolText(normalizeSandboxResult(bashResult(), "Read"))).toContain('"exit_code":0');
});

test("successful Bash returns exact stdout for matching metadata and single text results", () => {
  for (const stdout of ["output\n", "", "exact stdout"]) {
    const result = bashResult(stdout);
    expect(toolText(normalizeSandboxResult(result, "Bash"))).toBe(stdout);
    result.content.pop();
    expect(toolText(normalizeSandboxResult(result, "Bash"))).toBe(stdout);
  }
});

test("generic tool text preserves metadata until the sandbox adapter projects it", () => {
  const result = bashResult();
  expect(toolText(result)).toBe(result.content.map(block => block.text).join("\n"));
  const projected = normalizeSandboxResult(result, "Bash");
  expect(toolText(projected)).toBe(result.structuredContent.stdout);
  expect(result.content).toHaveLength(2);
  expect(projected.structuredContent).toEqual(result.structuredContent);
});

test("sandbox projection leaves malformed content for the execution result validator", () => {
  const result = { ...bashResult(), content: [null] };
  expect(normalizeSandboxResult(result, "Bash")).toBe(result);
  expect(() => toolText(result)).toThrow();
});
