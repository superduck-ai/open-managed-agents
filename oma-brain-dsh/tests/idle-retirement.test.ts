import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { IDLE_RETIREMENT_MS, SessionCapacity, idleExpired, type CapacityMember } from "../src/capacity.ts";
import { CapacityExhaustedError } from "../src/claim.ts";
import { OmaApi } from "../src/api.ts";
import { mountOma } from "../src/plugin.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { Backend } from "./backend.ts";

test("an initializing session is never retired as idle even when its idle clock is stale", () => {
  const opening: CapacityMember = { id: "opening", busy: false, lastIdle: 0, initializing: true };
  expect(idleExpired(opening, IDLE_RETIREMENT_MS * 10)).toBe(false);
});

test("capacity eviction skips an initializing session", () => {
  const sessions = new Map<string, CapacityMember>([["opening", { id: "opening", busy: false, lastIdle: 0, initializing: true }]]);
  const evicted: string[] = [];
  const capacity = new SessionCapacity(1, sessions, new Map(), (session) => evicted.push(session.id));
  expect(() => capacity.reserve()).toThrow(CapacityExhaustedError);
  expect(evicted).toEqual([]);
});

test("an initialized idle session expires after the idle window and busy sessions never do", () => {
  const idle: CapacityMember = { id: "idle", busy: false, lastIdle: 1_000, initializing: false };
  expect(idleExpired(idle, 1_000 + IDLE_RETIREMENT_MS)).toBe(false);
  expect(idleExpired(idle, 1_000 + IDLE_RETIREMENT_MS + 1)).toBe(true);
  expect(idleExpired({ ...idle, busy: true }, 1_000 + IDLE_RETIREMENT_MS * 10)).toBe(false);
});

test("a CCR session stays initializing until open completes and its idle clock starts there", async () => {
  const backend = new Backend();
  const ctx = new Context();
  try {
    await backend.ready;
    const api = new OmaApi(backend.url);
    await mountOma(ctx, api);
    const session = createOmaSession(ctx, api, structuredClone(backend.a));
    expect(session.initializing).toBe(true);
    expect(idleExpired(session, Date.now() + IDLE_RETIREMENT_MS * 10)).toBe(false);
    const opened = Date.now();
    await session.open();
    expect(session.initializing).toBe(false);
    expect(session.lastIdle).toBeGreaterThanOrEqual(opened);
    expect(idleExpired(session, session.lastIdle + IDLE_RETIREMENT_MS / 2)).toBe(false);
    expect(idleExpired(session, session.lastIdle + IDLE_RETIREMENT_MS + 1)).toBe(true);
  } finally { await ctx.fiber.dispose(); backend.stop(); }
});
