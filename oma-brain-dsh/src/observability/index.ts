import type { Context } from "@deepseek-ai/cordis";
import OTel, { type SessionLogReporter } from "@deepseek-ai/dsh-otel";
import { SessionId } from "@deepseek-ai/dsh-session";
import { SessionTelemetryBackend, SessionTelemetryCoordinator, type SessionTelemetryRecord } from "@deepseek-ai/dsh-session-telemetry";
import type { Assignment } from "../assignment.ts";
import type { OmaApi } from "../api.ts";
import { workerDiagnostic } from "../ccrv2/diagnostics.ts";

interface ReporterState {
  assignment: Assignment;
  reporter: SessionLogReporter;
  credentials: { token: string; closed: boolean };
  closing?: Promise<void>;
}

export class NativeTelemetry extends SessionTelemetryBackend {
  static inject = ["otel", "sessions"];
  readonly sharing = "full" as const;
  private readonly reporters = new Map<string, ReporterState>();
  private readonly closed = new WeakSet<Assignment>();
  private readonly draining = new Set<Promise<void>>();
  private stopped = false;
  private readonly owners = new Map<string, { id: string; header: string }>();
  private readonly api: OmaApi;

  constructor(ctx: Context, config: { api: OmaApi }) {
    super(ctx);
    this.api = config.api;
    this.api.telemetry = this;
    new SessionTelemetryCoordinator(ctx, this, { capture: "live", includeHistory: true });
  }

  private owner(sessionId: string): { id: string; header: string } | undefined {
    const cached = this.owners.get(sessionId);
    if (cached) return cached;
    const session = this.ctx.sessions.get(SessionId(sessionId));
    let current = session;
    const seen = new Set<string>();
    while (current && !seen.has(current.id)) {
      seen.add(current.id);
      const assignment = this.api.assignments.get(current.id);
      if (assignment && !this.closed.has(assignment) && session) {
        const owner = { id: current.id, header: JSON.stringify(session.header) };
        this.owners.set(sessionId, owner);
        return owner;
      }
      current = current.header.parentSession ? this.ctx.sessions.get(current.header.parentSession) : undefined;
    }
    return undefined;
  }

  private reporter(id: string): SessionLogReporter | undefined {
    const assignment = this.api.assignments.get(id);
    if (this.stopped || !assignment?.observability_enabled || this.closed.has(assignment)) return;
    const existing = this.reporters.get(id);
    if (existing?.assignment === assignment) return existing.closing ? undefined : existing.reporter;
    if (existing) void this.closeReporter(id, existing);
    const credentials = { token: assignment.ingress_token, closed: false };
    const reporter = this.ctx.otel.createSessionLogReporter({
      scope: { name: "@deepseek-ai/dsh-session", version: "0.2.1-alpha.2" },
      exporter: {
        url: `${this.api.base}/v1/code/sessions/${encodeURIComponent(id)}/worker/otlp/v1/logs`,
        headers: async () => {
          if (!credentials.closed) credentials.token = assignment.ingress_token;
          return { Authorization: `Bearer ${credentials.token}` };
        },
        timeoutMillis: 2_000,
      },
      resourceAttributes: { "service.name": "oma-brain-dsh" },
      processor: { scheduledDelayMillis: 250, maxQueueSize: 4096, maxExportBatchSize: 128 },
      onFailure: () => workerDiagnostic("warn", "trajectory_export_failed", { session_id: id }),
    });
    this.reporters.set(id, { assignment, reporter, credentials });
    return reporter;
  }

  emit(record: SessionTelemetryRecord): void {
    if (this.stopped || !record.sourceEvent) return;
    try {
      const source = record.sourceEvent;
      const owner = this.owner(source.sessionId);
      if (!owner) return;
      this.reporter(owner.id)?.reportSessionLog({
        sessionId: source.sessionId,
        event: { ...source.envelope, data: record.body },
        attributes: { "dsh.header": owner.header },
      });
    } catch {
      workerDiagnostic("warn", "trajectory_capture_failed", {});
    }
  }

  async shutdown(): Promise<void> {
    this.stopped = true;
    for (const id of this.reporters.keys()) void this.close(id);
    await Promise.allSettled([...this.draining]);
  }

  close(id: string): Promise<void> {
    const state = this.reporters.get(id);
    const assignment = this.api.assignments.get(id);
    if (assignment) this.closed.add(assignment);
    for (const [sessionId, owner] of this.owners) if (owner.id === id) this.owners.delete(sessionId);
    return state ? this.closeReporter(id, state) : Promise.resolve();
  }

  private closeReporter(id: string, state: ReporterState): Promise<void> {
    this.closed.add(state.assignment);
    if (state.closing) return state.closing;
    state.credentials.token = state.assignment.ingress_token;
    state.credentials.closed = true;
    const closing = this.drain(state.reporter, id).finally(() => {
      if (this.reporters.get(id) === state) this.reporters.delete(id);
      this.draining.delete(closing);
    });
    state.closing = closing;
    this.draining.add(closing);
    return closing;
  }

  private async drain(reporter: SessionLogReporter, id: string): Promise<void> {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const deadline = new Promise<void>(resolve => {
      timer = setTimeout(() => { reporter.stopPending(); resolve(); }, 2_500);
    });
    try { await Promise.race([reporter.shutdown(), deadline]); }
    catch { workerDiagnostic("warn", "trajectory_shutdown_failed", { session_id: id }); }
    finally { clearTimeout(timer); }
  }

}

export async function mountTelemetry(ctx: Context, api: OmaApi): Promise<void> {
  await ctx.plugin(OTel);
  await ctx.plugin(NativeTelemetry, { api });
}
