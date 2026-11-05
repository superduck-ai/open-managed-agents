// ============================================================================
// DSR Claims：worker 进程级“认领会话 / 释放会话”的 HTTP 客户端
// ============================================================================
//
// 这个文件很小，但它是整个 Brain 调度的入口，先把三种身份分清楚：
//
//   1. brain claim token（本类构造函数里的 `token`）
//      进程级凭证，来自 worker 配置。只用于 /claims 和 /claims/{id}/release 两个接口，
//      放在 Authorization: Bearer 里。它回答“这个进程有没有资格来领活”。
//
//   2. workerId（本类构造函数里的 `workerId`）
//      进程级随机 UUID（plugin.ts 用 crypto.randomUUID() 生成），放在 X-Worker-ID 头里。
//      服务器用它归属 claim、做释放时的归属校验。同一进程内所有会话共享这一个 ID。
//
//   3. session_token（在 Claim 结构里，不在这里使用）
//      认领成功后由服务器下发，代表“对某一个具体会话的访问权”。
//      DsrClient 用它调 register / context / inputs 等会话级接口。
//
// 一个典型流程（poster 是 worker.ts 的 startDsrWorkers）：
//
//   claim(25000) ──有活──> Claim {code_session_id, worker_epoch, session_token, ...}
//        │                          │
//        │                          └─> new DsrClient(transport, claim) -> Owner.run()
//        │
//        └──没活──> 204 No Content -> undefined -> 继续下一轮长轮询
//
//   会话结束时：release(claim, "idle" | "shutdown" | "permanent" | "admission_failed")
//
// 关于重试的分工：
//   - 本类只是在 HTTP 层调用一次（请求函数 attempts=1）；
//   - “失败后退避多久、哪些状态码要终止进程”由调用方 poll() / run() 决定。
//   这样分开是为了避免“透明重试”导致同一个 worker 意外认领到两个会话
//   （第一个请求超时但服务器其实已经分配成功）。
// ============================================================================

import {DsrError, object, parseClaim, request, type Transport} from "./http.ts";
import type {Claim} from "./wire.ts";

/**
 * 进程内唯一的 claim 客户端，通常由 plugin.ts 创建一次并传给所有会话。
 *
 * @example
 *   const claims = new DsrClaims(
 *     createDsrTransport(api),
 *     "brain-token-from-config",   // 32+ 字符的进程 token
 *     crypto.randomUUID(),          // 例："7f3c...-..."，进程内固定
 *   );
 */
export class DsrClaims {
    constructor(readonly transport: Transport, readonly token: string, readonly workerId: string) {
    }

    /**
     * 认领一个待执行的 code session。
     *
     * 这是一个“长轮询”接口，不是普通查询：
     *   - block_ms=0     立即返回，没有活就 204（worker.ts 启动时用它做首次认领）；
     *   - block_ms=25000 服务器最多挂起 25 秒，期间有活立刻返回，没活到点返回 204。
     *
     * 为什么返回 `Claim | undefined`：undefined 表示“暂时没活”，是正常情况，
     * 不是错误。真正的错误（401 token 失效、500 服务端故障等）会以 DsrError 抛出。
     *
     * @example
     *   // 阻塞等待，最多 25 秒
     *   const claim = await claims.claim(25000, abort.signal);
     *   if (claim) {
     *     // 例：{code_session_id: "cse_123", worker_epoch: "7",
     *     //      session_token: "...", model_token: "...",
     *     //      claim_expires_at: "2026-11-04T12:00:30Z"}
     *     await admit(claim);
     *   }
     *
     * @example
     *   // 参数校验（本地先挡一层，避免明显错误的请求打到服务器）
     *   await claims.claim(-1);      // 抛 DsrError("invalid_request")
     *   await claims.claim(30001);   // 抛 DsrError("invalid_request")
     */
    async claim(blockMs: number, signal?: AbortSignal): Promise<Claim | undefined> {
        // 服务器同样要求 0 <= block_ms <= 30000；这里提前校验可以省一次网络往返，
        // 也能让调用方在开发阶段立刻发现参数写错。
        if (!Number.isSafeInteger(blockMs) || blockMs < 0 || blockMs > 30000) throw new DsrError(0, "invalid_request");
        const response = await request(this.transport, {
            path: `/v1/dsr/claims?block_ms=${blockMs}`, token: this.token, method: "POST", body: {},
            // X-Worker-ID 是服务器识别“同一个 worker”的依据；
            // 它会被记录在 claim 上，后续 release 也会校验它，防止别的进程替你释放。
            //
            // 同一行的 timeout 必须比服务端阻塞时间更长：
            //   例：block_ms=25000 -> timeout=40000（25s 阻塞 + 最多 15s 网络/排队余量）。
            // 如果不传 timeout，底层 OmaApi 默认 20s，25s 的长轮询会被客户端提前掐断。
            headers: {"X-Worker-ID": this.workerId}, timeout: blockMs + 15000, signal
        }, 1);
        // 204 No Content 是“这段时间没有活”，用 undefined 表达；
        // 其他成功响应才会带 JSON body。
        return response.status === 204 ? undefined : parseClaim(await response.json());
    }

    /**
     * 归还一个 claim，让服务器可以把该会话重新分配给别的 worker。
     *
     * 四种 reason 的语义（服务器据此决定后续调度）：
     *   - "idle"            空闲超时（像 worker.ts 的 60s 无任务），会话可立刻重新派发；
     *   - "shutdown"        本进程正常退出/输入流结束，可重新派发；
     *   - "admission_failed"还没真正开始跑就失败（register/context 阶段），尽快重试；
     *   - "permanent"       永久性失败（例如不支持的输入类型），服务器会做失败计数，
     *                       而不是无限重试。
     *
     * 关于 epoch 校验：
     *   release 请求带上 claim.worker_epoch。如果会话已经被更新的 epoch 接管
     *   （例如本进程网络分区太久，服务器判租约过期后重新分配），服务器会返回
     *   409 Conflict，拒绝这次“迟到的释放”，避免旧 owner 把新 owner 的 claim 放掉。
     *   worker.ts 对这种失败是故意吞掉的（release(...).catch(() => {})），
     *   因为清理本身已经是 best-effort。
     *
     * @example
     *   await claims.release(claim, "idle", AbortSignal.timeout(15000));
     *   // 服务器返回 {"ok": true}
     *
     * @example
     *   // 永久失败：不再重试这个输入
     *   await claims.release(claim, "permanent");
     */
    async release(claim: Claim, reason: "idle" | "shutdown" | "admission_failed" | "permanent", signal?: AbortSignal): Promise<void> {
        const response = await request(this.transport, {
            // session id 来自服务器，编码后再拼进路径，避免特殊字符破坏 URL。
            path: `/v1/dsr/claims/${encodeURIComponent(claim.code_session_id)}/release`,
            token: this.token,
            method: "POST",
            // worker_epoch 与 release reason 一起放在 body 里：
            // 前者用于 fence，后者用于调度决策（见上方说明）。
            body: {epoch: claim.worker_epoch, reason},
            headers: {"X-Worker-ID": this.workerId},
            // 释放是收尾动作，不需要像 claim 那样长等；5 秒还不返回就放弃，
            // 服务器侧最终也会用租约过期（TTL + grace）兜底回收。
            timeout: 5000,
            signal
        });
        // 成功响应固定为 {"ok": true}。不是严格的 true 就按协议错误处理，
        // 避免把半成功的响应当成释放成功。
        if (object(await response.json()).ok !== true) throw new DsrError(0, "invalid_response");
    }
}
