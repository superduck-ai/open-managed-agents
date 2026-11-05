// ============================================================================
// Lease：会话租约心跳
// ============================================================================
//
// register 成功后，服务器会下发一组租约参数：
//
//   {
//     "lease": {
//       "ttl_ms": 30000,                 // 30 秒不续租就判过期
//       "grace_ms": 10000,               // 过期后再给 10 秒宽限
//       "heartbeat_interval_ms": 10000   // 每 10 秒心跳一次
//     }
//   }
//
// DsrLease 的唯一职责就是“按间隔续租”：
//
//   t=0    register 成功，拿到 lease 参数
//   t=10s  heartbeat -> lease_expires_at 往后推
//   t=20s  heartbeat
//   ...
//
// 如果某次心跳失败（网络中断、epoch 被新 worker 顶掉等），
// 就调用构造时传入的 `lost` 回调；DsrOwner 会把它接到 fail()，
// 从而终止输入流、关闭会话、释放 claim，避免“自认为还持有租约”地继续写数据。
//
// 关键点：
//   - 心跳请求的超时是 ttl_ms：如果一次心跳卡住超过 TTL，即使最终成功也没有意义了；
//   - close() 会 abort 等待与请求，并等待循环真正退出，避免进程关闭后还有定时器；
//   - start() 用 `??=` 保证只启动一次。
// ============================================================================

import type {DsrClient} from "./client.ts";
import type {Registration} from "./wire.ts";
import {delay} from "./http.ts";

/**
 * 会话租约心跳器。每个 DsrOwner 持有一个，register 之后 start()。
 *
 * @example
 *   const lease = new DsrLease(client, registration, error => this.fail(error));
 *   lease.start();
 *   // ... 会话运行期间自动每 10 秒心跳一次 ...
 *   await lease.close(); // 会话结束时停止心跳
 */
export class DsrLease {
    // 心跳循环的生命周期开关；close() 时 abort，delay 和 heartbeat 都会被唤醒。
    private readonly abort = new AbortController();
    // 循环 Promise。保存下来是为了 close() 能 await 到循环真正退出。
    private task?: Promise<void>;

    constructor(readonly client: DsrClient, readonly registration: Registration, readonly lost: (error: unknown) => void) {
    }

    /**
     * 启动心跳循环（幂等，多次调用只启动一次）。
     *
     * 失败处理有两点讲究：
     *   1. 如果是 close() 主动 abort 导致的错误，说明是我们自己停的，不回调 lost；
     *   2. 否则把错误交给 lost，由 owner 统一做失败收尾。
     *
     * @example
     *   lease.start();
     *   // 若 10 秒后心跳返回 409 epoch_superseded：
     *   // -> lost(DsrError) -> owner.fail() -> 会话停止
     */
    start(): void {
        this.task ??= this.run().catch(error => {
            if (!this.abort.signal.aborted) this.lost(error);
        });
    }

    /**
     * 心跳主循环：先等一个间隔，再续租，如此往复。
     *
     * 为什么先 delay 再 heartbeat：register 刚刚完成，租约是新鲜的，
     * 没必要立刻再打一次心跳。
     *
     * 为什么心跳超时设为 ttl_ms：一次心跳如果在 TTL 内都回不来，
     * 服务器很可能已经把租约判过期，继续等待只会延迟失败收尾。
     */
    private async run(): Promise<void> {
        while (!this.abort.signal.aborted) {
            // 等待下一个心跳时间点；abort 时 delay 会立即 reject/return。
            await delay(this.client.transport, this.registration.lease.heartbeat_interval_ms, this.abort.signal);
            // AbortSignal.any：close() 或单次 TTL 超时，任一触发都会中断请求。
            await this.client.heartbeat(AbortSignal.any([this.abort.signal, AbortSignal.timeout(this.registration.lease.ttl_ms)]));
        }
    }

    /**
     * 停止心跳并等待循环退出。
     *
     * close() 可以安全地重复调用：第二次 abort 没有副作用，
     * await 的仍是同一个已完成的 task。
     *
     * @example
     *   await Promise.all([lease.close(), session.close()]);
     *   // 关闭后不会再有心跳请求发出
     */
    async close(): Promise<void> {
        this.abort.abort();
        await this.task;
    }
}
