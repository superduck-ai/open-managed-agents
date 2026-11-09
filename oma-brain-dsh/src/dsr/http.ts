// ============================================================================
// DSR HTTP 基础设施：错误类型、响应校验、退避、重试
// ============================================================================
//
// 这一层是所有 DSR 网络调用的共同底座，回答四个问题：
//
//   1. 出错了怎么表示？        -> DsrError(status, code, nextOffset?, retryAfterMs?)
//   2. 响应结构怎么防御？      -> decimal() / object() / parseClaim()
//   3. HTTP 错误怎么分类？     -> errorOf() 读取 DSR-Error / Retry-After
//   4. 哪些错误值得重试？      -> request() 只重试 408 / 429 / 5xx / 网络错误
//
// status=0 的 DsrError 表示“本地检测到的协议/参数错误”，不是服务器返回的 HTTP 状态。
//
// 例子：
//   - 服务器 401 + DSR-Error: unauthorized
//       -> DsrError(status=401, code="unauthorized")，不重试，直接抛给调用方；
//   - 服务器 503 + Retry-After: 2
//       -> DsrError(status=503, code="unavailable", retryAfterMs=2000)，
//          request() 会等约 2 秒后重试；
//   - 网络层 TypeError
//       -> request() 按指数退避重试，最多 4 次；
//   - 响应体 {"epoch":"07"}
//       -> decimal() 抛 DsrError(0, "invalid_response")，拒绝前导零。
// ============================================================================

import type { Claim } from "./wire.ts";

// 一次 HTTP 请求的参数。timeout 的单位是毫秒；0 通常表示“不设总超时”（SSE 长连接）。
export interface HttpRequest {
  path: string;
  method: string;
  token: string;
  body?: unknown;
  headers?: Record<string, string>;
  signal?: AbortSignal;
  timeout?: number;
}

// 最小传输抽象。sleep 可选：测试可以注入假时钟，避免真实等待。
export interface Transport {
  request(options: HttpRequest): Promise<Response>;
  sleep?(ms: number, signal?: AbortSignal): Promise<void>;
}

/**
 * DSR 统一错误。
 *
 * @param status       HTTP 状态码；0 表示本地协议/参数校验失败。
 * @param code         稳定的协议错误码，例如 "lease_expired" / "invalid_response"。
 * @param nextOffset      可选，服务器通过 DSR-Next-Offset 返回的续读游标。
 * @param retryAfterMs 可选，服务器建议的重试等待时间（来自 Retry-After）。
 *
 * @example
 *   try {
 *     await client.heartbeat();
 *   } catch (error) {
 *     if (error instanceof DsrError && error.code === "lease_expired") {
 *       // 租约已经丢了，需要重新 register
 *     }
 *   }
 */
export class DsrError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    readonly nextOffset?: string,
    readonly retryAfterMs?: number,
  ) {
    super(`DSR ${code} (HTTP ${status})`);
  }
}

/**
 * 校验“非负十进制整数字符串”。
 *
 * 协议里所有序号、epoch 都用字符串传输，避免 JSON number 精度问题。
 * 规则：只允许 0 或非 0 开头；positive=true 时不允许 "0"。
 *
 * @example
 *   decimal("0")        // "0"
 *   decimal("42")       // "42"
 *   decimal("042")      // 抛 invalid_response（前导零容易造成歧义）
 *   decimal("0", true)  // 抛 invalid_response（epoch 必须 > 0）
 */
export function decimal(value: unknown, positive = false): string {
  if (
    typeof value !== "string" ||
    !/^(0|[1-9]\d*)$/.test(value) ||
    (positive && value === "0")
  )
    throw new DsrError(0, "invalid_response");
  return value;
}

/**
 * 校验值是“普通对象”（非 null、非数组）。
 * 用它包住所有外部 JSON，避免把数组/标量当对象访问时出现隐晦错误。
 */
export function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new DsrError(0, "invalid_response");
  return value as Record<string, unknown>;
}

/**
 * 校验并解析一个 claim 响应。
 *
 * 必需字段：code_session_id / session_token / model_token（非空字符串）、
 * worker_epoch（正十进制字符串）、claim_expires_at（可解析的时间字符串）。
 *
 * @example
 *   parseClaim({
 *     code_session_id: "cse_1",
 *     worker_epoch: "7",
 *     session_token: "tok",
 *     model_token: "mtok",
 *     claim_expires_at: "2026-11-04T12:00:30Z",
 *   }); // 通过
 *
 *   parseClaim({..., worker_epoch: "0"}); // 抛 invalid_response（epoch 必须 > 0）
 */
export function parseClaim(value: unknown): Claim {
  const body = object(value);
  for (const name of ["code_session_id", "session_token", "model_token"])
    if (typeof body[name] !== "string" || !body[name])
      throw new DsrError(0, "invalid_response");
  decimal(body.worker_epoch, true);
  if (
    typeof body.claim_expires_at !== "string" ||
    !Number.isFinite(Date.parse(body.claim_expires_at))
  )
    throw new DsrError(0, "invalid_response");
  return body as unknown as Claim;
}

/**
 * 把非 2xx 响应转换成 DsrError。
 *
 * 错误码来源优先级：
 *   1. 服务器显式返回的 DSR-Error 头（最准确）；
 *   2. 没有头时按状态推断：
 *        429 -> rate_limited
 *        >=500 -> unavailable
 *        其他 -> unclassified_response
 *
 * 同时读取：
 *   - DSR-Next-Offset：续读游标；
 *   - Retry-After：秒数，转换成毫秒并封顶 30 秒。
 *
 * 最后会 cancel 响应体，避免连接资源泄漏。
 *
 * @example
 *   // HTTP 409, DSR-Error: epoch_superseded
 *   -> DsrError(409, "epoch_superseded")
 *
 *   // HTTP 429, Retry-After: 10
 *   -> DsrError(429, "rate_limited", undefined, 10000)
 */
export async function errorOf(response: Response): Promise<DsrError> {
  const raw = response.headers.get("DSR-Error");
  const code =
    raw ||
    (response.status === 429
      ? "rate_limited"
      : response.status >= 500
        ? "unavailable"
        : "unclassified_response");
  const cursor = response.headers.get("DSR-Next-Offset");
  const retryAfter = response.headers.get("Retry-After");
  const seconds =
    retryAfter === null || retryAfter.trim() === "" ? NaN : Number(retryAfter);
  await response.body?.cancel().catch(() => {});
  return new DsrError(
    response.status,
    code,
    cursor === null ? undefined : decimal(cursor),
    Number.isFinite(seconds) && seconds >= 0
      ? Math.min(seconds * 1000, 30_000)
      : undefined,
  );
}

/**
 * 可被 AbortSignal 打断的 sleep。
 *
 * 两个用途：
 *   - 生产环境：重试退避、空闲检测、长轮询之间的等待；
 *   - 测试环境：transport.sleep 可注入假时钟，让测试无需真的等几秒。
 *
 * @example
 *   const abort = new AbortController();
 *   setTimeout(() => abort.abort(), 1000);
 *   await delay(transport, 5000, abort.signal); // 约 1 秒后 reject，而不是等 5 秒
 */
export async function delay(
  transport: Transport,
  ms: number,
  signal?: AbortSignal,
): Promise<void> {
  // 已经取消就不必再等，直接抛错。
  signal?.throwIfAborted();
  if (transport.sleep) return transport.sleep(ms, signal);
  await new Promise<void>((resolve, reject) => {
    const complete = () => {
      signal?.removeEventListener("abort", cancel);
      resolve();
    };
    const timer = setTimeout(complete, ms);
    const cancel = () => {
      clearTimeout(timer);
      reject(signal?.reason);
    };
    signal?.addEventListener("abort", cancel, { once: true });
  });
}

/**
 * 带重试的请求。
 *
 * 重试规则（默认最多 4 次尝试）：
 *   - 2xx：直接返回；
 *   - 408 / 429 / 5xx：可重试；
 *   - 其他 4xx（400/401/403/404/409...）：立即抛出，重试没有意义；
 *   - 网络异常（fetch 抛 TypeError 等）：可重试，除非 signal 已取消。
 *
 * 退避时间：优先用服务器给的 Retry-After，否则 100ms * 2^attempt，封顶 5 秒。
 *
 * @example
 *   await request(transport, {path: "/v1/dsr/claims", method: "POST", token});
 *   // 第 1 次 503 -> 等 100ms
 *   // 第 2 次 503 -> 等 200ms
 *   // 第 3 次 200 -> 返回
 *   //
 *   // 若第 1 次就是 401 -> 立即抛 DsrError，不重试
 */
export async function request(
  transport: Transport,
  options: HttpRequest,
  attempts = 4,
): Promise<Response> {
  for (let attempt = 0; ; attempt++) {
    // 每次尝试前检查取消状态，避免在已关闭的会话上发请求。
    options.signal?.throwIfAborted();
    try {
      // 强制带协议版本头；调用方可以覆盖同名头（后展开的优先）。
      const response = await transport.request({
        ...options,
        headers: { "DSR-Protocol": "1", ...options.headers },
      });
      if (response.ok) return response;
      const error = await errorOf(response);
      // 非可重试的 4xx：立即失败。
      if (
        response.status !== 408 &&
        response.status !== 429 &&
        response.status < 500
      )
        throw error;
      // 重试次数用尽：抛出最后一次错误。
      if (attempt + 1 >= attempts) throw error;
      // 退避后重试。
      await delay(
        transport,
        error.retryAfterMs ?? Math.min(100 * 2 ** attempt, 5000),
        options.signal,
      );
    } catch (error) {
      // DsrError 已经在上面处理过；取消或次数用尽也直接抛。
      if (
        error instanceof DsrError ||
        attempt + 1 >= attempts ||
        options.signal?.aborted
      )
        throw error;
      // 网络错误：指数退避后重试。
      await delay(
        transport,
        Math.min(100 * 2 ** attempt, 5000),
        options.signal,
      );
    }
  }
}
