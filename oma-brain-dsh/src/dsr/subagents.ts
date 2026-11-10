// ============================================================================
// 子 agent 桥接：DSR 模式下的子线程生命周期与协议事件
// ============================================================================
//
// DSH 的 subagent 运行时会创建“子线程”（child session）。DSR 对这一层有几个约束：
//
//   1. 只支持一层：子线程的 parent_dsh_session_id 必须等于根会话，
//      不允许子 agent 再派生子 agent（见 restoreThreads 的校验）。
//   2. 子线程的 transcript 也要同步到远端；bindPersistence 把子线程
//      绑定到根会话的复制通道。
//   3. 子线程的创建/运行/结束/消息都要转成 PublicEvent 上报给服务器，
//      前端才能展示“某个 agent 正在做什么”。
//   4. 恢复时要从远端 transcript 还原子线程列表（名字、提示词、模型、工具、
//      运行状态），否则接管后的会话会“忘记”自己有哪些子 agent。
//
// 数据流：
//
//   Subagents 运行时
//      ├─ emit(event)          -> PublicEvent -> owner.publish() -> 服务器
//      ├─ onEvent/onStream     -> owner.record/stream -> 事件流
//      └─ restoreThreads()     <- 远端 transcript -> retained[]
//
// 例子：
//   // 子线程 resume 前会调用：
//   await bridge.validateThread("ses_child", signal);        // 已终止则抛错
//   const ids = await bridge.archivedThreads(ids, signal);   // 过滤掉不可用的
// ============================================================================

import type { Context } from "@deepseek-ai/cordis";
import { dsrExecutor } from "./executor.ts";
import { traceHooks } from "../observability/trace-context.ts";
import { SessionId, Session } from "@deepseek-ai/dsh-session";
import { foldSubagentDescriptor } from "@deepseek-ai/dsh-subagent";
import { dshThreadConfig } from "../runtime/dsh-subagents.ts";
import type {
  SubagentBridge,
  SubagentEvent,
  SubagentThread,
} from "../runtime/contracts.ts";
import type { DsrSession } from "./session.ts";
import { openTurn } from "./session.ts";
import { DsrError, object } from "./http.ts";
import type { PublicEvent, Transcript } from "./wire.ts";

/**
 * 把所有子 agent 相关能力接到一个根会话（DsrSession）上。
 *
 * `retained` 保存本进程恢复/创建过的子线程元数据；Subagents 运行时通过
 * threads() 读取它来恢复调度。
 */
export class DsrSubagentBridge implements SubagentBridge {
  // 已恢复的子线程列表。注意这里只存元数据，真正的会话内容在 sessionPersistence。
  private readonly retained: SubagentThread[] = [];
  constructor(readonly owner: DsrSession) {}

  // 子 agent scope 里的工具执行也要走 DSR 的审批/恢复逻辑。
  setupTools(scope: Context): void {
    scope.tools.setExecutor(dsrExecutor(scope, this.owner));
  }

  // 运行时用这份列表决定哪些子 agent 还存在、状态如何。
  threads(): SubagentThread[] {
    return this.retained;
  }

  // 把子线程的 transcript 绑定到根会话的复制通道：
  // 之后子线程产生的事件会通过根会话的 DsrClient 上传。
  bindPersistence(id: string): void {
    this.owner.ctx.sessionReplication.bindThread(id, this.owner.id);
  }

  /**
   * 从远端 transcript 恢复子线程列表（会话被重新认领时调用）。
   *
   * 步骤：
   *   1. 列出该根会话的所有 transcript（含根会话自己，需要跳过）；
   *   2. 校验只有一层（parent 必须等于根会话）；
   *   3. 绑定复制、确保远端复制状态；
   *   4. 从事件流折叠出子线程描述符和配置；
   *   5. 查询远端 thread 状态，把已归档/已终止的标成 terminated。
   *
   * 对于“有 turn/start 但缺少完整配置”的线程直接报错，因为无法安全恢复；
   * 对于从未真正开始的空线程则跳过。
   *
   * @example
   *   // 远端返回：
   *   //   {dsh_session_id: "ses_root", ...}
   *   //   {dsh_session_id: "ses_child", parent_dsh_session_id: "ses_root", ...}
   *   // 恢复后 retained = [{id: "ses_child", name: "researcher", status: "idle", ...}]
   */
  async restoreThreads(directory?: readonly Transcript[]): Promise<void> {
    const transcripts =
      directory ??
      (await this.owner.client.listTranscripts(this.owner.abort.signal));
    for (const transcript of transcripts) {
      // 根会话本身不是子线程。
      if (transcript.dsh_session_id === this.owner.id) continue;
      // DSRv1 只支持一层子 agent；更深层级无法用现有协议表达。
      if (transcript.parent_dsh_session_id !== this.owner.id)
        throw new Error("DSRv1 supports one subagent level");
      this.bindPersistence(transcript.dsh_session_id);
      await this.owner.ctx.sessionReplication.ensure(
        transcript.dsh_session_id,
        { nextOffset: transcript.next_offset },
      );
      const handle = await this.owner.ctx.sessionPersistence.open(
        SessionId(transcript.dsh_session_id),
        "read",
      );
      let thread: SubagentThread | undefined;
      try {
        const { events } = await handle.read();
        // 从事件流里还原“这个子 agent 是谁/用什么配置”。
        const descriptor = foldSubagentDescriptor(events);
        const config = dshThreadConfig(events);
        // 已经有 turn/start 说明它真的运行过，配置缺失则无法安全恢复；
        // 否则只是创建了空壳线程，忽略即可。
        if (!descriptor || !config) {
          if (events.some((event) => event.type === "turn/start"))
            throw new Error("Subagent has incomplete configuration");
          continue;
        }
        this.owner.replay(
          Session.create(
            SessionId(transcript.dsh_session_id),
            events,
            handle.header,
          ),
        );
        thread = {
          id: transcript.dsh_session_id,
          name: config.name ?? descriptor.label ?? transcript.dsh_session_id,
          inline: config.inline,
          systemPrompt: config.systemPrompt,
          model: config.model ?? this.owner.config.model,
          tools: config.tools,
          // 有未闭合的 turn 就是 running，否则 idle；远端状态还会进一步覆盖。
          status: openTurn(events) === undefined ? "idle" : "running",
        };
      } finally {
        await handle.close();
      }
      if (thread) {
        // 远端状态是权威的：归档或终止后，即使本地事件看起来还在 running，也标 terminated。
        const state = await this.threadState(
          thread.id,
          this.owner.abort.signal,
        );
        if (state.archived || state.status === "terminated")
          thread.status = "terminated";
        this.retained.push(thread);
      }
    }
  }

  /**
   * 查询单个子线程的远端状态。
   *
   * 响应兼容两种形状：`{thread: {...}}` 或直接 `{...}`。
   * 校验 id 必须回显一致、status 必须是 idle/running/terminated。
   *
   * @example
   *   await this.threadState("ses_child", signal);
   *   // -> {id: "ses_child", status: "running", archived: false}
   */
  private async threadState(id: string, signal: AbortSignal) {
    const response = await this.owner.client.transport.request({
      path: `${this.owner.client.root}/threads/${encodeURIComponent(id)}`,
      method: "GET",
      token: this.owner.client.claim.session_token,
      headers: { "DSR-Protocol": "1" },
      signal,
    });
    if (!response.ok) throw new DsrError(response.status, "thread_unavailable");
    const body = object(await response.json());
    const thread = object(body.thread ?? body);
    if (
      thread.id !== id ||
      !["idle", "running", "terminated"].includes(String(thread.status))
    )
      throw new DsrError(0, "invalid_response");
    return thread;
  }

  /**
   * 校验线程当前是否可用。
   *
   * 默认把 `running` 也视为不可用（避免对正在运行的线程做 resume/重入）；
   * 某些明确允许接管运行中线程的调用方可以传 allowRunning=true。
   *
   * @example
   *   await bridge.validateThread("ses_child", signal);
   *   // running -> 抛 "Subagent thread is unavailable"
   */
  async validateThread(
    id: string,
    signal: AbortSignal,
    allowRunning = false,
  ): Promise<void> {
    const state = await this.threadState(id, signal);
    if (
      state.archived ||
      state.status === "terminated" ||
      (state.status === "running" && !allowRunning)
    )
      throw new Error("Subagent thread is unavailable");
  }

  /**
   * 从一批 id 中挑出已归档/已终止的线程。
   * 运行时可据此清理本地引用，避免继续向死线程发消息。
   */
  async archivedThreads(ids: string[], signal: AbortSignal): Promise<string[]> {
    const result: string[] = [];
    for (const id of ids) {
      const state = await this.threadState(id, signal);
      if (state.archived || state.status === "terminated") result.push(id);
    }
    return result;
  }

  /**
   * 把子 agent 运行时事件翻译成对外的 PublicEvent 并发布。
   *
   * id 的生成策略：
   *   - created 使用确定性的 `${threadId}_created`，服务器可以据此去重，
   *     避免恢复时重复创建同一个线程事件；
   *   - 其他状态/消息事件使用随机 UUID。
   *
   * 各事件映射：
   *   created          -> session.thread_created（附带 agent_snapshot）
   *   running          -> session.thread_status_running
   *   idle             -> session.thread_status_idle（stop_reason: end_turn / interrupted）
   *   terminated       -> session.thread_status_terminated
   *   message-sent     -> agent.thread_message_sent
   *   report-received  -> agent.thread_message_received
   *
   * @example
   *   await bridge.emit({kind: "idle", thread, completed: true});
   *   // -> {id: "<uuid>", type: "session.thread_status_idle",
   *   //     session_thread_id: "ses_child", stop_reason: {type: "end_turn"}}
   */
  async emit(event: SubagentEvent): Promise<void> {
    const { thread } = event;
    const base = {
      id:
        event.kind === "created" ? `${thread.id}_created` : crypto.randomUUID(),
      session_thread_id: thread.id,
    };
    let output: PublicEvent;
    switch (event.kind) {
      // inline 子 agent 没有预设配置，用它的系统提示词构造快照；
      // 非 inline 的从会话配置里按名字找到对应的 agentSnapshot。
      case "created":
        traceHooks.subagentCreated(thread.id, {
          name: thread.name,
          inline: thread.inline === true,
        });
        output = {
          ...base,
          type: "session.thread_created",
          dsh_session_id: thread.id,
          parent_thread_id: this.owner.id,
          agent_snapshot: !thread.inline
            ? this.owner.config.subagents?.agents?.find(
                (agent) => agent.name === thread.name,
              )?.agentSnapshot
            : {
                type: "inline",
                name: thread.name,
                system: thread.systemPrompt,
              },
        };
        break;
      case "running":
        output = { ...base, type: "session.thread_status_running" };
        break;
      // completed=true 表示正常结束；false 表示被中断（例如用户取消）。
      case "idle":
        output = {
          ...base,
          type: "session.thread_status_idle",
          stop_reason: { type: event.completed ? "end_turn" : "interrupted" },
        };
        break;
      case "terminated":
        output = { ...base, type: "session.thread_status_terminated" };
        break;
      case "message-sent":
        output = {
          id: base.id,
          type: "agent.thread_message_sent",
          to_session_thread_id: thread.id,
          content: [{ type: "text", text: event.text }],
        };
        break;
      case "report-received":
        output = {
          id: base.id,
          type: "agent.thread_message_received",
          from_session_thread_id: thread.id,
          content: [{ type: "text", text: event.text }],
        };
        break;
    }
    // publish 会先放到待发布队列，再 checkpoint 刷给服务器。
    await this.owner.publish(output);
  }

  // 子线程打开时：把已有事件补记到根会话的可观测事件流，然后刷盘。
  async onOpen(session: Session): Promise<void> {
    this.owner.replay(session);
    await this.owner.checkpoint(session);
  }
  async flushLog(session: Session): Promise<void> {
    await this.owner.checkpoint(session);
  }
  // 子线程事件走和根会话相同的记录逻辑（record 内部用 session.id 区分线程）。
  onEvent(
    _id: string,
    event: Parameters<SubagentBridge["onEvent"]>[1],
    session: Session,
  ): void {
    this.owner.record(event, session);
  }
  // 子线程的流式帧也要带线程 id 发布预览。
  onStream(id: string, frame: Parameters<SubagentBridge["onStream"]>[1]): void {
    this.owner.stream(frame, id);
  }
  // 子线程的工具执行桥接：审批、checkpoint 都复用根会话实现。
  toolExecution(id: string) {
    return this.owner.execution(id);
  }
}
