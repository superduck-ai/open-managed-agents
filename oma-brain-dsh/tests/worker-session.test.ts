import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { openWorkerSession } from "../src/worker-session.ts";
import { Backend } from "./backend.ts";

test("production admission consumes and acknowledges a new input on a restored blocked session without replay", async () => {
  const b = new Backend();
  await b.ready;
  const first = new Context();
  const firstApi = new OmaApi(b.url);
  await mountOma(first, firstApi);
  const original = createOmaSession(first, firstApi, b.a);
  await original.open();
  original.state.inputId = "original-uncertain";
  const call = { callId: "uncertain", readOnly: false, name: "Read", original: { file_path: "/tmp/owned" }, effective: { file_path: "/tmp/owned" } };
  await original.recordApproved(call);
  await original.recordDispatch(call.callId);
  await original.recordUnknown(call.callId);
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
    await expect(consuming!).rejects.toThrow("reconciliation");
    expect(b.processed).toContain("followup-while-blocked");
    expect(b.delivered.has("followup-while-blocked")).toBe(false);
    expect(b.publicLog.get("followup-while-blocked_failed_result")?.is_error).toBe(true);
    expect(restored.blocked()).toBe(true);
    expect(restored.execution(restored.id).callPhases?.uncertain).toBe("blocked");
    expect(b.requests).toHaveLength(0);
    expect(b.executions).toHaveLength(0);
  } finally {
    restored.stop();
    b.disconnect();
    await consuming?.catch(() => {});
    await ctx.fiber.dispose();
    b.stop();
  }
});
