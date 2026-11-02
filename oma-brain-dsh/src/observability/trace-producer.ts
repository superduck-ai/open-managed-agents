import { formatTraceParent, newSpanId, newTraceId, parseTraceParent, traceHooks, type InputTraceContext, type ToolTraceScope, type TraceHookTarget, type TurnTrace } from "./trace-context.ts";
import type { SpanExporter, SpanRecord } from "./span-exporter.ts";

const DELEGATION_TOOLS = new Set(["send_to_agent", "list_agents", "archive_agent"]);
const MAX_CONTENT_CHARS = 786_432;

type Payload = Record<string, unknown>;

interface ParentContext {
  traceId: string;
  parentSpanId?: string;
  traceState?: string;
  sampled: boolean;
}

interface TurnIdentity extends ParentContext {
  spanId: string;
  startMs: number;
}

interface TurnDraft extends TurnIdentity {
  inputs: string[];
  output?: string;
  model?: string;
  first: boolean;
}

interface ToolDraft {
  spanId: string;
  name: string;
  arguments?: string;
  step?: number;
  startMs: number;
  dispatchMs?: number;
  asked: boolean;
  skipped: boolean;
}

interface SessionState {
  owner: string;
  parent?: string;
  pending?: TurnIdentity;
  context?: ParentContext;
  turn?: TurnDraft;
  turns: number;
  paused: boolean;
  tools: Map<string, ToolDraft>;
  compactionStart?: number;
}

export interface TraceOwner {
  exporter: SpanExporter;
  content: boolean;
  model: string;
  systemPrompt: string;
  liveSince: number;
}

export interface SubagentInfo {
  name?: string;
  inline: boolean;
}

export interface ModelRequestTiming {
  "oma.model_request.started_at"?: number;
  "oma.model_request.first_token_at"?: number;
  "gen_ai.request.model"?: string;
}

function record(value: unknown): Payload {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Payload : {};
}

function text(value: unknown): string | undefined {
  return typeof value === "string" && value ? value : undefined;
}

function contentText(blocks: unknown): string {
  return Array.isArray(blocks)
    ? blocks.map(record).filter((block) => block.type === "text" && typeof block.text === "string").map((block) => block.text as string).join("\n")
    : "";
}

function messages(role: string, texts: string[]): string | undefined {
  const parts = texts.filter(Boolean).map((content) => ({ type: "text", content: content.slice(0, MAX_CONTENT_CHARS) }));
  return parts.length ? JSON.stringify([{ role, parts }]) : undefined;
}

export function modelErrorType(code: string | undefined): string | undefined {
  if (!code) return undefined;
  const normalized = code.toUpperCase();
  if (normalized.includes("RATE")) return "rate_limit_error";
  if (normalized.includes("OVERLOAD")) return "overloaded_error";
  if (normalized.includes("AUTH")) return "authentication_error";
  if (normalized.includes("PERMISSION")) return "permission_error";
  if (normalized.includes("TIMEOUT")) return "timeout_error";
  if (normalized.includes("INVALID") || normalized.includes("CONTEXT")) return "invalid_request_error";
  return "api_error";
}

function attemptOutcome(stream: unknown): { status?: "error"; code?: string } {
  const entries = Array.isArray(stream) ? stream.map(record) : [];
  for (let index = entries.length - 1; index >= 0; index--) {
    const chunk = record(entries[index]!.chunk);
    if (entries[index]!.type !== "chunk" || chunk.type !== "finish") continue;
    const reason = record(chunk.reason);
    const code = text(record(reason.failure).code);
    return reason.kind === "aborted" ? { code } : { status: "error", code };
  }
  return { status: "error" };
}

const STOP_REASONS: Record<string, string> = {
  completed: "end_turn", aborted: "interrupted", interrupted: "interrupted", error: "error", "max-tokens": "max_tokens", blocked: "blocked",
};

export class TraceProducer implements TraceHookTarget {
  private readonly owners = new Map<string, TraceOwner>();
  private readonly sessions = new Map<string, SessionState>();
  private readonly subagents = new Map<string, SubagentInfo>();

  attach(owner: string, config: TraceOwner): void {
    this.owners.set(owner, config);
  }

  owned(owner: string): boolean {
    return this.owners.has(owner);
  }

  async close(owner: string): Promise<void> {
    const config = this.owners.get(owner);
    if (!config) return;
    for (const [id, state] of this.sessions) {
      if (state.owner !== owner) continue;
      if (state.turn) this.endTurn(id, state, Date.now(), "interrupted");
      this.sessions.delete(id);
      this.subagents.delete(id);
    }
    this.owners.delete(owner);
    await config.exporter.close();
  }

  subagent(sessionId: string, info: SubagentInfo): void {
    this.subagents.set(sessionId, info);
  }

  private state(sessionId: string, owner: string, parent?: string): SessionState {
    let state = this.sessions.get(sessionId);
    if (!state) {
      state = { owner, parent, turns: 0, paused: false, tools: new Map() };
      this.sessions.set(sessionId, state);
    }
    return state;
  }

  observe(sessionId: string, owner: string, parent: string | undefined, type: string, time: number, body: unknown, timing: ModelRequestTiming): void {
    const config = this.owners.get(owner);
    if (!config || time < config.liveSince) return;
    const state = this.state(sessionId, owner, parent);
    const data = record(body);
    switch (type) {
      case "turn/start": if (!state.turn) this.startTurn(state, time); break;
      case "user/message": this.recordInput(state, data, config); break;
      case "assistant/message": this.modelRequest(sessionId, state, time, data, timing, false); this.recordOutput(state, data, config); break;
      case "assistant/attempt": this.modelRequest(sessionId, state, time, data, timing, true); break;
      case "tool/call": this.toolCall(state, time, data, config); break;
      case "tool/result": this.toolResult(sessionId, state, time, data, config); break;
      case "compaction/start": state.compactionStart = time; break;
      case "compaction/end": this.compaction(sessionId, state, time, data); break;
      case "turn/end": this.turnEnd(sessionId, state, time, data); break;
    }
  }

  inputReceived(sessionId: string, input: InputTraceContext | undefined): void {
    const state = this.sessions.get(sessionId) ?? (this.owners.has(sessionId) ? this.state(sessionId, sessionId) : undefined);
    if (!state) return;
    const parsed = parseTraceParent(input);
    const context = parsed ? { traceId: parsed.traceId, parentSpanId: parsed.spanId, traceState: parsed.traceState, sampled: parsed.sampled } : { traceId: newTraceId(), sampled: true };
    state.pending = { ...context, spanId: newSpanId(), startMs: Date.now() };
  }

  openTurns(owner: string): TurnTrace[] {
    const turns: TurnTrace[] = [];
    for (const [sessionId, state] of this.sessions) {
      const turn = state.owner === owner ? state.turn ?? state.pending : undefined;
      if (turn?.sampled) turns.push({ sessionId, traceId: turn.traceId, spanId: turn.spanId, parentSpanId: turn.parentSpanId, traceState: turn.traceState, startMs: turn.startMs });
    }
    return turns;
  }

  turnsInterrupted(owner: string, turns: TurnTrace[]): void {
    const config = this.owners.get(owner);
    if (!config) return;
    const now = Date.now();
    for (const turn of turns) {
      config.exporter.add({
        traceId: turn.traceId, spanId: turn.spanId, parentSpanId: turn.parentSpanId, traceState: turn.traceState,
        name: "oma.session.turn", startMs: turn.startMs, endMs: now, status: "error",
        attributes: {
          "gen_ai.operation.name": "invoke_agent", "gen_ai.conversation.id": turn.sessionId, "gen_ai.request.model": config.model,
          "oma.session.turn.stop_reason": "interrupted", "error.type": "worker_lost",
        },
      });
    }
    void config.exporter.flush();
  }

  approvalPending(sessionId: string, callId: string): void {
    const state = this.sessions.get(sessionId);
    if (!state) return;
    const tool = state.tools.get(callId);
    if (tool) tool.asked = true;
    if (state.turn && !state.paused) {
      this.endTurn(sessionId, state, Date.now(), "requires_action");
      state.paused = true;
    }
  }

  approvalResolved(sessionId: string, callId: string, outcome: "allowed" | "denied" | "external"): void {
    const state = this.sessions.get(sessionId);
    if (!state) return;
    const tool = state.tools.get(callId);
    if (tool && outcome !== "allowed") tool.skipped = true;
    if (state.paused) {
      state.paused = false;
      this.startTurn(state, Date.now(), state.context && { ...state.context, spanId: newSpanId(), startMs: Date.now() });
    }
  }

  toolDispatched(sessionId: string, callId: string): ToolTraceScope | undefined {
    const state = this.sessions.get(sessionId);
    const tool = state?.tools.get(callId);
    if (!state?.turn || !tool || tool.skipped) return undefined;
    tool.dispatchMs = Date.now();
    if (!state.turn.sampled) return undefined;
    return { traceparent: formatTraceParent(state.turn.traceId, tool.spanId), ...(state.turn.traceState ? { tracestate: state.turn.traceState } : {}) };
  }

  private nextTurn(state: SessionState, time: number): TurnIdentity {
    if (state.parent) {
      const parentTurn = this.sessions.get(state.parent)?.turn;
      if (parentTurn) return { traceId: parentTurn.traceId, parentSpanId: parentTurn.spanId, traceState: parentTurn.traceState, sampled: parentTurn.sampled, spanId: newSpanId(), startMs: time };
    }
    const pending = state.pending;
    state.pending = undefined;
    return pending ?? { traceId: newTraceId(), sampled: true, spanId: newSpanId(), startMs: time };
  }

  private startTurn(state: SessionState, time: number, next = this.nextTurn(state, time)): void {
    const { spanId, traceId, parentSpanId, traceState, sampled } = next;
    state.context = { traceId, parentSpanId, traceState, sampled };
    state.turn = { ...state.context, spanId, startMs: time, inputs: [], first: state.turns === 0 };
    state.turns++;
  }

  private recordInput(state: SessionState, data: Payload, config: TraceOwner): void {
    if (!config.content || !state.turn || record(data.source).kind !== "user") return;
    state.turn.inputs.push(contentText(data.content));
  }

  private recordOutput(state: SessionState, data: Payload, config: TraceOwner): void {
    const message = record(data.message);
    if (state.turn) state.turn.model = text(record(message.source).model) ?? state.turn.model;
    if (!config.content || !state.turn) return;
    const output = contentText(message.content);
    if (output) state.turn.output = output;
  }

  private emit(state: SessionState, span: SpanRecord): void {
    if (!state.turn?.sampled && span.name !== "oma.session.turn") return;
    this.owners.get(state.owner)?.exporter.add(span);
  }

  private child(state: SessionState, name: string, start: number, end: number, attributes: SpanRecord["attributes"], status?: "ok" | "error"): void {
    const turn = state.turn;
    if (!turn) return;
    this.emit(state, { traceId: turn.traceId, spanId: newSpanId(), parentSpanId: turn.spanId, traceState: turn.traceState, name, startMs: start, endMs: end, attributes, status });
  }

  private modelRequest(sessionId: string, state: SessionState, time: number, data: Payload, timing: ModelRequestTiming, attempt: boolean): void {
    const turn = state.turn;
    if (!turn) return;
    const started = timing["oma.model_request.started_at"] ?? turn.startMs;
    const firstToken = timing["oma.model_request.first_token_at"];
    const model = timing["gen_ai.request.model"] ?? text(record(record(data.message).source).model);
    const outcome = attempt ? attemptOutcome(data.stream) : data.interrupted === true ? {} : { status: "ok" as const };
    this.child(state, "oma.model_request", started, time, {
      "gen_ai.operation.name": "chat", "gen_ai.request.model": model, "gen_ai.provider.name": model?.includes("/") ? model.split("/")[0] : text(record(record(data.message).source).provider),
      "gen_ai.conversation.id": sessionId, "oma.step": typeof data.step === "number" ? data.step : undefined,
      "oma.model_request.ttft_ms": firstToken !== undefined ? Math.max(0, firstToken - started) : undefined,
      "error.type": outcome.status === "error" ? modelErrorType("code" in outcome ? outcome.code : undefined) ?? "api_error" : undefined,
    }, outcome.status);
  }

  private toolCall(state: SessionState, time: number, data: Payload, config: TraceOwner): void {
    const callId = text(data.callId);
    const name = text(data.name);
    if (!callId || !name) return;
    state.tools.set(callId, {
      spanId: newSpanId(), name, startMs: time, asked: false, skipped: DELEGATION_TOOLS.has(name),
      step: typeof data.step === "number" ? data.step : undefined,
      arguments: config.content && typeof data.arguments === "string" ? data.arguments.slice(0, MAX_CONTENT_CHARS) : undefined,
    });
  }

  private toolResult(sessionId: string, state: SessionState, time: number, data: Payload, config: TraceOwner): void {
    const message = record(data.message);
    const callId = text(message.toolCallId);
    const tool = callId ? state.tools.get(callId) : undefined;
    if (!callId || !tool) return;
    state.tools.delete(callId);
    const turn = state.turn;
    if (tool.skipped || !turn) return;
    const mcp = /^mcp__(.+?)__(.+)$/.exec(tool.name);
    const failed = message.isError === true;
    const code = text(record(data.error).code)?.toUpperCase();
    const errorType = !failed ? undefined : mcp && code?.includes("AUTH") ? "mcp_authentication_failed_error" : mcp && code?.includes("CONNECT") ? "mcp_connection_failed_error" : "tool_error";
    this.emit(state, {
      traceId: turn.traceId, spanId: tool.spanId, parentSpanId: turn.spanId, traceState: turn.traceState,
      name: mcp ? `oma.mcp_tool_use ${mcp[2]}` : `oma.tool_use ${tool.name}`, startMs: tool.dispatchMs ?? tool.startMs, endMs: time,
      attributes: {
        "gen_ai.operation.name": "execute_tool", "gen_ai.tool.name": mcp ? mcp[2] : tool.name, "gen_ai.tool.call.id": callId,
        "gen_ai.conversation.id": sessionId, "oma.mcp.server.name": mcp ? traceHooks.mcpServerName(mcp[1]!) ?? mcp[1] : undefined,
        "oma.tool.evaluated_permission": tool.asked ? "ask" : "allow", "oma.step": tool.step, "error.type": errorType,
        "gen_ai.tool.call.arguments": tool.arguments,
        "oma.tool_result.content": config.content ? contentText(message.content).slice(0, MAX_CONTENT_CHARS) : undefined,
      },
      status: failed ? "error" : "ok",
    });
  }

  private compaction(sessionId: string, state: SessionState, time: number, data: Payload): void {
    const start = state.compactionStart;
    state.compactionStart = undefined;
    if (start === undefined) return;
    const failed = data.error !== undefined || data.ok === false;
    this.child(state, "oma.compaction", start, time, { "gen_ai.conversation.id": sessionId, "error.type": failed ? "compaction_error" : undefined }, failed ? "error" : "ok");
  }

  private turnEnd(sessionId: string, state: SessionState, time: number, data: Payload): void {
    state.paused = false;
    if (!state.turn) return;
    const reason = record(data.reason);
    const kind = text(reason.kind) ?? "completed";
    this.endTurn(sessionId, state, time, STOP_REASONS[kind] ?? kind, kind === "error" ? modelErrorType(text(record(reason.error).code)) ?? "api_error" : undefined);
  }

  private endTurn(sessionId: string, state: SessionState, time: number, stopReason: string, errorType?: string): void {
    const turn = state.turn;
    state.turn = undefined;
    if (!turn?.sampled) return;
    const config = this.owners.get(state.owner);
    const subagent = this.subagents.get(sessionId);
    const status = stopReason === "end_turn" ? "ok" : errorType ? "error" : undefined;
    this.owners.get(state.owner)?.exporter.add({
      traceId: turn.traceId, spanId: turn.spanId, parentSpanId: turn.parentSpanId, traceState: turn.traceState,
      name: "oma.session.turn", startMs: turn.startMs, endMs: time, status,
      attributes: {
        "gen_ai.operation.name": "invoke_agent", "gen_ai.conversation.id": sessionId,
        "gen_ai.agent.name": subagent?.name, "oma.agent.inline": subagent?.inline ? true : undefined,
        "gen_ai.request.model": turn.model ?? config?.model, "oma.session.turn.stop_reason": stopReason, "error.type": errorType,
        "gen_ai.system_instructions": config?.content && turn.first && !state.parent && config.systemPrompt ? JSON.stringify([{ type: "text", content: config.systemPrompt.slice(0, MAX_CONTENT_CHARS) }]) : undefined,
        "gen_ai.input.messages": config?.content ? messages("user", turn.inputs) : undefined,
        "gen_ai.output.messages": config?.content && turn.output ? messages("assistant", [turn.output]) : undefined,
      },
    });
    if (stopReason !== "requires_action") void config?.exporter.flush();
  }
}
