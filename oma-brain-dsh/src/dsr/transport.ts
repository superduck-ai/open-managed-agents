// ============================================================================
// Transport 适配层：把 OmaApi 收窄成 DSR 模块依赖的最小 HTTP 接口
// ============================================================================
//
// DSR 下层的 claim / client / input-stream 等模块不直接依赖 OmaApi，
// 而是依赖 http.ts 里定义的 Transport 接口：
//
//   interface Transport {
//     request(options): Promise<Response>;
//     sleep?(ms, signal): Promise<void>;   // 测试可注入假时钟
//   }
//
// 好处有两个：
//   1. 单测可以传入内存 fake transport，不发真实网络请求；
//   2. 重试、超时、鉴权头的拼装各自有明确归属，不会散落在每个调用点。
//
// 注意这里只做字段透传，不做重试：
//   - 重试策略在 http.ts 的 request()（默认最多 4 次，指数退避）；
//   - 单次请求的超时由调用方传入 timeout（例如 claim 需要 block_ms + 15s）。
// ============================================================================

import type { OmaApi } from "../api.ts";
import type { Transport } from "./http.ts";

/**
 * 用真实的 OmaApi 构造 Transport。
 *
 * OmaApi.requestResponse 会把 token 放进 Authorization、把 body 序列化成 JSON，
 * 并处理 timeout + AbortSignal 的组合；这里只需要把 DSR 的字段一一对应过去。
 *
 * @example
 *   const transport = createDsrTransport(api);
 *   const claims = new DsrClaims(transport, token, workerId);
 *
 *   // 之后 claims.claim(25000) 内部调用 transport.request({...}) 时，
 *   // 实际执行的就是：
 *   //   api.requestResponse("/v1/dsr/claims?block_ms=25000", token, "POST", {}, headers, signal, 40000)
 */
export function createDsrTransport(api: OmaApi): Transport {
  return {
    request: options => api.requestResponse(options.path, options.token, options.method, options.body,
      options.headers, options.signal, options.timeout),
  };
}
