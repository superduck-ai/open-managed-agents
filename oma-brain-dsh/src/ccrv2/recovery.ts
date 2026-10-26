export class CcrRecoveryError extends Error {
  constructor(readonly sessionId: string, readonly reason: "unsupported_persistence_format", readonly field: string) {
    super(`Unsupported persistence format for session ${sessionId}: ${field}`);
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
