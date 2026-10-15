import { isDeepStrictEqual } from "node:util";
import type { Context } from "@deepseek-ai/cordis";
import type { AssistantStreamFrame } from "@deepseek-ai/dsh-agent";
import type { SessionEvent } from "@deepseek-ai/dsh-session";
import { ACCOUNT_QUOTA_EXCEEDED_CODE, QUOTA_EXCEEDED_CODE } from "@deepseek-ai/dsh-llm";
import { SessionRuntime } from "../runtime/session.ts";
import { Subagents } from "../runtime/subagents.ts";
import { CcrSubagentBridge } from "./subagents.ts";
import type {
  SessionRuntimeConfig,
  ToolApproval,
  ApprovalDecision,
  ApprovedToolCall,
  ToolOutcome,
} from "../runtime/contracts.ts";
import type { CcrBackend } from "./contracts.ts";
import { CcrHeartbeat, type CcrHeartbeatConfig } from "./heartbeat.ts";
import { CcrActivity, type CcrActivityOptions } from "./activity.ts";
import { emptyState, object, type BrainState, type ThreadExecutionState, type Payload } from "./protocol.ts";
import { startDeliveryProgress } from "./delivery-progress.ts";
import { failureDiagnostic, workerDiagnostic } from "./diagnostics.ts";
import { assistantMessages, CcrAssistantStream } from "./assistant.ts";

export class CcrSession {
  state: BrainState = emptyState();
  private saves: Promise<unknown> = Promise.resolve();
  private statuses: Promise<unknown> = Promise.resolve();
  private publications: Promise<unknown> = Promise.resolve();
  private previews: Promise<unknown> = Promise.resolve();
  private inputReceipts: Promise<void> = Promise.resolve();
  private readonly controlReceipts = new Set<Promise<void>>();
  private readonly assistantStream: CcrAssistantStream;
  private turnErrorMessage?: string;
  private interrupted = false;
  readonly approvals = new Map<
    string,
    { resolve: (decision: Payload) => void; reject: (error: Error) => void; threadId: string; details: import("./wire.ts").RequiresActionDetails }
  >();
  readonly runtime: SessionRuntime;
  readonly subagents: Subagents;
  private readonly heartbeat?: CcrHeartbeat;
  private readonly activity: CcrActivity;
  private readonly defaultModel: string;
  private modelOverride?: string;
  constructor(
    readonly ctx: Context,
    readonly backend: CcrBackend,
    readonly config: SessionRuntimeConfig,
    heartbeat?: CcrHeartbeatConfig,
    keepAlive?: CcrActivityOptions,
  ) {
    this.defaultModel = config.model;
    this.assistantStream = new CcrAssistantStream(config.id, config.modelMessageID);
    this.runtime = new SessionRuntime(ctx, { ...config, setup: async (scope) => {
      await config.setup?.(scope);
      // Resumed dsh requests can use a logged model. Apply the CCR override
      // through the public request hook, without rewriting the durable log.
      scope.on("agent/request", async (_request, next) => {
        const call = await next();
        return this.modelOverride ? { ...call, model: this.modelOverride } : call;
      });
    } }, {
      setupTools: (scope) => this.subagents.register(scope),
      beforeRequest: async () => {
        await this.inputReceipts;
        await this.flushEvents();
      },
      onEvent: (event) => this.record(event),
      onStream: (frame) => this.stream(frame),
      stop: () => this.stop(),
    }, {
      blocked: () => this.blocked(),
      approve: (request, signal) => this.approve(request, signal),
      recordApproved: (call) => this.recordApproved(call),
      flushLog: () => this.runtime.flushLog(),
      recordDispatch: (callId) => this.recordDispatch(callId),
      recordUnknown: (callId) => this.recordUnknown(callId),
      recordResult: (outcome) => this.recordResult(outcome),
      stop: () => this.stop(),
    });
    this.subagents = new Subagents(this.runtime, new CcrSubagentBridge(this));
    this.activity = new CcrActivity(
      (signal) => this.backend.events(this.id, [{ type: "keep_alive" }], signal),
      this.abort.signal, keepAlive,
    );
    if (heartbeat) this.heartbeat = new CcrHeartbeat(heartbeat, this.abort.signal);
  }
  get id() {
    return this.config.id;
  }
  get agent() {
    return this.runtime.agent;
  }
  get handle() {
    return this.runtime.handle;
  }
  get abort() {
    return this.runtime.abort;
  }
  get busy() {
    return this.runtime.busy || this.subagents.busy;
  }
  get lastIdle() {
    return this.subagents.lastIdle;
  }

  async open(): Promise<void> {
    const startMs = Date.now();
    let stage = "worker_state";
    try {
      const { metadata, durationMs } = await this.backend.getWorkerState(this.id, undefined, this.abort.signal);
      this.abort.signal.throwIfAborted();
      const model = metadata?.model;
      if (typeof model === "string" && model.trim()) this.applyModel(model);
      await this.openState((next) => { stage = next; });
      this.abort.signal.throwIfAborted();
      workerDiagnostic("info", "ccrv2_worker_lifecycle_initialized", {
        session_id: this.id, duration_ms: Date.now() - startMs,
      });
      workerDiagnostic("info", "ccrv2_worker_state_restored", {
        session_id: this.id, duration_ms: durationMs, had_state: metadata !== null,
        restored_fields: this.modelOverride ? ["model"] : [],
      });
    } catch (error) {
      workerDiagnostic("error", "ccrv2_worker_lifecycle_init_failed", {
        session_id: this.id, duration_ms: Date.now() - startMs,
        reason: this.abort.signal.aborted ? "cancelled" : "initialization_failed",
        stage, ...failureDiagnostic(error),
      });
      this.stop();
      throw error;
    }
  }

  private applyModel(model: string): void {
    this.modelOverride = model;
    this.config.model = model;
    this.runtime.config.model = model;
    if (this.runtime.handle) this.agent.options.model = model;
  }

  private async openState(setStage: (stage: string) => void): Promise<void> {
    setStage("private_events");
    for (const payload of await this.backend.privateEvents(this.id)) {
      if (payload.subtype === "dsh_brain_state") {
        if (payload.schema_version !== 1 || payload.data?.version !== 1)
          throw new Error("Unsupported Brain state");
        if (payload.data.revision > this.state.revision)
          this.state = structuredClone(payload.data);
      }
      if (payload.subtype === "grok_checkpoint")
        throw new Error(
          "Grok sessions require explicit migration; automatic conversion is disabled",
        );
    }
    const interrupted = this.recoverExecutions();
    setStage("runtime_open");
    await this.runtime.open();
    setStage("subagents_restore");
    await this.subagents.restore();
    if (
      this.state.inputId &&
      (interrupted || (!this.state.completed.includes(this.state.inputId) &&
      !["completed", "idle"].includes(this.state.phase)))
    ) {
      const unknown =
        this.state.phase === "dispatch" || this.state.phase === "blocked";
      this.state.phase = unknown ? "blocked" : "completed";
      if (!this.state.completed.includes(this.state.inputId)) this.state.completed.push(this.state.inputId);
      this.state.completed.push(...(this.state.turnInputs ?? []).filter((id) => !this.state.completed.includes(id)));
      this.state.turnInputs = [];
      this.addError(
        "Previous turn was interrupted and was not replayed. Tool outcomes may be unknown.",
        "recovery",
      );
      setStage("recovery_persistence");
      await this.runtime.flushLog();
      await this.save();
    }
    // Clear a previous worker's progress even when recovery remains blocked.
    // Keep recovery persistence before metadata/status and terminal publication.
    setStage("report_metadata");
    await this.backend.reportMetadata(this.id, {
      task_summary: null, pending_action: null, model: this.config.model,
    });
    setStage("update_status");
    if (!this.blocked()) await this.updateStatus();
    setStage("publish");
    await this.publish();
  }

  private recoverExecutions(): boolean {
    if (!this.state.executions) {
      this.state.executions = {};
      const legacy = this.execution(this.id);
      legacy.phase = this.state.phase;
      legacy.approvedCalls = structuredClone(this.state.approvedCalls);
      legacy.toolOutcomes = structuredClone(this.state.toolOutcomes);
    }
    let interrupted = false;
    for (const entry of Object.values(this.state.executions)) {
      if (!["idle", "completed", "interrupted"].includes(entry.phase)) interrupted = true;
      for (const [callId, phase] of Object.entries(entry.callPhases ?? {})) {
        if (phase === "dispatch") { entry.callPhases![callId] = "blocked"; interrupted = true; }
        else if (phase === "approved") { entry.callPhases![callId] = "interrupted"; interrupted = true; }
      }
      if (["dispatch", "blocked"].includes(entry.phase) || Object.values(entry.callPhases ?? {}).includes("blocked")) entry.phase = "blocked";
      else if (!["idle", "completed", "interrupted"].includes(entry.phase)) entry.phase = "interrupted";
    }
    if (this.blocked()) this.state.phase = "blocked";
    return interrupted;
  }

  stream(frame: AssistantStreamFrame, threadId?: string): void {
    if (frame.type === "start") this.activity.start(threadId ?? this.id, "api_call", frame.attemptId);
    else if (frame.type === "end") this.activity.end(threadId ?? this.id, "api_call", frame.attemptId);
    const inputId = this.state.inputId;
    const payloads = this.assistantStream.record(frame, inputId, threadId);
    if (payloads.length && inputId) {
      for (const payload of payloads) {
        payload.uuid = crypto.randomUUID();
        if (threadId) payload.session_thread_id = threadId;
      }
      // Enqueue immediately so one 100ms window can hold multiple frames.
      // The uploader preserves order; this barrier still waits for every HTTP confirmation.
      this.previews = Promise.all([this.previews, this.publishPreview(payloads)]).then(() => {});
    }
  }

  private async publishPreview(payloads: Payload[]): Promise<void> {
    try { await this.backend.events(this.id, payloads); }
    catch { this.stop(); }
  }

  save(): Promise<void> {
    const snapshot = structuredClone({
      ...this.state,
      revision: ++this.state.revision,
    });
    const promise = this.saves.then(() =>
      this.backend.persist(this.id, "dsh_brain_state", snapshot),
    );
    this.saves = promise.catch(() => {});
    return promise;
  }

  record(event: SessionEvent, threadId?: string) {
    const data = event.data as Payload;
    if (event.type === "turn/end") {
      this.activity.endThread(threadId ?? this.id);
      const reason = event.data.reason;
      if (!threadId && reason?.kind === "error"
        && [QUOTA_EXCEEDED_CODE, ACCOUNT_QUOTA_EXCEEDED_CODE].includes(reason.error.code)) {
        this.turnErrorMessage = "Model provider balance or quota is exhausted. Top up the provider account or switch model credentials.";
        workerDiagnostic("error", "ccrv2_turn_failed", {
          session_id: this.id, input_id: this.state.inputId, failure_reason: "model_quota_exhausted",
        });
      }
    }
    else if (event.type === "tool/result") this.activity.end(threadId ?? this.id, "tool_exec", data.message.toolCallId);
    const uuid =
      event.type === "assistant/message"
        ? `${this.state.inputId}_${threadId ? `${threadId}_` : ""}message_${data.turn}_${data.step}`
        : `${this.state.inputId}_${threadId ? `${threadId}_` : ""}dsh_${event.seq}`;
    if (!this.state.inputId) return;
    if (event.type === "assistant/message") {
      this.state.outbox.push(...assistantMessages(this.id, uuid, event.data.message, threadId));
    } else if (event.type === "tool/result") {
      this.state.outbox.push({
        type: "user",
        uuid,
        ...(threadId ? { session_thread_id: threadId } : {}),
        message: {
          role: "user",
          content: [
            {
              type: "tool_result",
              tool_use_id: data.message.toolCallId,
              is_error: !!data.message.isError,
              content: data.message.content,
            },
          ],
        },
      });
    }
  }

  async approve(request: ToolApproval, signal: AbortSignal, threadId?: string): Promise<ApprovalDecision> {
    const requestId = crypto.randomUUID();
    const ownerId = threadId ?? this.id;
    const entry = this.execution(ownerId);
    if (["idle", "completed", "interrupted"].includes(entry.phase)) entry.phase = "accepted";
    const details = { tool_name: request.toolName, action_description: `Use ${request.toolName}`,
      tool_use_id: request.callId, request_id: requestId, input: request.input,
      ...(threadId ? { session_thread_id: threadId } : {}) };
    const decisionPromise = new Promise<Payload>((resolve, reject) => {
      this.approvals.set(requestId, { resolve, reject, threadId: ownerId, details });
    });
    decisionPromise.catch(() => {});
    const cancel = () =>
      this.approvals.get(requestId)?.reject(new Error("Approval cancelled"));
    signal.addEventListener("abort", cancel, { once: true });
    let decision: Payload;
    try {
      signal.throwIfAborted();
      await this.flushEvents();
      signal.throwIfAborted();
      await this.updateStatus();
      signal.throwIfAborted();
      await this.backend.events(this.id, [
        {
          type: "control_request",
          uuid: requestId,
          request_id: requestId,
          ...(threadId ? { session_thread_id: threadId } : {}),
          request: {
            subtype: "can_use_tool",
            tool_name: request.toolName,
            tool_use_id: request.callId,
            input: request.input,
          },
        },
      ]);
      decision = await decisionPromise;
      signal.throwIfAborted();
    } finally {
      signal.removeEventListener("abort", cancel);
      this.approvals.delete(requestId);
      await this.updateStatus();
    }
    return { allowed: decision.behavior === "allow", input: decision.updatedInput };
  }

  execution(threadId: string): ThreadExecutionState {
    const entries = this.state.executions ??= {};
    return entries[threadId] ??= { inputId: this.state.inputId, phase: "idle", approvedCalls: {}, toolOutcomes: {} };
  }

  beginThread(threadId: string): void {
    if (this.blocked()) throw new Error("Session requires tool outcome reconciliation");
    this.state.executions ??= {};
    this.state.executions[threadId] = { inputId: this.state.inputId, phase: "accepted", approvedCalls: {}, toolOutcomes: {} };
  }

  endThread(threadId: string, completed: boolean): void {
    this.activity.endThread(threadId);
    const entry = this.execution(threadId);
    if (!["blocked", "dispatch"].includes(entry.phase)) entry.phase = completed ? "completed" : "interrupted";
    this.syncPhase();
  }

  private syncPhase(fallback: BrainState["phase"] = "result"): void {
    const entries = Object.values(this.state.executions ?? {});
    if (this.state.phase === "blocked" || entries.some((entry) => entry.phase === "blocked")) this.state.phase = "blocked";
    else if (entries.some((entry) => entry.phase === "dispatch")) this.state.phase = "dispatch";
    else this.state.phase = fallback;
  }

  private syncCallPhase(entry: ThreadExecutionState): void {
    const phases = Object.values(entry.callPhases ?? {});
    if (entry.phase === "blocked" || phases.includes("blocked")) entry.phase = "blocked";
    else if (phases.includes("dispatch")) entry.phase = "dispatch";
    else if (phases.includes("approved")) entry.phase = "approved";
    else if (phases.includes("result")) entry.phase = "result";
    this.syncPhase(entry.phase === "approved" ? "accepted" : entry.phase === "interrupted" ? "completed" : entry.phase);
  }

  async recordApproved(call: ApprovedToolCall, threadId = this.id): Promise<void> {
    const entry = this.execution(threadId);
    const phases = entry.callPhases ??= {};
    if (this.blocked() || phases[call.callId] || entry.approvedCalls[call.callId] || entry.toolOutcomes[call.callId])
      throw new Error("Invalid or duplicate tool approval");
    phases[call.callId] = "approved";
    entry.approvedCalls[call.callId] = { name: call.name, original: call.original, effective: call.effective };
    if (threadId === this.id) this.state.approvedCalls[call.callId] = entry.approvedCalls[call.callId]!;
    this.syncCallPhase(entry);
    await this.save();
    this.activity.start(threadId, "tool_exec", call.callId);
  }

  async recordDispatch(callId: string, threadId = this.id): Promise<void> {
    const entry = this.execution(threadId);
    if (this.blocked() || entry.callPhases?.[callId] !== "approved" || !entry.approvedCalls[callId])
      throw new Error("Invalid or blocked tool dispatch");
    entry.callPhases[callId] = "dispatch";
    this.syncCallPhase(entry);
    await this.save();
  }

  async recordUnknown(callId: string, threadId = this.id): Promise<void> {
    const entry = this.execution(threadId);
    if (entry.callPhases?.[callId] !== "dispatch") throw new Error("Tool outcome does not match dispatch");
    this.activity.end(threadId, "tool_exec", callId);
    entry.callPhases[callId] = "blocked";
    this.syncCallPhase(entry);
    await this.save();
  }

  async recordResult(outcome: ToolOutcome, threadId = this.id): Promise<void> {
    const entry = this.execution(threadId);
    if (entry.callPhases?.[outcome.callId] !== "dispatch") throw new Error("Tool result does not match dispatch");
    this.activity.end(threadId, "tool_exec", outcome.callId);
    entry.callPhases[outcome.callId] = "result";
    entry.toolOutcomes[outcome.callId] = { text: outcome.text, failed: outcome.failed };
    if (threadId === this.id) this.state.toolOutcomes[outcome.callId] = entry.toolOutcomes[outcome.callId]!;
    this.syncCallPhase(entry);
    const changed = !isDeepStrictEqual(outcome.original, outcome.effective);
    if (changed) this.state.outbox.push({
      type: "system.message", uuid: `${this.state.inputId}_${threadId}_approved_${outcome.callId}`,
      ...(threadId === this.id ? {} : { session_thread_id: threadId }),
      content: [{ type: "text", text: `Approved ${outcome.name} arguments: ${JSON.stringify(outcome.effective)}` }],
    });
    await this.save();
  }

  /** Serialize writes and compute from current activity to avoid stale status overwrites. */
  updateStatus(): Promise<void> {
    const next = this.statuses.then(async () => {
      const pending = [...this.approvals.values()];
      const paused = new Set(pending.map((approval) => approval.threadId));
      const children = this.state.threads?.filter((thread) => thread.status === "running") ?? [];
      const childRunning = children.some((thread) => !paused.has(thread.id));
      const root = this.state.executions?.[this.id];
      const primaryRunning = !children.length && root && !["idle", "completed", "interrupted", "blocked"].includes(root.phase) && !paused.has(this.id);
      const status = childRunning || primaryRunning ? "running" : pending.length ? "requires_action" : "idle";
      const first = pending[0]?.details;
      await this.backend.setStatus(this.id, status, pending.length && first ? {
        ...first, ...(pending.length > 1 ? { pending_actions: pending.map((approval) => approval.details) } : {}),
      } : undefined);
    });
    this.statuses = next.catch(() => {});
    return next;
  }

  async control(payload: Payload): Promise<boolean> {
    if (payload.type === "control_response") {
      const response = object(payload.response);
      if (response.subtype === "success")
        this.approvals
          .get(response.request_id)
          ?.resolve(object(response.response));
      else
        this.approvals
          .get(response.request_id)
          ?.reject(new Error("Approval response failed"));
      return true;
    }
    if (["interrupt", "user.interrupt"].includes(payload.type)) {
      if (payload.session_thread_id !== this.id)
        this.subagents.interrupt(payload.session_thread_id);
      if (!payload.session_thread_id || payload.session_thread_id === this.id) {
        this.interrupted = true;
        this.agent.cancel({ kind: "user" });
      }
      return true;
    }
    if (payload.type === "control_request") {
      const subtype = payload.request?.subtype;
      let controlError: string | undefined;
      if (subtype === "set_model") {
        const requested = payload.request.model;
        const model = requested == null || requested === "default" ? this.defaultModel : requested;
        if (typeof model !== "string" || !model.trim()) controlError = "Invalid model";
        else {
          // Confirm the configuration before acknowledging its control request.
          await this.backend.reportMetadata(this.id, { model });
          this.applyModel(model);
        }
      }
      if (subtype === "interrupt") {
        this.interrupted = true;
        this.subagents.interrupt();
        this.agent.cancel({ kind: "user" });
      }
      const supported = !controlError && ["initialize", "interrupt", "set_model"].includes(subtype);
      await this.backend.events(this.id, [
        {
          type: "control_response",
          uuid: crypto.randomUUID(),
          response: {
            subtype: supported ? "success" : "error",
            request_id: payload.request_id,
            ...(supported
              ? { response: {} }
              : { error: controlError ?? "Unsupported Brain control request" }),
          },
        },
      ]);
      return true;
    }
    if (payload.type === "keep_alive") return true;
    return false;
  }

  queueInput(inputId: string, input: unknown): boolean {
    if (!this.runtime.acceptingInput || this.interrupted || this.blocked()
      || inputId === this.state.inputId || this.state.completed.includes(inputId)
      || this.state.turnInputs?.includes(inputId)) return false;
    const text = userInputText(object(input));
    (this.state.turnInputs ??= []).push(inputId);
    this.inputReceipts = this.inputReceipts.then(async () => {
      await this.save();
      await this.backend.reportDelivery(this.id, inputId, "processing", this.abort.signal);
    });
    void this.inputReceipts.catch(() => this.stop());
    this.runtime.steer(text);
    return true;
  }

  async receive(inputId: string, input: unknown): Promise<void> {
    const payload = object(input);
    if (await this.control(payload)) {
      // Control effects/publication finish first, but delivery retries must not
      // hold the control queue and prevent a later approval or interrupt.
      const receipt = this.backend.acknowledge(this.id, inputId, this.abort.signal)
        .catch(() => { if (!this.abort.signal.aborted) this.stop(); })
        .finally(() => { this.controlReceipts.delete(receipt); });
      this.controlReceipts.add(receipt);
      return;
    }
    if (this.state.completed.includes(inputId)) {
      await this.publish();
      await this.backend.acknowledge(this.id, inputId, this.abort.signal);
      return;
    }
    if (this.state.phase === "blocked")
      throw new Error("Session requires operator reconciliation");
    if (this.busy || this.abort.signal.aborted)
      throw new Error("Session is busy or stopped");
    const text = userInputText(payload);
    let stopProgress = () => {};
    try {
      await this.runtime.run(text, {
        beforeRun: async () => {
          this.turnErrorMessage = undefined;
          this.interrupted = false;
          this.state.inputId = inputId;
          this.state.turnInputs = [];
          this.state.phase = "accepted";
          this.state.approvedCalls = {};
          this.state.toolOutcomes = {};
          this.state.executions = {};
          this.beginThread(this.id);
          await this.save();
          await this.backend.reportDelivery(this.id, inputId, "processing", this.abort.signal);
          stopProgress = startDeliveryProgress(
            () => this.backend.reportDelivery(this.id, inputId, "processing", this.abort.signal),
            this.abort.signal, () => this.stop(), undefined, false,
          );
          await this.updateStatus();
          return !this.interrupted;
        },
        afterRun: async (completed) => {
          await this.inputReceipts;
          const cancelled = this.interrupted && !this.blocked();
          const failed = this.blocked() || (!completed && !cancelled);
          if (failed)
            this.addError(this.turnErrorMessage ?? "The turn stopped before successful completion.", "failed");
          else
            this.state.outbox.push({
              type: "result",
              uuid: `${inputId}_result`,
              subtype: "success",
              is_error: false,
            });
          this.endThread(this.id, completed);
          if (!this.blocked()) this.state.phase = "completed";
          this.state.completed.push(inputId, ...(this.state.turnInputs ?? []));
          this.state.turnInputs = [];
          await this.save();
          await this.previews;
          await this.publish();
          await this.updateStatus();
          stopProgress();
          await this.backend.acknowledge(this.id, inputId, this.abort.signal);
        },
      });
    } finally { stopProgress(); }
  }

  addError(text: string, suffix = "failed") {
    const id = `${this.state.inputId}_${suffix}`;
    this.state.outbox.push(
      {
        type: "system.message",
        uuid: `${id}_message`,
        content: [{ type: "text", text }],
      },
      {
        type: "result",
        uuid: `${id}_result`,
        subtype: "error_during_execution",
        is_error: true,
      },
    );
  }
  async flushEvents() {
    await this.subagents.flush();
    await this.runtime.flushLog();
    await this.save();
    await this.previews;
    await this.publish();
  }
  publish(): Promise<void> {
    const next = this.publications.then(async () => {
      if (!this.state.outbox.length) return;
      const pending = this.state.outbox.slice();
      if (!pending.length) return;
      await this.backend.events(this.id, pending);
      this.state.outbox.splice(0, pending.length);
      await this.save();
    });
    this.publications = next.catch(() => {});
    return next;
  }
  stop() {
    this.subagents.stop();
    this.runtime.stop();
    for (const pending of this.approvals.values())
      pending.reject(new Error("Session stopped"));
  }
  blocked() {
    return this.state.phase === "blocked" || Object.values(this.state.executions ?? {}).some((entry) => entry.phase === "blocked");
  }
  async close() {
    this.stop();
    // Drain children before disposing the root agent and its shared providers.
    const results = await Promise.allSettled([this.subagents.close(), this.heartbeat?.close(), this.activity.close()]);
    results.push(...await Promise.allSettled([this.runtime.close()]));
    // Do not let an old generation's queued writes outlive host release/forget.
    await Promise.allSettled([this.saves, this.statuses, this.publications, this.previews, this.inputReceipts, ...this.controlReceipts]);
    const failed = results.find((result) => result.status === "rejected");
    if (failed?.status === "rejected") throw failed.reason;
  }
}

function userInputText(payload: Payload): string {
  const content = payload.message?.content ?? payload.content;
  const text =
    typeof content === "string"
    ? content
    : Array.isArray(content) &&
      content.every((b: Payload) => b.type === "text")
      ? content.map((b: Payload) => b.text).join("\n")
      : undefined;
  if (typeof text !== "string")
    throw new Error("Only text user input is supported");
  return text;
}
