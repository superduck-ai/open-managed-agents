import { expect, test } from "bun:test";
import { HttpError } from "../src/api.ts";
import { CcrHttpError } from "../src/ccrv2/client.ts";
import { shouldTerminateTask } from "../src/transport.ts";

test("task admission terminates invalid or permanently rejected assignments", () => {
  for (const stage of ["decode_task", "validate_assignment"]) {
    expect(shouldTerminateTask(new Error(), stage, 1)).toBe(true);
  }
  for (const status of [400, 401, 403, 404, 409, 410, 422]) {
    for (const error of [new HttpError(status), new CcrHttpError(status)]) {
      expect(shouldTerminateTask(error, "register_worker", 1)).toBe(true);
    }
  }
});

test("transient task admission retries are bounded", () => {
  for (const error of [new Error(), new HttpError(429), new HttpError(503)]) {
    expect(shouldTerminateTask(error, "register_worker", 1)).toBe(false);
    expect(shouldTerminateTask(error, "register_worker", 10)).toBe(true);
  }
});
