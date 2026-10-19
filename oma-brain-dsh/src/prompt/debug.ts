import { createHash } from "node:crypto";
import type { Payload } from "../json.ts";
import { workerDiagnostic } from "../ccrv2/diagnostics.ts";

const MAX_TRACKED_SESSIONS = 1000;
const logged = new Map<string, string>();

export function logSystemPrompt(sessionId: string, request: Payload): void {
  if (process.env.OMA_DEBUG_SYSTEM_PROMPT !== "1") return;
  const system = typeof request.system === "string" ? request.system : "";
  const tools: string[] = (request.tools ?? []).map((tool: Payload) => tool.name);
  const sha256 = createHash("sha256").update(system).update("\0").update(tools.join("\n")).digest("hex");
  if (logged.get(sessionId) === sha256) return;
  logged.delete(sessionId);
  logged.set(sessionId, sha256);
  if (logged.size > MAX_TRACKED_SESSIONS) logged.delete(logged.keys().next().value!);
  workerDiagnostic("info", "oma_system_prompt", { session_id: sessionId, sha256, tools, system });
}
