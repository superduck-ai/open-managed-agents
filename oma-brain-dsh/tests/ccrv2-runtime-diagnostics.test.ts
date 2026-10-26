import { expect, spyOn, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { SessionId } from "@deepseek-ai/dsh-session";
import { mountKernel } from "../src/kernel.ts";
import { recordRuntimeDiagnostic, recordRuntimeEvent } from "../src/ccrv2/runtime-diagnostics.ts";

test("CCRv2 runtime diagnostics exclude model messages and input error bodies", () => {
  const records: Record<string, unknown>[] = [];
  const warn = spyOn(console, "warn").mockImplementation(line => { records.push(JSON.parse(String(line))); });
  const error = spyOn(console, "error").mockImplementation(line => { records.push(JSON.parse(String(line))); });
  try {
    recordRuntimeDiagnostic("child", { kind: "model-retry", failure: { code: "SERVER", status: 503, message: "private-model-message" }, attempt: 2, delayMs: 2000 });
    recordRuntimeDiagnostic("root", { kind: "input-failed", error: new TypeError("private-input-body") });
    expect(records[0]).toEqual({ event: "oma_model_request_retry", session_id: "child", failure_code: "SERVER", http_status: 503, attempt: 2, delay_ms: 2000 });
    expect(records[1]).toMatchObject({ event: "oma_runtime_input_failed", session_id: "root", failure_reason: "type_error" });
    expect(JSON.stringify(records)).not.toContain("private-");
  } finally { warn.mockRestore(); error.mockRestore(); }
});

test("a failed diagnostic sink cannot change runtime failure handling", () => {
  const warn = spyOn(console, "warn").mockImplementation(() => { throw new Error("sink unavailable"); });
  const error = spyOn(console, "error").mockImplementation(() => { throw new Error("sink unavailable"); });
  try {
    expect(() => recordRuntimeDiagnostic("root", { kind: "model-retry", failure: { code: "SERVER", message: "failed" }, attempt: 1, delayMs: 1000 })).not.toThrow();
    expect(() => recordRuntimeDiagnostic("root", { kind: "input-failed", error: new Error("failed") })).not.toThrow();
  } finally { warn.mockRestore(); error.mockRestore(); }
});

test("CCRv2 debug timing observes native events without logging event payloads", async () => {
  const ctx = new Context();
  const timing = process.env.OMA_DEBUG_TIMING;
  const records: Record<string, unknown>[] = [];
  const info = spyOn(console, "info").mockImplementation(line => { records.push(JSON.parse(String(line))); });
  try {
    await mountKernel(ctx);
    const session = ctx.sessions.create(SessionId("child"));
    const event = session.append("turn/end", { turn: 1, reason: { kind: "completed" } });
    delete process.env.OMA_DEBUG_TIMING;
    recordRuntimeEvent(session, event);
    expect(records).toHaveLength(0);
    process.env.OMA_DEBUG_TIMING = "1";
    recordRuntimeEvent(session, event);
    expect(records).toEqual([{
      event: "oma_runtime_event", session_id: "child", event_type: "turn/end",
      timestamp: expect.any(String), reason: "completed",
    }]);
  } finally {
    if (timing === undefined) delete process.env.OMA_DEBUG_TIMING;
    else process.env.OMA_DEBUG_TIMING = timing;
    info.mockRestore();
    await ctx.fiber.dispose();
  }
});
