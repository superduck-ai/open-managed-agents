import { expect, test } from "bun:test";
import { appendedSystemContext } from "../src/ccrv2/system-context.ts";

test("system context snapshots retain order and reject corrupt or duplicate identities", () => {
  expect(appendedSystemContext({})).toBeUndefined();
  expect(appendedSystemContext({ oma_system_context: [] })).toBe("");
  expect(appendedSystemContext({ oma_system_context: [
    { id: "one", text: "first" }, { id: "two", text: "second" },
  ] })).toBe("first\n\nsecond");
  for (const value of [null, {}, [null], [{ id: "", text: "rule" }], [{ id: "one", text: 1 }],
    [{ id: "same", text: "one" }, { id: "same", text: "two" }]]) {
    expect(() => appendedSystemContext({ oma_system_context: value })).toThrow();
  }
});
