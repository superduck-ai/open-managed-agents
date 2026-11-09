import { expect, test } from "bun:test";
import { DsrInputInbox, DsrReceipts } from "../src/dsr/input-inbox.ts";
import type { Delivery, Input } from "../src/dsr/wire.ts";

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

test("control replay retains the original unsupported outcome", async () => {
  const updates: Delivery[] = [];
  let applied = 0;
  const inbox = new DsrInputInbox(
    new DsrReceipts(async (batch) => {
      updates.push(...batch);
    }),
    {
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

test("task delivery is rejected without sending a receipt", async () => {
  const updates: Delivery[] = [];
  const failures: unknown[] = [];
  const inbox = new DsrInputInbox(
    new DsrReceipts(async (batch) => {
      updates.push(...batch);
    }),
    {
      control: async () => {},
      fail: (error) => failures.push(error),
    },
  );
  expect(
    inbox.accept({
      id: "task",
      lane: "task",
      kind: "user.message",
      content: [],
    }),
  ).toBe(false);
  await inbox.drain();
  expect(updates).toEqual([]);
  expect(failures[0]).toMatchObject({ code: "unexpected_task_delivery" });
});
