import type { Context } from "@deepseek-ai/cordis";
import TranscriptPersistence from "../runtime/transcript-persistence.ts";
// OmaApi：访问 platform 的 HTTP 客户端 + 进程内共享的内存表（assignment、model 请求 ID 等）。
import { OmaApi } from "../api.ts";
// 启动前检查目录可写，避免跑到一半才发现 transcript 写不进去。
// 挂载原生 DSH 内核插件（session store、agent loop、tool runtime、LLM runtime ...）。
import { mountKernel } from "../kernel.ts";
// 子 agent（subagent）运行时；DSR 会话里也会用到。
import { mountDshSubagents } from "../runtime/dsh-subagents.ts";
// 会话 transcript 的复制层：把本地 DSH 会话事件同步到远端（这里是 DSR transcript API）。
import { SessionReplication } from "../runtime/replication.ts";
import { TranscriptUploadError } from "../runtime/transcript-batch.ts";
// 把一份 Assignment + OmaApi 转换成 SessionRuntimeConfig（模型、工具、系统提示词等）。
import { omaRuntimeConfig } from "../oma-session.ts";
import { mountTelemetry } from "../observability/index.ts";
// 结构化诊断日志：JSON 一行，落到 console 和 brain 日志管道。
import { workerDiagnostic } from "../observability/diagnostics.ts";
// 配置错误：启动阶段直接失败，process 退出码非 0。
import { ConfigurationError } from "../config.ts";
// Prometheus/OTel 指标，例如活跃会话数。
import { brainMetrics } from "../telemetry.ts";
import { DsrClient } from "./client.ts";
import { DsrClaims } from "./claims.ts";
import { DsrInputInbox, DsrReceipts } from "./input-inbox.ts";
import { DsrTaskWake } from "./task-wake.ts";
import { DsrInputStream } from "./input-stream.ts";
import { DsrSession } from "./session.ts";
import { DsrLease } from "./lease.ts";
import { dsrAssignment } from "./assignment.ts";
import { createRemoteTranscriptStore } from "./replication.ts";
import { createDsrTransport } from "./transport.ts";
// delay：可被 AbortSignal 打断的 sleep（测试里 transport.sleep 可注入假时钟）。
// DsrError：带 HTTP status + 协议错误码的统一错误类型。
import { delay, DsrError } from "./http.ts";
import type { Claim } from "./wire.ts";

// 这些是 DSR worker 运行所必需的 cordis 服务名。
// plugin.ts 用 `inject: dsrRuntimeServices` 声明依赖：只有这些服务都就绪后，
// startDsrWorkers 才会被调用，避免在 session store / LLM runtime 尚未挂载时认领会话。
// 例如 "sessions" 对应 SessionStore，"llm" 对应 LlmRuntime。
export const dsrRuntimeServices = [
  "attachments",
  "llm",
  "tokenMeter",
  "sessions",
  "sessionPersistence",
  "sessionReplication",
  "agents",
  "agentLoop",
  "tools",
  "systemPrompt",
];

// worker 的可调参数。典型值：
//   maxSessions: 8          —— 本进程最多同时拥有 8 个会话（每个会话可能长期占用内存）。
//   claimConcurrency: 2     —— 同时开 2 个长轮询，一个阻塞等待 25s 时另一个仍能抢到活。
//   shutdownDrainMs: 5000   —— 收到 SIGTERM 时，最多给在途任务 5s 收尾时间。
export interface DsrWorkerOptions {
  maxSessions: number;
  claimConcurrency: number;
  shutdownDrainMs: number;
}

// TypeScript 的模块增强（module augmentation），只影响类型系统，不产生运行时代码：
//   - dsrContinuation: 标记当前 cordis Context 运行在 DSR 模式下。
//     session runtime 据此允许“保留未结束的 turn”（审批挂起、子 agent 未完成时
//     暂停会话，等下次被认领后继续），而不是把它当成错误。
//   - oma/shutdown-drain: 进程收到 SIGINT/SIGTERM 时，main.ts 会调用
//     `ctx.parallel("oma/shutdown-drain")`，触发所有 worker 排空。
declare module "@deepseek-ai/cordis" {
  interface Context {
    dsrContinuation: boolean;
  }

  interface Events {
    "oma/shutdown-drain"(): Promise<void>;
  }
}

export async function mountDsr(
  ctx: Context,
  api: OmaApi,
): Promise<Map<string, DsrClient>> {
  await mountKernel(ctx, api);
  ctx.provide("dsrContinuation", true);
  await ctx.plugin(TranscriptPersistence);
  const clients = new Map<string, DsrClient>();
  // 复制层写数据时传入的是“根会话 ID”。这里延迟查找 client：
  // 只有该会话当前确实被本进程拥有时才允许写；否则抛错，让复制层把错误当作
  // fenced（例如 epoch_superseded）处理，避免旧 owner 覆盖新 owner 的 transcript。
  await ctx.plugin(SessionReplication, {
    remote: createRemoteTranscriptStore((rootSessionId) => {
      const client = clients.get(rootSessionId);
      if (!client) throw new Error("DSR transcript owner is unavailable");
      return client;
    }),
  });
  await mountDshSubagents(ctx);
  await mountTelemetry(ctx, api);
  return clients;
}

/**
 * 启动 claim 轮询与 worker 生命周期管理。
 *
 * 这个函数本身在“监听就绪”后就会返回，真正的会话执行发生在后台 Promise 中。
 *
 * @example
 *   // plugin.ts 中的用法（服务就绪后）：
 *   await startDsrWorkers(scope, api, clients, claims, {
 *     maxSessions: 8,
 *     claimConcurrency: 2,
 *     shutdownDrainMs: 5000,
 *   });
 */
export async function startDsrWorkers(
  ctx: Context,
  api: OmaApi,
  clients: Map<string, DsrClient>,
  claims: DsrClaims,
  options: DsrWorkerOptions,
): Promise<void> {
  // 进程级总开关。abort 之后所有长轮询、SSE 输入流、等待中的 sleep 都会被唤醒退出。
  const abort = new AbortController();
  // sessionId -> 当前 owner。注意：同一个 session 可能被重新认领（例如轮到新 epoch），
  // 新 owner 会替换旧 owner；旧 owner 收尾时用身份比较避免误删新 owner 的登记。
  const owners = new Map<string, DsrOwner>();
  // 所有后台任务的集合，仅用于 shutdown 时等待它们全部结束（防止“写了一半”）。
  const work = new Set<Promise<void>>();
  // 已通过容量检查、但还在等待 claim 返回的“预留名额”。
  // 例：maxSessions=8，已有 7 个 owner，两个 poll 同时看到“还有空位”，
  // 如果没有 reserved，两个都会发出 claim，瞬间变成 9 个会话。
  // reserved 在同一个同步片段里自增，JS 单线程下不会交错，从而把检查变成准原子操作。
  let reserved = 0;

  // 把一个后台 Promise 纳入统一管理。
  // 注意 catch 是空处理 + 诊断：owner.run() 内部已经记录了失败原因（fail()），
  // 这里再抛会导致 unhandled rejection，所以只记录并吞掉；finally 负责从集合移除。
  const watch = (task: Promise<void>) => {
    work.add(task);
    void task
      .catch(() => workerDiagnostic("warn", "dsr_owner_failed", {}))
      .finally(() => work.delete(task));
  };

  // 认领成功后的“收养”流程：建 client -> 建 owner -> 一直运行到会话结束 -> 清理。
  // 每个 claim 都会独立跑在 watch() 管理的后台任务里。
  const admit = async (claim: Claim) => {
    // 同一个 code session 已经有 owner 了？说明它被重新分配（通常是新 epoch 接管）。
    // 先把旧 owner 关掉，避免两个 owner 同时写同一份 transcript。
    const previous = owners.get(claim.code_session_id);
    if (previous) await previous.close();

    // 每个会话一个 client，绑定本次 claim 的 session_token / worker_epoch。
    const client = new DsrClient(createDsrTransport(api), claim);
    // 先写入 clients 再启动 owner：session.open() 可能会触发 transcript 复制，
    // 复制层必须能立刻按 sessionId 找到 client。
    clients.set(claim.code_session_id, client);
    const owner = new DsrOwner(
      ctx,
      api,
      client,
      claims,
      options.shutdownDrainMs,
    );
    owners.set(claim.code_session_id, owner);
    try {
      // 注意 `await`：这个 Promise 直到会话真正结束（idle/shutdown/permanent）才 resolve。
      await owner.run();
    } finally {
      // 只有“当前登记的仍是自己”时才清理。否则说明期间已有新 owner 接管，
      // 删除操作会把新 owner 的登记、assignment、telemetry 一并误删。
      if (owners.get(claim.code_session_id) === owner) {
        owners.delete(claim.code_session_id);
        if (clients.get(claim.code_session_id) === client)
          clients.delete(claim.code_session_id);
        ctx.sessionReplication.forget(claim.code_session_id);
        // 清掉进程内共享的会话级内存：assignment、模型响应 ID 映射等。
        api.assignments.delete(claim.code_session_id);
        api.modelResponses.delete(claim.code_session_id);
        api.modelRequestIDs.delete(claim.code_session_id);
        // 关闭该会话的 telemetry（trace/log 导出器），避免泄漏。
        await api.telemetry?.close(claim.code_session_id);
      }
    }
  };

  // 长轮询循环：不断问服务器“有没有活给我”。claimConcurrency 个实例同时跑。
  const poll = async () => {
    while (!abort.signal.aborted) {
      // 容量已满（活跃 + 已预留 >= 上限）：短暂等待，避免空转占用 CPU。
      // 例：8 个会话都在跑，就每 500ms 检查一次有没有会话结束。
      if (owners.size + reserved >= options.maxSessions) {
        await delay(claims.transport, 500, abort.signal);
        continue;
      }
      reserved++;
      try {
        // block_ms=25000：服务器最多挂起 25 秒，有活立刻返回，没活返回 204/undefined。
        // 这是长轮询而不是忙等，能显著降低请求频率。
        const claim = await claims.claim(25000, abort.signal);
        if (claim) watch(admit(claim));
      } catch (error) {
        if (abort.signal.aborted) return;
        // 400/401/403 属于“配置/凭证/协议错误”，重试没有意义：
        // 直接中止整个 worker，让进程退出并由上层报警。
        // 例：worker token 过期 -> 401 -> 不再继续空转。
        if (
          error instanceof DsrError &&
          [400, 401, 403].includes(error.status)
        ) {
          abort.abort(error);
          throw error;
        }
        // 其他错误（网络抖动、5xx、超时）退避 1 秒后重试。
        await delay(claims.transport, 1000, abort.signal);
      } finally {
        // 无论成功、失败、取消，都归还预留名额，让容量检查恢复准确。
        reserved--;
      }
    }
  };

  // 排空：收到 SIGTERM/SIGINT 时调用。
  // 1) 关掉总开关，停止认领新会话；
  // 2) 并行关闭所有 owner（各自最多等待 drainMs 处理完手头任务）；
  // 3) 等待所有后台任务真正结束。
  const shutdown = async () => {
    abort.abort();
    await Promise.allSettled(
      [...owners.values()].map((owner) => owner.close()),
    );
    await Promise.allSettled([...work]);
  };
  // 两种触发方式：
  //   ctx.on(...)  —— 响应显式的 drain 事件（main.ts 的优雅退出流程）；
  //   ctx.effect(...) —— cordis 释放该插件时自动执行 shutdown（兜底）。
  //   注意 ctx.effect 的回调返回值就是 disposer，所以这里把 shutdown 函数本身返回。
  ctx.on("oma/shutdown-drain", shutdown);
  ctx.effect(() => shutdown);

  // 启动时先做一次“立即认领”（block_ms=0，不等待）：
  //   - 好处 1：本地开发/单会话场景可以立刻开跑，不必等第一次长轮询；
  //   - 好处 2：如果 token 配置错了，服务器马上返回 401，这里映射成
  //     ConfigurationError，让进程在启动阶段就失败，而不是后台静默重试。
  // 例：worker token 无效 -> HTTP 401 -> ConfigurationError -> main.ts 打印启动失败并退出。
  await claims
    .claim(0, abort.signal)
    .then((claim) => {
      if (claim) watch(admit(claim));
    })
    .catch((error) => {
      if (error instanceof DsrError && [400, 401, 403].includes(error.status))
        throw new ConfigurationError(
          `Brain claim rejected with HTTP ${error.status}`,
        );
      throw error;
    });

  // 启动 claimConcurrency 个长轮询循环。它们不会被 await，会一直运行到 abort。
  for (let i = 0; i < options.claimConcurrency; i++) watch(poll());
  // 给人看的启动日志（stdout），以及给机器看的结构化诊断（带字段）。
  console.info("oma-brain-dsh ready");
  workerDiagnostic("info", "dsr_brain_ready", {
    max_sessions: options.maxSessions,
    claim_concurrency: options.claimConcurrency,
  });
}

/**
 * 一个会话的拥有者：从“认领成功”到“释放 claim”之间的全部状态都挂在这里。
 *
 * 为什么需要这个类而不是一个函数：会话生命周期长（几分钟到几小时），
 * 期间有输入流、心跳、空闲检测、任务队列、关闭流程等多个并发活动，
 * 需要集中管理它们的取消和收尾顺序。
 */
export class DsrOwner {
  session?: DsrSession;
  // 心跳租约。start() 后每 heartbeat_interval_ms 续一次；续租失败会调用 fail()。
  private lease?: DsrLease;
  // 输入流（SSE）的生命周期开关。空闲/失败/关闭时 abort，for-await 随即结束。
  private readonly stream = new AbortController();
  private readonly inbox: DsrInputInbox;
  // close() 可能被调用多次（shutdown、替换旧 owner），缓存 Promise 使其幂等。
  private closing?: Promise<void>;
  // run() 的 finally 全部执行完（包括 release claim）后 resolve。
  // close() 用它来等待“真的收尾干净”，而不是刚发出取消就返回。
  private settled = Promise.withResolvers<void>();
  // 释放 claim 时上报的原因，服务器据此决定是否立刻重新分配：
  //   idle            —— 空闲超时，可以马上给别人；
  //   shutdown        —— 本进程正常退出/输入流结束，可以重新分配；
  //   permanent       —— 输入类型永久不支持，换谁都一样，别再重试；
  //                      服务器可以尽快重试到别的 worker。
  // 初始值是 admission_failed：如果 open 之前就抛错，语义正好吻合。
  private reason: "idle" | "shutdown" | "permanent" | "admission_failed" =
    "admission_failed";
  // 记录第一个失败原因（fail() 可能被多个来源并发调用，只保留最早的）。
  private failure?: unknown;
  // 当前进行到哪一步，仅用于诊断日志。
  private stage = "register";
  private tasks?: Promise<void>;
  private taskActive = false;
  private readonly taskWake = new DsrTaskWake();

  constructor(
    readonly ctx: Context,
    readonly api: OmaApi,
    readonly client: DsrClient,
    readonly claims: DsrClaims,
    readonly drainMs: number,
  ) {
    const receipts = new DsrReceipts((updates) =>
      this.client.delivery(updates, this.stream.signal),
    );
    this.inbox = new DsrInputInbox(receipts, {
      control: (input, report) => this.session!.control(input, report),
      fail: (error) => this.fail(error),
    });
  }

  private async readControls(): Promise<void> {
    try {
      for await (const input of new DsrInputStream(
        this.client,
        () => this.taskWake.notify(),
        () => this.session?.permissionStreamConnected(),
      ).read(this.stream.signal)) {
        this.inbox.accept(input);
      }
    } catch (error) {
      if (!this.stream.signal.aborted) this.fail(error);
    }
  }

  private async runTasks(): Promise<void> {
    try {
      while (!this.stream.signal.aborted) {
        const version = this.taskWake.version;
        this.taskActive = true;
        const input = await this.client.claimInput(this.stream.signal);
        if (!input) {
          this.taskActive = false;
          await this.taskWake.wait(
            version,
            this.client.transport,
            this.stream.signal,
          );
          continue;
        }
        await this.session!.receive(input);
        this.taskActive = false;
        if (this.session!.suspended) {
          this.reason = "idle";
          this.stream.abort();
          return;
        }
      }
    } catch (error) {
      if (!this.stream.signal.aborted) this.fail(error);
    }
  }

  /**
   * 会话主流程。只有 run() 结束（正常或异常）之后，admit() 才会执行清理。
   *
   * 阶段划分：
   *   register  -> 拉租约参数并启动心跳
   *   inputs    -> 持续消费 SSE 输入流，直到空闲/失败/关闭
   */
  async run(): Promise<void> {
    // 空闲检测定时器（1s 精度）与 received 重报定时器（10s 精度）。
    let timer: ReturnType<typeof setInterval> | undefined;
    let progress: ReturnType<typeof setInterval> | undefined;
    try {
      // claim 自带过期时间。注册请求必须在这之前完成，否则 claim 已经失效，
      // 再注册也没有意义，直接按 claim_expired 失败（会被 release 掉）。
      // 例：claim_expires_at 距今还有 30s，就用 30s 作为 register 的超时上限。
      const remaining =
        Date.parse(this.client.claim.claim_expires_at) - Date.now();
      if (remaining <= 0) throw new DsrError(0, "claim_expired");
      // register 返回协议版本、租约参数（ttl/grace/heartbeat interval）、可观测性开关。
      const registration = await this.client.register(
        AbortSignal.timeout(remaining),
      );
      // 心跳续租：每 heartbeat_interval_ms 调一次 /heartbeat。
      // 续租失败（例如网络长期不通、epoch 被顶掉）回调 fail()，让会话尽快收尾。
      this.lease = new DsrLease(this.client, registration, (error) =>
        this.fail(error),
      );
      this.lease.start();
      const assignment = dsrAssignment(
        this.api,
        this.client.claim,
        registration,
      );
      this.api.assignments.set(assignment.session_id, assignment);
      // DsrSession 封装了 DSH 会话运行时：工具执行、审批、消息发布、transcript 同步。
      this.session = new DsrSession(
        this.ctx,
        this.client,
        omaRuntimeConfig(this.api, assignment),
      );
      // 指标：active sessions +1。只有在 session 真正建立后才加，finally 里对应减 1。
      brainMetrics().sessionsActive.add(1);
      this.stage = "open";
      // 确保 transcript 复制就绪、恢复子 agent 线程、打开本地会话并重放历史事件。
      await this.session.open();
      this.stage = "inputs";
      // 从这里开始，除非被改成 idle/permanent，异常结束一律按 shutdown 释放。
      // 因为此时进程已经“正常接管过”这个会话，服务器可以重新分配给别人。
      this.reason = "shutdown";

      // 空闲检测：每秒检查一次，连续 60 秒没有任务且没有子 agent 在跑，就判定空闲。
      // 这里只 abort 输入流，不直接关闭 session；输入流结束后走 run() 的 finally，
      // 按顺序关闭 session/lease 并 release(claim, "idle")，保证清理路径唯一。
      // 例：用户 3 分钟没发消息 -> 空闲计时达到 60s -> abort -> release -> 进程腾出槽位。
      timer = setInterval(() => {
        if (
          this.session &&
          !this.session.busy &&
          !this.taskActive &&
          this.inbox.unsettled.size === 0 &&
          Date.now() - this.session.lastIdle > 60000
        ) {
          this.reason = "idle";
          this.stream.abort();
        }
      }, 1000);

      // received 重报：每 10 秒把所有未完成输入重新标记为 received。
      // 用 reporting 防重入：上一次批量上报还没返回时跳过本轮，避免请求堆积。
      let reporting = false;
      progress = setInterval(() => {
        if (reporting) return;
        reporting = true;
        void this.inbox
          .reportReceived()
          .catch((error) => this.fail(error))
          .finally(() => {
            reporting = false;
          });
      }, 10000);

      this.tasks = this.runTasks();
      await Promise.all([this.readControls(), this.tasks]);
    } catch (error) {
      // stream 被主动 abort（空闲/关闭）时，for-await 抛出的取消错误不是真失败，
      // 只把真正的错误交给 fail()。
      if (!this.stream.signal.aborted) this.fail(error);
    } finally {
      // 收尾顺序很重要：先停定时器，再排空队列，再关 session/lease，最后 release claim。
      clearInterval(timer);
      clearInterval(progress);
      try {
        // 等待两条输入队列自然结束（即使很快失败，也要把已入队的处理完/取消完）。
        await this.inbox.drain();
        await this.session?.close(this.drainMs);
      } finally {
        if (this.session) brainMetrics().sessionsActive.add(-1);
        await this.lease?.close();
        try {
          const rejected =
            this.failure instanceof DsrError &&
            this.failure.code === "epoch_superseded";
          if (!rejected)
            await this.claims
              .release(
                this.client.claim,
                this.reason,
                AbortSignal.timeout(15000),
              )
              .catch(() => {});
        } finally {
          // 通知 close()：清理完成，可以返回了。
          this.settled.resolve();
        }
      }
    }
  }

  /**
   * 统一的失败入口。多个来源（心跳、输入流、任务队列、上报循环）都可能调用，
   * 这里做幂等处理：只记录第一个原因，并尽快让主流程退出。
   *
   * @example
   *   // 场景：处理 user.message 时发现输入类型不支持
   *   // -> failure = DsrError("unsupported_task_input")
   *   // -> reason = "permanent"（换 worker 重试也没用）
   *   // -> abort 输入流 -> run() 进入 finally -> release(claim, "permanent")
   */
  private fail(error: unknown): void {
    // 只给第一个失败写诊断，后续重复调用不刷日志。
    if (!this.failure)
      workerDiagnostic("warn", "dsr_owner_interrupted", {
        stage: this.stage,
        failure_reason:
          error instanceof DsrError ? error.code : "runtime_error",
        ...(error instanceof DsrError ? { status: error.status } : {}),
      });
    this.failure ??= error;
    // unsupported_task_input 是“永久性”的：不是网络抖动，换机器也会失败。
    // 用 permanent 告诉服务器不要再重试这个输入。
    if (
      error instanceof TranscriptUploadError ||
      (error instanceof DsrError && error.code === "unsupported_task_input")
    )
      this.reason = "permanent";
    // 终止输入流和会话，让 run() 尽早进入 finally。
    this.stream.abort();
    this.session?.stop();
  }

  /**
   * 外部关闭入口（shutdown、被新 owner 替换时调用）。保证幂等。
   *
   * 关闭分两段，体现“先礼后兵”：
   *   1. 先让在途任务最多跑 drainMs（例如工具调用正在写文件，给它时间收尾）；
   *   2. 超过期限还在忙，就强制取消 runtime 与子 agent，解除阻塞；
   *   3. 最后等待 run() 的 finally 完成（含 release claim），确保资源真的释放。
   *
   * @example
   *   // drainMs = 5000：
   *   // t=0 收到 SIGTERM -> close()
   *   // t=3s 任务自己结束 -> 不必强制取消 -> 正常 release
   *   // t=5s 任务仍在跑 -> cancelForShutdown() -> 任务抛取消错误 -> release
   */
  close(): Promise<void> {
    return (this.closing ??= (async () => {
      // 停止接收新输入（已经入队的仍会被排空）。
      this.stream.abort();
      const waiting = Promise.all([this.inbox.drain(), this.tasks]);
      // 用一个 unref 的定时器做超时线：不阻止进程退出，到点自动 resolve。
      let deadlineTimer: ReturnType<typeof setTimeout> | undefined;
      const deadline = new Promise<void>((resolve) => {
        deadlineTimer = setTimeout(resolve, this.drainMs);
        deadlineTimer.unref();
      });
      // 谁先完成都行：任务排空 或 到达排水上限。
      const drained = await Promise.race([
        waiting.then(() => true),
        deadline.then(() => false),
      ]);
      clearTimeout(deadlineTimer);
      if (!drained)
        this.ctx.sessionReplication.cancel(
          this.client.claim.code_session_id,
          new Error("Transcript shutdown drain timed out"),
        );
      if (this.session?.busy) {
        // 仍然忙：强制取消，避免 shutdown 被一个卡住的工具调用无限拖住。
        this.session.runtime.cancelForShutdown();
        this.session.subagents.cancelForShutdown();
      }
      // 等 run() 的 finally 全部执行完（关闭 session、lease、release claim）。
      await this.settled.promise;
    })());
  }
}
