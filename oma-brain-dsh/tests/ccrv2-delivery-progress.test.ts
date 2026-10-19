import { expect, test } from "bun:test";
import { startDeliveryProgress } from "../src/ccrv2/delivery-progress.ts";
import type { CcrHeartbeatClock } from "../src/ccrv2/heartbeat.ts";

class ManualClock implements Pick<CcrHeartbeatClock, "schedule"> {
  readonly pending = new Set<{ callback: () => void; delayMs: number }>();
  schedule(callback: () => void, delayMs: number) {
    const timer = { callback, delayMs };
    this.pending.add(timer);
    return () => { this.pending.delete(timer); };
  }
  fire() {
    const timer = [...this.pending][0]!;
    expect(timer.delayMs).toBe(60_000);
    this.pending.delete(timer);
    timer.callback();
  }
}
const settle = () => new Promise<void>((resolve) => setImmediate(resolve));

test("long tasks renew delivery each minute, including approval waits, with only one pending receipt", async () => {
  const clock = new ManualClock();
  const release = Promise.withResolvers<void>();
  const owner = new AbortController();
  let sends = 0;
  const stop = startDeliveryProgress(async () => { sends++; await release.promise; }, owner.signal,
    () => { throw new Error("Unexpected failure"); }, clock);
  expect(sends).toBe(1);
  for (let i = 0; i < 25; i++) clock.fire();
  expect(sends).toBe(1);
  release.resolve(); await settle();
  clock.fire(); await settle();
  expect(sends).toBe(2);
  stop();
  expect(clock.pending.size).toBe(0);
  owner.abort();
});

test("completion and session cancellation remove processing timers and ignore cancellation failures", async () => {
  for (const abortFirst of [true, false]) {
    const clock = new ManualClock();
    const owner = new AbortController();
    const release = Promise.withResolvers<void>();
    const failures: unknown[] = [];
    if (abortFirst) owner.abort();
    let sends = 0;
    startDeliveryProgress(async () => { sends++; await release.promise; }, owner.signal, (error) => failures.push(error), clock);
    expect(sends).toBe(abortFirst ? 0 : 1);
    owner.abort();
    if (!abortFirst) release.reject(new Error("cancelled"));
    await settle();
    expect(clock.pending.size).toBe(0);
    expect(failures).toEqual([]);
  }
});

test("terminal background delivery failures reach the session's stop handler", async () => {
  const clock = new ManualClock();
  const owner = new AbortController();
  const failure = new Error("claim lost");
  const failures: unknown[] = [];
  startDeliveryProgress(async () => { throw failure; }, owner.signal, (error) => {
    failures.push(error); owner.abort();
  }, clock);
  await settle();
  expect(failures).toEqual([failure]);
  expect(clock.pending.size).toBe(0);
});
