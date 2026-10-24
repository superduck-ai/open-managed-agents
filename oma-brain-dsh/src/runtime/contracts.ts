import type { ContinuableSubagentDescriptorData } from "@deepseek-ai/dsh-subagent";
import type { AssistantStreamFrame } from "@deepseek-ai/dsh-agent";
import type { SessionEvent } from "@deepseek-ai/dsh-session";
import type { ToolDefinition } from "@deepseek-ai/dsh-tools";
import type { Payload } from "../json.ts";
import type { Context, Fiber } from "@deepseek-ai/cordis";

export const MAX_LIVE_SUBAGENT_THREADS = 25;

export interface SubagentDefinition {
  agentSnapshot?: Payload;
  name: string;
  description: string;
  systemPrompt: string;
  model?: string;
  tools?: string[];
}

export interface SubagentConfig {
  inlineAgents?: boolean;
  agents?: SubagentDefinition[];
}

export interface SubagentThread {
  agentSnapshot?: Payload;
  id: string;
  inline?: boolean;
  name: string;
  systemPrompt: string;
  model: string;
  tools?: string[];
  status: "idle" | "running" | "terminated";
}

/** Runtime facts; protocol adapters choose wire names, IDs and envelopes. */
export type SubagentEvent =
  | { kind: "created" | "running" | "terminated"; thread: SubagentThread }
  | { kind: "idle"; thread: SubagentThread; completed: boolean }
  | { kind: "message-sent" | "report-received"; thread: SubagentThread; text: string };

/** Awaited storage, policy and event adapters for child orchestration. */
export interface SubagentBridge {
  validateThread?(threadId: string, signal: AbortSignal): Promise<void>;
  archivedThreads?(threadIds: string[], signal: AbortSignal): Promise<string[]>;
  threads(): SubagentThread[];
  restoreThreads?(): Promise<void>;
  bindPersistence(threadId: string): void;
  emit(event: SubagentEvent): Promise<void>;
  beforeRequest(): Promise<void>;
  onContextCompacted?(threadId: string, tokens?: number): Promise<void>;
  onEvent(threadId: string, event: SessionEvent): void;
  onStream(threadId: string, frame: AssistantStreamFrame): void;
  toolExecution(threadId: string): ToolExecutionBridge;
}

export interface RemoteTool {
  name: string;
  remoteName: string;
  permissionName?: string;
  description: string;
  inputSchema: Payload;
  readOnly?: boolean;
}

export type ToolPermission = "allow" | "ask" | "deny";
export type ToolPermissions = Record<string, ToolPermission>;

export class ToolNotForwardedError extends Error {}

export const SHUTDOWN_CANCEL = { kind: "hook", reason: "oma-worker-shutdown" } as const;

export function isShutdownCancel(reason: unknown): boolean {
  return typeof reason === "object" && reason !== null && "kind" in reason && reason.kind === SHUTDOWN_CANCEL.kind
    && "reason" in reason && reason.reason === SHUTDOWN_CANCEL.reason;
}

const CONNECT_FAILURE_CODES = new Set(["ConnectionRefused", "ECONNREFUSED", "FailedToOpenSocket", "ENOTFOUND", "EAI_AGAIN"]);

export function isConnectFailure(error: unknown): boolean {
  return typeof error === "object" && error !== null && "code" in error
    && CONNECT_FAILURE_CODES.has(String(error.code));
}

export async function notForwardedDetail(response: Response): Promise<string> {
  let detail = `HTTP ${response.status}`;
  try {
    const message = (await response.json())?.error?.message;
    if (typeof message === "string" && message) detail = message.slice(0, 500);
  } catch {
    return detail;
  }
  return detail;
}

/** Host-provided sandbox readiness and execution. Resource creation belongs to the provider service. */
export interface SandboxExecutor {
  provision?(signal: AbortSignal): Promise<void>;
  call(tool: RemoteTool, args: Payload, signal: AbortSignal, threadId?: string): Promise<Payload>;
}

declare module "@deepseek-ai/cordis" {
  interface Context {
    sandboxExecutor: SandboxExecutor;
    toolExecutionBridge: ToolExecutionBridge;
  }
}

export interface SessionRuntimeConfig {
  refreshTools?(signal: AbortSignal): Promise<{ tools: string[]; externalMcpConfig: unknown }>;
  threadMcpContext?(threadId: string): Promise<{ config: unknown; fetcher: typeof fetch }>;
  externalMcpConfig?: unknown;
  externalMcpFetch?: typeof fetch;
  id: string;
  descriptor?: ContinuableSubagentDescriptorData;
  provider: string;
  model: string;
  workDir: string;
  systemPrompt: string;
  /** Mount root-session services before tool registration. Children share these services. */
  setup?(scope: Context): Promise<void>;
  setupTools?(scope: Context, bridge: ToolExecutionBridge, tools?: string[], threadId?: string, permissions?: ToolPermissions): Promise<Fiber | void>;
  subagentSystemPrompt?(thread: SubagentThread): string;
  tools?: string[];
  toolPermissions?: ToolPermissions;
  refreshToolPermissions?(signal: AbortSignal, threadId?: string): Promise<ToolPermissions | undefined>;
  subagents?: SubagentConfig;
  /** Host binds a child model identity to the parent's credentials. */
  bindThread?(id: string): () => void;
  modelMessageID?(sessionId: string): string | undefined;
}

export type ToolExecutionContext = Pick<
  Parameters<ToolDefinition["execute"]>[1],
  "callId" | "signal" | "deferContext"
>;

export interface ToolApproval {
  callId: string;
  toolName: string;
  input: Payload;
  available?: boolean;
}

export interface ApprovalDecision {
  allowed: boolean;
  externalResult?: { content: {type: "text"; text: string}[]; isError: boolean };
  input?: unknown;
  reason?: string;
}

export interface ToolExecutionBridge {
  approve(request: ToolApproval, signal: AbortSignal): Promise<ApprovalDecision>;
  runTool<T>(callId: string, execute: () => Promise<T>, readOnly: boolean): Promise<T>;
  approvalChanged(name: string, callId: string, effective: Payload): void | Promise<void>;
}

export interface SessionRuntimeHooks {
  onContextCompacted?(tokens?: number): Promise<void>;
  onExternalMcpConnectionFailure?(serverName: string): Promise<void>;
  onExternalMcpAuthenticationFailure?(serverName: string, phase?: "discovery" | "tool_call"): Promise<void>;
  setupTools?(scope: Context): void;
  beforeRequest(): Promise<void>;
  onEvent(event: SessionEvent): void;
  onStream(frame: AssistantStreamFrame): void;
  stop(): void;
}

export interface TurnHooks {
  messageId?: string;
  beforeRun(): Promise<void | boolean>;
  afterRun(completed: boolean): Promise<void>;
}
