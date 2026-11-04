import { expect,test } from "bun:test";

import type { Context } from "@deepseek-ai/cordis";

import { DshSubagents } from "../src/runtime/dsh-subagents.ts";

import { SessionRuntime } from "../src/runtime/session.ts";

import type { SubagentBridge,SubagentThread } from "../src/runtime/contracts.ts";

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
