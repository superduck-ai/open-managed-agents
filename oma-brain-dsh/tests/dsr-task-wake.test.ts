import { expect, jest, test } from "bun:test";
import { DsrTaskWake } from "../src/dsr/task-wake.ts";
import { DsrOwner } from "../src/dsr/worker.ts";

async function tick() {
  for (let i = 0; i < 12; i++) await Promise.resolve();
}

test("task wait aborts and cancels its fallback timer", async () => {
  jest.useFakeTimers();
  try {
    const abort = new AbortController();
    const wake = new DsrTaskWake();
    const waiting = wake.wait(
      wake.version,
      { request: async () => new Response() },
      abort.signal,
    );
    abort.abort(new Error("closed"));
    await expect(waiting).rejects.toThrow("closed");
    jest.advanceTimersByTime(1000);
  } finally {
    jest.useRealTimers();
  }
});

test("a task hint during claim is retained before wait registration", async () => {
  const wake = new DsrTaskWake();
  const captured = wake.version;
  wake.notify();
  let sleeps = 0;
  await wake.wait(
    captured,
    {
      request: async () => new Response(),
      sleep: async () => {
        sleeps++;
      },
    },
    new AbortController().signal,
  );
  expect(sleeps).toBe(0);
});

test("task hint wakes an active wait without advancing the 1000ms timer", async () => {
  jest.useFakeTimers();
  try {
    const wake = new DsrTaskWake();
    let done = false;
    const waiting = wake
      .wait(
        wake.version,
        { request: async () => new Response() },
        new AbortController().signal,
      )
      .then(() => {
        done = true;
      });
    await tick();
    expect(done).toBe(false);
    wake.notify();
    await waiting;
    expect(done).toBe(true);
    jest.advanceTimersByTime(1000);
  } finally {
    jest.useRealTimers();
  }
});

test("missing or coalesced hints retain the one-second polling fallback", async () => {
  jest.useFakeTimers();
  try {
    const wake = new DsrTaskWake();
    let done = false;
    const waiting = wake
      .wait(
        wake.version,
        { request: async () => new Response() },
        new AbortController().signal,
      )
      .then(() => {
        done = true;
      });
    jest.advanceTimersByTime(999);
    await tick();
    expect(done).toBe(false);
    jest.advanceTimersByTime(1);
    await waiting;
    expect(done).toBe(true);
  } finally {
    jest.useRealTimers();
  }
});

for (const timing of ["during-claim", "during-wait"] as const) {
  test(`owner immediately reclaims after a hint ${timing}`, async () => {
    const reply = Promise.withResolvers<undefined>();
    let calls = 0;
    let received = false;
    const owner = new DsrOwner(
      {} as never,
      {} as never,
      {
        claimInput: async () => {
          calls++;
          return calls === 1
            ? reply.promise
            : { id: "task", lane: "task", kind: "user.message", content: [] };
        },
        transport: { request: async () => new Response() },
      } as never,
      {} as never,
      10,
    );
    const internal = owner as unknown as {
      taskWake: DsrTaskWake;
      runTasks(): Promise<void>;
      stream: AbortController;
    };
    owner.session = {
      receive: async () => {
        received = true;
        internal.stream.abort();
      },
      suspended: false,
    } as never;
    const running = internal.runTasks();
    if (timing === "during-claim") internal.taskWake.notify();
    reply.resolve(undefined);
    await tick();
    if (timing === "during-wait") {
      expect(calls).toBe(1);
      internal.taskWake.notify();
    }
    await running;
    expect(received).toBe(true);
    expect(calls).toBe(2);
  });
}
