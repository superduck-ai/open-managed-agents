# Open Managed Agents

## 本地重启脚本

- 在仓库根目录使用 `just restart-server` 重启本地后端服务，地址为 `127.0.0.1:38080`。
- `just restart-server` 会调用 `./scripts/restart-server.sh`，杀掉所有监听 `PORT`（默认 `38080`）的进程，等待端口释放，必要时升级为 `kill -9`，然后以前台方式执行 `ADDR=127.0.0.1:$PORT go run .`。
- 仅在有意测试不同绑定地址时，才使用 `PORT=...` 或 `ADDR=... just restart-server` 覆盖默认值。
- 如果修改了 `web/` 下的前端代码，在使用浏览器或 SuperDuck 验证前，也要从仓库根目录执行 `just restart-web` 重启前端开发服务器。该命令会调用 `./scripts/restart-web.sh`，只停止当前仓库路径启动的 Vite 监听进程；如果目标端口被其他路径的进程占用，则保留该进程并自动选择后续可用端口以前台方式启动前端。

## 前端设计方向

- 前端实现细节位于 `web/AGENTS.md`。
- 常见控件优先使用官方 shadcn/ui 组件目录，采用原生 shadcn 风格，并采用 `new-york` 风格、Base UI primitives、Tailwind CSS 和语义化 CSS 变量。
- 在采用原生 shadcn 风格的同时，保持 Open Managed Agent 的产品语义、API 兼容性、路由、鉴权和后端边界不变。
- 如果 shadcn/ui 已提供组件，不要手写菜单、对话框、选择器、表单字段、表格、开关、标签页、提示框或其他通用控件。

## 前端组件架构

- 前端代码优先遵循单一职责、关注点分离、高内聚、低耦合，以及 feature-sliced 模块化。
- 路由或页面入口文件应保持精简。它们只负责选择 workspace/route 上下文、挑选正确的功能界面，并维持稳定的公共导入；功能逻辑应放在下层的聚焦模块中。
- 在给大型前端界面继续加行为前，先按职责拆分：
  - 领域类型与 schema
  - API/数据访问与流式处理辅助函数
  - 功能页面与功能专属 hooks
  - 展示型组件与共享控件
  - 格式化、解析、路由与标签辅助函数
- 优先采用 `quickstart/`、`agents/`、`sessions/`、`resources/` 这类垂直功能切片，而不是兜底式 utility 文件。只有在代码确实复用且与领域无关时才抽共享。
- 依赖方向要清晰：共享基础模块不能导入功能页面；功能页面可以导入共享基础模块；避免循环依赖。
- 重构现有前端代码时，先做保持行为不变的机械迁移。除非用户明确要求修改行为，否则不要改变 API 请求、状态流、路由语义、文案、样式或测试预期。
- 在可能的情况下保留现有导入的公共外观，这样路由模块等调用方不需要承受无关改动。
- 验证前端重构时，运行窄范围功能测试加上 `bun run build`；如果 lint 失败来自既有逻辑且这次任务只是结构调整，应报告问题，而不是为了让 lint 通过去改写行为。

## HTTP 路由

- 顶层 HTTP 路由、资源挂载和资源级子路由统一使用 `github.com/go-chi/chi/v5`。
- 添加嵌套资源或共享中间件时，优先使用 chi 的 [Sub Routers](https://go-chi.io/#/pages/routing?id=sub-routers) 和 [Routing Groups](https://go-chi.io/#/pages/routing?id=routing-groups)。
- 业务 handler 应保持在标准 `net/http` 边界（`http.Handler`、`http.ResponseWriter`、`*http.Request`）上，这样流式下载、JSONL 结果、multipart 上传以及 SDK 兼容性行为都能保持显式。
- 在 `internal/api/server.go` 中使用 `chi.Mount`/路由组注册新的 API 资源，并用 `/{file_id}` 这类 chi pattern 实现资源子路由，而不是手动拆分路径。
- 请求 ID 注入、panic recovery、`/v1/*` 鉴权等横切关注点应放在 API 级中间件中。
- 通过 `internal/httpapi.WriteError` 保持与 Anthropic 兼容的错误结构；新增路由时不要让框架默认错误响应泄漏到 `/v1/*` API。

## 后端设计边界

- 保持单体内的清晰依赖方向，优先遵循现有垂直资源切片，而不是为了套用架构名进行大规模搬迁。
- `internal/api` 只负责服务组装、全局中间件、鉴权入口选择和资源路由挂载；不要在这里堆业务规则、SQL 或资源级请求处理细节。
- `internal/{agents,sessions,files,memory,...}` 这类资源包负责对应 API 资源的 handler、请求校验、业务编排和响应映射。
- `internal/db` 是持久化边界。它不能导入 `internal/api`、`internal/httpapi` 或任何 handler/resource 包；不能构造 HTTP 状态码、HTTP 响应、Anthropic error JSON。
- API 层可以依赖 DB 层；DB 层不能反向依赖 API 层。共享基础包也不能依赖具体功能 handler 或资源包。
- API request/response DTO 不要直接变成数据库 schema 的影子。数据库行结构、API 响应结构和内部业务结构可以相互映射，但不要因为方便而把 HTTP 字段泄漏进 DB 层。
- 业务错误应在 handler/resource 层映射为 `internal/httpapi.WriteError`；DB 层返回普通 Go error、not found/conflict 这类可识别错误或结果状态。
- 多租户边界必须显式：所有 workspace/org 级资源查询和写入都要带 `organization_id`、`workspace_id` 或对应 external scope，避免只按 external_id 全局查询导致越权。
- 鉴权和权限判断属于 API/resource/service 层；DB 层可以做 key lookup/hash 等数据访问，但不要承载“这个用户能否执行某动作”的业务授权决策。
- 多表写入、状态机推进、幂等写入和 outbox/event 写入应保持事务一致性；不要把半个事务散落在多个 handler 分支里。
- 新增抽象前先确认它真的减少重复或保护边界。不要为了 DDD 名词新增空泛的 repository/service/domain 目录。

## 领域建模原则

- 采用务实 DDD：使用业务语言命名包、类型、状态和方法，把核心不变量放在写入路径附近；但不强制引入完整 DDD 目录结构、泛型 repository 或贫血 service 层。
- 对外兼容 Anthropic API 的字段、错误和路由语义属于 API 合同；内部领域命名可以更贴近本项目，但必须在边界层做清晰映射。
- 新增状态字段或状态机时，要把允许的状态、转换条件、幂等行为和并发冲突处理写在靠近写入路径的位置，并覆盖失败场景测试。

## 设计文档同步

- 修改代码后，如果行为、公开 API、事件契约、状态机、数据模型、权限边界、架构边界、测试/验收路径或重要兼容策略发生变化，必须同步更新 `docs/design/` 下对应设计文档。
- 如果现有设计文档已经准确描述本次代码变化，应在最终说明中明确“设计文档无需更新”以及判断依据。
- 不要为了凑文档而写重复内容；优先更新最贴近该功能的后端、前端或跨端设计文档，并保持实现细节、兼容说明和测试计划一致。

## PostgreSQL Schema 规则

- 不要创建 PostgreSQL 外键约束。
- 核心表仍保留 `organization_id`、`workspace_id`、`created_by_api_key_id` 这类 bigint 引用列；完整性由应用写入、迁移代码、seed 代码和 E2E 测试保证。
- 每张核心业务表都使用：
  - `id bigint generated always as identity` 作为数据库内部主键。
  - `uuid uuid default gen_random_uuid()` 作为稳定的业务标识符。
  - `external_id text` 作为兼容 Anthropic API 的 ID，例如 `file_...`。
- 之后所有 DB schema 变更都必须通过 `internal/db/migrations` 中的 goose migration 管理。
- 每次变更新增一个带编号的 migration 文件，例如 `00002_add_xxx.sql`；不要修改已应用的 migration，也不要把新的 schema 变更追加到 `internal/db/schema.go`。
- `Migrate()` 在应用完 goose migrations 后，必须删除当前 schema 中发现的所有外键约束。
- 保留 `tests/files_api_test.go` 中的 no-FK 守卫测试。

## 测试要求

- 测试组织顺序应先写失败场景，再写成功场景。
- 修改 schema 或 handler 后，运行 `go test ./... -count=1`。
- 做真实 E2E 时，先用 `ADDR=127.0.0.1:18080 go run .` 启动本地服务，再以 `TEST_API_BASE_URL=http://127.0.0.1:18080` 和 `sk-ant-local-default` 运行 SDK 测试。
- 自定义 SDK E2E 覆盖：
  - Go：`go test ./tests -run TestGoSDKFilesE2E -count=1 -v`
  - Python：在官方 Python SDK virtualenv 中运行 `tests/e2e/python/files_e2e.py`。
- 官方 SDK files resource 测试：
  - Go SDK：`go test -run 'TestBetaFile' -count=1 -v`
  - TypeScript SDK：`./node_modules/.bin/jest tests/api-resources/beta/files.test.ts --runInBand`
  - Python SDK：`.venv/bin/pytest tests/api_resources/beta/test_files.py -q`
- 结束前停止所有为 E2E 启动的本地服务。
