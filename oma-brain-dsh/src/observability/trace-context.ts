import { AsyncLocalStorage } from "node:async_hooks";
import { randomBytes } from "node:crypto";

export interface TraceParent {
  traceId: string;
  spanId: string;
  sampled: boolean;
  traceState?: string;
}

export interface InputTraceContext {
  traceparent?: unknown;
  tracestate?: unknown;
}

const TRACE_PARENT = /^([0-9a-f]{2})-([0-9a-f]{32})-([0-9a-f]{16})-([0-9a-f]{2})(?:-.*)?$/;

export function parseTraceParent(context: InputTraceContext | undefined): TraceParent | undefined {
  if (!context || typeof context.traceparent !== "string") return undefined;
  const match = TRACE_PARENT.exec(context.traceparent.trim());
  if (!match || match[1] === "ff" || (match[1] === "00" && context.traceparent.trim().length !== 55)) return undefined;
  if (/^0+$/.test(match[2]!) || /^0+$/.test(match[3]!)) return undefined;
  const traceState = typeof context.tracestate === "string" && context.tracestate.length <= 512 ? context.tracestate : undefined;
  return { traceId: match[2]!, spanId: match[3]!, sampled: (parseInt(match[4]!, 16) & 1) === 1, ...(traceState ? { traceState } : {}) };
}

export function formatTraceParent(traceId: string, spanId: string, sampled = true): string {
  return `00-${traceId}-${spanId}-${sampled ? "01" : "00"}`;
}

export function newTraceId(): string {
  return randomBytes(16).toString("hex");
}

export function newSpanId(): string {
  return randomBytes(8).toString("hex");
}

export interface ToolTraceScope {
  traceparent: string;
  tracestate?: string;
}

const toolScope = new AsyncLocalStorage<ToolTraceScope>();

export function currentToolTrace(): ToolTraceScope | undefined {
  return toolScope.getStore();
}

export interface TurnTrace {
  sessionId: string;
  traceId: string;
  spanId: string;
  parentSpanId?: string;
  traceState?: string;
  startMs: number;
}

export interface TraceHookTarget {
  inputReceived(sessionId: string, context: InputTraceContext | undefined): void;
  openTurns(owner: string): TurnTrace[];
  turnsInterrupted(owner: string, turns: TurnTrace[]): void;
  approvalPending(sessionId: string, callId: string): void;
  approvalResolved(sessionId: string, callId: string, outcome: "allowed" | "denied" | "external"): void;
  toolDispatched(sessionId: string, callId: string): ToolTraceScope | undefined;
  subagent(sessionId: string, info: { name?: string; inline: boolean }): void;
}

const mcpServers = new Map<string, string>();

export const traceHooks = {
  target: undefined as TraceHookTarget | undefined,
  inputReceived(sessionId: string, context: InputTraceContext | undefined): void {
    try { this.target?.inputReceived(sessionId, context); } catch { return; }
  },
  openTurns(owner: string): TurnTrace[] | undefined {
    try {
      const turns = this.target?.openTurns(owner);
      return turns?.length ? turns : undefined;
    } catch { return undefined; }
  },
  turnsInterrupted(owner: string, turns: TurnTrace[] | undefined): void {
    if (!turns?.length) return;
    try { this.target?.turnsInterrupted(owner, turns); } catch { return; }
  },
  approvalPending(sessionId: string, callId: string): void {
    try { this.target?.approvalPending(sessionId, callId); } catch { return; }
  },
  approvalResolved(sessionId: string, callId: string, outcome: "allowed" | "denied" | "external"): void {
    try { this.target?.approvalResolved(sessionId, callId, outcome); } catch { return; }
  },
  runTool<T>(sessionId: string, callId: string, run: () => T): T {
    let scope: ToolTraceScope | undefined;
    try { scope = this.target?.toolDispatched(sessionId, callId); } catch { scope = undefined; }
    return scope ? toolScope.run(scope, run) : run();
  },
  subagentCreated(sessionId: string, info: { name?: string; inline: boolean }): void {
    try { this.target?.subagent(sessionId, info); } catch { return; }
  },
  registerMcpServer(prefix: string, name: string): void {
    mcpServers.set(prefix, name);
  },
  mcpServerName(prefix: string): string | undefined {
    return mcpServers.get(prefix);
  },
};
