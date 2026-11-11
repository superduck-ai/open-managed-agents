import type { Context } from "@deepseek-ai/cordis";
import type { AssistantStreamFrame } from "@deepseek-ai/dsh-agent";
import {
  SessionId,
  appendPluginRecord,
  pluginRecordOf,
  type Session,
  type SessionEvent,
} from "@deepseek-ai/dsh-session";
import { SessionRuntime } from "../runtime/session.ts";
import { Subagents } from "../runtime/subagents.ts";
import type {
  ApprovalDecision,
  SessionRuntimeConfig,
  ToolApproval,
  ToolExecutionBridge,
} from "../runtime/contracts.ts";
import {
  AssistantStream,
  assistantMessages,
  type AssistantBlockOutput,
} from "../runtime/assistant.ts";
import { userInputContent } from "../runtime/user-content.ts";
import { appendedSystemContext } from "../runtime/system-context.ts";
import { traceHooks } from "../observability/trace-context.ts";
import { withAttachmentSession } from "../attachments/remote.ts";
import { dsrExecutor, DsrSuspend } from "./executor.ts";
import { DsrPermissionWaiter } from "./permission-waiter.ts";
import { DsrPreviewBuffer } from "./preview-buffer.ts";
import { DsrError, object } from "./http.ts";
import { dsrToolIdentity } from "./tools.ts";
import { DsrSubagentBridge } from "./subagents.ts";
import type { DsrClient } from "./client.ts";
import type { Decision, Delivery, Finish, Input, PublicEvent } from "./wire.ts";
import { recordMcpError, projectMcpError } from "./mcp-errors.ts";
import { TranscriptPublication } from "./publication.ts";

// 扩展 DSH 插件记录类型：允许把“某个输入已经得出最终结论”写进会话事件流。
// 这是 DSR 幂等恢复的关键：结论一旦落盘，重放同一个输入就不会再执行一遍。
declare module "@deepseek-ai/dsh-session/types" {
  interface PluginRecordMap {
    "plugin:oma/dsr-finish": {
      version: 2;
      inputId: string;
      completion: Finish;
    };
  }
}

export class DsrSession {
  // 原生会话运行时：模型循环、工具、turn 管理都在这里。
  readonly runtime: SessionRuntime;
  // 子 agent 管理器：负责创建/恢复/中断子线程。
  readonly subagents: Subagents;
  private readonly subagentBridge: DsrSubagentBridge;
  // 当前正在处理的任务输入 ID。空字符串表示还没开始处理任何输入。
  inputId = "";
  // 会话是否处于“挂起”状态：不 finish 输入，等下次认领后继续。
  suspended = false;
  // 公共事件的串行发布链。所有 publish 都排在这条链上，保证服务器看到的顺序。
  private publications: Promise<void> = Promise.resolve();
  private readonly previews: DsrPreviewBuffer;
  // 待发布的公共事件缓冲区，由 checkpoint() 批量刷出。
  private pending: PublicEvent[] = [];
  // 已入队的 assistant 事件 ID，防止同一条消息被 record 和 plugin record 重复入队。
  private readonly assistantIDs = new Set<string>();
  private readonly publication = new TranscriptPublication();
  // 审批去重：key = "threadId:callId"。同一个工具调用只发一次审批请求，
  // 并发调用方共享同一个 Promise。
  private readonly approvals = new Map<string, Promise<ApprovalDecision>>();
  private readonly permissionWaiter = new DsrPermissionWaiter();
  // assistant 消息/块的事件生成器（负责把流式帧转成协议事件）。
  private readonly assistant: AssistantStream;
  // 第一个致命错误。一旦设置，所有 checkpoint 都会抛出它，阻止继续写坏状态。
  private failure?: unknown;
  // 最近一次根会话 turn/end 事件。runtime.run 结束后用它来生成 Finish。
  private ending?: Extract<SessionEvent, { type: "turn/end" }>;

  constructor(
    readonly ctx: Context,
    readonly client: DsrClient,
    readonly config: SessionRuntimeConfig,
    readonly options: { approvalWaitMs?: number } = {},
  ) {
    this.previews = new DsrPreviewBuffer((events) => {
      this.publications = this.publications.then(() =>
        this.client.publish(events, this.abort.signal),
      );
      void this.publications.catch((error) => {
        this.failure = error;
        this.stop();
      });
    });
    this.assistant = new AssistantStream(
      config.id,
      config.modelMessageID,
      config.modelRequestID,
    );
    // preserveOpenTurns: true —— DSR 的核心差异：允许会话在 turn 未结束时暂停，
    // 下次认领后继续，而不是把“打开状态”当错误。
    // cancelRestoredTurn：恢复一个打开 turn 时，如果对应输入已被服务器取消，
    // 就取消这个 turn。
    this.runtime = new SessionRuntime(
      ctx,
      {
        ...config,
        preserveOpenTurns: true,
        cancelRestoredTurn: () => this.client.inputCancelled(this.inputId),
      },
      {
        // 安装 DSR 工具执行器（审批 + 崩溃恢复），并注册子 agent 工具。
        setupTools: async (scope) => {
          scope.tools.setExecutor(dsrExecutor(scope, this));
          await this.subagents.register(scope);
        },
        // 所有会话事件先进入 record()，转成公共事件或更新内部状态。
        onExternalMcpAuthenticationFailure: (serverName, phase, session) =>
          this.mcpFailure(session, "mcp_authentication_failed_error", serverName, phase ?? "discovery"),
        onExternalMcpConnectionFailure: (serverName, session) =>
          this.mcpFailure(session, "mcp_connection_failed_error", serverName, "discovery"),
        onEvent: (event, session) => this.record(event, session),
        // 流式帧进入 stream()，生成 preview 事件。
        onStream: (frame) => this.stream(frame),
        flushLog: (session) => this.checkpoint(session),
        // 父会话结束前等待子 agent。
        waitForChildren: () => this.subagents.wait(),
        stop: () => this.stop(),
      },
      this.execution(),
    );
    this.subagentBridge = new DsrSubagentBridge(this);
    this.subagents = new Subagents(this.runtime, this.subagentBridge);
  }
  // 根会话 ID（也就是 code_session_id）。
  get id(): string {
    return this.config.id;
  }
  // 运行时取消信号：会话关闭时统一 abort 所有进行中的请求。
  get abort(): AbortController {
    return this.runtime.abort;
  }
  // 是否有活：原生 turn 在跑或子 agent 在跑。空闲检测依赖它。
  get busy(): boolean {
    return this.runtime.busy || this.subagents.busy;
  }
  // 最后一次“有活动”的时间戳，用于 60 秒空闲判定。
  get lastIdle(): number {
    return this.subagents.lastIdle;
  }

  async open(): Promise<void> {
    const directory = await this.client.listTranscripts(this.abort.signal);
    const transcript = directory.find(
      (entry) => entry.dsh_session_id === this.id,
    );
    await this.ctx.sessionReplication.ensure(
      this.id,
      transcript ? { nextOffset: transcript.next_offset } : null,
    );
    await this.subagentBridge.restoreThreads(directory);
    await this.runtime.open();
    this.replay(this.runtime.agent.session);
    await this.checkpoint();
  }

  replay(session: Session): void {
    this.publication.prepare(session);
    for (const event of session.snapshotEvents()) this.record(event, session);
  }

  checkpoint(session?: Session): Promise<void> {
    if (this.failure) return Promise.reject(this.failure);
    this.previews.flush();
    const pending = this.pending.splice(0);
    const sessions = new Set<Session>();
    if (this.runtime.handle) sessions.add(this.runtime.agent.session);
    if (session) sessions.add(session);
    this.publications = this.publications.then(() =>
      this.commitPublication(pending, [...sessions]),
    );
    return this.publications;
  }

  private async commitPublication(
    pending: PublicEvent[],
    sessions: Session[],
  ): Promise<void> {
    await this.ctx.sessionReplication.waitForCapacity(
      this.id,
      this.abort.signal,
    );
    await this.ctx.sessionReplication.flush(this.id, this.abort.signal);
    for (const session of sessions) await this.ctx.sessions.flush(session);
    if (!pending.length) return;
    for (let index = 0; index < pending.length; index += 200)
      await this.client.publish(
        pending.slice(index, index + 200),
        this.abort.signal,
      );
  }

  /**
   * 原生会话事件 -> 公共事件/内部状态。
   *
   * 三类处理：
   *   - assistant/message：完整的助手消息，转成 agent.message/agent.thinking；
   *   - plugin:oma/assistant-block：流式块的最终形态，直接入队；
   *   - tool/result（surfaceOp=append）：工具结果，转成 tool.result。
   *
   * thread 区分根会话与子线程：session.id === this.id 表示根会话。
   *
   * @example
   *   record({type: "turn/end", ...}, rootSession)
   *   // -> this.ending = event（供 runtime.run 结束后生成 Finish）
   */
  record(event: SessionEvent, session: Session): void {
    for (const entry of this.publication.read(event, session))
      this.project(entry, session);
  }

  private project(event: SessionEvent, session: Session): void {
    const thread = session.id === this.id ? undefined : session.id;
    if (!thread && event.type === "turn/end") this.ending = event;
    if (event.type === "assistant/message" && !event.data.interrupted) {
      const inputID = thread
        ? this.id
        : (this.publication.input(session, event.data.turn) ?? this.inputId);
      const fallback = `${inputID}_${thread ? `${thread}_` : ""}message_${event.data.turn}_${event.data.step}`;
      const outputs = assistantMessages(
        this.id,
        fallback,
        event.data.message,
        thread,
      );
      for (const payload of this.assistant.complete(outputs, thread)) {
        this.queueAssistant(payload as PublicEvent);
      }
    }
    const diagnostic = projectMcpError(event, session.id, this.id);
    if (diagnostic) this.pending.push(diagnostic);
    const record = pluginRecordOf(event);
    if (record?.type === "plugin:oma/assistant-block") {
      const output = (record.data as { output: PublicEvent }).output;
      this.queueAssistant(output);
    }
    if (event.type === "tool/result" && event.surfaceOp === "append") {
      const toolName = this.publication.tool(
        session,
        event.data.message.toolCallId,
      );
      if (!toolName) throw new Error("Tool result has no call identity");
      this.pending.push({
        id: `${session.id}_result_${event.seq}`,
        type: "tool.result",
        tool_use_id: event.data.message.toolCallId,
        tool: dsrToolIdentity(toolName),
        is_error: !!event.data.message.isError,
        content: [...event.data.message.content],
        ...(thread ? { session_thread_id: thread } : {}),
      });
    }
  }

  // assistant 事件去重入队：同一条消息可能由 record 和 plugin record 各触发一次。
  private queueAssistant(event: PublicEvent): boolean {
    if (this.assistantIDs.has(event.id)) return false;
    this.assistantIDs.add(event.id);
    this.pending.push(event);
    return true;
  }

  stream(frame: AssistantStreamFrame, thread?: string): void {
    for (const payload of this.assistant.record(
      frame,
      thread ? this.id : this.inputId,
      thread,
    )) {
      if (payload.type === "event_start" || payload.type === "event_delta") {
        const preview = {
          ...payload,
          id: crypto.randomUUID(),
          type:
            payload.type === "event_start" ? "preview.start" : "preview.delta",
          ...(thread ? { session_thread_id: thread } : {}),
        } as PublicEvent;
        this.previews.enqueue(preview);
      } else {
        this.previews.flush();
        // 块完成必须持久化：找到所属会话（子线程或根会话）后写入 plugin record。
        const session = thread
          ? this.ctx.sessions.get(SessionId(thread))
          : this.runtime.agent.session;
        if (!session) throw new Error("Assistant block owner is unavailable");
        appendPluginRecord(session, "plugin:oma/assistant-block", {
          output: payload as AssistantBlockOutput,
        });
      }
    }
  }

  async receive(input: Extract<Input, { lane: "task" }>): Promise<void> {
    this.inputId = input.id;
    traceHooks.inputReceived(this.id, input.trace_context);
    const session = this.runtime.agent.session;
    // 1. 查找这个输入是否已有持久化结论。
    const saved = session
      .snapshotEvents()
      .map(pluginRecordOf)
      .find(
        (record) =>
          record?.type === "plugin:oma/dsr-finish" &&
          (record.data as { inputId: string }).inputId === input.id,
      );
    if (saved) {
      const data = saved.data as { version: number; completion: Finish };
      // 版本不认识时不能猜测语义，直接报错。
      if (data.version !== 2)
        throw new DsrError(0, "unsupported_finish_version");
      await this.finish(input.id, data.completion);
      return;
    }
    // 3. 等待期间用户取消了输入：取消当前打开 turn，并写 cancelled 结论。
    if (this.client.inputCancelled(input.id)) {
      await this.subagents.restore();
      this.runtime.cancelOpenTurn();
      await this.completeCancelled(input.id);
      return;
    }
    // 4. 恢复子 agent。恢复过程中可能因为审批等待等原因需要挂起。
    try {
      await this.subagents.restore();
    } catch (error) {
      if (
        error instanceof Error &&
        "code" in error &&
        error.code === "OMA_DSR_SUSPEND"
      ) {
        this.suspended = true;
        return;
      }
      throw error;
    }
    const events = session.snapshotEvents();
    // 这条输入是否已经作为 user/message 写入历史。
    const admitted = events.find(
      (event) => event.type === "user/message" && event.data.id === input.id,
    );
    const turn = inputTurn(events, input.id);
    // 5. 历史里已存在该输入对应的 turn/end：直接复用当时结论。
    const completed =
      turn === undefined
        ? undefined
        : events.findLast(
            (event) => event.type === "turn/end" && event.data.turn === turn,
          );
    if (completed?.type === "turn/end") {
      await this.complete(input.id, completed);
      return;
    }
    const open = openTurn(events);
    // 6. 有未闭合 turn，但这条输入不在历史里：说明会话状态与输入错配，不能继续。
    if (open !== undefined && !admitted)
      throw new DsrError(0, "input_history_mismatch");
    // 清掉上一轮遗留的 ending，避免误用旧的 turn/end。
    this.ending = undefined;
    // 7. 执行。continuation 表示这是在继续一个已打开的 turn（恢复场景）。
    await withAttachmentSession(this.id, () =>
      this.runtime.run(
        async () => userInputContent(input, this.ctx.attachments),
        {
          messageId: input.id,
          continuation: open !== undefined,
          beforeRun: async () => {
            // 追加系统上下文（例如用户选择的文件、记忆），并可选刷新工具列表。
            const context = appendedSystemContext(input);
            if (context !== undefined) this.runtime.appendedSystem = context;
          },
          afterRun: async () => {
            // 仍然有未闭合 turn：说明会话需要暂停（批准/子 agent），不要 finish。
            // 输入保持 unsettled，等下次认领后服务器重投。
            if (openTurn(session.snapshotEvents()) !== undefined) {
              this.suspended = true;
              return;
            }
            // 正常情况下 runtime 一定产生过 turn/end；没有则说明状态损坏。
            if (!this.ending)
              throw new Error("Native turn has no durable ending");
            await this.complete(input.id, this.ending);
          },
        },
      ),
    );
  }

  // 记录“已取消”的结论并回执。先落盘 finish 记录，保证崩溃后重放仍然一致。
  private async completeCancelled(id: string): Promise<void> {
    const completion = { result: "cancelled" } as const;
    appendPluginRecord(this.runtime.agent.session, "plugin:oma/dsr-finish", {
      version: 2,
      inputId: id,
      completion,
    });
    await this.finish(id, completion);
  }

  /**
   * 根据 turn/end 生成 Finish 并持久化。
   *
   * 结束原因映射：
   *   completed                  -> success
   *   aborted + reason=user      -> cancelled
   *   其他（工具错误、内部异常等）-> failed + execution_failed
   *
   * 如果还有未完成的子 agent（有打开 turn），本次不 finish，标记 suspended，
   * 等子 agent 结束后重新认领再收尾。
   */
  private async complete(
    id: string,
    ending: Extract<SessionEvent, { type: "turn/end" }>,
  ): Promise<void> {
    if (await this.hasOpenChildren()) {
      this.suspended = true;
      return;
    }
    const reason = ending.data.reason;
    const completion: Finish =
      reason.kind === "completed"
        ? { result: "success" }
        : reason.kind === "aborted" && reason.reason.kind === "user"
          ? { result: "cancelled" }
          : { result: "failed", failure_code: "execution_failed" };
    appendPluginRecord(this.runtime.agent.session, "plugin:oma/dsr-finish", {
      version: 2,
      inputId: id,
      completion,
    });
    await this.finish(id, completion);
  }

  private async finish(id: string, completion: Finish): Promise<void> {
    await this.checkpoint();
    await this.client.finish(id, completion, this.abort.signal);
  }

  async control(
    input: Extract<Input, { lane: "control" }>,
    report: (update: Delivery) => Promise<unknown> = (update) =>
      this.client.delivery([update], this.abort.signal),
  ): Promise<void> {
    let error: "unsupported_input" | undefined;
    switch (input.kind) {
      case "permission.resolved":
        await report({ id: input.id, status: "processed" });
        this.permissionWaiter.resolve(input.request_id, input.resolution);
        return;
      case "user.interrupt":
        // 没有 thread id 或就是根会话：取消根 turn。
        if (!input.session_thread_id || input.session_thread_id === this.id)
          this.runtime.agent.cancel({ kind: "user" });
        // 指定了子线程：只中断那个子线程。
        if (input.session_thread_id !== this.id)
          this.subagents.interrupt(input.session_thread_id);
        break;
      default:
        error = "unsupported_input";
    }
    await report({
      id: input.id,
      status: "processed",
      ...(error ? { error } : {}),
    });
  }

  /**
   * 给工具执行器用的桥接对象。
   *
   *   approve             -> 走 DSR 审批流程（可能挂起）
   *   runTool             -> 执行前先等 register 完成，确保有租约
   *   approvalChanged     -> 用户修改了工具参数时，发一条 system.message 告知
   *
   * 子 agent 传入 thread，审批和消息都会带上 session_thread_id。
   */
  execution(thread?: string): ToolExecutionBridge {
    return {
      approve: (request, signal) => this.approve(request, signal, thread),
      runTool: async (_id, execute) => {
        await this.client.ready();
        return execute();
      },
      approvalChanged: async (name, callId, effective) => {
        this.pending.push({
          id: `${thread ?? this.id}_${callId}_approved`,
          type: "system.message",
          content: [
            {
              type: "text",
              text: `Approved ${name} arguments: ${JSON.stringify(effective)}`,
            },
          ],
          ...(thread ? { session_thread_id: thread } : {}),
        });
        await this.checkpoint();
      },
    };
  }

  /**
   * 审批入口，按 "thread:callId" 去重。
   *
   * 为什么需要去重：工具批次可能并行请求审批，或者恢复流程与正常流程同时到达；
   * 同一个调用只能对应一个审批请求，否则用户会看到重复弹窗。
   */
  approve(
    request: ToolApproval,
    signal: AbortSignal,
    thread?: string,
  ): Promise<ApprovalDecision> {
    const key = `${thread ?? this.id}:${request.callId}`;
    let task = this.approvals.get(key);
    if (!task) {
      task = this.requestApproval(request, signal, thread);
      this.approvals.set(key, task);
    }
    return task;
  }

  permissionStreamConnected(): void {
    this.permissionWaiter.reconnect();
  }

  private async requestApproval(
    request: ToolApproval,
    signal: AbortSignal,
    thread?: string,
  ): Promise<ApprovalDecision> {
    // 审批必须属于一个真实存在的会话和未闭合 turn。
    const session = thread
      ? this.ctx.sessions.get(SessionId(thread))
      : this.runtime.agent.session;
    if (!session) throw new Error("Approval owner is unavailable");
    const turn = openTurn(session.snapshotEvents());
    if (turn === undefined)
      throw new Error("Approval requires an open native turn");
    // 审批请求体：把工具身份、参数、所属输入/turn 都告诉服务器。
    const body = {
      input_id: this.inputId,
      turn: String(turn),
      dsh_session_id: thread ?? this.id,
      tool_use_id: request.callId,
      tool: dsrToolIdentity(request.toolName),
      input: request.input,
      ...(request.available === undefined
        ? {}
        : { available: request.available }),
    };
    traceHooks.approvalPending(thread ?? this.id, request.callId);
    const response = await this.permissionWaiter.wait(
      (requestSignal) => this.client.permission(body, requestSignal),
      signal,
      this.options.approvalWaitMs ?? 60000,
    );
    if (!response)
      throw new DsrSuspend("Approval is pending; preserve the native turn");
    if (response.state !== "resolved")
      return { allowed: false, reason: "Tool approval is closed" };
    const decision = response.decision;
    traceHooks.approvalResolved(
      thread ?? this.id,
      request.callId,
      decision.behavior === "external_result"
        ? "external"
        : decision.behavior === "allow"
          ? "allowed"
          : "denied",
    );
    return approvalDecision(decision);
  }

  /**
   * 是否还有未完成的子 agent（存在打开 turn 的子线程）。
   *
   * 父输入要等所有子 agent 结束才算完成；否则会把子线程丢在半路。
   * 读的是本地持久化的事件，而不是内存状态，确保恢复场景也准确。
   */
  private async hasOpenChildren(): Promise<boolean> {
    for (const thread of this.subagents.threads) {
      const handle = await this.ctx.sessionPersistence.open(
        SessionId(thread.id),
        "read",
      );
      try {
        if (openTurn((await handle.read()).events) !== undefined) return true;
      } finally {
        await handle.close();
      }
    }
    return false;
  }

  // 入队一个公共事件并立即 checkpoint。子 agent 桥接层用它发布事件。
  private async mcpFailure(session: Session, type: "mcp_authentication_failed_error" | "mcp_connection_failed_error", serverName: string, phase: "discovery" | "tool_call"): Promise<void> {
    this.record(recordMcpError(session, type, serverName, phase), session);
    await this.checkpoint(session);
  }

  async publish(event: PublicEvent): Promise<void> {
    this.pending.push(event);
    await this.checkpoint();
  }
  // 停止原生运行时和所有子 agent；不负责关闭持久化（由 close 做）。
  stop(): void {
    this.previews.close();
    this.runtime.stop();
    this.subagents.stop();
  }
  /**
   * 关闭会话。顺序：
   *   1. 关闭子 agent（取消/等待）；
   *   2. 关闭原生 runtime（flush、释放工具/沙箱等）；
   *   3. 等待已排队的公共事件发布完成；发布失败忽略（会话已经在收尾）。
   */
  async close(timeoutMs = 5000): Promise<void> {
    const deadline = setTimeout(
      () =>
        this.ctx.sessionReplication.cancel(
          this.id,
          new Error("Transcript shutdown drain timed out"),
        ),
      timeoutMs,
    );
    try {
      await this.subagents.close();
      await this.runtime.close();
      await this.ctx.sessionReplication.drain(this.id, timeoutMs);
      this.previews.flush();
      await this.publications.catch(() => {});
    } finally {
      this.previews.close();
      clearTimeout(deadline);
    }
  }
}

/**
 * 扫描事件流，返回当前未闭合的 turn 编号。
 *
 * 规则：最后一个 turn/start 开启一个 turn；遇到它的 turn/end 就关闭。
 * 所有 turn 都闭合时返回 undefined。
 *
 * @example
 *   openTurn([turn/start 1, turn/end 1])          // undefined
 *   openTurn([turn/start 1, turn/end 1, turn/start 2]) // 2
 */
export function openTurn(events: readonly SessionEvent[]): number | undefined {
  let turn: number | undefined;
  for (const event of events) {
    if (event.type === "turn/start") turn = event.data.turn;
    if (event.type === "turn/end") turn = undefined;
  }
  return turn;
}

/**
 * 把协议 Decision 转成运行时 ApprovalDecision。
 *
 *   allow           -> allowed=true，可带修改后的参数；
 *   external_result -> allowed=true，并携带外部执行结果；
 *   deny            -> allowed=false，reason 用服务器给的消息。
 *
 * @example
 *   approvalDecision({behavior: "allow", updated_input: {command: "ls"}})
 *   // -> {allowed: true, input: {command: "ls"}}
 */
function approvalDecision(value: Decision): ApprovalDecision {
  const decision = object(value);
  if (decision.behavior === "allow")
    return {
      allowed: true,
      ...(value.behavior === "allow" && value.updated_input
        ? { input: value.updated_input }
        : {}),
    };
  if (value.behavior === "external_result")
    return { allowed: true, externalResult: value.external_result };
  return {
    allowed: false,
    reason: value.behavior === "deny" ? value.message : "Tool denied",
  };
}

/**
 * 查找某条 user/message 属于哪个 turn。
 *
 * @example
 *   inputTurn([turn/start 1, user/message id="in_1", turn/end 1], "in_1") // 1
 *   inputTurn([...], "in_missing")                                        // undefined
 */
function inputTurn(
  events: readonly SessionEvent[],
  id: string,
): number | undefined {
  let turn: number | undefined;
  for (const event of events) {
    if (event.type === "turn/start") turn = event.data.turn;
    if (event.type === "user/message" && event.data.id === id) return turn;
  }
  return undefined;
}
