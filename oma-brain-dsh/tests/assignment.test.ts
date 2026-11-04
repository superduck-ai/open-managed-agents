import { expect, test } from "bun:test";

import { validateAssignment } from "../src/assignment.ts";

import { assignment } from "./backend.ts";

import { canonicalTraceParent } from "../src/observability/trace-context.ts";

test("assignment rejects invalid launch tool permissions", () => {
  for (const permissions of [[], { read: "always_allow" }, null]) {
    const a = assignment();
    a.launch.tool_permissions = permissions as never;
    expect(() => validateAssignment(a)).toThrow(
      "Invalid launch tool permissions",
    );
  }
});

test("assignment rejects unknown launch tools", () => {
  const a = assignment();
  a.launch.tools = ["Read", "UnknownTool"];
  expect(() => validateAssignment(a)).toThrow("Invalid launch tools");
});

test("trace context ignores malformed and all-zero traceparent values", () => {
  for (const value of [
    undefined,
    null,
    "",
    "ff-0123456789abcdef0123456789abcdef-0123456789abcdef-01",
    "00-00000000000000000000000000000000-0123456789abcdef-01",
    "00-0123456789abcdef0123456789abcdef-0000000000000000-01",
    "00-0123456789ABCDEF0123456789abcdef-0123456789abcdef-01",
    "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01-extra",
  ])
    expect(canonicalTraceParent(value)).toBeUndefined();
  const traceParent = "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01";
  expect(canonicalTraceParent(traceParent)).toBe(traceParent);
  expect(
    canonicalTraceParent(
      "01-0123456789abcdef0123456789abcdef-0123456789abcdef-01-future",
    ),
  ).toBe(traceParent);
});
