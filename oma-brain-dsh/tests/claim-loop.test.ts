import { expect, spyOn, test } from "bun:test";
import { HttpError } from "../src/api.ts";
import {
  CapacityExhaustedError, PermanentAdmissionError, claimTasks, type ClaimClient, type ClaimRequest, type ClaimedTask, type ReleaseReason,
} from "../src/claim.ts";
import { ConfigurationError } from "../src/config.ts";
import { eventually } from "./backend.ts";

class FakeClaims implements ClaimClient {
  readonly queue: unknown[] = [];
  readonly calls: number[] = [];
  readonly requests: ClaimRequest[] = [];
  readonly acks: string[] = [];
  readonly rejectedAcks = new Set<string>();
  readonly releases: { task: ClaimedTask; reason: ReleaseReason }[] = [];
  failures: Error[] = [];

  async claim(request: ClaimRequest, blockMs: number, signal: AbortSignal): Promise<unknown[]> {
    this.calls.push(blockMs);
    this.requests.push(request);
    const failure = this.failures.shift();
    if (failure) throw failure;
    const deadline = Date.now() + Math.min(blockMs, 50);
    while (this.queue.length === 0 && Date.now() < deadline && !signal.aborted) await Bun.sleep(5);
    return this.queue.splice(0, request.max_tasks);
  }

  async ack(task: ClaimedTask): Promise<void> {
    if (this.rejectedAcks.has(task.session_id)) throw new HttpError(409);
    this.acks.push(task.session_id);
  }

  async release(task: ClaimedTask, reason: ReleaseReason): Promise<void> {
    this.releases.push({ task, reason });
  }
}

const load = () => ({ max_sessions: 8, busy_sessions: 1, resident_sessions: 3 });
const task = (id: string) => ({ session_id: id, worker_epoch: 2, ingress_token: "ingress", model_token: "model" });

test("startup rejects an unauthorized claim token", async () => {
  const claims = new FakeClaims();
  claims.failures.push(new HttpError(401));
  const abort = new AbortController();
  await expect(claimTasks(claims, abort.signal, {
    concurrency: 1, capacity: () => 1, load, slotFreed: async () => {}, admit: async () => {},
  })).rejects.toBeInstanceOf(ConfigurationError);
});

test("startup retries transient probe failures before reporting ready", async () => {
  const claims = new FakeClaims();
  claims.failures.push(new HttpError(503));
  const abort = new AbortController();
  let ready = false;
  const running = claimTasks(claims, abort.signal, {
    concurrency: 1, blockMs: 50, capacity: () => 1, load, slotFreed: async () => {}, admit: async () => {},
    ready: () => { ready = true; },
  });
  await Bun.sleep(100);
  expect(ready).toBe(false);
  await eventually(() => ready, 3_000);
  expect(claims.calls.slice(0, 2)).toEqual([0, 0]);
  abort.abort();
  await running;
});

test("failed and invalid admissions are released without blocking later claims", async () => {
  const claims = new FakeClaims();
  claims.queue.push("not a task", task("broken"), task("invalid-context"), task("full"), task("healthy"));
  const abort = new AbortController();
  const admitted: string[] = [];
  const running = claimTasks(claims, abort.signal, {
    concurrency: 1, blockMs: 50, capacity: () => 1, load, slotFreed: async () => {},
    admit: async (value) => {
      const id = (value as ClaimedTask).session_id;
      if (typeof value !== "object") throw new Error("Invalid task envelope");
      if (id === "broken") throw new HttpError(503);
      if (id === "invalid-context") throw new PermanentAdmissionError("Invalid session_context");
      if (id === "full") throw new CapacityExhaustedError();
      admitted.push(id);
    },
  });
  await eventually(() => admitted.includes("healthy"));
  abort.abort();
  await running;
  expect(claims.releases).toEqual([
    { task: { session_id: "broken", worker_epoch: 2 }, reason: "admission_failed" },
    { task: { session_id: "invalid-context", worker_epoch: 2 }, reason: "permanent" },
    { task: { session_id: "full", worker_epoch: 2 }, reason: "shutdown" },
  ]);
  expect(claims.acks).toEqual(["broken", "invalid-context", "full", "healthy"]);
});

test("claims stop while local capacity is exhausted", async () => {
  const claims = new FakeClaims();
  const abort = new AbortController();
  let free = 0;
  let wake = () => {};
  const running = claimTasks(claims, abort.signal, {
    concurrency: 3, blockMs: 50, capacity: () => free, load,
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
  expect(claims.requests.slice(1).every((request) => request.max_tasks === 1)).toBe(true);
  abort.abort();
  wake();
  await running;
});

test("shutdown during admission releases the claim for another worker", async () => {
  const claims = new FakeClaims();
  claims.queue.push(task("admitting"));
  const abort = new AbortController();
  const running = claimTasks(claims, abort.signal, {
    concurrency: 1, blockMs: 50, capacity: () => 1, load, slotFreed: async () => {},
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
    concurrency: 1, blockMs: 50, capacity: () => 1, load, slotFreed: async () => {},
    admit: async (value) => { admitted.push((value as ClaimedTask).session_id); },
  });
  await eventually(() => claims.calls.length >= 1);
  claims.failures.push(new HttpError(503));
  claims.queue.push(task("after-retry"));
  await eventually(() => admitted.includes("after-retry"), 5_000);
  abort.abort();
  await running;
});

test("claims rejected while running are reported once and keep retrying", async () => {
  const claims = new FakeClaims();
  const errors = spyOn(console, "error").mockImplementation(() => {});
  const abort = new AbortController();
  const admitted: string[] = [];
  try {
    const running = claimTasks(claims, abort.signal, {
      concurrency: 1, blockMs: 50, capacity: () => 1, load, slotFreed: async () => {},
      admit: async (value) => { admitted.push((value as ClaimedTask).session_id); },
    });
    await eventually(() => claims.calls.length >= 1);
    claims.failures.push(new HttpError(401), new HttpError(401));
    await eventually(() => claims.failures.length === 0, 6_000);
    claims.queue.push(task("after-rotation"));
    await eventually(() => admitted.includes("after-rotation"), 6_000);
    abort.abort();
    await running;
    const rejections = errors.mock.calls.filter(([message]) => String(message).includes("Brain claim rejected with HTTP 401"));
    expect(rejections).toHaveLength(1);
  } finally {
    errors.mockRestore();
  }
});

test("one long poll claims a batch bounded by free slots and reports worker load", async () => {
  const claims = new FakeClaims();
  claims.queue.push(task("a"), task("b"), task("c"), task("d"), task("e"));
  const abort = new AbortController();
  const gates = new Map<string, () => void>();
  const admitted: string[] = [];
  const running = claimTasks(claims, abort.signal, {
    concurrency: 3, blockMs: 50, capacity: () => 8, load, slotFreed: () => Bun.sleep(5),
    admit: (value) => new Promise((resolve) => {
      const id = (value as ClaimedTask).session_id;
      gates.set(id, () => { admitted.push(id); resolve(); });
    }),
  });
  await eventually(() => gates.size === 3);
  expect(claims.requests).toHaveLength(1);
  expect(claims.calls).toEqual([0]);
  expect(claims.requests[0]).toEqual({ ...load(), max_tasks: 3, admission_failures: 0 });
  const open = (id: string) => {
    gates.get(id)!();
    gates.delete(id);
  };
  open("a");
  await eventually(() => gates.has("d"));
  expect(claims.requests[1]!.max_tasks).toBe(1);
  for (const id of ["b", "c", "d"]) open(id);
  await eventually(() => gates.has("e"));
  open("e");
  await eventually(() => admitted.length === 5);
  abort.abort();
  await running;
  expect(claims.acks).toEqual(["a", "b", "c", "d", "e"]);
});

test("an unacknowledged claim is never admitted or released", async () => {
  const claims = new FakeClaims();
  claims.rejectedAcks.add("taken");
  claims.queue.push(task("taken"), task("kept"));
  const abort = new AbortController();
  const admitted: string[] = [];
  const running = claimTasks(claims, abort.signal, {
    concurrency: 1, blockMs: 50, capacity: () => 1, load, slotFreed: async () => {},
    admit: async (value) => { admitted.push((value as ClaimedTask).session_id); },
  });
  await eventually(() => admitted.includes("kept"));
  abort.abort();
  await running;
  expect(admitted).toEqual(["kept"]);
  expect(claims.releases).toEqual([]);
});

test("consecutive admission failures are reported until an admission succeeds", async () => {
  const claims = new FakeClaims();
  claims.queue.push(task("fail-1"), task("fail-2"), task("permanent"), task("ok"));
  const abort = new AbortController();
  const admitted: string[] = [];
  const running = claimTasks(claims, abort.signal, {
    concurrency: 1, blockMs: 50, capacity: () => 1, load, slotFreed: async () => {},
    admit: async (value) => {
      const id = (value as ClaimedTask).session_id;
      if (id.startsWith("fail")) throw new HttpError(503);
      if (id === "permanent") throw new PermanentAdmissionError("Invalid session_context");
      admitted.push(id);
    },
  });
  await eventually(() => admitted.includes("ok"));
  await eventually(() => claims.requests.length >= 5);
  abort.abort();
  await running;
  expect(claims.requests.slice(0, 5).map((request) => request.admission_failures)).toEqual([0, 1, 2, 2, 0]);
});
