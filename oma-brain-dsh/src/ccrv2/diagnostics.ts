import { emitBrainLog } from "../telemetry.ts";
/** Protocol fields only: callers must not include tokens, payloads or error messages. */
export function workerDiagnostic(
  level: "info" | "warn" | "error", event: string, fields: Record<string, unknown>, traceParent?: string,
): void {
  try { console[level](JSON.stringify({ event, ...fields })); }
  catch { /* A log sink cannot alter protocol completion or retries. */ }
  emitBrainLog(level, event, fields, traceParent);
}
import { CcrRecoveryError } from "./recovery.ts";
import { CcrHttpError } from "./client.ts";
import { HttpError } from "../api.ts";

/** Keep code locations, never exception text, function arguments or URL queries. */
function errorLocations(error: Error): string[] {
  return (error.stack ?? "").split("\n").slice(1).flatMap((line) => {
    const match = line.match(/\/(src\/[\w./@-]+|dist\/[\w./@-]+|node_modules\/[\w./@-]+):(\d+):(\d+)\)?$/);
    return match ? [`${match[1]}:${match[2]}:${match[3]}`] : [];
  }).slice(0, 6);
}

/** Classify known failures without logging arbitrary exception text or request data. */
export function failureDiagnostic(error: unknown): Record<string, unknown> {
  if (error instanceof CcrRecoveryError)
    return { failure_reason: error.reason, persistence_field: error.field };
  if (error instanceof CcrHttpError || error instanceof HttpError)
    return { failure_reason: "http_error", http_status: error.status };
  const known = new Map([
    ["Unsupported Brain state", "unsupported_brain_state"],
    ["Grok sessions require explicit migration; automatic conversion is disabled", "migration_required"],
    ["Session requires a SandboxExecutor provider", "sandbox_provider_missing"],
    ["Invalid private event page", "invalid_private_event_page"],
    ["Invalid private event cursor", "invalid_private_event_cursor"],
    ["Private event cursor did not advance", "private_event_cursor_stalled"],
    ["Unsupported dsh log envelope", "unsupported_log_envelope"],
    ["Log header changed", "log_header_changed"],
    ["Invalid dsh log batch", "invalid_log_batch"],
    ["Worker capacity exhausted", "worker_capacity_exhausted"],
    ["Dispatched worker epoch changed", "worker_epoch_changed"],
  ]);
  const locations = error instanceof Error ? errorLocations(error) : [];
  return { failure_reason: error instanceof Error
    ? known.get(error.message) ?? (error instanceof SyntaxError ? "invalid_json"
      : error instanceof TypeError ? "type_error"
      : error.name === "AbortError" ? "cancelled"
      : error.name === "TimeoutError" ? "timeout" : "unexpected_error")
    : "unexpected_error",
    ...(locations.length ? { error_stack: locations } : {}),
  };
}
