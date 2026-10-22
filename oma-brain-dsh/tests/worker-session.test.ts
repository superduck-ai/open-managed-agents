import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { openWorkerSession } from "../src/worker-session.ts";
import { Backend, eventually } from "./backend.ts";

test("production admission continues a restored blocked session on new input without replay", async () => {
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
    await eventually(() => b.processed.includes("followup-while-blocked"));
    expect(b.delivered.has("followup-while-blocked")).toBe(false);
    expect(b.publicLog.get("followup-while-blocked_unknown_outcomes")?.type).toBe("system.message");
    expect(b.publicLog.get("followup-while-blocked_result")?.is_error).toBe(false);
    expect(restored.blocked()).toBe(false);
    expect(b.requests).toHaveLength(1);
    expect(JSON.stringify(b.requests[0]!.messages)).toContain("Read (uncertain)");
    expect(b.executions).toHaveLength(0);
  } finally {
    restored.stop();
    b.disconnect();
    await consuming?.catch(() => {});
    await ctx.fiber.dispose();
    b.stop();
  }
});
