import { seedInterruptedTool } from "./session-log.ts";
import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { openWorkerSession } from "../src/worker-session.ts";
import { Backend, eventually, textEvents } from "./backend.ts";
import { createMessage, ToolCallId } from "@deepseek-ai/dsh-llm";

test("production admission uses native interrupted-turn recovery before accepting fresh input", async () => {
  const b = new Backend();
  await b.ready;
  const first = new Context();
  const firstApi = new OmaApi(b.url);
  await mountOma(first, firstApi);
  const original = createOmaSession(first, firstApi, b.a);
  await original.open();
  await seedInterruptedTool(original, "original-uncertain");
  await original.close();
  await first.fiber.dispose();
  const ctx = new Context();
  const api = new OmaApi(b.url);
  const client = await mountOma(ctx, api);
  const restored = createOmaSession(ctx, api, b.a);
  const stages: string[] = [];
  let consuming: Promise<void> | undefined;
  b.deliver("followup-while-blocked", { type: "user", content: "read another server" });
  try {
    await openWorkerSession(restored, client, (task) => {
      consuming = task;
      task.catch(() => {});
    }, async (session) => { await session.close(); }, (stage) => { stages.push(stage); });
    expect(stages).toContain("start_inbound");
    expect(consuming).toBeDefined();
    await eventually(() => b.processed.includes("followup-while-blocked"));
    expect(b.delivered.has("followup-while-blocked")).toBe(false);
    expect(b.publicLog.get("followup-while-blocked_result")?.is_error).toBe(false);
    expect(restored.abort.signal.aborted).toBe(false);
    expect(b.requests).toHaveLength(1);
    expect(JSON.stringify(b.requests[0]!.messages)).toContain("not submitted before");
    expect(b.executions).toHaveLength(0);
  } finally {
    restored.stop();
    b.disconnect();
    await consuming?.catch(() => {});
    await ctx.fiber.dispose();
    b.stop();
  }
});

async function recoveringWorker(callId: string, extraCallIds: string[] = [], onCancel?: (calls: number) => Promise<void>) {
  const b = new Backend();
  await b.ready;
  const first = new Context();
  const firstApi = new OmaApi(b.url);
  await mountOma(first, firstApi);
  const original = createOmaSession(first, firstApi, b.a);
  await original.open();
  await seedInterruptedTool(original, `${callId}-input`, callId);
  for (const extra of extraCallIds) {
    original.agent.session.append("assistant/message", {
      turn: 1, step: 1, stream: [],
      message: createMessage({ role: "assistant", source: { kind: "model", provider: "oma", model: "test-model" },
        content: [{ type: "tool-call", id: ToolCallId(extra), name: "Bash", arguments: '{"command":"write"}' }] }),
    }, { surfaceOp: "append" });
    original.agent.session.append("tool/call", { turn: 1, step: 1, callId: ToolCallId(extra), name: "Bash", arguments: '{"command":"write"}' });
  }
  await original.runtime.flushLog();
  await original.close();
  await first.fiber.dispose();
  const call: { state: string; result?: Record<string, unknown> } = { state: "running" };
  const calls = new Map([[callId, call]]);
  for (const extra of extraCallIds) calls.set(extra, { state: "running" });
  for (const [id, entry] of calls) b.toolCallResults.set(`root/${id}`, entry);
  let cancels = 0;
  b.onCancel = async ({ requestId }: Record<string, unknown>) => {
    cancels++;
    calls.get(String(requestId))!.result = { isError: true, content: [{ type: "text", text: `partial output before cancel ${requestId}` }] };
    await onCancel?.(cancels);
  };
  const ctx = new Context();
  const api = new OmaApi(b.url);
  const client = await mountOma(ctx, api);
  const restored = createOmaSession(ctx, api, b.a);
  let consuming: Promise<void> | undefined;
  const opening = openWorkerSession(restored, client, (task) => {
    consuming = task;
    task.catch(() => {});
  }, async (session) => { await session.close(); }, () => {});
  opening.catch(() => {});
  await eventually(() => b.connections > 0);
  const toolResults = () => restored.agent.session.snapshotEvents().filter(event => event.type === "tool/result");
  return {
    b, call, restored, opening, toolResults, cancels: () => cancels,
    async cleanup() {
      restored.stop();
      b.disconnect();
      await opening.catch(() => {});
      await consuming?.catch(() => {});
      await ctx.fiber.dispose();
      b.stop();
    },
  };
}

test("shutdown during recovery leaves the unfinished call to the next owner", async () => {
  const f = await recoveringWorker("shutdown-call");
  try {
    f.restored.stop();
    await expect(f.opening).rejects.toThrow();
    expect(f.cancels()).toBe(0);
  } finally { await f.cleanup(); }
});

test("input during recovery waits until the unfinished call settles", async () => {
  const f = await recoveringWorker("settling-call");
  try {
    f.b.deliver("followup-during-recovery", { type: "user", content: "next question" });
    await Bun.sleep(200);
    expect(f.b.processed).not.toContain("followup-during-recovery");
    f.call.result = { content: [{ type: "text", text: "finished output" }] };
    f.call.state = "succeeded";
    await f.opening;
    expect(JSON.stringify(f.toolResults())).toContain("finished output");
    await eventually(() => f.b.processed.includes("followup-during-recovery"));
    expect(f.cancels()).toBe(0);
  } finally { await f.cleanup(); }
});

test("user interrupt during recovery cancels the unfinished call and keeps its partial output", async () => {
  const f = await recoveringWorker("running-call");
  try {
    f.b.deliver("interrupt-during-recovery", { type: "control_request", request_id: "interrupt-request", request: { subtype: "interrupt" } });
    await f.opening;
    expect(f.cancels()).toBe(1);
    const results = f.toolResults();
    expect(results).toHaveLength(1);
    expect(JSON.stringify(results[0])).toContain("partial output before cancel");
    expect(f.b.publicLog.get("running-call-input_result")?.is_error).toBe(false);
    await eventually(() => f.b.processed.includes("interrupt-during-recovery"));
    expect(f.restored.abort.signal.aborted).toBe(false);
    expect(f.b.executions).toHaveLength(0);
  } finally { await f.cleanup(); }
});

test("an input queued before a recovery interrupt still runs to completion", async () => {
  const f = await recoveringWorker("queued-call");
  try {
    f.b.modelScript = [textEvents("followup answer")];
    f.b.deliver("followup-before-interrupt", { type: "user", content: "next question" });
    await Bun.sleep(100);
    f.b.deliver("interrupt-after-followup", { type: "control_request", request_id: "interrupt-after", request: { subtype: "interrupt" } });
    await f.opening;
    await eventually(() => f.b.processed.includes("followup-before-interrupt"));
    expect(f.cancels()).toBe(1);
    expect(JSON.stringify([...f.b.publicLog.values()])).toContain("followup answer");
  } finally { await f.cleanup(); }
});

test("recovery interrupt cancels every unfinished call concurrently", async () => {
  const bothCancelling = Promise.withResolvers<void>();
  const f = await recoveringWorker("parallel-a", ["parallel-b"], async (cancels) => {
    if (cancels === 2) bothCancelling.resolve();
    await Promise.race([bothCancelling.promise, Bun.sleep(5000)]);
  });
  try {
    const started = Date.now();
    f.b.deliver("interrupt-parallel", { type: "control_request", request_id: "interrupt-parallel", request: { subtype: "interrupt" } });
    await f.opening;
    expect(Date.now() - started).toBeLessThan(4000);
    expect(f.cancels()).toBe(2);
    const results = JSON.stringify(f.toolResults());
    expect(results).toContain("partial output before cancel parallel-a");
    expect(results).toContain("partial output before cancel parallel-b");
  } finally { await f.cleanup(); }
});
