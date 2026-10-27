import { seedInterruptedTool } from "./session-log.ts";
import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { openWorkerSession } from "../src/worker-session.ts";
import { Backend, eventually } from "./backend.ts";

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
    expect(JSON.stringify(b.requests[0]!.messages)).toContain("Do not retry blindly");
    expect(b.executions).toHaveLength(0);
  } finally {
    restored.stop();
    b.disconnect();
    await consuming?.catch(() => {});
    await ctx.fiber.dispose();
    b.stop();
  }
});
