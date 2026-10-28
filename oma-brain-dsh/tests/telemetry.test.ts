import { afterEach, expect, spyOn, test } from "bun:test";
import { InMemoryLogRecordExporter, SimpleLogRecordProcessor } from "@opentelemetry/sdk-logs";
import { AggregationTemporality, InMemoryMetricExporter, PeriodicExportingMetricReader } from "@opentelemetry/sdk-metrics";
import { workerDiagnostic } from "../src/ccrv2/diagnostics.ts";
import { PermanentAdmissionError, claimTasks, type ClaimClient, type ReleaseReason } from "../src/claim.ts";
import { brainMetrics, emitBrainLog, startTelemetry, stopTelemetry, telemetryEnabled } from "../src/telemetry.ts";
import { eventually } from "./backend.ts";

const ENV = { OTEL_EXPORTER_OTLP_ENDPOINT: "http://127.0.0.1:1/api/test", OTEL_SERVICE_NAME: "oma-brain-test" };

afterEach(async () => { await stopTelemetry(); });

test("telemetry stays disabled without an OTLP endpoint or when the SDK is disabled", () => {
  expect(telemetryEnabled({})).toBe(false);
  expect(telemetryEnabled({ ...ENV, OTEL_SDK_DISABLED: "true" })).toBe(false);
  expect(telemetryEnabled({ OTEL_EXPORTER_OTLP_LOGS_ENDPOINT: "http://127.0.0.1:1/v1/logs" })).toBe(true);
  expect(telemetryEnabled({ ...ENV, OTEL_LOGS_EXPORTER: "none", OTEL_METRICS_EXPORTER: "none" })).toBe(false);
  expect(telemetryEnabled({ OTEL_EXPORTER_OTLP_LOGS_ENDPOINT: "http://127.0.0.1:1/v1/logs", OTEL_LOGS_EXPORTER: "none" })).toBe(false);
  startTelemetry({});
  const noop = brainMetrics();
  expect(() => noop.sessionsActive.add(1)).not.toThrow();
  expect(() => emitBrainLog("info", "noop", { session_id: "cse_a" })).not.toThrow();
});

test("telemetry starts only the signals with a configured endpoint", async () => {
  const noop = brainMetrics();
  const logs = new InMemoryLogRecordExporter();
  startTelemetry({ OTEL_EXPORTER_OTLP_LOGS_ENDPOINT: "http://127.0.0.1:1/v1/logs" }, { logProcessor: new SimpleLogRecordProcessor(logs) });
  emitBrainLog("info", "logs_only", {});
  expect(logs.getFinishedLogRecords().map((record) => record.body)).toEqual(["logs_only"]);
  expect(brainMetrics()).toBe(noop);
  await stopTelemetry();

  const metrics = new InMemoryMetricExporter(AggregationTemporality.CUMULATIVE);
  const reader = new PeriodicExportingMetricReader({ exporter: metrics, exportIntervalMillis: 60_000 });
  const unused = new InMemoryLogRecordExporter();
  startTelemetry({ ...ENV, OTEL_LOGS_EXPORTER: "none" }, { logProcessor: new SimpleLogRecordProcessor(unused), metricReader: reader });
  emitBrainLog("info", "logs_disabled", {});
  expect(unused.getFinishedLogRecords()).toEqual([]);
  expect(brainMetrics()).not.toBe(noop);
  await stopTelemetry();

  startTelemetry({ ...ENV, OTEL_METRICS_EXPORTER: "none" }, { logProcessor: new SimpleLogRecordProcessor(new InMemoryLogRecordExporter()) });
  expect(brainMetrics()).toBe(noop);
});

test("diagnostics export structured log records with code session identity and dispatch trace context", () => {
  const exporter = new InMemoryLogRecordExporter();
  startTelemetry(ENV, { logProcessor: new SimpleLogRecordProcessor(exporter) });
  const original = console.error;
  console.error = () => {};
  try {
    workerDiagnostic("error", "oma_task_admission_failed", {
      session_id: "cse_a", worker_epoch: 3, terminated: false, error_stack: ["src/worker.ts:1:1"],
      trace_id: "0123456789abcdef0123456789abcdef", status: undefined, nested: { stage: "open" },
    }, "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01");
    workerDiagnostic("warn", "invalid_trace", { session_id: "cse_b" }, "not-a-traceparent");
  } finally { console.error = original; }
  const [failed, invalid] = exporter.getFinishedLogRecords();
  expect(failed?.body).toBe("oma_task_admission_failed");
  expect(failed?.severityText).toBe("ERROR");
  expect(failed?.attributes).toEqual({
    event: "oma_task_admission_failed", code_session_id: "cse_a", worker_epoch: 3, terminated: false,
    error_stack: ["src/worker.ts:1:1"], nested: JSON.stringify({ stage: "open" }),
  });
  expect(failed?.spanContext?.traceId).toBe("0123456789abcdef0123456789abcdef");
  expect(failed?.spanContext?.spanId).toBe("0123456789abcdef");
  expect(failed?.resource.attributes["service.name"]).toBe("oma-brain-test");
  expect(invalid?.severityText).toBe("WARN");
  expect(invalid?.spanContext).toBeUndefined();
});

test("Brain metrics use bounded attributes", async () => {
  const exporter = new InMemoryMetricExporter(AggregationTemporality.CUMULATIVE);
  const reader = new PeriodicExportingMetricReader({ exporter, exportIntervalMillis: 60_000 });
  startTelemetry(ENV, { metricReader: reader });
  brainMetrics().sessionsActive.add(1);
  brainMetrics().sessionsCapacity.add(8);
  brainMetrics().sessionTasksStopped.add(1, { reason: "idle" });
  await reader.forceFlush();
  const names = exporter.getMetrics().flatMap((batch) => batch.scopeMetrics.flatMap((scope) => scope.metrics))
    .map((metric) => metric.descriptor.name);
  expect(names).toEqual(expect.arrayContaining(["oma.brain.sessions.active", "oma.brain.sessions.capacity", "oma.brain.session_tasks.stopped"]));
  const active = brainMetrics();
  await stopTelemetry();
  expect(brainMetrics()).not.toBe(active);
  expect(() => brainMetrics().leaseLost.add(1, { stop_reason: "request_rejected" })).not.toThrow();
});

test("claim admissions carry the dispatch trace context and record bounded outcomes", async () => {
  const logs = new InMemoryLogRecordExporter();
  const metrics = new InMemoryMetricExporter(AggregationTemporality.CUMULATIVE);
  const reader = new PeriodicExportingMetricReader({ exporter: metrics, exportIntervalMillis: 60_000 });
  startTelemetry(ENV, { logProcessor: new SimpleLogRecordProcessor(logs), metricReader: reader });
  const traceParent = "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01";
  const queue: unknown[] = [
    { session_id: "cse_ok", worker_epoch: 2, trace_parent: traceParent },
    { session_id: "cse_bad", worker_epoch: 3, trace_parent: "malformed" },
  ];
  const releases: ReleaseReason[] = [];
  const claims: ClaimClient = {
    claim: async () => { await Bun.sleep(5); return queue.shift(); },
    release: async (_task, reason) => { releases.push(reason); },
  };
  const abort = new AbortController();
  const quiet = [spyOn(console, "info").mockImplementation(() => {}), spyOn(console, "error").mockImplementation(() => {})];
  try {
    const running = claimTasks(claims, abort.signal, {
      concurrency: 1, blockMs: 10, capacity: () => 1, slotFreed: async () => {},
      admit: async (value) => {
        if ((value as { session_id: string }).session_id === "cse_bad") throw new PermanentAdmissionError("Invalid session_context");
      },
    });
    await eventually(() => releases.length === 1, 3_000);
    abort.abort();
    await running;
  } finally { for (const spy of quiet) spy.mockRestore(); }
  expect(releases).toEqual(["permanent"]);
  const records = logs.getFinishedLogRecords();
  const admitted = records.find((record) => record.body === "oma_task_admitted");
  expect(admitted?.attributes.code_session_id).toBe("cse_ok");
  expect(admitted?.spanContext?.traceId).toBe("0123456789abcdef0123456789abcdef");
  const failed = records.find((record) => record.body === "oma_task_admission_failed");
  expect(failed?.attributes.release_reason).toBe("permanent");
  expect(failed?.spanContext).toBeUndefined();
  await reader.forceFlush();
  const admissions = metrics.getMetrics().flatMap((batch) => batch.scopeMetrics.flatMap((scope) => scope.metrics))
    .find((metric) => metric.descriptor.name === "oma.brain.task.admissions");
  expect(admissions?.dataPoints.map((point) => point.attributes)).toEqual(expect.arrayContaining([
    { outcome: "admitted" },
    { outcome: "failed", stage: "validate_assignment", release_reason: "permanent", failure_reason: "unexpected_error" },
  ]));
});
