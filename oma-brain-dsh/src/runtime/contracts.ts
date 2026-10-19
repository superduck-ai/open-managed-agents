import type { AssistantStreamFrame } from "@deepseek-ai/dsh-agent";
import type { SessionEvent } from "@deepseek-ai/dsh-session";
import type { ToolDefinition } from "@deepseek-ai/dsh-tools";
import type { Payload } from "../json.ts";
import type { Context } from "@deepseek-ai/cordis";

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
  threads(): SubagentThread[];
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
  description: string;
  inputSchema: Payload;
}

/** Host-provided sandbox readiness and execution. Resource creation belongs to the provider service. */
export interface SandboxExecutor {
  provision(signal: AbortSignal): Promise<void>;
  call(tool: RemoteTool, args: Payload, signal: AbortSignal): Promise<Payload>;
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
  provider: string;
  model: string;
  workDir: string;
  systemPrompt: string;
  /** Mount root-session services before tool registration. Children share these services. */
  setup?(scope: Context): Promise<void>;
  subagentSystemPrompt?: string;
  tools?: string[];
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

export interface ApprovedToolCall {
  callId: string;
  name: string;
  original: Payload;
  effective: Payload;
}

export interface ToolOutcome extends ApprovedToolCall {
  remoteOutcome?: "unknown";
  text: string;
  failed: boolean;
}

/** Awaited policy and durability barriers, independent of any wire protocol. */
export interface ToolExecutionBridge {
  blocked(): boolean;
  approve(request: ToolApproval, signal: AbortSignal): Promise<ApprovalDecision>;
  recordApproved(call: ApprovedToolCall): Promise<void>;
  flushLog(): Promise<void>;
  recordDispatch(callId: string): Promise<void>;
  recordUnknown(callId: string): Promise<void>;
  recordResult(outcome: ToolOutcome): Promise<void>;
  stop(): void;
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
  beforeRun(): Promise<void | boolean>;
  afterRun(completed: boolean): Promise<void>;
}
