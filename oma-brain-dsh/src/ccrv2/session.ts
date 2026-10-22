import type { Context } from "@deepseek-ai/cordis";
import type { AssistantStreamFrame } from "@deepseek-ai/dsh-agent";
import { SessionId, pluginRecordOf, type SessionEvent } from "@deepseek-ai/dsh-session";
import { ACCOUNT_QUOTA_EXCEEDED_CODE, QUOTA_EXCEEDED_CODE } from "@deepseek-ai/dsh-llm";
import { ToolNotForwardedError } from "../runtime/contracts.ts";
import { SessionRuntime } from "../runtime/session.ts";
import { Subagents } from "../runtime/subagents.ts";
import { CcrSubagentBridge } from "./subagents.ts";
import type {
  SessionRuntimeConfig,
  ToolApproval,
  ApprovalDecision,
} from "../runtime/contracts.ts";
import type { CcrBackend } from "./contracts.ts";
import { CcrHeartbeat, type CcrHeartbeatConfig } from "./heartbeat.ts";
import { CcrActivity, type CcrActivityOptions } from "./activity.ts";
import { isControlInput, object, type Payload } from "./protocol.ts";
import { startDeliveryProgress } from "./delivery-progress.ts";
import { failureDiagnostic, workerDiagnostic } from "./diagnostics.ts";
import { nativeOutput, failedOutput } from "./native-output.ts";
import { CcrToolResults } from "./tool-results.ts";
import { CcrAssistantStream } from "./assistant.ts";
import { userInputContent } from "./user-content.ts";
import { appendedSystemContext } from "./system-context.ts";

export class CcrSession {
  inputId = "";
  readonly receivedInputIds = new Set<string>();
  pendingEvents: Payload[] = [];
  private opening = true;
  private inputFinished = false;
  restoredFromRemote = false;
  private closing?: Promise<void>;
  private readonly outcomeConfirmations = new Set<Promise<void>>();
  private readonly activeDispatches = new Map<string, Set<string>>();
  private statuses: Promise<unknown> = Promise.resolve();
  private publications: Promise<unknown> = Promise.resolve();
  private previews: Promise<unknown> = Promise.resolve();
  private readonly controlReceipts = new Set<Promise<void>>();
  private readonly handledControls = new Set<string>();
  private readonly assistantStream: CcrAssistantStream;
  private readonly toolResults = new CcrToolResults();
  private turnErrorMessage?: string;
  private budgetPaused = false;
  private interrupted = false;
  private draining = false;
  private readonly dispatchWaiters = new Set<() => void>();
  private waitingOnInterruptedChild = false;
  private readonly interruptedApprovals = new Set<string>();
  readonly approvals = new Map<
    string,
    { phase: "policy" | "confirmation" | "resolved"; resolve: (decision: Payload) => void; reject: (error: Error) => void; threadId: string; details: import("./wire.ts").RequiresActionDetails }
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
      onExternalMcpConnectionFailure: (serverName) => this.reportError({
        type: "session.error", uuid: crypto.randomUUID(), processed_at: new Date().toISOString(),
        error: { type: "mcp_connection_failed_error", mcp_server_name: serverName,
          message: "MCP server connection failed during tool discovery.", retry_status: { type: "exhausted" } },
      }),
      onExternalMcpAuthenticationFailure: (serverName, phase) => this.reportError({
        type: "session.error", uuid: phase === "tool_call" ? crypto.randomUUID() : `${this.id}_mcp_auth_${serverName}`,
        processed_at: new Date().toISOString(),
        error: {
          type: "mcp_authentication_failed_error", mcp_server_name: serverName,
          message: phase === "tool_call" ? "MCP server authentication failed during tool execution." : "MCP server authentication failed during tool discovery.",
          retry_status: { type: "exhausted" },
        },
      }),
      setupTools: (scope) => this.subagents.register(scope),
      beforeRequest: () => this.beforePrimaryRequest(),
      onContextCompacted: tokens => this.contextCompacted(tokens),
      onEvent: (event) => this.record(event),
      onStream: (frame) => this.stream(frame),
      stop: () => this.stop(),
    }, {
      approve: (request, signal) => this.approve(request, signal),
      runTool: (callId, execute, readOnly) => this.runTool(this.id, callId, execute, readOnly),
      approvalChanged: (name, callId, effective) => this.approvalChanged(this.id, name, callId, effective),
    });
    this.subagents = new Subagents(this.runtime, new CcrSubagentBridge(this));
    this.activity = new CcrActivity(
      (signal) => this.backend.events(this.id, [{ type: "keep_alive" }], signal),
      this.abort.signal, keepAlive,
    );
    if (heartbeat) this.heartbeat = new CcrHeartbeat(heartbeat, this.abort.signal);
  }
  get restoring(): boolean { return this.opening; }
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
    setStage("local_history");
    this.restoredFromRemote = await this.ctx.ccrReplication.ensure(this.id);
    setStage("runtime_open");
    await this.runtime.open();
    setStage("subagents_restore");
    await this.subagents.restore();
    const recoveryEvents = this.pendingEvents.splice(0);
    this.replay(this.agent.session.snapshotEvents());
    for (const thread of this.subagents.threads) {
      const handle = await this.ctx.sessionPersistence.open(SessionId(thread.id), "read");
      try { this.replay((await handle.read()).events, thread.id); }
      finally { await handle.close(); }
    }
    this.pendingEvents.push(...recoveryEvents);
    this.opening = false;
    await this.checkpoint();
    setStage("report_metadata");
    await this.backend.reportMetadata(this.id, { task_summary: null, pending_action: null, model: this.config.model });
    setStage("publish");
    await this.publish();
    setStage("update_status");
    await this.updateStatus();
  }

  stream(frame: AssistantStreamFrame, threadId?: string): void {
    if (frame.type === "start") this.activity.start(threadId ?? this.id, "api_call", frame.attemptId);
    else if (frame.type === "end") this.activity.end(threadId ?? this.id, "api_call", frame.attemptId);
    const inputId = threadId ? this.id : this.inputId;
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

  private async reportError(payload: Payload): Promise<void> {
    this.pendingEvents.push(payload);
    if (this.handle) await this.publish();
  }

  private async publishPreview(payloads: Payload[]): Promise<void> {
    try { await this.checkpoint(); await this.backend.events(this.id, payloads); }
    catch { this.stop(); }
  }

  async checkpoint(): Promise<void> {
    await this.runtime.flushLog();
    await this.ctx.ccrReplication.flush(this.id);
  }

  private replay(events: readonly SessionEvent[], threadId?: string): void {
    const tools = new CcrToolResults();
    let inputId = "";
    let inputSeen = false;
    for (const event of events) {
      if (event.type === "turn/start") { inputSeen = false; inputId = ""; }
      if (!threadId && !inputSeen && event.type === "user/message" && event.data.source.kind === "user") {
        inputSeen = true;
        inputId = event.data.id;
        this.receivedInputIds.add(inputId);
        this.inputId = inputId;
      }
      this.pendingEvents.push(...nativeOutput(this.id, inputId, event, tools.record(event, threadId ?? this.id), threadId));
    }
  }

  async contextCompacted(tokens?: number, threadId?: string): Promise<void> {
    this.pendingEvents.push({ type: "agent.thread_context_compacted", uuid: crypto.randomUUID(),
      ...(threadId ? { session_thread_id: threadId } : {}),
      ...(tokens !== undefined ? { pre_compaction_tokens: tokens } : {}) });
    await this.flushEvents();
  }

  record(event: SessionEvent, threadId?: string) {
    if (this.opening || pluginRecordOf(event)) return;
    if (!threadId && event.type === "user/message" && event.data.source.kind === "user" && event.data.id === this.inputId)
      this.receivedInputIds.add(event.data.id);
    if (event.type === "tool/result" && event.surfaceOp !== "append") return;
    const toolName = this.toolResults.record(event, threadId ?? this.id);
    const data = event.data as Payload;
    if (event.type === "turn/end") {
      if (!threadId) this.inputFinished = true;
      this.activity.endThread(threadId ?? this.id);
      const reason = event.data.reason;
      if (reason?.kind === "error" && reason.error.code === "SESSION_BUDGET_REACHED") this.budgetPaused = true;
      if (!threadId && reason?.kind === "error" && reason.error.code === "MODEL_PRICE_UNAVAILABLE")
        this.turnErrorMessage = "Model pricing is unavailable. Retry after pricing recovers.";
      if (!threadId && reason?.kind === "error"
        && [QUOTA_EXCEEDED_CODE, ACCOUNT_QUOTA_EXCEEDED_CODE].includes(reason.error.code)) {
        this.turnErrorMessage = "Model provider balance or quota is exhausted. Top up the provider account or switch model credentials.";
        workerDiagnostic("error", "ccrv2_turn_failed", {
          session_id: this.id, input_id: this.inputId, failure_reason: "model_quota_exhausted",
        });
      }
    }
    else if (event.type === "tool/result") {
      const owner = threadId ?? this.id;
      const callId = data.message.toolCallId;
      this.activity.end(owner, "tool_exec", callId);
      const confirmation = Promise.resolve().then(() => this.publish()).catch(() => { this.stop(); }).finally(() => {
        this.outcomeConfirmations.delete(confirmation);
        const calls = this.activeDispatches.get(owner);
        calls?.delete(callId);
        if (!calls?.size) this.activeDispatches.delete(owner);
        for (const done of [...this.dispatchWaiters]) done();
      });
      this.outcomeConfirmations.add(confirmation);
    }
    if (threadId || event.type !== "turn/end" || !this.budgetPaused)
      this.pendingEvents.push(...nativeOutput(this.id, this.inputId, event, toolName, threadId));
  }

  async approve(request: ToolApproval, signal: AbortSignal, threadId?: string): Promise<ApprovalDecision> {
    const requestId = crypto.randomUUID();
    const ownerId = threadId ?? this.id;
    const details = { tool_name: request.toolName, action_description: `Use ${request.toolName}`,
      tool_use_id: request.callId, request_id: requestId, input: request.input,
      ...(threadId ? { session_thread_id: threadId } : {}) };
    const decisionPromise = new Promise<Payload>((resolve, reject) => {
      this.approvals.set(requestId, { phase: "policy", resolve, reject, threadId: ownerId, details });
    });
    decisionPromise.catch(() => {});
    const cancel = () =>
      this.approvals.get(requestId)?.reject(new Error(this.interruptedApprovals.has(ownerId) ? "Tool execution was interrupted before completion. Please retry." : "Approval cancelled"));
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
            ...(request.available === false ? { tool_available: false } : {}),
          },
        },
      ]);
      if (this.approvals.get(requestId)?.phase === "policy") {
        const lookup = new AbortController();
        let pending: boolean;
        try {
          pending = await Promise.race([
            this.backend.isToolConfirmationPending(this.id, requestId, AbortSignal.any([signal, lookup.signal])),
            decisionPromise.then(() => false, () => false),
          ]);
        } finally {
          lookup.abort();
        }
        const approval = this.approvals.get(requestId);
        if (pending && approval?.phase === "policy") {
          approval.phase = "confirmation";
          await this.updateStatus();
        }
      }
      decision = await decisionPromise;
      signal.throwIfAborted();
    } finally {
      signal.removeEventListener("abort", cancel);
      this.approvals.delete(requestId);
      await this.updateStatus();
    }
    return { allowed: decision.behavior === "allow", input: decision.updatedInput,
      ...(decision.externalResult !== undefined ? { externalResult: validatedExternalToolResult(decision.externalResult) } : {}),
      reason: typeof decision.message === "string" ? decision.message :
        typeof decision.denyMessage === "string" ? decision.denyMessage : undefined };
  }

  async runTool<T>(threadId: string, callId: string, execute: () => Promise<T>, readOnly = false): Promise<T> {
    if (this.draining && !readOnly) throw new ToolNotForwardedError("Brain worker is shutting down; the tool was not executed.");
    const calls = this.activeDispatches.get(threadId) ?? new Set<string>();
    if (!readOnly) {
      this.activeDispatches.set(threadId, calls);
      calls.add(callId);
    }
    this.activity.start(threadId, "tool_exec", callId);
    try { await this.checkpoint(); return await execute(); }
    finally {
      this.activity.end(threadId, "tool_exec", callId);
      for (const done of [...this.dispatchWaiters]) done();
    }
  }

  async approvalChanged(threadId: string, name: string, callId: string, effective: Payload): Promise<void> {
    this.pendingEvents.push({
      type: "system.message", uuid: `${this.inputId}_${threadId}_approved_${callId}`,
      ...(threadId === this.id ? {} : { session_thread_id: threadId }),
      content: [{ type: "text", text: `Approved ${name} arguments: ${JSON.stringify(effective)}` }],
    });
    await this.publish();
  }

  /** Serialize writes and compute from current activity to avoid stale status overwrites. */
  updateStatus(): Promise<void> {
    const next = this.statuses.then(async () => {
      const approvals = [...this.approvals.values()];
      const pending = approvals.filter((approval) => approval.phase === "confirmation");
      const paused = new Set(pending.map((approval) => approval.threadId));
      const children = this.subagents.threads.filter((thread) => thread.status === "running") ?? [];
      const childRunning = children.some((thread) => !paused.has(thread.id));
      const primaryRunning = !children.length && this.runtime.busy && !this.inputFinished && !paused.has(this.id);
      const evaluating = approvals.some((approval) => approval.phase === "policy");
      const status = childRunning || primaryRunning || evaluating ? "running" : pending.length ? "requires_action" : "idle";
      const first = pending[0]?.details;
      await this.backend.setStatus(this.id, status, pending.length && first ? {
        ...first, ...(pending.length > 1 ? { pending_actions: pending.map((approval) => approval.details) } : {}),
      } : undefined);
    });
    this.statuses = next.catch(() => {});
    return next;
  }

  private async beforePrimaryRequest(): Promise<void> {
    if (this.waitingOnInterruptedChild) {
      this.interrupted = true;
      this.agent.cancel({ kind: "user" });
    }
  }

  wakeParentFromChildReport(): void {
    this.waitingOnInterruptedChild = false;
  }

  consumeInterruptedApproval(threadId: string): boolean {
    return this.interruptedApprovals.delete(threadId);
  }

  private interrupt(threadId?: string): void {
    if (threadId && threadId !== this.id && this.subagents.threads.some((thread) => thread.id === threadId && thread.status === "running")) this.waitingOnInterruptedChild = true;
    for (const approval of this.approvals.values()) {
      if (approval.phase === "confirmation" && (!threadId || approval.threadId === threadId)) this.interruptedApprovals.add(approval.threadId);
    }
    if (threadId !== this.id) this.subagents.interrupt(threadId);
    if (!threadId || threadId === this.id) {
      this.interrupted = true;
      this.agent.cancel({ kind: "user" });
    }
  }

  async control(payload: Payload): Promise<boolean> {
    if (payload.type === "control_response") {
      const response = object(payload.response);
      const approval = this.approvals.get(response.request_id);
      if (approval) {
        approval.phase = "resolved";
        if (response.subtype === "success") approval.resolve(object(response.response));
        else approval.reject(new Error("Approval response failed"));
      }
      return true;
    }
    if (["interrupt", "user.interrupt"].includes(payload.type)) {
      this.interrupt(payload.session_thread_id);
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
      if (subtype === "interrupt") this.interrupt(payload.session_thread_id);
      const supported = !controlError && ["initialize", "interrupt", "set_model"].includes(subtype);
      await this.checkpoint();
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

  async receive(inputId: string, input: unknown): Promise<void> {
    const payload = object(input);
    const repeatedControl = isControlInput(payload.type) && this.handledControls.has(inputId);
    if (repeatedControl || await this.control(payload)) {
      this.rememberControl(inputId);
      // Control effects/publication finish first, but delivery retries must not
      // hold the control queue and prevent a later approval or interrupt.
      const receipt = this.checkpoint().then(() => this.backend.acknowledge(this.id, inputId, this.abort.signal))
        .catch(() => { if (!this.abort.signal.aborted) this.stop(); })
        .finally(() => { this.controlReceipts.delete(receipt); });
      this.controlReceipts.add(receipt);
      return;
    }
    if (this.receivedInputIds.has(inputId)) {
      if (this.busy && inputId === this.inputId) return;
      await this.publish();
      await this.checkpoint();
      await this.backend.acknowledge(this.id, inputId, this.abort.signal);
      return;
    }
    if (this.busy || this.abort.signal.aborted)
      throw new Error("Session is busy or stopped");
    let stopProgress = () => {};
    try {
      await this.runtime.run(() => {
        const system = appendedSystemContext(payload);
        if (system !== undefined) this.runtime.appendedSystem = system;
        return userInputContent(payload, this.ctx.attachments);
      }, {
        messageId: inputId,
        beforeRun: async () => {
          this.inputFinished = false;
          this.turnErrorMessage = undefined;
          this.budgetPaused = false;
          this.interrupted = false;
          this.waitingOnInterruptedChild = false;
          this.inputId = inputId;
          await this.backend.reportDelivery(this.id, inputId, "processing", this.abort.signal);
          stopProgress = startDeliveryProgress(
            () => this.backend.reportDelivery(this.id, inputId, "processing", this.abort.signal),
            this.abort.signal, () => this.stop(), undefined, false,
          );
          await this.updateStatus();
          if (!this.interrupted && payload.oma_refresh_tools === true) await this.runtime.refreshTools();
          return !this.interrupted;
        },
        afterRun: async () => {
          this.inputFinished = true;
          if (!this.receivedInputIds.has(inputId) && !this.budgetPaused) {
            if (this.interrupted) this.pendingEvents.push({ type: "result", uuid: `${inputId}_result`, subtype: "success", is_error: false });
            else this.addError(this.turnErrorMessage ?? "The turn stopped before successful completion.", "failed");
          }
          this.receivedInputIds.add(inputId);
          await this.checkpoint();
          await this.previews;
          await this.publish();
          await this.updateStatus();
          stopProgress();
          await this.checkpoint();
          await this.backend.acknowledge(this.id, inputId, this.abort.signal);
        },
      });
    } finally { stopProgress(); }
  }

  private rememberControl(inputId: string): void {
    this.handledControls.delete(inputId);
    this.handledControls.add(inputId);
    if (this.handledControls.size > 256) this.handledControls.delete(this.handledControls.values().next().value!);
  }

  addError(text: string, suffix = "failed", inputId = this.inputId) {
    this.pendingEvents.push(...failedOutput(inputId, text, suffix));
  }
  async flushEvents() {
    await this.subagents.flush();
    await this.runtime.flushLog();
    await this.checkpoint();
    await this.previews;
    await this.publish();
  }
  publish(): Promise<void> {
    const next = this.publications.then(async () => {
      if (!this.pendingEvents.length) return;
      const through = this.pendingEvents.length;
      const pending = [...new Map(this.pendingEvents.slice(0, through).map((event) => [event.uuid, event])).values()];
      if (!pending.length) return;
      const previews = this.previews;
      await this.checkpoint();
      await previews;
      await this.backend.events(this.id, pending);
      this.pendingEvents.splice(0, through);
    });
    this.publications = next.catch(() => {});
    return next;
  }
  dispatching(): boolean { return this.activeDispatches.size > 0 || this.outcomeConfirmations.size > 0; }

  async drain(timeoutMs: number): Promise<boolean> {
    this.draining = true;
    const deadline = Date.now() + timeoutMs;
    while (this.dispatching() && !this.abort.signal.aborted) {
      const remaining = deadline - Date.now();
      if (remaining <= 0) return false;
      await new Promise<void>((resolve) => {
        const done = () => {
          clearTimeout(timer);
          this.dispatchWaiters.delete(done);
          this.abort.signal.removeEventListener("abort", done);
          resolve();
        };
        const timer = setTimeout(done, remaining);
        this.dispatchWaiters.add(done);
        this.abort.signal.addEventListener("abort", done, { once: true });
      });
    }
    return !this.dispatching();
  }

  async interruptForShutdown(settleMs: number): Promise<void> {
    if (!this.busy) return;
    this.turnErrorMessage = "The Brain worker shut down during this turn. Unfinished tool calls were interrupted and their outcomes are unknown.";
    this.subagents.cancelForShutdown();
    this.runtime.cancelForShutdown();
    const deadline = Date.now() + settleMs;
    while (this.busy && !this.abort.signal.aborted && Date.now() < deadline)
      await new Promise((resolve) => setTimeout(resolve, 50));
  }

  stop() {
    this.subagents.stop();
    this.runtime.stop();
    for (const pending of this.approvals.values())
      pending.reject(new Error("Session stopped"));
  }
  close(): Promise<void> { return this.closing ??= this.closeSession(); }

  private async closeSession(): Promise<void> {
    this.stop();
    // Drain children before disposing the root agent and its shared providers.
    const results = await Promise.allSettled([this.subagents.close(), this.heartbeat?.close(), this.activity.close()]);
    results.push(...await Promise.allSettled([this.runtime.close()]));
    results.push(...await Promise.allSettled([this.ctx.ccrReplication.flush(this.id)]));
    await this.ctx.ccrReplication.drain(this.id);
    await Promise.allSettled([this.statuses, this.publications, this.previews, ...this.controlReceipts, ...this.outcomeConfirmations]);
    const failed = results.find((result) => result.status === "rejected");
    if (failed?.status === "rejected") throw failed.reason;
  }
}

function validatedExternalToolResult(value: unknown): {content:{type:"text";text:string}[];isError:boolean} {
 const result=object(value);
 if (typeof result.isError!=="boolean" || !Array.isArray(result.content) || !result.content.length || result.content.some((block:Payload)=>!block || block.type!=="text" || typeof block.text!=="string")) throw new Error("Invalid external tool result");
 return {content:result.content,isError:result.isError};
}
