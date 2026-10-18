import { expect, test } from "bun:test";
import { HttpError } from "../src/api.ts";
import { CapacityExhaustedError, claimTasks, type ClaimClient, type ClaimedTask, type ReleaseReason } from "../src/claim.ts";
import { ConfigurationError } from "../src/config.ts";
import { eventually } from "./backend.ts";

class FakeClaims implements ClaimClient {
  readonly queue: unknown[] = [];
  readonly calls: number[] = [];
  readonly releases: { task: ClaimedTask; reason: ReleaseReason }[] = [];
  failures: Error[] = [];

  async claim(blockMs: number, signal: AbortSignal): Promise<unknown | undefined> {
    this.calls.push(blockMs);
    const failure = this.failures.shift();
    if (failure) throw failure;
    const deadline = Date.now() + Math.min(blockMs, 50);
    while (this.queue.length === 0 && Date.now() < deadline && !signal.aborted) await Bun.sleep(5);
    return this.queue.shift();
  }

  async release(task: ClaimedTask, reason: ReleaseReason): Promise<void> {
    this.releases.push({ task, reason });
  }
}

const task = (id: string) => ({ session_id: id, worker_epoch: 2, ingress_token: "ingress", model_token: "model" });

test("startup rejects an unauthorized claim token", async () => {
  const claims = new FakeClaims();
  claims.failures.push(new HttpError(401));
  const abort = new AbortController();
  await expect(claimTasks(claims, abort.signal, {
    concurrency: 1, capacity: () => 1, slotFreed: async () => {}, admit: async () => {},
  })).rejects.toBeInstanceOf(ConfigurationError);
});

test("failed and invalid admissions are released without blocking later claims", async () => {
  const claims = new FakeClaims();
  claims.queue.push("not a task", task("broken"), task("full"), task("healthy"));
  const abort = new AbortController();
  const admitted: string[] = [];
  const running = claimTasks(claims, abort.signal, {
    concurrency: 1, blockMs: 50, capacity: () => 1, slotFreed: async () => {},
    admit: async (value) => {
      const id = (value as ClaimedTask).session_id;
      if (typeof value !== "object") throw new Error("Invalid task envelope");
      if (id === "broken") throw new HttpError(503);
      if (id === "full") throw new CapacityExhaustedError();
      admitted.push(id);
    },
  });
  await eventually(() => admitted.includes("healthy"));
  abort.abort();
  await running;
  expect(claims.releases).toEqual([
    { task: { session_id: "broken", worker_epoch: 2 }, reason: "admission_failed" },
    { task: { session_id: "full", worker_epoch: 2 }, reason: "shutdown" },
  ]);
});

test("claims stop while local capacity is exhausted", async () => {
  const claims = new FakeClaims();
  const abort = new AbortController();
  let free = 0;
  let wake = () => {};
  const running = claimTasks(claims, abort.signal, {
    concurrency: 3, blockMs: 50, capacity: () => free,
    slotFreed: (signal) => new Promise((resolve) => {
      wake = resolve;
      signal.addEventListener("abort", () => resolve(), { once: true });
    }),
    admit: async () => {},
  });
  await eventually(() => claims.calls.length === 1);
  await Bun.sleep(100);
  expect(claims.calls).toEqual([0]);
  free = 1;
  wake();
  await eventually(() => claims.calls.length >= 3);
  expect(claims.calls.slice(1).every((blockMs) => blockMs === 50)).toBe(true);
  abort.abort();
  wake();
  await running;
});

test("shutdown during admission releases the claim for another worker", async () => {
  const claims = new FakeClaims();
  claims.queue.push(task("admitting"));
  const abort = new AbortController();
  const running = claimTasks(claims, abort.signal, {
    concurrency: 1, blockMs: 50, capacity: () => 1, slotFreed: async () => {},
    admit: async () => {
      abort.abort();
      throw new Error("cancelled");
    },
  });
  await running;
  expect(claims.releases).toEqual([{ task: { session_id: "admitting", worker_epoch: 2 }, reason: "shutdown" }]);
});

test("transient claim failures back off and recover", async () => {
  const claims = new FakeClaims();
  const abort = new AbortController();
  const admitted: string[] = [];
  const running = claimTasks(claims, abort.signal, {
    concurrency: 1, blockMs: 50, capacity: () => 1, slotFreed: async () => {},
    admit: async (value) => { admitted.push((value as ClaimedTask).session_id); },
  });
  await eventually(() => claims.calls.length >= 1);
  claims.failures.push(new HttpError(503));
  claims.queue.push(task("after-retry"));
  await eventually(() => admitted.includes("after-retry"), 5_000);
  abort.abort();
  await running;
});
