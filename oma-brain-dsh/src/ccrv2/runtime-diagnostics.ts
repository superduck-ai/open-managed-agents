import type { Session, SessionEvent } from "@deepseek-ai/dsh-session";
import type { RuntimeDiagnostic } from "../runtime/contracts.ts";
import { failureDiagnostic, workerDiagnostic } from "./diagnostics.ts";

export function recordRuntimeEvent(session: Session, event: SessionEvent): void {
  if (process.env.OMA_DEBUG_TIMING !== "1" || !["assistant/message", "tool/result", "turn/end"].includes(event.type)) return;
  const data = event.data as { message?: { content?: { type: string }[] }; reason?: { kind: string } };
  workerDiagnostic("info", "oma_runtime_event", {
    session_id: session.id, event_type: event.type, timestamp: new Date().toISOString(),
    block_types: data.message?.content?.map(block => block.type), reason: data.reason?.kind,
  });
}

export function recordRuntimeDiagnostic(sessionId: string, diagnostic: RuntimeDiagnostic): void {
  if (diagnostic.kind === "model-retry") {
    workerDiagnostic("warn", "oma_model_request_retry", {
      session_id: sessionId, failure_code: diagnostic.failure.code, http_status: diagnostic.failure.status,
      attempt: diagnostic.attempt, delay_ms: diagnostic.delayMs,
    });
  } else {
    workerDiagnostic("error", "oma_runtime_input_failed", { session_id: sessionId, ...failureDiagnostic(diagnostic.error) });
  }
}
