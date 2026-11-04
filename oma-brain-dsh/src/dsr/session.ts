import type { Context } from "@deepseek-ai/cordis";
import type { AssistantStreamFrame } from "@deepseek-ai/dsh-agent";
import { SessionId, appendPluginRecord, pluginRecordOf, type Session, type SessionEvent } from "@deepseek-ai/dsh-session";
import { SessionRuntime } from "../runtime/session.ts";
import { Subagents } from "../runtime/subagents.ts";
import type { ApprovalDecision, SessionRuntimeConfig, ToolApproval, ToolExecutionBridge } from "../runtime/contracts.ts";
import { AssistantStream, assistantMessages, type AssistantBlockOutput } from "../runtime/assistant.ts";
import { userInputContent } from "../runtime/user-content.ts";
import { appendedSystemContext } from "../runtime/system-context.ts";
import { traceHooks } from "../observability/trace-context.ts";
import { withAttachmentSession } from "../attachments/remote.ts";
import { dsrExecutor, DsrSuspend } from "./executor.ts";
import { DsrError, delay, object } from "./http.ts";
import { dsrToolIdentity } from "./tools.ts";
import { DsrSubagentBridge } from "./subagents.ts";
import type { DsrClient } from "./client.ts";
import type { Decision, Finish, Input, PublicEvent } from "./wire.ts";

declare module "@deepseek-ai/dsh-session/types" {
  interface PluginRecordMap {
    "plugin:oma/dsr-finish": { version: 2; inputId: string; completion: Finish };
  }
}

export class DsrSession {
  readonly runtime: SessionRuntime;
  readonly subagents: Subagents;
  inputId = "";
  suspended = false;
  private publications: Promise<void> = Promise.resolve();
  private pending: PublicEvent[] = [];
  private readonly assistantIDs = new Set<string>();
  private readonly approvals = new Map<string, Promise<ApprovalDecision>>();
  private readonly decisions = new Map<string, Decision>();
  private readonly assistant: AssistantStream;
  private failure?: unknown;
  private readonly defaultModel: string;
  private reportedTraceTurns?: string;
  private ending?: Extract<SessionEvent, { type: "turn/end" }>;

  constructor(readonly ctx: Context, readonly client: DsrClient, readonly config: SessionRuntimeConfig, readonly options: { approvalWaitMs?: number } = {}) {
    this.defaultModel = config.model;
    this.assistant = new AssistantStream(config.id, config.modelMessageID, config.modelRequestID);
    this.runtime = new SessionRuntime(ctx, { ...config, preserveOpenTurns: true, cancelRestoredTurn: () => this.client.inputCancelled(this.inputId) }, {
      setupTools: async scope => { scope.tools.setExecutor(dsrExecutor(scope, this)); await this.subagents.register(scope); },
      onEvent: (event, session) => this.record(event, session),
      onStream: frame => this.stream(frame),
      beforeRequest: () => this.checkpoint(),
      afterFlushLog: () => this.checkpoint(),
      waitForChildren: () => this.subagents.wait(),
      stop: () => this.stop(),
    }, this.execution());
    this.subagents = new Subagents(this.runtime, new DsrSubagentBridge(this));
  }
  get id(): string { return this.config.id; }
  get abort(): AbortController { return this.runtime.abort; }
  get busy(): boolean { return this.runtime.busy || this.subagents.busy; }
  get lastIdle(): number { return this.subagents.lastIdle; }

  async open(): Promise<void> {
    const state = await this.client.workerState(this.abort.signal);
    if (state.model) this.config.model = this.runtime.config.model = state.model;
    traceHooks.turnsInterrupted(this.id, state.trace_turns ?? undefined);
    await this.ctx.sessionReplication.ensure(this.id);
    await this.subagents.loadThreads();
    await this.runtime.open();
    for (const event of this.runtime.agent.session.snapshotEvents()) this.record(event, this.runtime.agent.session);
    await this.checkpoint();
  }

  async checkpoint(): Promise<void> {
    if (this.failure) throw this.failure;
    if (this.runtime.handle) await this.ctx.sessions.flush(this.runtime.agent.session);
    await this.ctx.sessionReplication.flush(this.id);
    const pending = this.pending.splice(0);
    if (pending.length) this.publications = this.publications.then(async () => {
      for (let i = 0; i < pending.length; i += 200) await this.client.publish(pending.slice(i, i + 200), this.abort.signal);
    });
    await this.publications;
    const turns = traceHooks.openTurns(this.id) ?? null;
    const encoded = JSON.stringify(turns);
    if (encoded !== this.reportedTraceTurns) {
      await this.client.patchWorkerState({ trace_turns: turns }, this.abort.signal);
      this.reportedTraceTurns = encoded;
    }
  }

  record(event: SessionEvent, session: Session): void {
    const thread = session.id === this.id ? undefined : session.id;
    if (!thread && event.type === "turn/end") this.ending = event;
    if (event.type === "assistant/message" && !event.data.interrupted) {
      const inputID = thread ? this.id : inputIDForTurn(session.snapshotEvents(), event.data.turn) ?? this.inputId;
      const fallback = `${inputID}_${thread ? `${thread}_` : ""}message_${event.data.turn}_${event.data.step}`;
      const outputs = assistantMessages(this.id, fallback, event.data.message, thread);
      for (const payload of this.assistant.complete(outputs, thread)) this.queueAssistant(payload as PublicEvent);
    }
    const record = pluginRecordOf(event);
    if (record?.type === "plugin:oma/assistant-block") this.queueAssistant((record.data as { output: PublicEvent }).output);
    if (event.type === "tool/result" && event.surfaceOp === "append") {
      const call = session.snapshotEvents().find(item => item.type === "tool/call" && item.data.callId === event.data.message.toolCallId);
      const name = call?.type === "tool/call" ? call.data.name : session.snapshotEvents().flatMap(item => item.type === "assistant/message" ? item.data.message.content : [])
        .find(block => block.type === "tool-call" && block.id === event.data.message.toolCallId);
      const toolName = typeof name === "string" ? name : name?.type === "tool-call" ? name.name : undefined;
      if (!toolName) throw new Error("Tool result has no call identity");
      this.pending.push({ id: `${session.id}_result_${event.seq}`, type: "tool.result", tool_use_id: event.data.message.toolCallId,
        tool: dsrToolIdentity(toolName), is_error: !!event.data.message.isError, content: [...event.data.message.content], ...(thread ? { session_thread_id: thread } : {}) });
    }
  }

  private queueAssistant(event: PublicEvent): void {
    if (this.assistantIDs.has(event.id)) return;
    this.assistantIDs.add(event.id);
    this.pending.push(event);
  }

  stream(frame: AssistantStreamFrame, thread?: string): void {
    for (const payload of this.assistant.record(frame, thread ? this.id : this.inputId, thread)) {
      if (payload.type === "event_start" || payload.type === "event_delta") {
        const preview = { ...payload, id: crypto.randomUUID(), type: payload.type === "event_start" ? "preview.start" : "preview.delta", ...(thread ? { session_thread_id: thread } : {}) } as PublicEvent;
        this.publications = this.publications.then(() => this.client.publish([preview], this.abort.signal));
        void this.publications.catch(error => { this.failure = error; this.stop(); });
      } else {
        const session = thread ? this.ctx.sessions.get(SessionId(thread)) : this.runtime.agent.session;
        if (!session) throw new Error("Assistant block owner is unavailable");
        appendPluginRecord(session, "plugin:oma/assistant-block", { output: payload as AssistantBlockOutput });
      }
    }
  }

  async receive(input: Extract<Input, { lane: "task" }>): Promise<void> {
    this.inputId = input.id;
    traceHooks.inputReceived(this.id, input.trace_context);
    const session = this.runtime.agent.session;
    const saved = session.snapshotEvents().map(pluginRecordOf).find(record => record?.type === "plugin:oma/dsr-finish" && (record.data as { inputId: string }).inputId === input.id);
    if (saved) {
      const data = saved.data as { version: number; completion: Finish };
      if (data.version !== 2) throw new DsrError(0, "unsupported_finish_version");
      await this.finish(input.id, data.completion);
      return;
    }
    const state = await this.client.processing(input.id, this.abort.signal);
    if (state === "completed") { await this.client.delivery([{ id: input.id, status: "processed" }], this.abort.signal); return; }
    if (this.client.inputCancelled(input.id)) {
      await this.subagents.restore();
      this.runtime.cancelOpenTurn();
      await this.completeCancelled(input.id);
      return;
    }
    try { await this.subagents.restore(); }
    catch (error) { if (error instanceof Error && "code" in error && error.code === "OMA_DSR_SUSPEND") { this.suspended = true; return; } throw error; }
    const events = session.snapshotEvents();
    const admitted = events.find(event => event.type === "user/message" && event.data.id === input.id);
    const turn = inputTurn(events, input.id);
    const completed = turn === undefined ? undefined : events.findLast(event => event.type === "turn/end" && event.data.turn === turn);
    if (completed?.type === "turn/end") { await this.complete(input.id, completed); return; }
    const open = openTurn(events);
    if (open !== undefined && !admitted) throw new DsrError(0, "input_history_mismatch");
    this.ending = undefined;
    await withAttachmentSession(this.id, () => this.runtime.run(async () => userInputContent(input, this.ctx.attachments), {
      messageId: input.id, continuation: open !== undefined,
      beforeRun: async () => {
        const context = appendedSystemContext(input);
        if (context !== undefined) this.runtime.appendedSystem = context;
        if (input.oma_refresh_tools) await this.runtime.refreshTools();
      },
      afterRun: async () => {
        if (openTurn(session.snapshotEvents()) !== undefined) { this.suspended = true; return; }
        if (!this.ending) throw new Error("Native turn has no durable ending");
        await this.complete(input.id, this.ending);
      },
    }));
  }

  private async completeCancelled(id: string): Promise<void> {
    const completion = { result: "cancelled" } as const;
    appendPluginRecord(this.runtime.agent.session, "plugin:oma/dsr-finish", { version: 2, inputId: id, completion });
    await this.checkpoint();
    await this.finish(id, completion);
  }

  private async complete(id: string, ending: Extract<SessionEvent, { type: "turn/end" }>): Promise<void> {
    if (await this.hasOpenChildren()) { this.suspended = true; return; }
    const reason = ending.data.reason;
    const completion: Finish = reason.kind === "completed" ? { result: "success" }
      : reason.kind === "aborted" && reason.reason.kind === "user" ? { result: "cancelled" }
      : { result: "failed", failure_code: "execution_failed" };
    appendPluginRecord(this.runtime.agent.session, "plugin:oma/dsr-finish", { version: 2, inputId: id, completion });
    await this.checkpoint();
    await this.finish(id, completion);
  }

  private async finish(id: string, completion: Finish): Promise<void> {
    await this.client.finish(id, completion, this.abort.signal);
    await this.client.delivery([{ id, status: "processed" }], this.abort.signal);
  }

  async control(input: Extract<Input, { lane: "control" }>, sequence?: string): Promise<void> {
    let error: "unsupported_input" | "invalid_model" | undefined;
    switch (input.kind) {
      case "permission.resolved": this.decisions.set(input.request_id, input.resolution); break;
      case "user.interrupt":
        if (!input.session_thread_id || input.session_thread_id === this.id) this.runtime.agent.cancel({ kind: "user" });
        if (input.session_thread_id !== this.id) this.subagents.interrupt(input.session_thread_id);
        break;
      case "session.set_model": {
        const context = await this.client.context(undefined, this.abort.signal);
        const models = object(context.models);
        if (input.model !== null && !Object.hasOwn(models, input.model)) { error = "invalid_model"; break; }
        if (!sequence) throw new DsrError(0, "control_sequence_required");
        const state = await this.client.patchWorkerState({ model: input.model, control_input_id: input.id, control_sequence: sequence }, this.abort.signal);
        const model = state.model ?? this.defaultModel;
        this.runtime.agent.model.select({ provider: this.config.provider, model });
        this.runtime.config.model = model;
        break;
      }
      default: error = "unsupported_input";
    }
    await this.client.delivery([{ id: input.id, status: "processed", ...(error ? { error } : {}) }], this.abort.signal);
  }

  execution(thread?: string): ToolExecutionBridge {
    return { approve: (request, signal) => this.approve(request, signal, thread), runTool: async (_id, execute) => { await this.client.ready(); return execute(); },
      approvalChanged: async (name, callId, effective) => { this.pending.push({ id: `${thread ?? this.id}_${callId}_approved`, type: "system.message", content: [{ type: "text", text: `Approved ${name} arguments: ${JSON.stringify(effective)}` }], ...(thread ? { session_thread_id: thread } : {}) }); await this.checkpoint(); } };
  }

  approve(request: ToolApproval, signal: AbortSignal, thread?: string): Promise<ApprovalDecision> {
    const key = `${thread ?? this.id}:${request.callId}`;
    let task = this.approvals.get(key);
    if (!task) { task = this.requestApproval(request, signal, thread); this.approvals.set(key, task); }
    return task;
  }

  private async requestApproval(request: ToolApproval, signal: AbortSignal, thread?: string): Promise<ApprovalDecision> {
    const session = thread ? this.ctx.sessions.get(SessionId(thread)) : this.runtime.agent.session;
    if (!session) throw new Error("Approval owner is unavailable");
    const turn = openTurn(session.snapshotEvents());
    if (turn === undefined) throw new Error("Approval requires an open native turn");
    const body = { input_id: this.inputId, turn: String(turn), dsh_session_id: thread ?? this.id, tool_use_id: request.callId,
      tool: dsrToolIdentity(request.toolName), input: request.input, ...(request.available === undefined ? {} : { available: request.available }) };
    traceHooks.approvalPending(thread ?? this.id, request.callId);
    const deadline = Date.now() + (this.options.approvalWaitMs ?? 60_000);
    for (;;) {
      const response = await this.client.permission(body, signal);
      const decision = response.state === "resolved" ? response.decision : this.decisions.get(response.request_id);
      if (decision) {
        traceHooks.approvalResolved(thread ?? this.id, request.callId, decision.behavior === "external_result" ? "external" : decision.behavior === "allow" ? "allowed" : "denied");
        return approvalDecision(decision);
      }
      if (response.state === "cancelled" || response.state === "closed") return { allowed: false, reason: "Tool approval is closed" };
      if (Date.now() >= deadline) throw new DsrSuspend("Approval is pending; preserve the native turn");
      await delay(this.client.transport, 1000, signal);
    }
  }

  private async hasOpenChildren(): Promise<boolean> {
    for (const thread of this.subagents.threads) {
      const handle = await this.ctx.sessionPersistence.open(SessionId(thread.id), "read");
      try { if (openTurn((await handle.read()).events) !== undefined) return true; }
      finally { await handle.close(); }
    }
    return false;
  }

  async publish(event: PublicEvent): Promise<void> { this.pending.push(event); await this.checkpoint(); }
  stop(): void { this.runtime.stop(); this.subagents.stop(); }
  async close(): Promise<void> { await this.subagents.close(); await this.runtime.close(); await this.publications.catch(() => {}); }
}

export function openTurn(events: readonly SessionEvent[]): number | undefined {
  let turn: number | undefined;
  for (const event of events) { if (event.type === "turn/start") turn = event.data.turn; if (event.type === "turn/end") turn = undefined; }
  return turn;
}

function approvalDecision(value: Decision): ApprovalDecision {
  const decision = object(value);
  if (decision.behavior === "allow") return { allowed: true, ...(value.behavior === "allow" && value.updated_input ? { input: value.updated_input } : {}) };
  if (value.behavior === "external_result") return { allowed: true, externalResult: value.external_result };
  return { allowed: false, reason: value.behavior === "deny" ? value.message : "Tool denied" };
}

function inputTurn(events: readonly SessionEvent[], id: string): number | undefined {
  let turn: number | undefined;
  for (const event of events) {
    if (event.type === "turn/start") turn = event.data.turn;
    if (event.type === "user/message" && event.data.id === id) return turn;
  }
  return undefined;
}

function inputIDForTurn(events: readonly SessionEvent[], target: number): string | undefined {
  let turn: number | undefined;
  for (const event of events) {
    if (event.type === "turn/start") turn = event.data.turn;
    if (event.type === "user/message" && turn === target && event.data.source.kind === "user") return event.data.id;
  }
  return undefined;
}
