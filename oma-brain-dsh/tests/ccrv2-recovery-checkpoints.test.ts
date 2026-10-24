import { afterEach, expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { appendPluginRecord } from "@deepseek-ai/dsh-session";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { Backend, receiptPayloads } from "./backend.ts";

const cleanups: (() => Promise<void>)[] = [];
afterEach(async () => {
  for (const cleanup of cleanups.splice(0).reverse()) await cleanup();
});

async function mount(backend: Backend) {
  await backend.ready;
  const ctx = new Context();
  const api = new OmaApi(backend.url);
  await mountOma(ctx, api);
  const session = createOmaSession(ctx, api, backend.a);
  let closed = false;
  const close = async () => {
    if (closed) return;
    closed = true;
    try { await session.close(); }
    finally { await ctx.fiber.dispose(); }
  };
  cleanups.push(close);
  await session.open();
  return { session, close };
}

for (const phase of ["approved", "dispatch"] as const) {
  test(`recovered ${phase} execution transitions persist across another restart`, async () => {
    const backend = new Backend();
    cleanups.push(async () => { backend.stop(); });
    const first = await mount(backend);
    const native = first.session.agent.session;
    const call = { callId: "interrupted-call", name: "Bash", original: { command: "write" }, effective: { command: "write" }, readOnly: false };
    const records = [
      { type: "input/accepted" as const, inputId: "interrupted-input" },
      { type: "thread/started" as const, threadId: first.session.id, inputId: "interrupted-input" },
      { type: "tool/approved" as const, threadId: first.session.id, call },
      ...(phase === "dispatch" ? [{ type: "tool/dispatched" as const, threadId: first.session.id, call }] : []),
    ];
    for (const receipt of records) appendPluginRecord(native, "plugin:oma/receipt", { version: 1, receipt });
    await first.session.save();
    await first.close();
    const recovered = await mount(backend);
    expect(recovered.session.state.executions?.[recovered.session.id]?.callPhases?.[call.callId])
      .toBe(phase === "dispatch" ? "blocked" : "interrupted");
    await recovered.close();
    const receipts = [...backend.privateLog.values()].flatMap(receiptPayloads);
    const transition = phase === "dispatch" ? "tool/unknown" : "tool/interrupted";
    expect(receipts.filter(receipt => receipt.type === transition)).toHaveLength(1);
    const reopened = await mount(backend);
    expect(reopened.session.state.executions?.[reopened.session.id]?.callPhases?.[call.callId])
      .toBe(phase === "dispatch" ? "blocked" : "interrupted");
    await reopened.close();
    expect([...backend.privateLog.values()].flatMap(receiptPayloads)).toEqual(receipts);
    expect(backend.requests).toHaveLength(0);
    expect(backend.executions).toHaveLength(0);
  });
}

test("publication leaves events arriving during its checkpoint for the next confirmed batch", async () => {
  const backend = new Backend();
  cleanups.push(async () => { backend.stop(); });
  const { session } = await mount(backend);
  const started = Promise.withResolvers<void>();
  const release = Promise.withResolvers<void>();
  const save = session.save.bind(session);
  let first = true;
  session.save = async () => {
    if (first) {
      first = false;
      started.resolve();
      await release.promise;
    }
    await save();
  };
  session.state.outbox.push({ type: "system.message", uuid: "before-checkpoint", content: [{ type: "text", text: "first" }] });
  const publishing = session.publish();
  try {
    await started.promise;
    session.state.outbox.push({ type: "system.message", uuid: "during-checkpoint", content: [{ type: "text", text: "second" }] });
    expect(backend.publicLog.has("before-checkpoint")).toBe(false);
    expect(backend.publicLog.has("during-checkpoint")).toBe(false);
    release.resolve();
    await publishing;
    expect(backend.publicLog.has("before-checkpoint")).toBe(true);
    expect(backend.publicLog.has("during-checkpoint")).toBe(false);
    expect(session.state.outbox.map(payload => payload.uuid)).toEqual(["during-checkpoint"]);
    await session.publish();
    expect(backend.publicLog.has("during-checkpoint")).toBe(true);
    expect(session.state.outbox).toEqual([]);
  } finally {
    release.resolve();
    await publishing;
    session.save = save;
  }
});
