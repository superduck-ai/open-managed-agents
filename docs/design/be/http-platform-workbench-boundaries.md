# HTTP、平台与 Workbench 包边界

本文记录后端应用错误、HTTP 公共层、平台业务路由和 Workbench 业务流的包边界，避免 `internal/httpapi` 承担业务语义。

## 包职责

- `internal/apperr`
  - 是不依赖传输层和具体功能包的叶子包，只定义稳定的错误 `Kind`、安全公开文案和私有 cause。
  - 不维护全局错误码注册表，也不决定 HTTP status、wire error type 或日志级别。
- `internal/httpapi`
  - 仅保留通用 HTTP helper，例如 Anthropic-compatible error shape、JSON 写入、请求解析等。
  - `ErrorAdapter` 在普通 JSON endpoint 的最终边界把 `apperr` 映射为 HTTP 响应，并统一记录最终的 Internal 或未知错误。
  - JSON object 请求体通过公共类型化解码边界处理：资源层传入自身大小上限并使用命名 DTO；patch 的字段存在性、显式 `null` 和多态嵌套值由 DTO 中对应的 `json.RawMessage` 字段保留，不使用顶层 `map[string]json.RawMessage` 字段表。
  - 公共解码边界解析请求体中的首个 JSON object，拒绝 malformed JSON、`null`、非 object 和超限 body；不额外扫描 trailing data 或连续 JSON 值。admin/webhooks 等允许空 body 或要求空 object 的特殊合同继续使用显式薄包装。
  - 不注册业务路由，不持有平台/Workbench 领域类型别名，不直接依赖具体 feature 包。
- 普通 JSON 资源包
  - `internal/agents`、`batches`、`deployments`、`environments`、`models`、`sessions`、`skills`、`vaults` 和 `webhooks` 在当前操作拥有业务语义的位置，把数据库 sentinel、校验错误和资源专属失败翻译为 `apperr`；数据库层不负责公开文案或 HTTP 分类。
  - 每个资源包在根目录的 `errors.go` 集中保存稳定公开文案、应用错误构造和下层错误映射；普通 JSON 路由返回 `error`，并由该资源持有的同一个 `ErrorAdapter` 收口。
  - 成功响应、Webhook/后台副作用和数据库调用顺序保持不变；best-effort 副作用继续由拥有它的资源层记录，因为其失败不会成为当前 HTTP 请求的最终响应。
- `internal/db`
  - 继续返回普通 Go error 或可识别 sentinel，不依赖 `apperr` 或 `httpapi`，也不构造公开错误文案。
- `internal/platformapi`
  - 承载平台/console 相关 HTTP route registration、请求解析、响应映射和轻量业务编排。
  - 继续依赖 `internal/platform` 的领域类型与错误，并在 HTTP 边界完成 JSON shape 映射。
  - Agent 可观测查询在这里完成 organization/workspace scope、HTTP body/query 提取和响应写入；变量类型、时间范围及 response DTO 由 `internal/observability` 统一维护。
  - 负责目录、登录、组织 profile/SSO、console workspace/API key/member/invite、billing/usage、environment token，以及管理后台独立的 platform Messages proxy。
- `internal/observability`
  - 持有 Agent 可观测的变量合同、业务查询、结果 DTO 与稳定错误，并把后端查询错误映射为业务错误；不依赖 `internal/platformapi`。中立核心不引用 OpenObserve 方言，OpenObserve 适配位于其子包。
- `internal/messages`
  - 承载共享的 Anthropic-compatible `POST /v1/messages` handler、敏感 header 清洗、上游凭证注入与 JSON/SSE 响应转发。
  - 服务 service API 和 platform `/v1`；不替代管理后台的 organization-scoped platform proxy，也不负责入口鉴权。
- `internal/workbench`
  - 承载 Workbench HTTP route registration、prompt/revision/evaluation/KV 业务流，以及上游 Anthropic 代理调用。
  - 只通过 `RegisterOrgWorkbenchRoutes` 暴露路由挂载入口给 `internal/api`；入口接收数据库和 Vault service，按请求 workspace 与真实模型 ID 解析 Provider。
- `internal/codesessions`
  - `Handler` 是 code-session 的 HTTP/协议边界，负责 chi 路由注册、请求鉴权、CCR worker ingress、upstream proxy、MITM CA 生命周期与 OTLP 文件日志锁。
  - `Service` 是可跨入口复用的业务边界，只依赖数据库并负责编排 code-session 创建、事件队列、worker 输出映射、tool permission 与公开 session 事件发布。
  - `Handler`、`sessions.Handler` 共享同一个 `Service` 实例；这样 worker 输出与公开 session stream 使用同一个 `PublicEventSink`，不会因拆分而中断事件发布。

## 路由组装

`internal/api/server.go` 仍负责顶层 chi router、全局 middleware、鉴权入口选择和资源挂载：

- `registerVersionedAPIRoutes` 统一挂载 `/v1`、`/v2`；`/v1` 通用资源只注册一次，并由凭据感知中间件选择 service API key 或 platform session 鉴权链。
- `/v1` platform privacy consent 路由从 `platformapi` 注册；code-session worker、ingress 与 upstream proxy 路由由 `codesessions.Handler` 注册，并在 handler 内执行专用鉴权。
- `/v1/messages` 进入通用凭据感知中间件；code-session Messages token 只在 service auth 的这个 `POST` 路径被接受。
- `registerPlatformConsoleRoutes` 将 `/api`、`/auth`、`/oauth`、`/web-api` 的平台 console 路由直接注册到根 chi router，不再通过成对的精确路径和 wildcard handler 转发到第二个 router。
- `/api/organizations/{orgUuid}` 下的 Workbench 子路由从 `workbench` 注册，并由 `internal/api` 注入数据库和 Vault service。
- `observability.enabled=true` 时，`/api/organizations/{orgUuid}/observability/*` 在同一鉴权组内注册；关闭时不注册并返回 404。原先只返回零值的 `/analytics/sessions/{overview,timeseries}` 兼容路由已删除。

除上述 observability 路由替换外，其他路径、middleware 顺序、鉴权入口和响应结构保持不变。

## 依赖方向

```mermaid
flowchart LR
    API["internal/api"] --> Resources["普通 JSON 资源包"]
    Resources --> DB["internal/db"]
    Resources --> HTTP["internal/httpapi"]
    Resources --> AppErr["internal/apperr"]
    HTTP --> AppErr
```

- `internal/api` 可以依赖 `internal/httpapi`、`internal/messages`、`internal/platformapi`、`internal/workbench`。
- `internal/platformapi` 和 `internal/workbench` 可以依赖 `internal/httpapi` 与 `internal/llmproviders`；二者不读取模型进程配置。`internal/platformapi` 还可以依赖 `internal/observability` 的业务类型与错误，反向依赖禁止。
- `internal/httpapi` 不依赖 `internal/platformapi`、`internal/workbench` 或具体业务 handler。
- `internal/platform` 保持领域类型/错误包，不引入 HTTP handler，避免与 `internal/db` 形成反向依赖或 import cycle。
- `internal/api` 只保存 `codesessions.Handler` 作为 HTTP 资源入口；需要创建 code session 或发布事件的 `sessions`、`environments` 依赖 `codesessions.Service`，不依赖 HTTP handler。
- `codesessions.Service` 不持有 `config.Config`、WebSocket/CA cache 或 HTTP client。协议状态只能由长生命周期的 `codesessions.Handler` 持有。

## 应用错误与最终 HTTP 边界

- 默认映射覆盖 `InvalidArgument`、`InvalidState`、`PreconditionFailed`、`RequestTooLarge`、`Unauthenticated`、`Billing`、`PermissionDenied`、`NotFound`、`Conflict`、`RateLimited`、`Timeout`、`Internal`、`Unavailable` 和 `Overloaded`，对应 Anthropic 的标准 HTTP status 与 `error.type`。
- `InvalidState`、`PreconditionFailed`、`RequestTooLarge` 和 `Unavailable` 分别保留现有的 `409 / invalid_request_error`、`412 / invalid_request_error`、`413 / invalid_request_error` 和 `503 / api_error` 合同；这些 Kind 表达跨资源可复用的应用语义，不包含 HTTP 类型或状态码。
- 只有 Kind 和安全公开文案都合法的应用错误才采用该映射。未知 Kind、空文案和普通 error 一律返回通用 500，避免意外泄漏内部错误。
- 响应遵循 Anthropic error shape，只包含既有的 `error.type` 和 `error.message`，不增加 feature error code。
- 已知 4xx 不写 Error 日志。Internal、Timeout、Unavailable、Overloaded 和未知错误只在 adapter 记录一次，稳定字段为 `request_id`、`method`、`path`、`error_kind`、`error`；`path` 不包含 query string。
- `ErrorAdapter.Wrap` 只用于尚未提交响应的普通 JSON endpoint。Batches results、Skills content 和 Sessions SSE 不使用 wrapper；它们在提交下载或事件流之前可以调用 `ErrorAdapter.Write`，提交之后的 I/O 失败仍沿用协议专属日志策略。
- 下列边界不接入默认 adapter，除非后续逐项确认并扩展对应协议：
  - Filestore 使用独立的 rclone-filestore 错误 envelope 和 code；Files 同时包含下载、multipart 与平台 Cookie 鉴权合同。
  - Messages 与 Code Sessions 包含上游响应透传、SSE/流式代理，以及 `405`、`410`、`415` 等专属错误类型。
  - Memory 的 path conflict 和 precondition failure 带自定义 `error.type` 与附加字段；MCP Catalog 对上游 `5xx` 也维持既有 `invalid_request_error` 类型。
  - Admin、platform console 和 `internal/api` 全局 middleware 具有动态 status/type、重定向、Cookie 或全局鉴权合同，不按单一资源默认映射处理。

## 兼容与测试

本次拆分是保持行为不变的机械迁移。验证重点：

- `go test ./internal/apperr ./internal/httpapi ./internal/vaults -count=1`
- `go test ./internal/httpapi ./internal/platformapi ./internal/workbench -count=1`
- `go test ./internal/agents ./internal/batches ./internal/deployments ./internal/environments ./internal/models ./internal/sessions ./internal/skills ./internal/vaults ./internal/webhooks -count=1`
- `go test ./internal/codesessions ./internal/sessions ./internal/environments -count=1`
- `go test ./internal/api -count=1`
- `go test ./... -count=1`

若全量测试失败，应先区分是否来自既有 platform-host 分流/会话恢复问题，避免把行为修复混入包边界迁移。

Workbench 上游测试必须覆盖 workspace Provider 解析、真实模型 ID 原样使用，以及未配置 Provider 时失败关闭。
