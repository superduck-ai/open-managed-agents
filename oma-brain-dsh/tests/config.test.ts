import { expect,test } from "bun:test";
import {
ConfigurationError,
readBrainClaimOptions,
readBrainProtocol,
readShutdownDrainMs,
} from "../src/config.ts";

test("Brain rejects unsupported protocols and never selects a legacy transport", () => {
  for (const value of ["ccrv2", "unknown", "", " dsr1 "]) {
    expect(() => readBrainProtocol({ OMA_BRAIN_PROTOCOL: value })).toThrow(
      ConfigurationError,
    );
  }
  expect(readBrainProtocol({})).toBe("dsr1");
  expect(readBrainProtocol({ OMA_BRAIN_PROTOCOL: "dsr1" })).toBe("dsr1");
});

test("claim configuration rejects invalid credentials and concurrency", () => {
  expect(() => readBrainClaimOptions({})).toThrow(
    "OMA_BRAIN_CLAIM_TOKEN is required",
  );
  for (const value of ["", "0", "33", "1.5", "invalid"]) {
    expect(() =>
      readBrainClaimOptions({
        OMA_BRAIN_CLAIM_TOKEN: "token",
        OMA_BRAIN_CLAIM_CONCURRENCY: value,
      }),
    ).toThrow(ConfigurationError);
  }
  expect(readBrainClaimOptions({ OMA_BRAIN_CLAIM_TOKEN: " token " })).toEqual({
    token: "token",
    concurrency: 4,
  });
});

test("shutdown drain remains bounded", () => {
  for (const value of ["", "-1", "600001", "NaN", "1.5"])
    expect(() =>
      readShutdownDrainMs({ OMA_BRAIN_SHUTDOWN_DRAIN_MS: value }),
    ).toThrow(ConfigurationError);
  expect(readShutdownDrainMs({})).toBe(20000);
  expect(readShutdownDrainMs({ OMA_BRAIN_SHUTDOWN_DRAIN_MS: "0" })).toBe(0);
});
