# HTTP、平台与 Workbench 包边界

本文记录后端 HTTP 公共层、平台业务路由和 Workbench 业务流的包边界，避免 `internal/httpapi` 继续承担兜底业务职责。

## 包职责

- `internal/httpapi`
  - 仅保留通用 HTTP helper，例如 Anthropic-compatible error shape、JSON 写入、请求解析等。
  - 不注册业务路由，不持有平台/Workbench 领域类型别名，不直接依赖具体 feature 包。
- `internal/platformapi`
  - 承载平台/console 相关 HTTP route registration、请求解析、响应映射和轻量业务编排。
  - 继续依赖 `internal/platform` 的领域类型与错误，并在 HTTP 边界完成 JSON shape 映射。
  - 负责目录、登录、组织 profile/SSO、console workspace/API key/member/invite、billing/usage、environment token、platform proxy 等平台 HTTP 资源。
- `internal/workbench`
  - 承载 Workbench HTTP route registration、prompt/revision/evaluation/KV 业务流，以及上游 Anthropic 代理调用。
  - 只通过 `RegisterOrgWorkbenchRoutes` 暴露路由挂载入口给 `internal/api`。
- `internal/codesessions`
  - `Handler` 是 code-session 的 HTTP/协议边界，负责 chi 路由注册、请求鉴权、CCR worker ingress、模型代理、upstream proxy、MITM CA 生命周期与 OTLP 文件日志锁。
  - `Service` 是可跨入口复用的业务边界，只依赖数据库并负责编排 code-session 创建、事件队列、worker 输出映射、tool permission 与公开 session 事件发布。
  - `Handler`、`sessions.Handler` 共享同一个 `Service` 实例；这样 worker 输出与公开 session stream 使用同一个 `PublicEventSink`，不会因拆分而中断事件发布。

## 路由组装

`internal/api/server.go` 仍负责顶层 chi router、全局 middleware、鉴权入口选择和资源挂载：

- `registerVersionedAPIRoutes` 统一挂载 `/v1`、`/v2`；`/v1` 通用资源只注册一次，并由凭据感知中间件选择 service API key 或 platform session 鉴权链。
- `/v1` platform privacy consent 路由从 `platformapi` 注册；code-session runtime 路由由 `codesessions.Handler` 注册，并在 handler 内执行专用鉴权。
- `registerPlatformConsoleRoutes` 将 `/api`、`/auth`、`/oauth`、`/web-api` 的平台 console 路由直接注册到根 chi router，不再通过成对的精确路径和 wildcard handler 转发到第二个 router。
- `/api/organizations/{orgUuid}` 下的 Workbench 子路由从 `workbench` 注册。

路径、middleware 顺序、鉴权入口和响应结构在本次迁移中保持不变。

## 依赖方向

- `internal/api` 可以依赖 `internal/httpapi`、`internal/platformapi`、`internal/workbench`。
- `internal/platformapi` 和 `internal/workbench` 可以依赖 `internal/httpapi` 的公共 helper。
- `internal/httpapi` 不依赖 `internal/platformapi`、`internal/workbench` 或具体业务 handler。
- `internal/platform` 保持领域类型/错误包，不引入 HTTP handler，避免与 `internal/db` 形成反向依赖或 import cycle。
- `internal/api` 只保存 `codesessions.Handler` 作为 HTTP 资源入口；需要创建 code session 或发布事件的 `sessions`、`environments` 依赖 `codesessions.Service`，不依赖 HTTP handler。
- `codesessions.Service` 不持有 `config.Config`、bridge authenticator、WebSocket/CA cache 或 HTTP client。协议状态只能由长生命周期的 `codesessions.Handler` 持有。

## 兼容与测试

本次拆分是保持行为不变的机械迁移。验证重点：

- `go test ./internal/httpapi ./internal/platformapi ./internal/workbench -count=1`
- `go test ./internal/codesessions ./internal/sessions ./internal/environments -count=1`
- `go test ./internal/api -count=1`
- `go test ./... -count=1`

若全量测试失败，应先区分是否来自既有 platform-host 分流/会话恢复问题，避免把行为修复混入包边界迁移。
