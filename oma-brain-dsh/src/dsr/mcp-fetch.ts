// ============================================================================
// MCP fetch：给 MCP 客户端注入会话凭证并限制可访问的目标
// ============================================================================
//
// DSR 会话里的工具可能连接多个 MCP server：
//   - 内置的 sandbox MCP（服务器地址固定，见 assignment.ts 的 mcp_config）；
//   - 用户配置的外部 MCP server（地址可能来自会话上下文）。
//
// MCP 客户端通常直接调用全局 fetch。这里返回一个“包装过的 fetch”：
//
//   1. 如果请求目标就是本会话的 MCP 端点（或它的 OAuth 元数据路径），直接放行；
//   2. 否则视为外部 MCP 地址，改写成“网关地址 + mcp_url 参数”，
//      由服务器决定是否允许访问，而不是让 Brain 进程直连任意 URL；
//   3. 无论哪种，都强制注入本次会话的 Authorization 和 DSR-Protocol 头；
//   4. 线程头先删除再按当前 threadId 设置，防止调用方把别的线程上下文带进来。
//
// 例子（baseURL = "https://api.example.com"）：
//
//   const fetch = createDsrMcpFetch(baseURL, claim, globalThis.fetch, "thread_9");
//
//   // 情况 A：请求本会话 MCP 端点
//   await fetch("https://api.example.com/v1/dsr/sessions/cse_1/mcp");
//   // -> 原样请求，附带 Authorization: Bearer <session_token>
//
//   // 情况 B：请求外部 MCP server
//   await fetch("https://mcp.vendor.com/sse");
//   // -> 实际请求：
//   //    https://api.example.com/v1/dsr/sessions/cse_1/mcp?mcp_url=https%3A%2F%2Fmcp.vendor.com%2Fsse
//   //    Authorization / DSR-Protocol / X-OMA-Session-Thread-ID 由这里设置
// ============================================================================

import type { Claim } from "./wire.ts";

/**
 * 构造一个绑定到具体会话的 fetch。
 *
 * @param baseURL 服务器地址（api.base），用于判断“是否直接命中本会话端点”。
 * @param claim   至少包含 code_session_id 和 session_token。
 * @param fetcher 实际执行请求的 fetch（测试可注入假实现）。
 * @param threadId 当前子线程 ID；存在时会写入 X-OMA-Session-Thread-ID。
 *
 * 安全细节：
 *   - 覆盖而不是追加 Authorization，调用方无法用自己的 token 调 MCP；
 *   - 先 delete 线程头再 set，避免外部把请求伪装成另一个线程；
 *   - redirect: "error" 禁止跟随重定向，防止凭证被带到第三方地址。
 */
export function createDsrMcpFetch(baseURL: string, claim: Readonly<Pick<Claim, "code_session_id" | "session_token">>, fetcher: typeof fetch, threadId?: string): typeof fetch {
  // 预先解析 base，后面只比较 origin，避免字符串前缀匹配被伪造域名绕过。
  const base = new URL(baseURL);
  const token = claim.session_token;
  // 本会话的 MCP 端点；路径里对 session id 做 URL 编码。
  const path = `/v1/dsr/sessions/${encodeURIComponent(claim.code_session_id)}/mcp`;
  // OAuth Protected Resource Metadata 端点也属于会话 MCP 资源，需要允许直达。
  const metadata = `/.well-known/oauth-protected-resource${path}`;
  return ((input: string | URL | Request, init?: RequestInit) => {
    // 用 Request 包装一次，兼容字符串 / URL / Request 三种入参，并复制原请求头与 body。
    const request = new Request(input, init);
    const target = new URL(request.url);
    // direct 判定必须同时满足：同源 + 路径命中三个允许前缀之一。
    // 例：https://evil.example/v1/dsr/sessions/cse_1/mcp 不同源 -> 走网关。
    const direct = target.origin === base.origin && (target.pathname === path || target.pathname.startsWith(`${path}/`) || target.pathname.startsWith(`${metadata}/`));
    // 非直达请求：统一改写成网关地址，并把原 URL 放进 mcp_url 参数。
    const gateway = direct ? target : new URL(`${baseURL.replace(/\/$/, "")}${path}`);
    if (!direct) gateway.searchParams.set("mcp_url", target.href);
    // new Request(gateway, request)：保留 method/body/headers，替换 URL。
    const forwarded = new Request(gateway, request);
    forwarded.headers.set("Authorization", `Bearer ${token}`);
    forwarded.headers.set("DSR-Protocol", "1");
    // 关键：调用方传入的线程头一律丢弃，只允许当前上下文设置。
    forwarded.headers.delete("X-OMA-Session-Thread-ID");
    if (threadId) forwarded.headers.set("X-OMA-Session-Thread-ID", threadId);
    return fetcher(forwarded, { redirect: "error" });
  }) as typeof fetch;
}
