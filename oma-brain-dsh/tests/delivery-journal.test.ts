import { afterEach, expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { pluginRecordOf, type Session } from "@deepseek-ai/dsh-session";
import { mountKernel } from "../src/kernel.ts";
import { DeliveryJournal, type DeliveryDelta } from "../src/ccrv2/delivery-journal.ts";
import { emptyDelivery } from "../src/ccrv2/protocol.ts";

const contexts: Context[] = [];
afterEach(async () => { for (const ctx of contexts.splice(0)) await ctx.fiber.dispose(); });

async function native(): Promise<Session> {
  const ctx = new Context();
  contexts.push(ctx);
  await mountKernel(ctx);
  return ctx.sessions.create();
}

function delta(fields: Partial<DeliveryDelta> = {}): DeliveryDelta {
  return { version: 2, revision: 1, inputId: "", logOffsets: {}, completedAdded: [], queued: [], sent: [], ...fields };
}

for (const invalid of [
  delta({ revision: 2 }), delta({ revision: 0 }), delta({ sent: [""] }),
  { ...delta(), completedAdded: null }, { ...delta(), queued: [{ type: "result" }] },
  { ...delta(), logOffsets: { child: -1 } }, { version: 3 },
]) {
  test(`invalid delivery delta is rejected: ${JSON.stringify(invalid)}`, () => {
    expect(() => new DeliveryJournal("main").restore(invalid)).toThrow();
  });
}

test("append rejection retains completed additions and pending publication for retry", async () => {
  const session = await native();
  const journal = new DeliveryJournal("main");
  const delivery = emptyDelivery();
  delivery.completed.push("done");
  delivery.outbox.push({ type: "result", uuid: "result" });
  delivery.logOffsets = { main: -1 };
  expect(() => journal.append(session, delivery)).toThrow();
  expect(session.snapshotEvents()).toHaveLength(0);
  expect(delivery.revision).toBe(0);
  delivery.logOffsets = {};
  expect(journal.append(session, delivery)).toBe(true);
  const record = pluginRecordOf(session.snapshotEvents()[0]!)!.data as DeliveryDelta;
  expect(record.completedAdded).toEqual(["done"]);
  expect(record.queued).toEqual([{ type: "result", uuid: "result" }]);
});

test("completed IDs are appended once and publication updates contain only changes", async () => {
  const session = await native();
  const journal = new DeliveryJournal("main");
  const delivery = emptyDelivery();
  for (let index = 0; index < 100; index++) {
    delivery.inputId = `input-${index}`;
    delivery.completed.push(delivery.inputId);
    journal.append(session, delivery);
  }
  const before = session.snapshotEvents().length;
  expect(journal.append(session, delivery)).toBe(false);
  expect(session.snapshotEvents()).toHaveLength(before);
  const payload = { type: "result", uuid: "result", content: "answer" };
  delivery.outbox.push(payload);
  expect(journal.append(session, delivery)).toBe(true);
  expect(journal.append(session, delivery)).toBe(false);
  delivery.outbox = [];
  expect(journal.append(session, delivery)).toBe(true);
  const records = session.snapshotEvents().map(event => pluginRecordOf(event)!.data as DeliveryDelta);
  expect(records.flatMap(record => record.completedAdded)).toEqual(delivery.completed);
  expect(records.flatMap(record => record.queued)).toEqual([payload]);
  expect(records.flatMap(record => record.sent)).toEqual([payload.uuid]);
  expect(records.every(record => !("completed" in record) && !("outbox" in record))).toBe(true);
  const restored = new DeliveryJournal("main");
  for (const record of records) restored.restore(record);
  expect(restored.state).toEqual(delivery);
});

test("legacy snapshots and deltas restore completed deduplication, pending outbox and historical children", async () => {
  const journal = new DeliveryJournal("main");
  journal.restore({ version: 1, delivery: { ...emptyDelivery(), revision: 5, completed: ["old", "old"],
    outbox: [{ type: "session.thread_created", uuid: "created", session_thread_id: "child" }] } });
  journal.restore({ version: 1, delivery: { ...emptyDelivery(), revision: 6, completed: ["old"], outbox: [] } });
  journal.restore(delta({ revision: 7, inputId: "new", completedAdded: ["old", "new", "new"],
    queued: [{ type: "result", uuid: "pending" }] }));
  expect(journal.state.completed).toEqual(["old", "new"]);
  expect(journal.state.outbox).toEqual([{ type: "result", uuid: "pending" }]);
  expect([...journal.restoredThreadIds]).toEqual(["child"]);
  const session = await native();
  expect(journal.append(session, journal.state)).toBe(false);
  journal.state.outbox = [];
  expect(journal.append(session, journal.state)).toBe(true);
  expect(pluginRecordOf(session.snapshotEvents()[0]!)!.data).toMatchObject({ version: 2, revision: 8,
    completedAdded: [], queued: [], sent: ["pending"] });
});
