import { expect, spyOn, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { ConfigurationError, readBrainClaimOptions, readCcrHeartbeatOptions, readShutdownDrainMs } from "../src/config.ts";
import { mountOma } from "../src/plugin.ts";
import { OmaApi } from "../src/api.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { logOmaHeartbeatHealth, stopOmaSessionOnLeaseLoss } from "../src/oma-ccrv2.ts";
import { assignment } from "./backend.ts";

test("deployment heartbeat configuration rejects invalid values and timer overflow", () => {
  for (const [name, value] of [
    ["OMA_CCR_HEARTBEAT_INTERVAL_MS", "0"],
    ["OMA_CCR_HEARTBEAT_INTERVAL_MS", "2147483648"],
    ["OMA_CCR_HEARTBEAT_TIMEOUT_MS", "-1"],
    ["OMA_CCR_HEARTBEAT_TIMEOUT_MS", "Infinity"],
    ["OMA_CCR_HEARTBEAT_TIMEOUT_MS", "private-invalid-config"],
    ["OMA_CCR_HEARTBEAT_JITTER_FRACTION", "0.51"],
    ["OMA_CCR_HEARTBEAT_JITTER_FRACTION", " "],
    ["OMA_CCR_HEARTBEAT_MAX_FAILURES", "-1"],
    ["OMA_CCR_HEARTBEAT_MAX_FAILURES", "1.5"],
    ["OMA_CCR_HEARTBEAT_MAX_FAILURES", "9007199254740992"],
    ["OMA_CCR_HEARTBEAT_MAX_AUTH_FAILURES", "0"],
    ["OMA_CCR_HEARTBEAT_MAX_AUTH_FAILURES", "1.5"],
    ["OMA_CCR_HEARTBEAT_MAX_AUTH_FAILURES", "Infinity"],
  ]) {
    expect(() => readCcrHeartbeatOptions({ [name!]: value })).toThrow(ConfigurationError);
  }
  expect(() => readCcrHeartbeatOptions({
    OMA_CCR_HEARTBEAT_INTERVAL_MS: "2000000000", OMA_CCR_HEARTBEAT_JITTER_FRACTION: "0.1",
  })).toThrow("32-bit timer");
});

test("invalid heartbeat deployment configuration fails startup before connecting to the backend", async () => {
  const child = Bun.spawn([process.execPath, "src/main.ts"], {
    env: {
      ...process.env,
      OMA_API_URL: "http://127.0.0.1:1",
      OMA_BRAIN_CLAIM_TOKEN: "test-claim-token", OMA_CCR_HEARTBEAT_INTERVAL_MS: "private-invalid-config",
    },
    stdout: "pipe", stderr: "pipe",
  });
  const [code, stdout, stderr] = await Promise.all([
    child.exited, new Response(child.stdout).text(), new Response(child.stderr).text(),
  ]);
  expect(code).toBe(1);
  expect(stdout).toBe("");
  expect(stderr).toContain("OMA_CCR_HEARTBEAT_INTERVAL_MS must be a finite number");
  expect(stderr).not.toContain("private-");
});

test("heartbeat configuration must renew the worker lease before it expires", () => {
  expect(() => readCcrHeartbeatOptions({ OMA_CCR_HEARTBEAT_INTERVAL_MS: "20000" })).toThrow("below the 20000 ms worker lease");
  expect(() => readCcrHeartbeatOptions({
    OMA_CCR_HEARTBEAT_INTERVAL_MS: "14000", OMA_CCR_HEARTBEAT_TIMEOUT_MS: "5000",
  })).toThrow(ConfigurationError);
  expect(readCcrHeartbeatOptions({ OMA_CCR_HEARTBEAT_INTERVAL_MS: "10000", OMA_CCR_HEARTBEAT_TIMEOUT_MS: "4000" }).intervalMs).toBe(10000);
});

test("brain claim configuration requires a token and bounds concurrency", () => {
  expect(() => readBrainClaimOptions({})).toThrow("OMA_BRAIN_CLAIM_TOKEN is required");
  expect(() => readBrainClaimOptions({ OMA_BRAIN_CLAIM_TOKEN: "  " })).toThrow(ConfigurationError);
  for (const value of ["0", "33", "1.5", " ", "many"])
    expect(() => readBrainClaimOptions({ OMA_BRAIN_CLAIM_TOKEN: "token", OMA_BRAIN_CLAIM_CONCURRENCY: value })).toThrow(ConfigurationError);
  expect(readBrainClaimOptions({ OMA_BRAIN_CLAIM_TOKEN: " token " })).toEqual({ token: "token", concurrency: 4 });
  expect(readBrainClaimOptions({ OMA_BRAIN_CLAIM_TOKEN: "token", OMA_BRAIN_CLAIM_CONCURRENCY: "8" })).toEqual({ token: "token", concurrency: 8 });
});

test("shutdown drain configuration rejects invalid values and defaults to twenty seconds", () => {
  for (const value of ["-1", "1.5", " ", "soon", "600001"])
    expect(() => readShutdownDrainMs({ OMA_BRAIN_SHUTDOWN_DRAIN_MS: value })).toThrow(ConfigurationError);
  expect(readShutdownDrainMs({})).toBe(20_000);
  expect(readShutdownDrainMs({ OMA_BRAIN_SHUTDOWN_DRAIN_MS: "0" })).toBe(0);
  expect(readShutdownDrainMs({ OMA_BRAIN_SHUTDOWN_DRAIN_MS: " 45000 " })).toBe(45_000);
});

test("deployment heartbeat configuration matches the reference cadence and accepts explicit overrides", () => {
  expect(readCcrHeartbeatOptions({})).toEqual({
    intervalMs: 6000, timeoutMs: 5000, jitterFraction: 0.1, maxFailures: 0, maxAuthFailures: 10,
  });
  expect(readCcrHeartbeatOptions({
    OMA_CCR_HEARTBEAT_INTERVAL_MS: " 10000 ", OMA_CCR_HEARTBEAT_TIMEOUT_MS: "4000",
    OMA_CCR_HEARTBEAT_JITTER_FRACTION: "0.1", OMA_CCR_HEARTBEAT_MAX_FAILURES: "4",
    OMA_CCR_HEARTBEAT_MAX_AUTH_FAILURES: "8",
  })).toEqual({ intervalMs: 10000, timeoutMs: 4000, jitterFraction: 0.1, maxFailures: 4, maxAuthFailures: 8 });
});

test("OMA applies deployment heartbeat limits and logs lease loss without exposing credentials", async () => {
  for (const status of [503, 409, 410, 401]) {
    const ctx = new Context();
    const a = assignment();
    a.ingress_token = "private-test-ingress-token";
    a.worker_epoch = 7;
    let calls = 0;
    let api: OmaApi;
    const server = Bun.serve({
      hostname: "127.0.0.1", port: 0,
      fetch: async (request) => {
        expect(new URL(request.url).pathname).toEndWith("/worker/heartbeat");
        expect(request.headers.get("Authorization")).toBe(`Bearer ${a.ingress_token}`);
        const body: unknown = await request.json();
        expect(body).toEqual({ session_id: a.session_id, worker_epoch: 7 });
        calls++;
        // The log must identify the request's epoch, even if credentials rotate in flight.
        if (status === 409) api.assignments.set(a.session_id, { ...a, worker_epoch: 8 });
        return new Response("private-response-body", { status });
      },
    });
    const reported = Promise.withResolvers<string>();
    const warn = spyOn(console, "warn").mockImplementation((message: unknown) => {
      if (String(message).includes('"event":"ccrv2_lease_lost"')) reported.resolve(String(message));
    });
    try {
      api = new OmaApi(server.url.toString());
      await mountOma(ctx, api, readCcrHeartbeatOptions({
        OMA_CCR_HEARTBEAT_INTERVAL_MS: "5", OMA_CCR_HEARTBEAT_TIMEOUT_MS: "500",
        OMA_CCR_HEARTBEAT_MAX_FAILURES: "2", OMA_CCR_HEARTBEAT_JITTER_FRACTION: "0",
      }));
      const session = createOmaSession(ctx, api, a);
      const line = await reported.promise;
      const log: unknown = JSON.parse(line);
      expect(log).toEqual({
        event: "ccrv2_lease_lost", session_id: a.session_id, worker_epoch: 7,
        http_status: status, consecutive_failures: status === 503 ? 2 : status === 401 ? 10 : 1,
        failure_reason: status === 503 ? "server_error" : status === 409 ? "epoch_mismatch"
          : status === 410 ? "lease_expired" : "authentication_rejected",
        stop_reason: status === 503 ? "consecutive_failures" : status === 401 ? "auth_failures_exhausted" : "request_rejected",
      });
      expect(session.abort.signal.aborted).toBe(true);
      expect(calls).toBe(status === 503 ? 2 : status === 401 ? 10 : 1);
      expect(warn.mock.calls.filter(([line]) => String(line).includes('"event":"ccrv2_lease_lost"'))).toHaveLength(1);
      expect(line).not.toContain("private-");
    } finally {
      await ctx.fiber.dispose();
      warn.mockRestore();
      await server.stop(true);
    }
  }
});

test("a failing diagnostic sink cannot prevent the host from stopping the session", () => {
  let stopped = false;
  const warn = spyOn(console, "warn").mockImplementation(() => { throw new Error("Log sink unavailable"); });
  try {
    expect(() => stopOmaSessionOnLeaseLoss({ id: "test", stop: () => { stopped = true; } }, {
      status: "rejected", httpStatus: 409, reason: "epoch_mismatch", workerEpoch: 7, consecutiveFailures: 1,
    })).not.toThrow();
    expect(stopped).toBe(true);
  } finally {
    warn.mockRestore();
  }
});

test("the OMA host logs heartbeat failure and recovery using only safe protocol metadata", async () => {
  const ctx = new Context();
  const a = assignment();
  let calls = 0;
  const server = Bun.serve({
    hostname: "127.0.0.1", port: 0,
    fetch: () => new Response("private-body", { status: ++calls === 1 ? 503 : 200 }),
  });
  const recovered = Promise.withResolvers<void>();
  const warnings: string[] = [];
  const infos: string[] = [];
  const warn = spyOn(console, "warn").mockImplementation((message: unknown) => { warnings.push(String(message)); });
  const info = spyOn(console, "info").mockImplementation((message: unknown) => {
    infos.push(String(message)); recovered.resolve();
  });
  try {
    const api = new OmaApi(server.url.toString());
    await mountOma(ctx, api, { intervalMs: 5, jitterFraction: 0 });
    const session = createOmaSession(ctx, api, a);
    await recovered.promise;
    session.stop();
    expect(warnings).toHaveLength(1);
    expect(infos).toHaveLength(1);
    expect(JSON.parse(warnings[0]!)).toEqual({
      event: "ccrv2_heartbeat_failed", session_id: a.session_id, worker_epoch: 1,
      http_status: 503, failure_reason: "server_error", consecutive_failures: 1, duration_ms: 0,
    });
    expect(JSON.parse(infos[0]!)).toMatchObject({
      event: "ccrv2_heartbeat_recovered", session_id: a.session_id, worker_epoch: 1, consecutive_failures: 1,
    });
    expect(JSON.parse(infos[0]!).duration_ms).toBeGreaterThanOrEqual(0);
    logOmaHeartbeatHealth("safe-session", {
      type: "failure", reason: "transport_error", consecutiveFailures: 1, durationMs: 0,
      ...{ token: "private-token", error: "private-error", response: "private-response" },
    });
    expect([...warnings, ...infos].join("\n")).not.toContain("private-");
  } finally {
    await ctx.fiber.dispose();
    warn.mockRestore(); info.mockRestore(); await server.stop(true);
  }
});
