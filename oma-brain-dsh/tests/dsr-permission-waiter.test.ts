import { expect, jest, test } from "bun:test";
import { DsrPermissionWaiter } from "../src/dsr/permission-waiter.ts";
import type { Permission } from "../src/dsr/wire.ts";

async function tick() {
  for (let i = 0; i < 16; i++) await Promise.resolve();
}

const pending: Permission = { request_id: "pr_one", state: "pending" };

test("aborting approval cancels the wait", async () => {
  const waiter = new DsrPermissionWaiter();
  const abort = new AbortController();
  const waiting = waiter.wait(async () => pending, abort.signal, 60000);
  await tick();
  abort.abort(new Error("interrupted"));
  await expect(waiting).rejects.toThrow("interrupted");
});

test("pending approval suspends when its budget expires", async () => {
  const waiter = new DsrPermissionWaiter();
  expect(
    await waiter.wait(async () => pending, new AbortController().signal, 10),
  ).toBeUndefined();
});

for (const state of ["cancelled", "closed"] as const) {
  test(`${state} approval returns without waiting`, async () => {
    const waiter = new DsrPermissionWaiter();
    expect(
      await waiter.wait(
        async () => ({ request_id: "pr_one", state }),
        new AbortController().signal,
        60000,
      ),
    ).toEqual({ request_id: "pr_one", state });
  });
}

test("notification received during creation retains the initial HTTP validation", async () => {
  const waiter = new DsrPermissionWaiter();
  const response = Promise.withResolvers<Permission>();
  let done = false;
  const waiting = waiter
    .wait(() => response.promise, new AbortController().signal, 60000)
    .then((value) => {
      done = true;
      return value;
    });
  waiter.resolve("pr_one", { behavior: "allow" });
  await tick();
  expect(done).toBe(false);
  response.resolve(pending);
  expect(await waiting).toEqual({
    request_id: "pr_one",
    state: "resolved",
    decision: { behavior: "allow" },
  });
});

test("notification wakes approval without another POST", async () => {
  const waiter = new DsrPermissionWaiter();
  let calls = 0;
  const waiting = waiter.wait(
    async () => {
      calls++;
      return pending;
    },
    new AbortController().signal,
    60000,
  );
  await tick();
  waiter.resolve("pr_other", { behavior: "allow" });
  waiter.resolve("pr_one", { behavior: "deny", message: "no" });
  expect(await waiting).toEqual({
    request_id: "pr_one",
    state: "resolved",
    decision: { behavior: "deny", message: "no" },
  });
  expect(calls).toBe(1);
});

test("reconnection immediately requeries pending approval", async () => {
  const waiter = new DsrPermissionWaiter();
  let calls = 0;
  const waiting = waiter.wait(
    async () =>
      ++calls === 1
        ? pending
        : {
            request_id: "pr_one",
            state: "resolved",
            decision: { behavior: "allow" },
          },
    new AbortController().signal,
    60000,
  );
  await tick();
  waiter.reconnect();
  expect((await waiting)?.state).toBe("resolved");
  expect(calls).toBe(2);
});

test("missing notification falls back after ten seconds", async () => {
  jest.useFakeTimers();
  try {
    const waiter = new DsrPermissionWaiter();
    let calls = 0;
    const waiting = waiter.wait(
      async () =>
        ++calls === 1 ? pending : { request_id: "pr_one", state: "closed" },
      new AbortController().signal,
      60000,
    );
    await tick();
    jest.advanceTimersByTime(9999);
    await tick();
    expect(calls).toBe(1);
    jest.advanceTimersByTime(1);
    expect((await waiting)?.state).toBe("closed");
    expect(calls).toBe(2);
  } finally {
    jest.useRealTimers();
  }
});

test("notification interrupts an in-flight fallback query", async () => {
  jest.useFakeTimers();
  try {
    const waiter = new DsrPermissionWaiter();
    let calls = 0;
    let fallbackSignal: AbortSignal | undefined;
    const waiting = waiter.wait(
      async (signal) => {
        if (++calls === 1) return pending;
        fallbackSignal = signal;
        return new Promise<Permission>((_, reject) =>
          signal.addEventListener("abort", () => reject(signal.reason), {
            once: true,
          }),
        );
      },
      new AbortController().signal,
      60000,
    );
    await tick();
    jest.advanceTimersByTime(10000);
    await tick();
    expect(calls).toBe(2);
    waiter.resolve("pr_one", { behavior: "allow" });
    expect((await waiting)?.state).toBe("resolved");
    expect(fallbackSignal?.aborted).toBe(true);
  } finally {
    jest.useRealTimers();
  }
});
