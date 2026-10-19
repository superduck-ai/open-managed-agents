import { expect, test } from "bun:test";
import { SessionCapacity, type CapacityMember } from "../src/capacity.ts";
import { CapacityExhaustedError } from "../src/claim.ts";

function harness(maxSessions: number, members: CapacityMember[]) {
  const sessions = new Map(members.map((member) => [member.id, member]));
  const retiring = new Map<string, true>();
  const evicted: string[] = [];
  const capacity = new SessionCapacity(maxSessions, sessions, retiring, (session) => {
    evicted.push(session.id);
    retiring.set(session.id, true);
  });
  const finishRetire = () => {
    for (const id of retiring.keys()) sessions.delete(id);
    retiring.clear();
  };
  return { sessions, retiring, evicted, capacity, finishRetire };
}

test("rejects admission when every session is busy", () => {
  const { capacity, evicted } = harness(1, [{ id: "busy", busy: true, lastIdle: 0 }]);
  expect(() => capacity.reserve()).toThrow(CapacityExhaustedError);
  expect(evicted).toEqual([]);
  expect(() => capacity.reserve()).toThrow(CapacityExhaustedError);
});

test("concurrent admissions evict distinct idle sessions and never exceed capacity", () => {
  const { sessions, evicted, capacity, finishRetire } = harness(2, [
    { id: "A", busy: false, lastIdle: 1 },
    { id: "B", busy: false, lastIdle: 2 },
  ]);
  const releaseC = capacity.reserve();
  const releaseD = capacity.reserve();
  expect(evicted).toEqual(["A", "B"]);
  finishRetire();
  sessions.set("C", { id: "C", busy: false, lastIdle: 3 });
  releaseC();
  sessions.set("D", { id: "D", busy: false, lastIdle: 4 });
  releaseD();
  expect([...sessions.keys()]).toEqual(["C", "D"]);
  expect(() => capacity.reserve()).not.toThrow();
  expect(evicted).toEqual(["A", "B", "C"]);
});

test("a failed admission returns its reservation once", () => {
  const { evicted, capacity } = harness(1, [{ id: "A", busy: false, lastIdle: 1 }]);
  const release = capacity.reserve();
  release();
  release();
  expect(evicted).toEqual(["A"]);
  const again = capacity.reserve();
  expect(evicted).toEqual(["A"]);
  again();
});

test("claim capacity counts only busy sessions", () => {
  const { capacity } = harness(3, [
    { id: "A", busy: true, lastIdle: 1 },
    { id: "B", busy: false, lastIdle: 2 },
  ]);
  expect(capacity.available()).toBe(2);
});
