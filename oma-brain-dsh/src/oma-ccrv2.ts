import { OmaApi } from "./api.ts";
import { workerDiagnostic } from "./ccrv2/diagnostics.ts";
import {
  CcrClient, type CcrSession, type CcrLeaseFailure, type CcrHeartbeatHealth,
} from "./ccrv2/index.ts";

/** Emit only protocol metadata; transport errors and credentials stay out of logs. */
export function stopOmaSessionOnLeaseLoss(session: Pick<CcrSession, "id" | "stop">, failure: CcrLeaseFailure): void {
  session.stop();
  const stopReason = failure.status === "rejected" ? "request_rejected"
    : failure.httpStatus === 401 || failure.httpStatus === 403 ? "auth_failures_exhausted" : "consecutive_failures";
  const fields = {
    session_id: session.id,
    worker_epoch: failure.workerEpoch ?? null,
    http_status: failure.httpStatus ?? null,
    failure_reason: failure.reason ?? "unknown",
    stop_reason: stopReason,
    consecutive_failures: failure.consecutiveFailures,
  };
  workerDiagnostic("warn", "ccrv2_lease_lost", fields);
}

export function logOmaHeartbeatHealth(id: string, health: CcrHeartbeatHealth): void {
  const event = health.type === "failure" ? "ccrv2_heartbeat_failed" : "ccrv2_heartbeat_recovered";
  const fields = {
    session_id: id,
    worker_epoch: health.workerEpoch ?? null,
    ...(health.type === "failure" ? {
      http_status: health.httpStatus ?? null,
      failure_reason: health.reason ?? "unknown",
    } : {}),
    consecutive_failures: health.consecutiveFailures,
    duration_ms: Math.round(health.durationMs),
  };
  workerDiagnostic(health.type === "failure" ? "warn" : "info", event, fields);
}

/** Bind OMA's HTTP transport and current assignment credentials to CCRv2. */
export function createOmaCcrClient(api: OmaApi): CcrClient {
  return new CcrClient({
    request: ({ sessionId, path, token, method, body, signal, timeout, headers }) =>
      api.requestResponse(path, token, method, body, { ...api.traceHeaders(sessionId), ...headers }, signal, timeout),
    credentials: (id) => {
      const assignment = api.assignment(id);
      return {
        token: assignment.ingress_token,
        workerEpoch: assignment.worker_epoch,
      };
    },
    sessionIds: () => api.assignments.keys(),
  });
}
