const TRACE_PARENT = /^00-(?!0{32})[0-9a-f]{32}-(?!0{16})[0-9a-f]{16}-[0-9a-f]{2}$/;

export const TRACE_PARENT_HEADER = "traceparent";

export function validTraceParent(value: string | null | undefined): string | undefined {
  return value && TRACE_PARENT.test(value) ? value : undefined;
}

export function traceIdOf(traceParent: string): string {
  return traceParent.slice(3, 35);
}

export function traceHeaders(traceParent: string | undefined): Record<string, string> {
  return traceParent ? { [TRACE_PARENT_HEADER]: traceParent } : {};
}
