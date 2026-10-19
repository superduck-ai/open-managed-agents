import type { Payload } from "./protocol.ts";

export function appendedSystemContext(payload: Payload): string | undefined {
  if (!("oma_system_context" in payload)) return undefined;
  if (!Array.isArray(payload.oma_system_context)) throw new Error("Invalid primary system context");
  const seen = new Set<string>();
  return payload.oma_system_context.map((entry: unknown) => {
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) throw new Error("Invalid primary system context entry");
    const value = entry as { id?: unknown; text?: unknown };
    if (typeof value.id !== "string" || !value.id || seen.has(value.id) || typeof value.text !== "string")
      throw new Error("Invalid primary system context identity or text");
    seen.add(value.id);
    return value.text;
  }).join("\n\n");
}
