import type { DeliveryState } from "./protocol.ts";

export class CcrRecoveryError extends Error {
  constructor(readonly sessionId: string, readonly reason: "invalid_delivery" | "unsupported_persistence_format", readonly field: string) {
    super(`${reason === "invalid_delivery" ? "Invalid CCRv2 delivery" : "Unsupported persistence format"} for session ${sessionId}: ${field}`);
  }
}

function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

export function assertPersistenceFormat(payload: Record<string, unknown>, sessionId: string): void {
  const legacyTypes = ["dsh_header", "dsh_brain_state", "grok_checkpoint", "ccrv2_delivery"];
  const legacySubtypes = [...legacyTypes, "dsh_log", "ccrv2_delivery"];
  if ((typeof payload.type === "string" && legacyTypes.includes(payload.type))
    || (typeof payload.subtype === "string" && legacySubtypes.includes(payload.subtype)))
    throw new CcrRecoveryError(sessionId, "unsupported_persistence_format", "legacy envelope");
  if (payload.type === "dsh_log" && record(payload.data) && ("header" in payload.data || "events" in payload.data))
    throw new CcrRecoveryError(sessionId, "unsupported_persistence_format", "batched dsh_log");
}

export function readDelivery(value: unknown, sessionId: string): DeliveryState {
  const invalid = (field: string): never => { throw new CcrRecoveryError(sessionId, "invalid_delivery", field); };
  if (!record(value)) return invalid("data");
  if (!Number.isSafeInteger(value.revision) || (value.revision as number) < 1) return invalid("revision");
  if (typeof value.inputId !== "string") return invalid("inputId");
  if (!record(value.logOffsets)) return invalid("logOffsets");
  for (const offset of Object.values(value.logOffsets))
    if (!Number.isSafeInteger(offset) || (offset as number) < 0) return invalid("logOffsets");
  if (!Array.isArray(value.completed) || value.completed.some((id) => typeof id !== "string")) return invalid("completed");
  if (!Array.isArray(value.outbox) || value.outbox.some((event) => !record(event)
    || typeof event.type !== "string" || !event.type || typeof event.uuid !== "string" || !event.uuid)) return invalid("outbox");
  return structuredClone(value) as unknown as DeliveryState;
}
