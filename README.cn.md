# Open Managed Agents

Open Managed Agents 是一个用 Go 实现的本地优先 Managed Agents API 服务，目标是提供兼容 Anthropic SDK 的 `/v1` API 表面，并配套一个 React 控制台。它把 agents、sessions、environments、files、memory stores、skills、deployments、message batches、vaults 和 webhooks 等资源放在同一个单体服务中，方便本地开发、SDK 兼容性验证和后续产品化迭代。

## 当前定位

- **API 兼容层**：`/v1/*` 提供面向 Anthropic SDK 的资源接口，使用 `Authorization: Bearer ...` 或 `X-Api-Key` 鉴权，默认开发 API key 为 `sk-ant-local-default`。
- **控制台后端**：`/api/*`、`/auth/*`、`/web-api/*` 等路由服务于前端控制台，使用 cookie session。
- **托管 Agent 资源**：支持 agent 定义、session 事件流、deployment 手动运行、environment work、credential vault、memory store、skills 等资源。
- **本地基础设施**：PostgreSQL 存储元数据，Redis 存储平台会话，MinIO/S3 存储文件、skills 和 batch 结果。
- **前端控制台**：`web/` 是 Vite + React + TypeScript + Bun 应用，使用 TanStack Router/Query/Table 和 shadcn 风格组件。

## 技术栈

- 后端：Go `1.26.2`、`chi`、`pgx`、`goose`、MinIO SDK、Redis、Anthropic Go SDK。
- 前端：Bun、Vite、React、TypeScript、Tailwind CSS、TanStack Router/Query/Table、Base UI、shadcn/ui 风格组件。
- 存储：PostgreSQL、Redis、S3 兼容对象存储，默认本地使用 MinIO。
- 沙箱：E2B 相关能力通过 `E2B_*` 环境变量启用；没有配置时，多数 API/单元测试仍可在 fake store 或非真实沙箱路径下运行。

## 目录结构

```text
.
├── main.go                     # 服务入口：加载配置、连接依赖、启动 worker 和 HTTP server
├── cmd/migrate                 # 手动运行数据库迁移
├── internal/api                # HTTP server 组装、中间件、顶层路由挂载
├── internal/{agents,...}       # 垂直资源 handler/service
├── internal/db                 # PostgreSQL 数据访问、seed、goose migrations
├── internal/httpapi            # Console API、兼容错误响应、HTTP 辅助函数
├── internal/storage            # S3/MinIO 对象存储边界
├── assets/skills               # 内置和示例 skill 包
├── tests                       # Go API/SDK/E2E 测试
└── web                         # 前端控制台
```

核心依赖方向大致是：`internal/api` 负责服务组装和路由，资源包负责 HTTP handler 与业务编排，`internal/db` 只做持久化边界，不能反向依赖 API 或 handler 包。

## Docker Compose 一键部署

项目支持通过 `docker compose up -d` 一条命令启动完整环境（PostgreSQL、Redis、MinIO、e2b-local 沙箱网关、oma-server 以及前端 Caddy 反代）。详见 `docs/design/docker-compose-deployment.md`。

```bash
docker compose up -d
```

启动后前端访问 `http://localhost:28080`（端口可通过 `CADDY_HOST_PORT` 环境变量配置），API 访问 `http://localhost:38080`。

> **平台要求**：仅支持 Linux Docker Engine 20.10+ 或 OrbStack（macOS）。

## 本地依赖

需要先准备：

- Go，与 `go.mod` 中声明的版本保持一致。
- Bun，用于前端开发、测试和构建。
- PostgreSQL，默认连接串是 `postgresql://claude:123456@localhost:5432/claude_api?sslmode=disable`。
- Redis，默认 `redis://localhost:6379`。
- MinIO 或其他 S3 兼容存储，默认 `http://localhost:9000`、bucket `claude-files`、账号密码 `minioadmin/minioadmin`。

服务启动时会尝试用 `POSTGRES_ADMIN_URL` 创建默认数据库和角色；如果你的本地 PostgreSQL 不允许默认的 `postgres` 用户免密连接，请在 `.env` 中显式配置可用的管理连接串。

## 配置

项目会从当前目录向上查找 `.env`，直到遇到 `go.mod`。常用本地配置如下：

```env
ADDR=127.0.0.1:38080
DATABASE_URL=postgresql://claude:123456@localhost:5432/claude_api?sslmode=disable
POSTGRES_ADMIN_URL=postgresql://postgres@localhost:5432/postgres?sslmode=disable
REDIS_URL=redis://localhost:6379
S3_ENDPOINT=http://localhost:9000
S3_BUCKET=claude-files
S3_REGION=us-east-1
S3_ACCESS_KEY_ID=minioadmin
S3_SECRET_ACCESS_KEY=minioadmin
S3_FORCE_PATH_STYLE=true

# 需要真实上游或真实沙箱时再配置；上游 key 只由服务端持有
ANTHROPIC_UPSTREAM_API_KEY=
PUBLIC_BASE_URL=
E2B_API_KEY=

CODE_SESSION_JWT_SIGNING_KEY_FILE=/run/secrets/code-session-jwt-key.pem

# 仅在启用 CCRv2 HTTPS MITM 时需要
CODE_SESSION_UPSTREAM_PROXY_MITM_ENABLED=true
CODE_SESSION_UPSTREAM_PROXY_CA_KEY_FILE=/run/secrets/ccrv2-mitm-ca-key.pem
```

### 创建 code session 私钥

先创建一个只有当前用户可访问的目录，再使用仓库脚本生成两份私钥：

```bash
mkdir -p "$HOME/.config/open-managed-agents"
chmod 700 "$HOME/.config/open-managed-agents"

just generate-code-session-jwt-key \
  "$HOME/.config/open-managed-agents/code-session-jwt-ed25519.pem"

just generate-upstream-proxy-ca-key \
  "$HOME/.config/open-managed-agents/ccrv2-mitm-ca-key.pem"
```

两个脚本都会将文件权限设置为 `0600`，并在目标文件已经存在时拒绝覆盖，避免意外轮换密钥。然后在 `.env` 中配置私钥的绝对路径；不要写 `$HOME`，因为 `.env` 不负责 shell 变量展开：

```env
CODE_SESSION_JWT_SIGNING_KEY_FILE=/Users/your-name/.config/open-managed-agents/code-session-jwt-ed25519.pem

# 只有启用 CCRv2 HTTPS MITM 时才需要下面两项
CODE_SESSION_UPSTREAM_PROXY_MITM_ENABLED=true
CODE_SESSION_UPSTREAM_PROXY_CA_KEY_FILE=/Users/your-name/.config/open-managed-agents/ccrv2-mitm-ca-key.pem
```

`CODE_SESSION_JWT_SIGNING_KEY_FILE` 是 PKCS#8 PEM 格式的 Ed25519 私钥，用于签发 session-ingress JWT。开发和测试环境省略时会在进程内生成临时密钥；生产环境缺失会拒绝启动。

`CODE_SESSION_UPSTREAM_PROXY_CA_KEY_FILE` 是 PKCS#8 PEM 格式的 ECDSA P-256 私钥。只有 `CODE_SESSION_UPSTREAM_PROXY_MITM_ENABLED=true` 时才需要配置；服务启动时使用它在内存中签发根证书，不会在磁盘上生成证书文件。

更完整的配置入口在 `internal/config/config.go`。开发环境默认 `DB_AUTO_MIGRATE=true`，生产环境默认关闭自动迁移。

## 启动后端

推荐使用仓库脚本，它会释放默认端口并以前台方式运行服务：

```bash
./scripts/restart-server.sh
```

默认监听 `127.0.0.1:38080`。也可以覆盖端口：

```bash
PORT=18080 ADDR=127.0.0.1:18080 ./scripts/restart-server.sh
```

健康检查：

```bash
curl http://127.0.0.1:38080/healthz
```

预期返回：

```json
{ "status": "ok" }
```

## 启动前端

首次进入前端目录安装依赖：

```bash
cd web
bun install
```

从仓库根目录启动 Vite 开发服务器：

```bash
./scripts/restart-web.sh
```

默认前端地址为 `http://127.0.0.1:5173`，并把 `/api` 和 `/v1` 代理到 `http://127.0.0.1:38080`。如果后端跑在其他端口：

```bash
API_PORT=18080 PORT=5173 ./scripts/restart-web.sh
```

控制台本地登录走 magic link 兼容接口；当前后端会创建/解析平台 session，适合本地开发控制台流程。

## API 快速验证

默认 seed 会创建开发 API key：`sk-ant-local-default`。

列出模型：

```bash
curl http://127.0.0.1:38080/v1/models \
  -H 'Authorization: Bearer sk-ant-local-default'
```

上传文件：

```bash
curl 'http://127.0.0.1:38080/v1/files?beta=true' \
  -H 'Authorization: Bearer sk-ant-local-default' \
  -F 'file=@README.cn.md;type=text/markdown'
```

使用 Go SDK 运行 files E2E：

```bash
TEST_API_BASE_URL=http://127.0.0.1:38080 \
  go test ./tests -run TestGoSDKFilesE2E -count=1 -v
```

使用 TypeScript SDK smoke command 运行一个 agent session：

```bash
cd tests/js-test
bun install
TEST_API_BASE_URL=http://127.0.0.1:38080 \
  bun run run-agent --message "Create a Python script that writes hello.txt"
```

如果要跑真实 E2B 沙箱链路，需要配置 `E2B_API_KEY`，并确保 `PUBLIC_BASE_URL`、`CODE_SESSION_API_BASE_URL` 或 `CODE_SESSION_SANDBOX_API_BASE_URL` 能从沙箱内部访问到本服务。

## 主要 API 资源

`internal/api/server.go` 中挂载了当前共享 `/v1` 资源：

- `GET /v1/models`
- `/v1/files`
- `/v1/agents` 和 `POST /v1/agents:search`
- `/v1/sessions`
- `/v1/environments`
- `/v1/deployments`
- `/v1/deployment_runs`
- `/v1/memory_stores`
- `POST /v1/messages`
- `/v1/messages/batches`
- `/v1/skills`
- `/v1/vaults`
- `/v1/webhooks`
- `/v1/organizations`

很多 beta 资源在测试和前端中会附带 `?beta=true`，部分 SDK 资源还会带对应 `anthropic-beta` header；实现时应保持这些兼容语义。

## 数据库与迁移

数据库 schema 通过 `internal/db/migrations` 下的 goose migration 管理。手动迁移：

```bash
go run ./cmd/migrate up
```

项目约定不使用 PostgreSQL 外键约束。`DB.Migrate()` 在应用 goose migrations 后会删除当前 schema 中发现的外键约束，并由 `tests/files_api_test.go` 中的守卫测试覆盖。

新增 schema 变更时：

- 在 `internal/db/migrations` 增加新的编号 SQL 文件，例如 `00010_add_xxx.sql`。
- 不要修改已应用 migration。
- 不要把新的 schema 变更追加到 `internal/db/schema.go`。
- 多租户查询和写入必须显式携带 organization/workspace scope。

## 测试

后端常用测试：

```bash
go test ./... -count=1
```

前端测试和构建：

```bash
cd web
bun test
bun run build
```

仓库也提供 `just` 任务：

```bash
just test
just web-test
just web-build
```

真实 E2E 通常需要先启动本地服务，再指定 base URL：

```bash
ADDR=127.0.0.1:18080 go run .

TEST_API_BASE_URL=http://127.0.0.1:18080 \
  go test ./tests -run TestGoSDKFilesE2E -count=1 -v
```

跑完真实 E2E 后记得停止临时启动的本地服务。

## 开发约定

- HTTP 路由使用 `github.com/go-chi/chi/v5`，新增资源优先用 `chi.Mount`、sub-router 或 route group。
- `/v1/*` 错误响应应通过 `internal/httpapi.WriteError` 保持 Anthropic 兼容结构。
- `internal/api` 只做组装、中间件、鉴权入口选择和资源挂载，不放业务规则、SQL 或资源级细节。
- DB 层返回普通 Go error 或可识别的领域错误，不构造 HTTP 状态码或 API JSON。
- 文件、skills、memory content、batch result 等对象内容通过 S3/MinIO 存储，元数据在 PostgreSQL。
- 前端 API 边界要区分 `/api` Console API 和 `/v1` SDK 兼容 API，不要为了控制台便利改变 `/v1` 行为。
- 修改 `web/` 后，浏览器验证前从仓库根目录运行 `./scripts/restart-web.sh`。

## 参考文档

- 后端权限桥设计：`docs/design/be/managed-agent-claude-code-permission-bridge.md`
- 权限策略设计：`docs/design/be/permission-policies.md`
- 前端约定：`web/AGENTS.md`
