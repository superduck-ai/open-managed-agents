import { expect, test } from "bun:test";
import { DsrInputInbox, DsrReceipts } from "../src/dsr/input-inbox.ts";
import type { Delivery, Input } from "../src/dsr/wire.ts";

const task = (id: string): Input => ({
  id,
  lane: "task",
  kind: "user.message",
  content: [],
});
const interrupt = (id: string): Input => ({
  id,
  lane: "control",
  kind: "user.interrupt",
});
async function tick() {
  for (let i = 0; i < 10; i++) await Promise.resolve();
}

test("receipt failure rejects coalesced waiters and later uploads", async () => {
  const error = new Error("fenced");
  const receipts = new DsrReceipts(async () => {
    throw error;
  });
  const first = receipts.enqueue({ id: "a", status: "received" });
  expect(receipts.enqueue({ id: "a", status: "received" })).toBe(first);
  await expect(first).rejects.toBe(error);
  await expect(receipts.enqueue({ id: "b", status: "received" })).rejects.toBe(
    error,
  );
});

test("interrupt bypasses blocked received and earlier processed; duplicates never reapply", async () => {
  const gate = Promise.withResolvers<void>();
  const applied: string[] = [];
  const errors: unknown[] = [];
  const receipts = new DsrReceipts(async () => gate.promise);
  const inbox = new DsrInputInbox(receipts, {
    task: async () => true,
    control: async (input) => {
      applied.push(input.id);
      await receipts.enqueue({ id: input.id, status: "processed" });
    },
    fail: (error) => errors.push(error),
  });
  inbox.accept(interrupt("first"));
  await tick();
  inbox.accept(interrupt("second"));
  inbox.accept(interrupt("first"));
  expect(applied).toEqual(["first", "second"]);
  gate.resolve();
  await inbox.drain();
  inbox.accept(interrupt("first"));
  await tick();
  expect(applied).toEqual(["first", "second"]);
  expect(errors).toEqual([]);
});

test("capacity rejects excess tasks without received and preserves control access", async () => {
  const gate = Promise.withResolvers<void>();
  const updates: Delivery[] = [];
  const applied: string[] = [];
  const inbox = new DsrInputInbox(
    new DsrReceipts(async (batch) => {
      updates.push(...batch);
    }),
    {
      task: async () => {
        await gate.promise;
        return true;
      },
      control: async (input) => {
        applied.push(input.id);
      },
      fail: (error) => {
        throw error;
      },
    },
  );
  for (let i = 0; i < 64; i++) expect(inbox.accept(task(String(i)))).toBe(true);
  expect(inbox.accept(task("excess"))).toBe(false);
  inbox.accept(interrupt("stop"));
  await tick();
  expect(applied).toEqual(["stop"]);
  expect(updates.some((update) => update.id === "excess")).toBe(false);
  gate.resolve();
  await inbox.drain();
  expect(inbox.accept(task("excess"))).toBe(true);
  await inbox.drain();
});

test("byte capacity rejects large input before admitting or acknowledging it", () => {
  const inbox = new DsrInputInbox(new DsrReceipts(async () => {}), {
    task: async () => true,
    control: async () => {},
    fail: () => {},
  });
  expect(
    inbox.accept({
      id: "large",
      lane: "task",
      kind: "user.message",
      content: ["x".repeat(16 * 1024 * 1024)],
    }),
  ).toBe(false);
  expect(inbox.unsettled.size).toBe(0);
});

test("queued and running duplicates share one execution without early processed", async () => {
  const gate = Promise.withResolvers<void>();
  const batches: Delivery[][] = [];
  const started: string[] = [];
  const inbox = new DsrInputInbox(
    new DsrReceipts(async (batch) => {
      batches.push(batch);
    }),
    {
      task: async (input) => {
        started.push(input.id);
        await gate.promise;
        return true;
      },
      control: async () => {},
      fail: (error) => {
        throw error;
      },
    },
  );
  for (let i = 0; i < 100; i++) inbox.accept(task("a"));
  inbox.accept(task("b"));
  await tick();
  expect(started).toEqual(["a"]);
  for (let i = 0; i < 100; i++) inbox.accept(task("a"));
  gate.resolve();
  await inbox.drain();
  expect(started).toEqual(["a", "b"]);
  expect(batches[0]).toEqual([
    { id: "a", status: "received" },
    { id: "b", status: "received" },
  ]);
  expect(inbox.unsettled.size).toBe(0);
  expect(batches.flat().some((update) => update.status === "processed")).toBe(
    false,
  );
});

test("receipt uploads are serial, ordered and batched in groups of 64", async () => {
  const gate = Promise.withResolvers<void>();
  const batches: Delivery[][] = [];
  const receipts = new DsrReceipts(async (batch) => {
    batches.push(batch);
    await gate.promise;
  });
  const pending = Array.from({ length: 100 }, (_, i) =>
    receipts.enqueue({ id: String(i), status: "received" }),
  );
  pending.push(receipts.enqueue({ id: "0", status: "processed" }));
  await tick();
  expect(batches.map((batch) => batch.length)).toEqual([64]);
  gate.resolve();
  await Promise.all(pending);
  expect(batches.map((batch) => batch.length)).toEqual([64, 37]);
  expect(
    batches
      .flat()
      .filter((update) => update.id === "0")
      .map((update) => update.status),
  ).toEqual(["received", "processed"]);
});

test("approval still waits for received while interrupt bypasses it", async () => {
  const gate = Promise.withResolvers<void>();
  const applied: string[] = [];
  const inbox = new DsrInputInbox(new DsrReceipts(async () => gate.promise), {
    task: async () => true,
    control: async (input) => {
      applied.push(input.id);
    },
    fail: () => {},
  });
  inbox.accept({
    id: "approval",
    lane: "control",
    kind: "permission.resolved",
    request_id: "request",
    resolution: { behavior: "allow" },
  });
  inbox.accept(interrupt("stop"));
  expect(applied).toEqual(["stop"]);
  gate.resolve();
  await inbox.drain();
  expect(applied).toEqual(["stop", "approval"]);
});

test("a failed received confirmation never starts a task", async () => {
  const error = new Error("owner lost");
  let started = false;
  const errors: unknown[] = [];
  const inbox = new DsrInputInbox(
    new DsrReceipts(async () => {
      throw error;
    }),
    {
      task: async () => {
        started = true;
        return true;
      },
      control: async () => {},
      fail: (error) => errors.push(error),
    },
  );
  inbox.accept(task("a"));
  await inbox.drain();
  expect(started).toBe(false);
  expect(errors).toContain(error);
});

test("control replay retains the original unsupported outcome", async () => {
  const updates: Delivery[] = [];
  let applied = 0;
  const inbox = new DsrInputInbox(
    new DsrReceipts(async (batch) => {
      updates.push(...batch);
    }),
    {
      task: async () => true,
      control: async (input, report) => {
        applied++;
        await report({
          id: input.id,
          status: "processed",
          error: "unsupported_input",
        });
      },
      fail: (error) => {
        throw error;
      },
    },
  );
  inbox.accept(interrupt("a"));
  await inbox.drain();
  inbox.accept(interrupt("a"));
  await tick();
  expect(applied).toBe(1);
  expect(updates.filter((update) => update.status === "processed")).toEqual([
    { id: "a", status: "processed", error: "unsupported_input" },
    { id: "a", status: "processed", error: "unsupported_input" },
  ]);
});

test("control history retires the owner at capacity instead of forgetting applied interrupts", async () => {
  let applied = 0;
  const errors: unknown[] = [];
  const inbox = new DsrInputInbox(new DsrReceipts(async () => {}), {
    task: async () => true,
    control: async () => {
      applied++;
    },
    fail: (error) => errors.push(error),
  });
  for (let i = 0; i < 4096; i++) inbox.accept(interrupt(String(i)));
  await inbox.drain();
  expect(inbox.accept(interrupt("overflow"))).toBe(false);
  expect(applied).toBe(4096);
  expect(errors).toHaveLength(1);
  expect(errors[0]).toMatchObject({ code: "input_control_capacity" });
  inbox.accept(interrupt("0"));
  expect(applied).toBe(4096);
  await tick();
});

test("interrupt receipts respect the backend's unique-ID batch contract", async () => {
  const batches: Delivery[][] = [];
  const failures: unknown[] = [];
  const inbox = new DsrInputInbox(
    new DsrReceipts(async (batch) => {
      batches.push(batch);
      if (new Set(batch.map((item) => item.id)).size !== batch.length)
        throw new Error("HTTP 400 invalid_request: repeated input ID");
    }),
    {
      task: async () => true,
      control: async (input, report) => {
        await report({ id: input.id, status: "processed" });
      },
      fail: (error) => failures.push(error),
    },
  );
  inbox.accept(interrupt("a"));
  await inbox.drain();
  expect(failures).toEqual([]);
  expect(batches).toEqual([
    [{ id: "a", status: "received" }],
    [{ id: "a", status: "processed" }],
  ]);
});

test("interleaved receipt transitions retain confirmation boundaries for each ID", async () => {
  const first = Promise.withResolvers<void>();
  const batches: Delivery[][] = [];
  const receipts = new DsrReceipts(async (batch) => {
    batches.push(batch);
    if (batches.length === 1) await first.promise;
  });
  const received = receipts.enqueue({ id: "a", status: "received" });
  let processedConfirmed = false;
  const processed = receipts
    .enqueue({ id: "a", status: "processed" })
    .then(() => {
      processedConfirmed = true;
    });
  const other = receipts.enqueue({ id: "b", status: "received" });
  await tick();
  const initial = structuredClone(batches[0]);
  const premature = processedConfirmed;
  first.resolve();
  await Promise.all([received, processed, other]);
  expect(initial).toEqual([
    { id: "a", status: "received" },
    { id: "b", status: "received" },
  ]);
  expect(premature).toBe(false);
  expect(batches[1]).toEqual([{ id: "a", status: "processed" }]);
  expect(processedConfirmed).toBe(true);
});
