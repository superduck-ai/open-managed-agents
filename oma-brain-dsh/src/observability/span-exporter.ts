import { workerDiagnostic } from "../ccrv2/diagnostics.ts";

export type SpanAttributeValue = string | number | boolean;

export interface SpanRecord {
  traceId: string;
  spanId: string;
  parentSpanId?: string;
  traceState?: string;
  name: string;
  startMs: number;
  endMs: number;
  attributes: Record<string, SpanAttributeValue | undefined>;
  status?: "ok" | "error";
}

const MAX_QUEUE = 2048;
const MAX_BATCH = 256;
const MAX_BATCH_BYTES = 8 << 20;
const FLUSH_DELAY_MS = 500;
const REQUEST_TIMEOUT_MS = 2_000;

function bytes64(hex: string): string {
  return Buffer.from(hex, "hex").toString("base64");
}

function nanos(ms: number): string {
  return (BigInt(Math.round(ms)) * 1_000_000n).toString();
}

function attributeValue(value: SpanAttributeValue): Record<string, unknown> {
  if (typeof value === "boolean") return { boolValue: value };
  if (typeof value === "number") return Number.isInteger(value) ? { intValue: String(value) } : { doubleValue: value };
  return { stringValue: value };
}

export function otlpSpan(span: SpanRecord): Record<string, unknown> {
  return {
    traceId: bytes64(span.traceId),
    spanId: bytes64(span.spanId),
    ...(span.parentSpanId ? { parentSpanId: bytes64(span.parentSpanId) } : {}),
    ...(span.traceState ? { traceState: span.traceState } : {}),
    name: span.name,
    kind: 1,
    startTimeUnixNano: nanos(span.startMs),
    endTimeUnixNano: nanos(Math.max(span.endMs, span.startMs)),
    attributes: Object.entries(span.attributes)
      .filter((entry): entry is [string, SpanAttributeValue] => entry[1] !== undefined && entry[1] !== "")
      .map(([key, value]) => ({ key, value: attributeValue(value) })),
    ...(span.status ? { status: { code: span.status === "ok" ? 1 : 2 } } : {}),
  };
}

export class SpanExporter {
  private queue: string[] = [];
  private queuedBytes = 0;
  private timer?: ReturnType<typeof setTimeout>;
  private sending: Promise<void> = Promise.resolve();
  private closed = false;

  constructor(
    private readonly url: string,
    private readonly token: () => string,
    private readonly fetcher: typeof fetch,
    private readonly sessionId: string,
  ) {}

  add(span: SpanRecord): void {
    if (this.closed) return;
    if (this.queue.length >= MAX_QUEUE) {
      workerDiagnostic("warn", "trace_export_queue_full", { session_id: this.sessionId });
      return;
    }
    const encoded = JSON.stringify(otlpSpan(span));
    this.queue.push(encoded);
    this.queuedBytes += Buffer.byteLength(encoded);
    if (this.queue.length >= MAX_BATCH || this.queuedBytes >= MAX_BATCH_BYTES) this.flush();
    else this.timer ??= setTimeout(() => this.flush(), FLUSH_DELAY_MS);
  }

  flush(): Promise<void> {
    if (this.timer) clearTimeout(this.timer);
    this.timer = undefined;
    while (this.queue.length) {
      let count = 0;
      let bytes = 0;
      for (const encoded of this.queue) {
        const size = Buffer.byteLength(encoded);
        if (count === MAX_BATCH || (count > 0 && bytes + size > MAX_BATCH_BYTES)) break;
        count++;
        bytes += size;
      }
      const batch = this.queue.splice(0, count);
      this.queuedBytes -= bytes;
      this.sending = this.sending.then(() => this.send(batch));
    }
    return this.sending;
  }

  async close(deadlineMs = 2_500): Promise<void> {
    this.closed = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    await Promise.race([this.flush(), new Promise<void>((resolve) => { timer = setTimeout(resolve, deadlineMs); })]);
    clearTimeout(timer);
  }

  private async send(batch: string[]): Promise<void> {
    const body = `{"resourceSpans":[{"resource":{"attributes":[]},"scopeSpans":[{"scope":{"name":"oma.platform"},"spans":[${batch.join(",")}]}]}]}`;
    try {
      const response = await this.fetcher(this.url, {
        method: "POST",
        headers: { "Content-Type": "application/json", Authorization: `Bearer ${this.token()}` },
        body,
        signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
      });
      if (!response.ok) workerDiagnostic("warn", "trace_export_failed", { session_id: this.sessionId, http_status: response.status });
      await response.body?.cancel();
    } catch {
      workerDiagnostic("warn", "trace_export_failed", { session_id: this.sessionId });
    }
  }
}
