import { afterEach, expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { SessionId, pluginRecordOf } from "@deepseek-ai/dsh-session";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { Backend, textEvents, toolEvents } from "./backend.ts";
import { seedInterruptedTool } from "./session-log.ts";

const cleanups: (() => Promise<unknown>)[] = [];
afterEach(async () => { for (const cleanup of cleanups.splice(0).reverse()) await cleanup(); });

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
    try { await session.close(); } finally { await ctx.fiber.dispose(); }
  };
  cleanups.push(close);
  await session.open();
  return { ctx, session, close };
}

function backend() {
  const backend = new Backend();
  cleanups.push(async () => backend.stop());
  return backend;
}

test("native-only crash recovery never re-executes an accepted input or tool", async () => {
  const b = backend();
  const first = await mount(b);
  await seedInterruptedTool(first.session, "accepted-before-crash");
  await first.close();
  const second = await mount(b);
  const calls = b.requests.length;
  await second.session.receive("accepted-before-crash", { content: "Duplicate input" });
  expect(b.requests).toHaveLength(calls);
  expect(b.executions).toHaveLength(0);
  expect(b.publicLog.get("accepted-before-crash_recovery_result")?.is_error).toBe(true);
  expect(b.processed).toContain("accepted-before-crash");
});

test("external input identity uses the native id and every replicated record matches the native log", async () => {
  const b = backend();
  b.modelScript = [textEvents("first"), textEvents("second")];
  const { session, ctx } = await mount(b);
  await session.receive("first-input", { content: "Same content" });
  await session.receive("second-input", { content: "Same content" });
  await session.receive("first-input", { content: "Same content" });
  expect(b.requests).toHaveLength(2);
  const events = session.agent.session.snapshotEvents();
  expect(events.flatMap(event => event.type === "user/message" && event.data.source.kind === "user" ? [String(event.data.id)] : []))
    .toEqual(["first-input", "second-input"]);
  expect(events.map(pluginRecordOf).filter(Boolean).map(record => record!.type))
    .toEqual(["plugin:oma/assistant-block", "plugin:oma/assistant-block"]);
  expect(JSON.stringify(events)).not.toContain("oma-interruption");
  const handle = await ctx.sessionPersistence.open(SessionId(session.id), "read");
  try { expect((await handle.read()).events).toEqual([...events]); }
  finally { await handle.close(); }
  const remote = [...b.privateLog.values()].filter(payload => payload.type === "dsh_log").map(payload => payload.data);
  expect(remote).toEqual([session.agent.session.header, ...events]);
});

test("native history regenerates stable public events after response loss", async () => {
  const b = backend();
  b.modelScript = [textEvents("confirmed native answer")];
  const first = await mount(b);
  await first.session.receive("durable-input", { content: "Hello" });
  const original = structuredClone([...b.publicLog.values()].filter(payload => ["agent.message", "result"].includes(payload.type)));
  b.publicLog.clear();
  await first.close();
  const second = await mount(b);
  await second.session.receive("durable-input", { content: "Redelivery" });
  expect(b.requests).toHaveLength(1);
  expect([...b.publicLog.values()].filter(payload => ["agent.message", "result"].includes(payload.type))).toEqual(original);
});


test("approval context remains native and cannot replace the accepted input identity on resume", async () => {
  const b = backend();
  b.modelScript = [toolEvents(), textEvents("approved execution complete")];
  const first = await mount(b);
  b.onApproval = async payload => first.session.control({
    type: "control_response", response: { subtype: "success", request_id: payload.request_id,
      response: { behavior: "allow", updatedInput: { command: "approved" } } },
  }).then(() => {});
  await first.session.receive("original-input", { content: "Run once" });
  const calls = b.requests.length;
  expect(first.session.agent.session.snapshotEvents().filter(event => event.type === "user/message" && event.data.source.kind === "user").length).toBeGreaterThan(1);
  await first.close();
  const second = await mount(b);
  expect([...second.session.receivedInputIds]).toEqual(["original-input"]);
  await second.session.receive("original-input", { content: "Duplicate" });
  expect(b.requests).toHaveLength(calls);
  expect(b.executions).toHaveLength(1);
  expect(b.publicLog.get("original-input_result")?.is_error).toBe(false);
  expect([...b.publicLog.keys()].filter(uuid => uuid.endsWith("_result"))).toEqual(["original-input_result"]);
});
