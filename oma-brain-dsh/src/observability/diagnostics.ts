import { DsrError } from "../dsr/http.ts";
import { HttpError } from "../api.ts";
import { emitBrainLog } from "../telemetry.ts";
export function workerDiagnostic(
  level: "info" | "warn" | "error",
  event: string,
  fields: Record<string, unknown>,
  traceParent?: string,
): void {
  try {
    console[level](JSON.stringify({ event, ...fields }));
  } catch {}
  emitBrainLog(level, event, fields, traceParent);
}
function errorLocations(error: Error): string[] {
  return (error.stack ?? "")
    .split("\n")
    .slice(1)
    .flatMap((line) => {
      const match = line.match(
        /\/(src\/[\w./@-]+|dist\/[\w./@-]+|node_modules\/[\w./@-]+):(\d+):(\d+)\)?$/,
      );
      return match ? [`${match[1]}:${match[2]}:${match[3]}`] : [];
    })
    .slice(0, 6);
}
export function failureDiagnostic(error: unknown): Record<string, unknown> {
  if (error instanceof DsrError || error instanceof HttpError)
    return { failure_reason: "http_error", http_status: error.status };
  const known = new Map([
    ["Session requires a SandboxExecutor provider", "sandbox_provider_missing"],
  ]);
  const locations = error instanceof Error ? errorLocations(error) : [];
  return {
    failure_reason:
      error instanceof Error
        ? (known.get(error.message) ??
          (error instanceof SyntaxError
            ? "invalid_json"
            : error instanceof TypeError
              ? "type_error"
              : error.name === "AbortError"
                ? "cancelled"
                : error.name === "TimeoutError"
                  ? "timeout"
                  : "unexpected_error"))
        : "unexpected_error",
    ...(locations.length ? { error_stack: locations } : {}),
  };
}
