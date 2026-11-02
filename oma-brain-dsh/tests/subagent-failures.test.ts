import { expect, test } from "bun:test";
import type { Context } from "@deepseek-ai/cordis";
import { DshSubagents } from "../src/runtime/dsh-subagents.ts";
import { SessionRuntime } from "../src/runtime/session.ts";
import type { SubagentBridge, SubagentThread } from "../src/runtime/contracts.ts";
import { CcrSession } from "../src/ccrv2/session.ts";

function harness(existing = true) {
  const handlers = new Map<string, (...args: any[]) => any>();
  const threads: SubagentThread[] = existing ? [{ id: "child", name: "child", systemPrompt: "", model: "test-model", status: "idle" }] : [];
  const events: string[] = [];
  let stops = 0;
  const ctx = { get: (name: string) => name === "agentPresets" ? { serviceFor: () => undefined } : {},
    on: (name: string, handler: (...args: any[]) => any) => { handlers.set(name, handler); return () => handlers.delete(name); } };
  const parent = new SessionRuntime(ctx as unknown as Context, { id: "root" } as any, { stop: () => { stops++; } } as any, {} as any);
  const bridge = { threads: () => threads, emit: async (event: {kind: string}) => { events.push(event.kind); }, bindPersistence: () => {} } as unknown as SubagentBridge;
  const original = new Error("child initialization failed");
  const dsh = new DshSubagents(parent, bridge, async () => { throw original; });
  const agent = { id: "child", ctx: { effect: () => {} }, session: { header: { parentSession: "root" }, snapshotEvents: () => [{ type: "subagent/descriptor", data: { version: 3, mode: "continuable", provider: "oma-spawn:root", label: "child" } }, { type: "plugin:oma/subagent-config", ignorable: true, data: { version: 1, systemPrompt: "" } }] } };
  return { parent, bridge, dsh, events, handlers, agent, original, stops: () => stops };
}

test("subagents stop and close before the root agent is initialized", async () => {
  const f = harness();
  expect(() => f.dsh.interrupt()).not.toThrow();
  await f.dsh.close();
  expect(f.handlers.size).toBe(0);
});

test("stop still aborts the runtime and rejects approvals when child interruption throws", () => {
  let stopped = false;
  let rejected = false;
  const session = { subagents: { stop: () => { throw new Error("interrupt failed"); } }, runtime: { stop: () => { stopped = true; } }, approvals: new Map([["approval", { reject: () => { rejected = true; } }]]) };
  expect(() => CcrSession.prototype.stop.call(session as unknown as CcrSession)).toThrow("interrupt failed");
  expect(stopped).toBe(true);
  expect(rejected).toBe(true);
});

test("a rejected event publication stops the parent without poisoning later events or close", async () => {
  const f = harness();
  let attempts = 0;
  f.bridge.emit = async event => { if (++attempts === 1) throw new Error("publish failed"); f.events.push(event.kind); };
  f.handlers.get("subagent/end")!({ id: "child", stopReason: "completed" });
  await expect(f.dsh.settleEvents()).resolves.toBeUndefined();
  expect(f.stops()).toBe(1);
  f.handlers.get("subagent/end")!({ id: "child", stopReason: "completed" });
  await f.dsh.settleEvents();
  expect(f.events).toEqual(["report-received", "idle"]);
  await f.dsh.close();
});

test("child initialization retains the original error when termination publication fails", async () => {
  const f = harness(false);
  f.bridge.emit = async event => { if (event.kind === "terminated") throw new Error("publish failed"); };
  await expect(f.handlers.get("agent/created")!({ agent: f.agent })).rejects.toBe(f.original);
  expect(f.bridge.threads()[0]!.status).toBe("terminated");
  await f.dsh.close();
});

test("cold child initialization failure does not publish an interrupted turn", async () => {
  const f = harness();
  await expect(f.handlers.get("agent/created")!({ agent: f.agent })).rejects.toBe(f.original);
  expect(f.events).toEqual([]);
  expect(f.bridge.threads()[0]!.status).toBe("idle");
  await f.dsh.close();
});

test("runtime marker lookup stops before inspecting a large log tail", async () => {
  let visited = 0;
  const events = [{ type: "plugin:oma/subagent-runtime", ignorable: true, data: { version: 1, runtime: "dsh" } }, ...Array.from({ length: 100_000 }, () => ({ get type() { visited++; return "turn/start"; } }))];
  const done = new Error("lookup complete");
  const session = {
    id: "root", ctx: { ccrReplication: { ensure: async () => false, acquiredEvents: () => events }, sessionPersistence: {
      stat: async () => true, open: async () => ({ read: async () => ({ events }), close: async () => {} }),
    } }, subagents: { loadThreads: async () => {} }, runtime: { open: async () => { throw done; } },
  };
  const methods = CcrSession.prototype as unknown as { openState: (stage: (value: string) => void) => Promise<void> };
  await expect(methods.openState.call(session, () => {})).rejects.toBe(done);
  expect(visited).toBe(0);
});

test("runtime preflight reuses acquired events even when the marker is last", async () => {
  let reads = 0;
  let visited = 0;
  const events = [...Array.from({ length: 100_000 }, () => ({ get type() { visited++; return "turn/start"; } })), { type: "plugin:oma/subagent-runtime", ignorable: true, data: { version: 1, runtime: "dsh" } }];
  const done = new Error("preflight complete");
  const session = {
    id: "root", ctx: { ccrReplication: { ensure: async () => false, acquiredEvents: () => events }, sessionPersistence: {
      stat: async () => true, open: async () => ({ read: async () => { reads++; return { events }; }, close: async () => {} }),
    } }, subagents: { loadThreads: async () => {} }, runtime: { open: async () => { throw done; } },
  };
  const methods = CcrSession.prototype as unknown as { openState: (stage: (value: string) => void) => Promise<void> };
  await expect(methods.openState.call(session, () => {})).rejects.toBe(done);
  expect(reads).toBe(0);
  expect(visited).toBe(100_000);
});
