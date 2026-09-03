# MCP 连接/认证失败产生 session.error 事件

> Issue: #254
> 日期: 2026-08-16
> 分支: `feat/mcp-failure-events`
> 依赖: #251（session.error 事件模型，已完成）

## 背景

官方 Claude Managed Agents 的 MCP connector 契约要求：**MCP server 连接失败或认证失败时，发 `session.error` 事件**，错误对象带 `mcp_server_name` 和 `retry_status`（`mcp_connection_failed_error` / `mcp_authentication_failed_error` 类型）。客户端据此感知「哪个 MCP server 挂了、是连接还是认证问题、会不会重试」。

OMA 的 MCP 代理当前**只返回 HTTP 502**（`internal/codesessions/mcp_proxy.go:108-113` 的 `ErrorHandler`），**不发任何事件**——客户端无法感知 MCP server 故障。

## 官方契约

- MCP 连接失败 → `session.error`，类型 `mcp_connection_failed_error`，带 `mcp_server_name` + `retry_status`
- MCP 认证失败 → `session.error`，类型 `mcp_authentication_failed_error`，带 `mcp_server_name` + `retry_status`
- `retry_status` 表示是否自动重试

## 现状

- `internal/codesessions/mcp_proxy.go:108-113`：上游不可达 → 502 `api_error`，无事件
- `internal/codesessions/mcp_proxy.go:69-73`：凭证不可用 → 502，无事件
- `#251` 已完成：`session.error` 事件模型可用（`CategoryFor` 分类 + worker 失败路径产生）

## 方案

1. **连接失败**（`ErrorHandler`）：上游不可达时，构造 `session.error`：
   - `type`: `"mcp_connection_failed_error"`
   - `mcp_server_name`: 从 `target.Hostname()` 派生（MCP server 的 host；与官方文档按配置 `name` 的契约存在偏差，改为配置名需扩展 policy 编译链，见 issue #254 后续）
   - `retry_status`: `"retryable"`（网络瞬时故障可重试）
   - `message`: 上游错误信息（脱敏，不含 URL query）
   - 经 `publishMCPErrorEvent` → `Service.publishPublicPayloads` → `PublishCodeSessionEvents`（写事件流 + 广播，2s 超时上界）
   - 客户端主动断开（`request.Context().Err() != nil`）不发布，与日志守卫一致
2. **认证失败**（ReverseProxy ErrorHandler 收到裸 `ErrInjectionRejected`——host 需要注入但无匹配凭证）：
   - `type`: `"mcp_authentication_failed_error"`
   - `retry_status`: `"not_retryable"`（凭证无效，重试无意义）
   - 带内部 cause 的注入拒绝（vault 读取/解密故障）归为连接失败，不误报认证失败（`mcpAuthRejected`）
3. **HTTP 502 保留**（API 层兼容），事件流额外发 `session.error`（双通道）

### 安全边界

- 错误消息**不含** URL query / 凭证信息（脱敏）
- `mcp_server_name` 只取 hostname，不含 path/query

## 测试

- MCP proxy 连接失败 → 事件流出现 `session.error`（`mcp_connection_failed_error` + retry_status + mcp_server_name）
- 凭证注入失败 → `session.error`（`mcp_authentication_failed_error`）
- HTTP 层仍返回 502（回归）
- 错误消息无敏感信息

## 验收

- MCP server 连接失败 / 认证失败时，事件流出现 `session.error`（含 mcp_server_name + retry_status + 正确类型）
- HTTP 层仍返回 502（兼容）
- 测试覆盖两种失败场景

## Review 修正（2026-08-16）

**已修**：
- 事件通道改为 `publishPublicPayloads` → `PublishCodeSessionEvents`（写 session 事件流 + 广播，SSE 消费者可读）；原实现走 `QueueRawPublicSessionEvents`（code_session 入站队列，消费者读不到）
- `publishPublicPayloads` 增加 nil db 防护（测试 handler 用 nil db 构造）
