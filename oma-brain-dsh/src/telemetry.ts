import { hostname } from "node:os";
import { context, createNoopMeter, trace, TraceFlags, type Attributes, type Counter, type Meter, type UpDownCounter } from "@opentelemetry/api";
import { SeverityNumber, type Logger } from "@opentelemetry/api-logs";
import { OTLPLogExporter } from "@opentelemetry/exporter-logs-otlp-http";
import { OTLPMetricExporter } from "@opentelemetry/exporter-metrics-otlp-http";
import { Resource } from "@opentelemetry/resources";
import { BatchLogRecordProcessor, LoggerProvider, type LogRecordProcessor } from "@opentelemetry/sdk-logs";
import { MeterProvider, PeriodicExportingMetricReader, type MetricReader } from "@opentelemetry/sdk-metrics";
import { parseTraceParent } from "./observability/trace-context.ts";

export type LogLevel = "info" | "warn" | "error";

const SEVERITY: Record<LogLevel, SeverityNumber> = {
  info: SeverityNumber.INFO, warn: SeverityNumber.WARN, error: SeverityNumber.ERROR,
};
const ATTRIBUTE_NAMES: Record<string, string> = { session_id: "code_session_id" };
const SCOPE = "oma-brain-dsh";

interface BrainInstruments {
  sessionsActive: UpDownCounter;
  sessionsCapacity: UpDownCounter;
  taskAdmissions: Counter;
  claimFailures: Counter;
  sessionTasksStopped: Counter;
  workerRequestFailures: Counter;
  leaseLost: Counter;
}

let logger: Logger | undefined;
const NOOP_INSTRUMENTS = createInstruments(createNoopMeter());
let instruments = NOOP_INSTRUMENTS;
let shutdowns: (() => Promise<void>)[] = [];

function signalEnabled(env: Record<string, string | undefined>, signal: "LOGS" | "METRICS"): boolean {
  if (env.OTEL_SDK_DISABLED === "true" || env[`OTEL_${signal}_EXPORTER`] === "none") return false;
  return Boolean(env.OTEL_EXPORTER_OTLP_ENDPOINT || env[`OTEL_EXPORTER_OTLP_${signal}_ENDPOINT`]);
}

export function telemetryEnabled(env: Record<string, string | undefined> = process.env): boolean {
  return signalEnabled(env, "LOGS") || signalEnabled(env, "METRICS");
}

export interface TelemetryExporters {
  logProcessor?: LogRecordProcessor;
  metricReader?: MetricReader;
}

export function startTelemetry(env: Record<string, string | undefined> = process.env, exporters: TelemetryExporters = {}): void {
  if (shutdowns.length || !telemetryEnabled(env)) return;
  const resource = new Resource({
    "service.name": env.OTEL_SERVICE_NAME || "oma-brain",
    "service.instance.id": `${hostname()}:${process.pid}`,
  });
  if (signalEnabled(env, "LOGS")) {
    const loggerProvider = new LoggerProvider({ resource });
    loggerProvider.addLogRecordProcessor(exporters.logProcessor ?? new BatchLogRecordProcessor(new OTLPLogExporter()));
    logger = loggerProvider.getLogger(SCOPE);
    shutdowns.push(() => loggerProvider.shutdown());
  }
  if (signalEnabled(env, "METRICS")) {
    const interval = Number(env.OTEL_METRIC_EXPORT_INTERVAL ?? "10000");
    const meterProvider = new MeterProvider({
      resource,
      readers: [exporters.metricReader ?? new PeriodicExportingMetricReader({
        exporter: new OTLPMetricExporter(),
        exportIntervalMillis: Number.isSafeInteger(interval) && interval > 0 ? interval : 10_000,
      })],
    });
    instruments = createInstruments(meterProvider.getMeter(SCOPE));
    shutdowns.push(() => meterProvider.shutdown());
  }
}

export async function stopTelemetry(): Promise<void> {
  const pending = shutdowns;
  shutdowns = [];
  logger = undefined;
  instruments = NOOP_INSTRUMENTS;
  await Promise.allSettled(pending.map((shutdown) => shutdown()));
}

function createInstruments(meter: Meter): BrainInstruments {
  return {
    sessionsActive: meter.createUpDownCounter("oma.brain.sessions.active", { unit: "{session}", description: "Sessions owned by this Brain process" }),
    sessionsCapacity: meter.createUpDownCounter("oma.brain.sessions.capacity", { unit: "{session}", description: "Maximum sessions this Brain process accepts" }),
    taskAdmissions: meter.createCounter("oma.brain.task.admissions", { unit: "{task}", description: "Brain task admissions by outcome" }),
    claimFailures: meter.createCounter("oma.brain.claim.failures", { unit: "{request}", description: "Failed Brain claim and release requests" }),
    sessionTasksStopped: meter.createCounter("oma.brain.session_tasks.stopped", { unit: "{session}", description: "Brain session tasks stopped by reason" }),
    workerRequestFailures: meter.createCounter("oma.brain.worker_requests.failed", { unit: "{request}", description: "Failed CCRv2 worker requests" }),
    leaseLost: meter.createCounter("oma.brain.lease.lost", { unit: "{session}", description: "CCRv2 leases lost by stop reason" }),
  };
}

export function brainMetrics(): BrainInstruments {
  return instruments;
}

function attributes(fields: Record<string, unknown>): Attributes {
  const result: Attributes = {};
  for (const [key, value] of Object.entries(fields)) {
    if (value === undefined || value === null || key === "trace_id") continue;
    const name = ATTRIBUTE_NAMES[key] ?? key;
    if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") result[name] = value;
    else if (Array.isArray(value) && value.every((item) => typeof item === "string")) result[name] = value as string[];
    else result[name] = JSON.stringify(value);
  }
  return result;
}

function traceContext(traceParent: string | undefined) {
  const parsed = parseTraceParent({ traceparent: traceParent });
  if (!parsed) return undefined;
  return trace.setSpanContext(context.active(), {
    traceId: parsed.traceId, spanId: parsed.spanId, traceFlags: parsed.sampled ? TraceFlags.SAMPLED : TraceFlags.NONE, isRemote: true,
  });
}

export function emitBrainLog(level: LogLevel, event: string, fields: Record<string, unknown>, traceParent?: string): void {
  if (!logger) return;
  try {
    logger.emit({
      severityNumber: SEVERITY[level], severityText: level.toUpperCase(), body: event,
      attributes: { event, ...attributes(fields) }, context: traceContext(traceParent),
    });
  } catch {
    return;
  }
}
