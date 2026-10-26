import type { ContinuableSubagentDescriptorData } from "@deepseek-ai/dsh-subagent";
import type { AssistantStreamFrame } from "@deepseek-ai/dsh-agent";
import type { Session, SessionEvent } from "@deepseek-ai/dsh-session";
import type { ToolDefinition } from "@deepseek-ai/dsh-tools";
import type { Payload } from "../json.ts";
import type { LlmCallConfig, LlmFailure } from "@deepseek-ai/dsh-llm";
import type { Context, Fiber } from "@deepseek-ai/cordis";

export const MAX_LIVE_SUBAGENT_THREADS = 25;

export interface SubagentDefinition {
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
  createThreadId?(): string;
  threads(): SubagentThread[];
  restoreThreads?(): Promise<void>;
  bindPersistence(threadId: string): void;
  emit(event: SubagentEvent): Promise<void>;
  beforeRequest?(threadId: string): Promise<void>;
  onOpen?(session: Session): Promise<void>;
  afterFlushLog?(session: Session): Promise<void>;
  onDiagnostic?(threadId: string, diagnostic: RuntimeDiagnostic): void;
  onEvent(threadId: string, event: SessionEvent, session: Session): void;
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
  configureModel?(call: LlmCallConfig): LlmCallConfig;
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
  refreshSessionContext?(signal: AbortSignal, threadId?: string): Promise<{ toolPermissions?: ToolPermissions }>;
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

export type RuntimeDiagnostic =
  | { kind: "model-retry"; failure: LlmFailure; attempt: number; delayMs: number }
  | { kind: "input-failed"; error: unknown };

export interface SessionRuntimeHooks {
  onDiagnostic?(diagnostic: RuntimeDiagnostic): void;
  selectModel?(model: string): string;
  onOpen?(session: Session): Promise<void>;
  afterFlushLog?(session: Session): Promise<void>;
  onExternalMcpConnectionFailure?(serverName: string): Promise<void>;
  onExternalMcpAuthenticationFailure?(serverName: string, phase?: "discovery" | "tool_call"): Promise<void>;
  setupTools?(scope: Context): void;
  beforeRequest?(): Promise<void>;
  onEvent(event: SessionEvent, session: Session): void;
  onStream(frame: AssistantStreamFrame): void;
  stop(): void;
}

export interface TurnHooks {
  messageId?: string;
  beforeRun(): Promise<void | boolean>;
  afterRun(completed: boolean): Promise<void>;
}
