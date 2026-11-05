// ============================================================================
// 输入流：通过 SSE 持续接收服务器下发的输入
// ============================================================================
//
//
//   GET /v1/dsr/sessions/{id}/inputs/stream?from_sequence_num=<cursor>
//   Accept: text/event-stream
//   Last-Event-ID: <cursor>          // 断线重连时带上次收到的帧序号
//
// 每个 SSE frame：
//   id: 12
//   event: input
//   data: {"id":"in_abc","lane":"task","kind":"user.message","content":[...]}
//
// 这个 AsyncGenerator 的设计目标：
//   1. 断线自动重连，并从上次的 cursor 继续，不丢输入；
//   2. 用 45 秒“无数据看门狗”处理半开连接（TCP 没断但服务器不再发数据）；
//   3. 区分可重试错误和致命错误（例如 401 直接抛出，让 owner 失败退出）；
//
// 注意：这里只负责“读”，不负责回执。收到输入后由 DsrOwner 决定何时
// 回 "received" / "processed"。
// ============================================================================

import { sse } from "../sse.ts";
import { DsrError, decimal, delay, errorOf, object } from "./http.ts";
import type { DsrClient } from "./client.ts";
import type { Input } from "./wire.ts";

/**
 * 校验并解析单个输入。
 *
 * 规则：
 *   - id 必须是非空字符串；
 *   - lane 只有 task / control；
 *   - task 车道目前只支持 user.message，且 content 必须是数组；
 *   - control 车道不允许 user.message。
 *
 * 关键错误码：
 *   - invalid_input          —— 协议结构不对；
 *   - unsupported_task_input —— 任务输入类型不支持。上层 DsrOwner 会把这种
 *                               失败标记为 permanent，让服务器不要反复重试。
 */
function parseInput(value: unknown): Input {
  const input = object(value);
  if (
    typeof input.id !== "string" ||
    !input.id ||
    typeof input.kind !== "string" ||
    !["task", "control"].includes(String(input.lane))
  )
    throw new DsrError(0, "invalid_input");
  if (
    input.lane === "task" &&
    (input.kind !== "user.message" || !Array.isArray(input.content))
  )
    throw new DsrError(0, "unsupported_task_input");
  if (input.lane === "control" && input.kind === "user.message")
    throw new DsrError(0, "invalid_input");
  return input as unknown as Input;
}

/**
 * 输入流读取器。每个 DsrOwner 创建一个，cursor 从 "0" 开始。
 *
 * @example
 *   for await (const input of new DsrInputStream(client).read(stream.signal)) {
 *     // input = {id: "in_1", lane: "task", kind: "user.message", content: [...],
 *     await session.receive(input);
 *   }
 *   // 当 signal abort（空闲/关闭/失败）时，循环结束
 */
export class DsrInputStream {
  // 已确认收到的最大帧序号。重连时用它作为 from_sequence_num / Last-Event-ID，
  // 保证服务器只补发没收到的事件。
  private cursor = "0";
  constructor(readonly client: DsrClient) {}

  /**
   * 循环：建立 SSE 连接 -> 消费事件 -> 断线后重连。
   *
   * 失败策略：
   *   - lease_expired：先重新 register 刷新租约，再重连；
   *   - 4xx（除 408/429）：致命，直接抛出（例如 token 失效、会话被 purge）；
   *   - 其他（网络错误、5xx、超时）：最多连续失败 10 次，之后抛出；
   *   - 每次失败后退避：1s, 2s, 4s ... 最长 30s。
   *
   * @param signal 外部生命周期信号；abort 后循环正常结束，不再重连。
   */
  async *read(signal: AbortSignal): AsyncGenerator<Input> {
    let failures = 0;
    while (!signal.aborted) {
      // connection 只控制“当前这条 SSE 连接”；lifetime 同时受外部 signal 与它影响。
      const connection = new AbortController();
      const lifetime = AbortSignal.any([signal, connection.signal]);
      let watchdog: ReturnType<typeof setTimeout> | undefined;
      // 每收到一个 SSE frame（包括心跳注释）就重置 45 秒看门狗。
      // 超时说明连接已“半死”：abort 当前连接，走下面的重连逻辑。
      const touch = () => {
        clearTimeout(watchdog);
        watchdog = setTimeout(
          () => connection.abort(new Error("DSR input stream idle")),
          45000,
        );
      };
      try {
        const headers: Record<string, string> = {
          "DSR-Protocol": "1",
          Accept: "text/event-stream",
        };
        // 非首次连接才带 Last-Event-ID；首次 cursor="0" 表示从头开始。
        if (this.cursor !== "0") headers["Last-Event-ID"] = this.cursor;
        // timeout: 0 表示不设总超时——SSE 是长连接，不能按普通请求超时处理。
        const response = await this.client.transport.request({
          path: `${this.client.root}/inputs/stream?from_sequence_num=${this.cursor}`,
          method: "GET",
          token: this.client.claim.session_token,
          headers,
          timeout: 0,
          signal: lifetime,
        });
        if (!response.ok) throw await errorOf(response);
        if (!response.body) throw new DsrError(0, "invalid_response");
        // 成功建立连接，重置连续失败计数。
        failures = 0;
        touch();
        for await (const frame of sse(response.body, lifetime, touch)) {
          if (!frame.data) continue;
          // 目前只接受 event: input；其他事件类型视为协议错误。
          if (frame.event !== "input") throw new DsrError(0, "invalid_input");
          const input = parseInput(JSON.parse(frame.data));
          // 帧序号必须是非负十进制字符串；只前进不后退。
          // 例：cursor="7"，收到 frame id="9" -> cursor 变成 "9"。
          if (frame.id) {
            decimal(frame.id);
            if (BigInt(frame.id) > BigInt(this.cursor)) this.cursor = frame.id;
          }
          yield input;
        }
      } catch (error) {
        // 外部主动关闭：直接结束，不记录失败、不重连。
        if (signal.aborted) return;
        // 租约过期：重新注册（register 会刷新租约参数），随后重连。
        if (error instanceof DsrError && error.code === "lease_expired")
          await this.client.register(signal);
        // 明确的客户端错误（400/401/403/404...）重试也没用，直接抛给 owner。
        // 408（超时）与 429（限流）例外，仍按可重试处理。
        else if (
          error instanceof DsrError &&
          error.status < 500 &&
          error.status !== 408 &&
          error.status !== 429
        )
          throw error;
        // 连续失败达到 10 次：放弃，让 owner 失败收尾。
        if (++failures >= 10) throw error;
      } finally {
        // 无论正常结束还是异常，都清掉看门狗并关闭当前连接。
        clearTimeout(watchdog);
        connection.abort();
      }
      // 退避后再重连：failures=1 -> 1s，2 -> 2s，3 -> 4s ... 上限 30s。
      if (!signal.aborted)
        await delay(
          this.client.transport,
          Math.min(1000 * 2 ** Math.max(0, failures - 1), 30000),
          signal,
        );
    }
  }
}
