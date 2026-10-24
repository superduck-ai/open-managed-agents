import type { Payload } from "../json.ts";
import type { SubagentThread } from "../runtime/contracts.ts";
export { object, type Json, type Payload } from "../json.ts";

const controlInputTypes = new Set(["control_response", "control_request", "interrupt", "user.interrupt", "keep_alive"]);
export const isControlInput = (type: unknown): boolean => typeof type === "string" && controlInputTypes.has(type);

export interface ThreadExecutionState {
  inputId: string;
  phase: "idle" | "accepted" | "approved" | "dispatch" | "result" | "completed" | "interrupted" | "blocked";
  callPhases?: Record<string, "approved" | "dispatch" | "result" | "blocked" | "interrupted">;
  approvedCalls: SessionProjection["approvedCalls"];
  toolOutcomes: SessionProjection["toolOutcomes"];
}

export interface SessionProjection {
  checkpointRevision: number;
  inputId: string;
  phase: "idle" | "accepted" | "dispatch" | "result" | "completed" | "blocked";
  completed: string[];
  outbox: Payload[];
  approvedCalls: Record<
    string,
    { name: string; original: Payload; effective: Payload }
  >;
  toolOutcomes: Record<string, { text: string; failed: boolean; remoteOutcome?: "unknown" }>;
  threads?: SubagentThread[];
  executions?: Record<string, ThreadExecutionState>;
}

export const emptyProjection = (): SessionProjection => ({
  checkpointRevision: 0,
  inputId: "",
  phase: "idle",
  completed: [],
  outbox: [],
  approvedCalls: {},
  toolOutcomes: {},
});
