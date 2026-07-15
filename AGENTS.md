# Open Managed Agents

## 本地重启脚本

- 在仓库根目录使用 `just restart-server` 重启本地后端服务，地址为 `127.0.0.1:38080`。
- `just restart-server` 会调用 `./scripts/restart-server.sh`，杀掉所有监听 `PORT`（默认 `38080`）的进程，等待端口释放，必要时升级为 `kill -9`，然后以前台方式执行 `ADDR=127.0.0.1:$PORT go run .`。
- 仅在有意测试不同绑定地址时，才使用 `PORT=...` 或 `ADDR=... just restart-server` 覆盖默认值。
- 如果修改了 `web/` 下的前端代码，在使用浏览器或 SuperDuck 验证前，也要从仓库根目录执行 `just restart-web` 重启前端开发服务器。该命令会调用 `./scripts/restart-web.sh`，只停止当前仓库路径启动的 Vite 监听进程；如果目标端口被其他路径的进程占用，则保留该进程并自动选择后续可用端口以前台方式启动前端。

## GitHub PR 提交身份

- 本仓库的 Pull Request 必须通过本机已认证的 `gh` CLI 创建；禁止使用 Codex GitHub Connector 或其 GitHub App 创建 PR。
- 创建 PR 前必须依次运行 `gh auth status` 和 `gh api user --jq .login`，记录当前登录账号。任意已认证账号均可使用；仅当用户明确指定了账号且当前登录账号不匹配时，才应停止并请求用户处理。
- 分支、提交和推送仍使用本地 `git`；分支推送成功后，使用 `gh pr create --draft ...` 创建 Draft PR，并通过 `gh pr view --json author,url,isDraft` 确认 PR 作者与创建前记录的登录账号一致且状态为 Draft。

## 提交前质量门禁

- 首次 clone 仓库或发现 hook 尚未安装时运行 `just hooks-install`，为当前 Git 仓库安装受管的 pre-commit hook；同一 clone 下的 worktree 共用该 hook。缺少 `pre-commit` 时，脚本会优先通过 `uv` 安装固定版本。
- hook 对暂存文件执行通用文件卫生检查，对 Go 文件执行 `gofmt`、对应 package 的 golangci-lint、不可达声明检测、重复代码检测、文件行数、函数长度和生产代码复杂度检查，并用项目固定版本的 Prettier 格式化前端文件，同时检查 TypeScript 重复代码、命名、文件行数、函数长度与复杂度。
- 使用 `just hooks-run` 对全部跟踪文件复跑相同检查；不要使用 `SKIP` 绕过失败项，除非用户明确批准并记录原因。
- 使用 `just large-files` 通过仓库固定版本的 `check-added-large-files --enforce-all` 检查全部受跟踪文件，统一上限为 1 MiB。嵌入式目录快照 `internal/platformapi/directory_servers.json` 必须保存为紧凑 JSON。不要通过改名、忽略路径或提高预算绕过失败；有意引入大二进制文件时，应单独评审 Git LFS 策略。

## 死代码检测

- 修改 Go 代码后运行 `just dead-code`。`.golangci-dead-code.yml` 使用 `unused` 分析器检查生产代码和测试中的不可达包级函数、方法、变量、常量与类型。
- pre-commit 和 `.github/workflows/dead-code.yml` 使用同一配置；不要通过 `nolint`、全局排除、伪造引用或保留无调用路径的兼容包装来绕过失败。确认不再可达的声明及其专属辅助链应直接删除。

## 重复代码预算

- 修改 Go 或 TypeScript/TSX 生产代码后运行 `just duplicates`。项目固定的 jscpd 以 strict token 模式检测至少 12 行且至少 70 token 的复制代码；Go 与前端分别执行，避免一个应用较低的比例掩盖另一个应用的增长。
- `.jscpd.json` 将 Go 生产代码重复率限制为 3.75%，`web/.jscpd.json` 将前端生产代码重复率限制为 1.1%。测试、suite 和生成文件不计入生产预算；不要通过扩大 ignore、提高百分比、提高最小行数/token 数或拆成近似副本绕过门禁。
- pre-commit 和 `.github/workflows/duplicate-code.yml` 使用 `scripts/check-duplicates.sh` 执行相同门禁。预算失败时优先抽取领域辅助函数、共享数据映射或展示组件；只有确实共享同一语义与演进方向的代码才应合并。

## 复杂度预算

- 修改 Go 或 TypeScript/TSX 生产代码后运行 `just complexity`。Go 使用 `cyclop`，单函数最大圈复杂度为 30；`funlen` 以当前最长函数作为 ratchet，限制函数最多 163 行且最多 100 条语句；测试文件不计入生产函数指标。
- Go 生产文件默认最多 500 个物理行，测试文件默认最多 1000 行；`scripts/go-file-line-budgets.txt` 中的历史热点必须与当前行数精确一致。文件缩短时在同一变更中下调预算，文件增长会直接失败；生成文件不参与该门禁。
- 新增前端生产文件默认最多 500 个有效行、单函数最多 200 个有效行，并使用 ESLint modified cyclomatic complexity 上限 20。`web/eslint.complexity.config.js` 中列出的历史热点以当前测量上限作为 ratchet，修改这些文件时不得提高预算，并应优先通过拆分纯函数、数据映射或展示组件降低预算。
- 不要通过 `nolint`、ESLint disable 注释、忽略新增生产文件或提高任何行数、函数长度或复杂度阈值来绕过失败。确需调整预算时，必须同时说明无法拆分的边界原因，并更新 `docs/design/development-complexity-guardrails.md`。
- pre-commit 和 `.github/workflows/complexity.yml` 都调用仓库固定的复杂度配置；本地验收入口为 `just complexity`。

## 命名规范

- Go package 名使用简短的小写单词；导出类型、函数和方法使用 PascalCase，未导出标识符使用 mixedCaps。缩写保持 Go 惯例并在同一标识符中一致，例如 `API`、`HTTP`、`ID`、`URL`、`UUID`；接收器名应简短且在同一类型的方法中一致。
- TypeScript/React 的类型、接口、类和组件使用 PascalCase；普通变量、函数和参数使用 camelCase；模块级常量可使用 UPPER_CASE；泛型类型参数使用 PascalCase。以 PascalCase 命名的函数参数只用于组件或构造器引用等可调用类型。
- Anthropic/API、数据库和第三方 payload 的字段名属于外部合同，可在边界 DTO、对象属性和解构中保留 `snake_case`；进入内部变量或业务模型后应映射为上述语言惯例，不要把例外扩散到业务标识符。
- Go 命名由 `.golangci.yml` 中 `revive/var-naming` 强制；前端命名由 `bun run lint:naming` 强制，并在 pre-commit 与 `.github/workflows/web-naming.yml` 中执行。

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
- 编写或更新设计文档时，优先用 Mermaid 辅助说明复杂流程、状态机、组件/服务依赖、时序交互和数据流；图示应服务于理解，不要替代必要的文字说明。

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
- 修改 `web/` 下的文件后，运行 `just web-format-check`，确保 Prettier 格式门禁通过。
- 修改 Go 代码后，运行 `just lint`；该命令使用仓库根目录的 `.golangci.yml` 执行与 CI 相同的静态分析和格式检查。
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

## Cursor Cloud specific instructions

这些是为 Cursor Cloud 环境准备的补充说明。系统依赖（Go 1.26.2、Bun、`just`、`golangci-lint` v2.12.2、`uv`、PostgreSQL 16、Redis、MinIO/`mc`）已经安装在 VM 快照中，启动脚本（update script）只负责刷新 `go mod download` 和 `web` 的 `bun install`，不会启动任何服务。

### 每次会话开始时先启动基础设施

后端启动时会 **自动建库、自动迁移（dev 默认 `DB_AUTO_MIGRATE=true`）、自动 seed API key、自动创建 MinIO bucket**，所以只要把三项基础设施拉起来即可，无需手动建 `claude_api` 库或 `claude-files` bucket。这三项 **不会** 在 VM 启动时自动运行，需要手动启动一次（都写好了默认连接串，见 `internal/config/config.go`）：

```bash
sudo pg_ctlcluster 16 main start || true          # PostgreSQL :5432
sudo redis-server --daemonize yes --port 6379 --dir /var/lib/redis
# MinIO 前台进程，建议放到 tmux/后台：
MINIO_ROOT_USER=minioadmin MINIO_ROOT_PASSWORD=minioadmin \
  minio server /home/ubuntu/minio-data --address :9000 --console-address :9001
```

- PostgreSQL 的 `pg_hba.conf` 已改为对 `127.0.0.1/32` 和 `::1/128` 使用 `trust`，因此 `postgres` 管理连接（`POSTGRES_ADMIN_URL`）和 `claude:123456` 业务连接都能免密/按默认串直接连上。
- 起服务用现成的 `just restart-server`（后端 `127.0.0.1:38080`）和 `just restart-web`（前端 `127.0.0.1:5173`，`/api` 与 `/v1` 反代到后端）。两者都是前台阻塞命令，在 Cloud 环境里请放进 tmux 或后台运行。

### 控制台登录（本地 magic-link）

- 本地登录是 dev 版 magic-link：登录页输入任意邮箱（如 `demo@example.com`）+ 任意 6 位验证码（如 `000000`）即可，后端 `verify_magic_link` 会直接创建用户并下发 session cookie。
- 已知的前端遗留问题：点击 “Verify email” 完成登录跳转的一瞬间，前端可能报 `Maximum update depth exceeded` 并白屏（后端此时已返回 200 且 cookie 已写入）。**刷新一次页面即可进入已登录的控制台**，属于既有前端行为，与环境配置无关。

### Lint 的 node_modules 误报

- 执行过 `web` 的 `bun install` 后，`just lint`（即 `golangci-lint run ./...`）会对 `web/node_modules/flatted/golang/...` 里一个自带的 `.go` 文件报 `govet` 误报。CI 的 Go lint job 不安装前端依赖，所以不受影响。
- 想在本地复现 CI 的 Go lint 结果，把范围限定到仓库自身的 Go 源码目录即可：`golangci-lint run --config .golangci.yml ./ ./cmd/... ./internal/... ./tests/...`（`go test`/`go list` 本身会忽略 `node_modules`，不受影响）。

### 其它注意点

- Files API 需要带 header `anthropic-beta: files-api-2025-04-14`，否则返回 400（`?beta=true` 单独不够）。
- 已知既有失败用例：`go test ./... -count=1` 中 `TestFilesAPI/routing_group_auth_applies_only_to_v1` 会失败（未鉴权访问 `/v1/unknown` 期望 401，实际返回 404）。这是当前 base 分支上与环境无关的既有行为，其余用例通过。
