import type { SessionEvent, SessionHeader } from "@deepseek-ai/dsh-session";
import type { TurnTrace } from "../observability/trace-context.ts";

export interface Claim {
  code_session_id: string;
  worker_epoch: string;
  session_token: string;
  model_token: string;
  claim_expires_at: string;
  trace_parent?: string;
}

export interface Registration {
  epoch: string;
  protocol: "1";
  lease: { ttl_ms: number; grace_ms: number; heartbeat_interval_ms: number };
  observability_enabled: boolean;
  trace_export?: { content: boolean };
}

export interface WorkerState {
  model_control_sequence: string | null;
  model: string | null;
  trace_turns: TurnTrace[] | null;
}

export type WorkerPatch = { trace_turns?: TurnTrace[] | null } & ({ model?: never; control_input_id?: never; control_sequence?: never } | { model: string | null; control_input_id: string; control_sequence: string });
export type Finish = { result: "success" | "cancelled" } | {
  result: "failed";
  failure_code: "execution_failed" | "worker_shutdown" | "interrupted_recovery" | "quota_exhausted";
};

export type ToolIdentity = { kind: "builtin" | "custom" | "delegation"; name: string }
  | { kind: "mcp"; name: string; server: string };

export interface PermissionRequest {
  input_id: string;
  turn: string;
  dsh_session_id: string;
  tool_use_id: string;
  tool: ToolIdentity;
  input: Record<string, unknown>;
  available?: boolean;
}

export interface ExternalResult {
  content: { type: "text"; text: string }[];
  isError: boolean;
}

export type Decision = { behavior: "allow"; updated_input?: Record<string, unknown> }
  | { behavior: "deny"; message?: string }
  | { behavior: "external_result"; external_result: ExternalResult };

export type Permission = { request_id: string; state: "pending" | "cancelled" | "closed" }
  | { request_id: string; state: "resolved"; decision: Decision };

export type Input = { id: string; lane: "task"; kind: "user.message"; content: unknown[]; session_thread_id?: string;
  oma_system_context?: { id: string; text: string }[]; oma_refresh_tools?: boolean; trace_context?: Record<string, string> }
  | { id: string; lane: "control"; kind: "permission.resolved"; request_id: string; resolution: Decision }
  | { id: string; lane: "control"; kind: "user.interrupt"; session_thread_id?: string }
  | { id: string; lane: "control"; kind: "session.set_model"; model: string | null };

export interface Delivery {
  id: string;
  status: "received" | "processing" | "processed";
  error?: "unsupported_input" | "invalid_model";
}

export interface DeliveryResult {
  applied: number;
  scheduled: number;
  ignored: number;
  results: { id: string; status: Delivery["status"]; outcome: "applied" | "ignored" | "scheduled"; cancel_requested_at?: string; execution_status?: "accepted" | "running" | "completed" }[];
}

export interface Transcript {
  dsh_session_id: string;
  parent_dsh_session_id: string | null;
  session_thread_id: string | null;
  header_present: boolean;
  next_seq: string;
}

export interface TranscriptPage {
  header: SessionHeader | null;
  events: SessionEvent[];
  next_from_seq: string | null;
}

export type PublicEvent = { id: string; type: "preview.start"; event: { type: "agent.message" | "agent.thinking"; id: string } }
  | { id: string; type: "preview.delta"; event_id: string; delta: Record<string, unknown> }
  | { id: string; type: "agent.message"; content: unknown[]; model_request_start_id?: string; session_thread_id?: string }
  | { id: string; type: "agent.thinking"; model_request_start_id?: string; session_thread_id?: string }
  | { id: string; type: "tool.result"; tool_use_id: string; tool: ToolIdentity; is_error: boolean; content: unknown[]; session_thread_id?: string }
  | { id: string; type: "system.message"; content: unknown[]; session_thread_id?: string }
  | { id: string; type: "session.thread_created" | "session.thread_status_running" | "session.thread_status_idle" | "session.thread_status_terminated"
      | "agent.thread_message_sent" | "agent.thread_message_received" | "agent.thread_context_compacted" | "session.error"; session_thread_id?: string; [key: string]: unknown };
