import { spawn } from "node:child_process";
import { once } from "node:events";
import { expect, test } from "bun:test";
import { eventually } from "./backend.ts";

async function checkShutdown(
  mode: "clean" | "fail" | "hang",
  signal: "SIGINT" | "SIGTERM",
  expectedCode: number,
  expectedError = "",
  force = false,
) {
  const child = spawn(process.execPath, ["tests/shutdown-worker.ts"], {
    env: { ...process.env, TEST_SHUTDOWN_MODE: mode, OMA_BRAIN_SHUTDOWN_DRAIN_MS: "0" },
    stdio: ["ignore", "pipe", "pipe"],
  });
  const exited = once(child, "exit");
  let stdout = "";
  let stderr = "";
  child.stdout.on("data", (data) => { stdout += data; });
  child.stderr.on("data", (data) => { stderr += data; });
  try {
    await eventually(() => stdout.includes("ready"));
    child.kill(signal);
    if (force) {
      await eventually(() => stdout.includes("disposing"));
      child.kill("SIGINT");
    }
    const [code] = await exited;
    expect(code).toBe(expectedCode);
    expect(stdout.match(/disposing/g)).toHaveLength(1);
    expect(stderr.trim()).toBe(expectedError);
  } finally {
    if (child.exitCode === null && child.signalCode === null) child.kill("SIGKILL");
    await exited;
  }
}

test("SIGINT finishes graceful shutdown", () => checkShutdown("clean", "SIGINT", 0));
test("shutdown failure exits with code 1", () =>
  checkShutdown("fail", "SIGINT", 1, "Brain shutdown failed"));
test("hung shutdown forces exit after the drain, settle and cleanup budget", () =>
  checkShutdown("hang", "SIGINT", 1, "Brain shutdown timed out after 8 seconds; forcing exit"),
  15_000);
test("a second SIGINT forces exit during shutdown", () =>
  checkShutdown("hang", "SIGINT", 130, "", true));
test("SIGINT forces exit after SIGTERM starts shutdown", () =>
  checkShutdown("hang", "SIGTERM", 130, "", true));
