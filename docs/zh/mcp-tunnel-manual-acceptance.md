# MCP Tunnel 手动验收

本文用于基于当前 checkout 验证管理 API、Connector、MCP Ingress、Console 和 Managed Agent 的完整链路。
默认流程不会清理现有 Tunnel、Agent、Environment 或 Session。不要把 workspace API key、Tunnel token 或
Private MCP 凭据写入 shell history、日志或文档。

## 1. 非破坏性预检

1. 确认当前分支和工作区符合预期，并确认数据库已应用 `internal/db/migrations/` 中当前 checkout 所包含的全部 migration。
2. 启动前检查 `goose_db_version`，确认没有失败或乱序 migration。若启用 `database.auto_migrate`，应在 Server
   启动日志中确认迁移成功；生产式配置关闭自动迁移时，应先通过受控发布流程完成迁移。
3. 确认 `mcp_tunnels`、`mcp_tunnel_token_versions`、`mcp_tunnel_certificates` 存在且无 PostgreSQL 外键；
   `mcp_tunnels.external_id` 应受 `^tunnel_[0-9a-f]{32}$` 格式约束，`workspace_uuid` 必须非空。
4. 记录验收前已有 Tunnel 的精确 external ID 和 UUID，后续只操作本次新建的 Tunnel。不要为了运行验收而清空
   现有表、共享 NATS stream 或 KV bucket。

## 2. 启动与变量

1. 终端 A 执行 `just restart-server`，确认 `http://127.0.0.1:38080/readyz` 成功。
2. 若要验证 Console，终端 B 执行 `just restart-web`，使用命令输出的实际 Vite 地址登录。
3. 在新的 shell 中设置非敏感地址和协议变量：

   ```bash
   export OMA_API_BASE_URL=http://127.0.0.1:38080
   export OMA_TUNNEL_PUBLIC_BASE_URL=http://127.0.0.1:38080
   export OMA_ANTHROPIC_VERSION=2023-06-01
   export OMA_TUNNELS_BETA=mcp-tunnels-2026-06-22
   ```

4. 通过密码管理器、受保护的临时文件描述符或不会落入 history 的安全输入设置
   `OMA_WORKSPACE_API_KEY`。创建或 reveal 后用同样方式临时设置 `OMA_TUNNEL_TOKEN`；验收结束必须清除。

## 3. 管理 API

1. 携带 `X-Api-Key`、`anthropic-version` 和 `anthropic-beta` 创建 Tunnel，记录响应 ID。ID 必须严格匹配
   `^tunnel_[0-9a-f]{32}$`。公开 `/v1/tunnels` 创建响应不得包含明文 token，也不提供 Console 专用的
   `mcp_url` 字段。
2. 使用返回的 ID 构造 canonical URL：

   ```bash
   export OMA_TUNNEL_ID='tunnel_<创建响应中的 32 位小写十六进制 ID>'
   export OMA_TUNNEL_MCP_URL="${OMA_TUNNEL_PUBLIC_BASE_URL%/}/v1/mcp/${OMA_TUNNEL_ID}"
   export OMA_TUNNEL_METADATA_URL="${OMA_TUNNEL_PUBLIC_BASE_URL%/}/.well-known/oauth-protected-resource/v1/mcp/${OMA_TUNNEL_ID}"
   ```

3. 使用同一个 ID 依次调用 retrieve、list、`reveal_token`、`rotate_token`。轮换后的 token 必须变化；旧 token
   不能再调用 Connector metadata 或 poll，但允许为轮换前已经 claim 的请求提交精确匹配的在途 response。
4. 调用 `/v1/organizations/tunnels` 必须返回 404。使用单个 PEM 编码的 X.509 CA 证书依次验证
   `/v1/tunnels/{id}/certificates` 的 create、retrieve、list 和 archive：响应均为 200，创建结果的 ID 匹配
   `^tcrt_[0-9a-f]{32}$`，fingerprint 为证书 DER 的小写 SHA-256。Certificate 仅持久化，不应改变 Tunnel
   metadata、Connector、Broker、Ingress 或 MCP 请求行为。
5. 暂不归档本次 Tunnel，留给后续 Connector 和 Managed Agent 验证。最终归档 Tunnel 时，已创建且尚未归档的
   Certificate 仍须保持原状态；Certificate 生命周期只能通过自身 archive API 改变。

## 4. 原版 tunnel-client

1. 使用 OpenAI `tunnel-client` 官方源码构建的二进制。准备 Console 展示的 YAML：

   ```yaml
   config_version: 1
   control_plane:
     base_url: http://127.0.0.1:38080
     url_path: /connector
     tunnel_id: tunnel_0123456789abcdef0123456789abcdef
     api_key: env:OMA_TUNNEL_TOKEN
   mcp:
     server_urls:
       - channel: main
         url: http://127.0.0.1:<private-mcp-port>/mcp
   ```

2. 把示例 ID 替换为 `OMA_TUNNEL_ID`，安全设置当前 active `OMA_TUNNEL_TOKEN` 后启动 client。
3. 确认 client 能加载配置并完成 metadata；服务端 Connector presence 变为 connected。
4. 观察 client 请求，确认 metadata、poll 和 response 路径都使用同一个 `tunnel_<32 位>` ID。instance ID 只用于展示，提供时必须合法且无首尾空白；缺省为 legacy。

## 5. MCP 与 OAuth 数据面

1. 使用 workspace `X-Api-Key` 对 `OMA_TUNNEL_MCP_URL` 发送 `initialize`，响应必须包含 MCP protocol version
   和 Private MCP server info。
2. 发送 `notifications/initialized`，再发送 `tools/list`，工具列表必须来自 Private MCP Server。
3. 选择一个无副作用的工具执行 `tools/call`，验证参数与结果完整往返。
4. 如有非 `main` channel，仅使用匹配 `[a-z0-9_-]{1,64}` 的名称，并对
   `/v1/mcp/{id}/{channel}` 重复 `initialize`、`tools/list`、`tools/call`。
5. 请求 `OMA_TUNNEL_METADATA_URL`，验证 OAuth protected-resource metadata 的 `resource` 等于
   `OMA_TUNNEL_MCP_URL`。
6. 若测试 Private MCP 支持 OAuth challenge，让它返回带 Private 地址的 401 与
   `WWW-Authenticate: Bearer ... resource_metadata="<private-url>"`。直接 Ingress 和 Managed Agent Runtime
   Gateway 都必须保留 401，但将 metadata body 的 `resource` 和 challenge 的 `resource_metadata` 分别改写为
   调用方实际可达的 public/runtime URL，响应中不得出现 Private MCP 地址。

## 6. Console 与 Managed Agent

1. Console Tunnel 列表展示同一个 ID、服务端生成的 `mcp_url` 和 connected 状态；复制出的 YAML 中
   `tunnel_id` 必须与公开管理 API 返回的 ID 完全一致。
2. 创建 Managed Agent，打开 MCP Server picker；Tunnel 必须排在 Directory 服务之前。
3. 选择 Tunnel 和 Channel 并保存，检查 Agent 的 `mcp_servers[].name` 为 `<Tunnel ID>.<Channel>`，
   名称中的 Channel 后缀将 `_` 转为 `.`，URL 使用原始 Channel；对应 `mcp_toolset.mcp_server_name` 必须同名。
4. 启动真实 Managed Agent Session，让模型先列出工具，再调用刚才验证过的无副作用工具。成功标准是
   sandbox → Runtime Gateway → TunnelInvoker → NATS Broker → 原版 tunnel-client → Private MCP Server
   全链路返回结果。
5. Console probe、presence 或 `/readyz` 成功只能作为辅助证据，不能替代真实 Managed Agent 工具调用。

## 7. 轮换、归档与在线展示

1. 发起长轮询并确认入口鉴权已通过，再 rotate 或 archive。该 Poll 在原期限内仍可领取，下一次旧 token Poll 返回 401。
2. 保持一个已领取请求未完成，再轮换或归档；用领取时 token、匹配的 requestId/channel 和 `X-Tunnel-Shard-Token=requestId` 回传，应返回 200。用新 token 或错误 Tunnel/绑定回传应为 404。
3. 检查 PostgreSQL 事务一致性；Response 不读取数据库，管理操作不读写 NATS/Redis。合法重复终态不覆盖结果；仅原 OMA 的本地完成标记有效期间返回 200，重启后不恢复。
4. Redis 在线记录按实例独立过期，默认 60 秒；轮换不清空记录，归档在 Console 立即显示离线。Redis 暂不可用时显示 unknown，Poll/Response 仍正常。
5. 停掉 Connector 后直接发起请求，再在 deadline 前启动，应领取成功；始终不启动则超时。不同亲和声明的 Connector 共享同一 channel，无 Session ID 映射或固定 owner。
6. 等待请求记录过期；停止验收启动的 Server、Web、Private MCP 和 tunnel-client，清除 shell 敏感变量。使用无状态 MCP fixture；不要求依赖特定进程的旧会话继续工作。


### 单次投递与本地完成确认

- 多个 OMA 和 connector 同时工作：请求只领取一次；检查 consumer 的 MaxDeliver=1。模拟 ACK 丢失后不再次交付。
- 使用两个 channel 并传 limit=1，确认每次返回不超过一条，剩余命令仍能由下次 Poll 领取；传 limit=30 可返回超过旧服务端上限的命令。
- Poll 已发出的有限拉取必须收束，不能因另一路先返回而丢弃已取命令；正常返回后没有持续预取。
- MCP HTTP 调用方断开后，已入队命令仍可在 deadline 前被领取；响应因无人等待返回 404。队列过期命令不能执行。
- 原请求在 OMA A、Poll 在 B、Response 在 C：通知和最终结果回到 A，Redis 仅保存不可变领取绑定，结果交付不修改记录、不续期。
- 并发重复最终响应不覆盖或重复交付，A 本地标记到期后返回 404；A 退出后旧 Origin 不可达返回 503，不从 KV 恢复结果。
- 确认 Poll 不再按累计大小截断；超过 NATS 上限的正文按下文[超过 NATS 上限的正文验收](#超过-nats-上限的正文验收)执行。

## 8. 可选的破坏性重置

只有在明确要求物理清理测试数据时才执行本节，并把破坏性步骤放在全部验收之后：

1. 停止 OMA Server 和所有 `tunnel-client`，防止清理期间继续创建或领取请求。
2. 按本次 `OMA_TUNNEL_ID` 查询并再次确认唯一的 organization、workspace、Tunnel UUID、token version 和
   certificate 行；确认没有把其他环境或历史 Tunnel 纳入范围。
3. 备份待删除行，按精确内部 Tunnel UUID 和 `brokerKey` 的摘要算法盘点请求 key 和 consumer（新 key 仅由全局 requestId 生成摘要）。命令 subject 只匹配该 Tunnel 的摘要。不要清空共享 NATS stream、KV bucket 或整个账号。
4. 获得明确确认后，才按已盘点的范围清理测试资源。请求和命令优先等待 TTL。Certificate 是独立资源，另行确认是否需要删除。
5. 删除后复核目标 PostgreSQL 数据及精确 NATS 状态，确认其他 Tunnel、Agent、Environment 和 Session 仍然存在且可读。

## 9. 自动化验收

Connector 验收依赖为 OpenAI `tunnel-client` 未修改的官方发行版 `v0.0.14`，下载后核对发行版 SHA256SUMS。

```bash
./scripts/generate-go.sh
TEST_TUNNEL_CLIENT_BINARY=/absolute/path/to/tunnel-client go test ./internal/tunnels -count=1 -v
```

此测试使用隔离 NATS，覆盖原版 Connector 的 HTTP JSON、SSE、stdio 和 v2 声明；DB 授权是 fixture。
真实 TypeScript/Python Claude Agent SDK、`claude` CLI 和 Managed Agent Sandbox 工具调用须分别执行，记录版本、模型配置与私有 MCP 返回的本次随机标记。不能由 generic MCP SDK、Console probe 或 `/readyz` 代替。

### 9.1 真实 Claude SDK 与 CLI

准备隔离的 Node 安装目录和 Python virtualenv，验收依赖版本如下：

```bash
npm install --prefix /private/tmp/oma-claude-acceptance --no-audit --no-fund \
  @anthropic-ai/claude-agent-sdk@0.3.263 @anthropic-ai/claude-code@2.1.263
uv venv /private/tmp/oma-claude-acceptance-venv
uv pip install --python /private/tmp/oma-claude-acceptance-venv/bin/python claude-agent-sdk==0.2.152
```

模型配置存入权限为 0600 的文件，使用 `ANTHROPIC_BASE_URL` 和 `ANTHROPIC_AUTH_TOKEN`（或 `ANTHROPIC_API_KEY`）单行赋值；不要 source 文件。
下面只有路径和模型名，认证值由 Python runner 按数据解析并传给子进程：

```bash
./scripts/generate-go.sh
TEST_CLAUDE_AUTH_FILE=/absolute/path/to/claude-config.txt \
TEST_CLAUDE_MODEL=claude-sonnet-4-6 \
TEST_CLAUDE_PYTHON=/private/tmp/oma-claude-acceptance-venv/bin/python \
TEST_CLAUDE_NODE_DIR=/private/tmp/oma-claude-acceptance \
TEST_CLAUDE_CLI=/private/tmp/oma-claude-acceptance/node_modules/.bin/claude \
TEST_TUNNEL_CLIENT_BINARY=/absolute/path/to/tunnel-client \
go test ./internal/tunnels -run '^TestClaudeClientsTunnelIntegration$' -count=1 -v -timeout=15m
```

预期 9 个子用例通过：TypeScript、Python、CLI 各覆盖 HTTP JSON、HTTP SSE 和 stdio。
每组实际调用一次 `mcp__tunnel__tunnel_proof`，最终回答包含只由私有 MCP 返回的随机标记。
内置工具关闭、项目和用户 settings 不加载、会话不持久化；日志只输出工具调用次数、连接状态和结果校验状态。
测试创建的三副本 NATS、Connector、私有 MCP 与临时客户端目录自动清理；安装目录保留供复跑使用。

### 9.2 真实 Managed Agent Sandbox

`TestManagedAgentNATSTunnelE2E` 使用真实 DB 鉴权、Runtime Gateway、R3 NATS、原版 Connector 和实际 E2B Sandbox。
Worker 事件与 Session 广播也按生产方式复用同一个 NATS 连接。文件对象存储使用内存 fixture，此测试不覆盖 S3 持久化。
测试组装接入真实 Sandbox 续租 provider；无人值守验收只为 `tunnel_proof` 显式配置 `always_allow`。Claude MCP config 的 `tools[].permission_policy` 使用 `always_allow` / `always_ask` 枚举。
它仅接受本机 PostgreSQL，自动创建唯一的 `oma_tunnel_e2e_<timestamp>` 数据库，并在停止测试 Sandbox、关闭连接后删除该库。
不会把现有数据库传入会替换 LLM provider 的测试 helper。验收中 provider 凭据仅加密写入独立测试库。

先准备权限 0600 的独立 `CONFIG_FILE`：使用当前 checkout 的完整配置，保留可用 E2B 地址，确认镜像存在；
`environment_runner.claude_agent_version` 必须与镜像内的 `/opt/claude-code/bin/claude --version` 一致，否则启动脚本会在 Agent 启动前退出。
所有相对密钥文件和 manager/Claude 路径应改为相对于原配置目录的绝对路径。服务使用 `127.0.0.1:18080`，Sandbox 从
`http://host.docker.internal:18080` 访问；端口占用时直接失败，不停止已有服务。此用例面向本机 E2B，不能原样用于云 Sandbox。

从仓库根目录生成后，在独立临时目录执行 runner（将下方仓库路径替换为实际绝对路径）：

```bash
./scripts/generate-go.sh
mkdir -p /private/tmp/oma-managed-tunnel-run
cd /private/tmp/oma-managed-tunnel-run
CONFIG_FILE=/absolute/path/to/test-config.yaml \
TEST_CLAUDE_AUTH_FILE=/absolute/path/to/claude-config.txt \
TEST_CLAUDE_MODEL=claude-sonnet-4-6 \
TEST_TUNNEL_CLIENT_BINARY=/absolute/path/to/tunnel-client \
/private/tmp/oma-claude-acceptance-venv/bin/python \
  /absolute/path/to/open-managed-agents/tests/e2e/claude/tunnel_clients.py managed
```

Managed runner 要求认证文件提供 `ANTHROPIC_BASE_URL` 和 `ANTHROPIC_AUTH_TOKEN`。
成功标准是 Sandbox 的最终回答包含私有工具随机标记、私有工具只执行一次，并确实经过 `/v2/ccr-sessions/.../mcp/...`。
失败时只输出脱敏错误，不将初始化、presence、probe 或 Sandbox 创建成功算作工具调用通过。

### Redis 8 与单次授权回归

```bash
# 指向独立、可短暂暂停的 Redis 8 测试实例；测试会使用 CLIENT PAUSE 验证 500ms 上限。
TEST_TUNNEL_REDIS_ADDR=127.0.0.1:<test-port> go test ./internal/tunnels -run '^TestPresence' -count=1 -v
go test ./internal/tunnels -run '^TestConnector' -count=1 -v
```

这些测试覆盖字段刷新/过期、多实例与多 channel 聚合、Redis 故障降级、单次 Poll 查询、轮换/归档后的在途响应。
完整 Go 检查前先生成 Mapper；真实 PostgreSQL 配置用 `CONFIG_FILE` 指向隔离环境。不要在开发共享库上执行重置。

### 2026-09-21 简化变更的本地验证记录

- `just lint`、`just dead-code`、`just duplicates`、`just complexity`、`just large-files` 通过。
- `just test` 已执行；Tunnel package、真实 PostgreSQL Token Mapper、无 Broker 的管理事务回滚/轮换/归档测试通过。
- Redis 8 隔离实例通过独立字段过期、刷新、完整 channel 声明、聚合与 `CLIENT PAUSE` 超时测试；故障时 Poll/Response 正常、Console 显示 unknown。
- 校验官方发布的 SHA256SUMS 后，未修改的 tunnel-client v0.0.14 通过 HTTP JSON、HTTP SSE、stdio、HTTP v2 转发测试。HTTP fixture 使用无状态 MCP，DB 授权为 fixture；不将此结果等同于真实模型或 Sandbox 验收。
- 带 `e2e` 标签的 Tunnel workspace 授权、Gateway 凭据和 NATS 分页探测测试通过。
- 前端 Tunnel 功能 24 项、Agent 的 Tunnel 相关用例 6 项通过；命名、`just web-format-check` 与 `bun run build` 通过。
- 全量 Go 测试未全绿：`TestCodeSessionAskUserQuestionUsesCustomToolResult` 缺少 `session.status_idle`，`TestTranscriptArchiveRestoreAfterBlobGC` 的清理计数为 3 而非 0；两项均在未修改的基线 `ada53ffb409af0a508b063a5f0c97efab5b5301a` 临时副本中复现，未扩大本次范围修改它们。
- 整个大型 Agent 页面 Bun 套件曾因 Bun 1.3.14 segmentation fault 中止；改为独立运行上述 Tunnel 相关用例后通过，不将大型套件标记为通过。

### 2026-09-22 Request KV 简化的本地验证记录

- Tunnel package 全部普通测试以及 `go test -race ./internal/tunnels -count=1` 通过。覆盖不可覆盖绑定、ACK 失败和丢失后不重投、跨 OMA 响应、本地取消、重复响应、Origin 丢失、通知背压、客户端 limit、有限拉取收尾和交付前过期。
- 未修改的 tunnel-client v0.0.14 通过 HTTP JSON、HTTP SSE、stdio、HTTP v2 转发；独立 Redis 8 通过 presence 回归。上述用例也随竞态测试运行；本轮未执行真实模型和 Managed Agent Sandbox 验收。
- `just lint`、`just dead-code`、`just duplicates`、`just complexity`、`just large-files` 与 `git diff --check` 通过。本轮没有前端、Mapper 或 PostgreSQL schema 变更。
- 最终 `just test` 使用独立 PostgreSQL 17 的全新测试库运行，所有 `internal/` package 通过，但 `tests` package 有两项失败，不能标记全量测试通过：
  - `TestCodeSessionAskUserQuestionUsesCustomToolResult` 缺少 `session.status_idle`。在未修改的基线 `ca41d7afd7f627059866e6b2f360386f7a84115b` 临时源码副本中，单项和完整 `tests` package 均复现。
  - `TestSandboxLifecycleDurableScheduleDispatchesReclaim` 等待 15 秒未收到回收事件。本轮全量运行失败，但修改前后单项运行均通过，基线完整 `tests` package 也通过该项；原因尚未确认，不将单项通过等同于全量通过，也不据此宣称已排除回归。
- 基线完整 `tests` package 另出现 `TestTranscriptArchiveRestoreAfterBlobGC` 清理计数失败；最终变更代码的全新库运行通过该项。保留测试隔离与时序问题的排查范围，不修改无关模块。
- 已复核投递、鉴权绑定、期限、重复响应与缓冲释放边界；当时尚未实现 Response 大正文方案，后续实现与验证见下文[超过 NATS 上限的正文验收](#超过-nats-上限的正文验收)。本轮不提交、推送或部署。MCP Tunnel 尚未正式上线，直接调整实现并沿用原有 NATS 资源名称，无旧资源迁移或退役要求。

## 超过 NATS 上限的正文验收

默认配置：`tunnel.max_body_bytes=16777216`，已删除 `max_stored_requests`。每节点 Tunnel Commands 存储预算固定 513 MiB，三副本约 1.50 GiB；Worker Stream 的预算不变。不要为通过验收提高 NATS `max_payload` 或存储容量。

1. 先测失败：16 MiB+1 正文、对象不存在、长度/摘要不符、对象上传/读取失败、清理任务登记失败；错误沿用当前合同，不重投。错误 Response 绑定不能上传对象。
2. 检查编码后完整 NATS 消息（包含命令去重 header）在上限前、恰好达到上限和超过上限的行为。前两者不访问存储，后者只发布引用，接收方恢复完整正文。
3. 两个 OMA 实例分别领取和接收 Response；验证 16 MiB 正文完整性、小/大 SSE 通知交错顺序与最终结果，以及合法重复响应不再次交付。
4. 在独立 PostgreSQL、版本化 S3 bucket 和官方 tunnel-client v0.0.14 下运行：

```sh
TEST_TUNNEL_PAYLOAD_DATABASE_URL=postgresql://postgres:payload-test@127.0.0.1:55439/tunnel_payload \
TEST_TUNNEL_PAYLOAD_S3_ENDPOINT=http://127.0.0.1:59009 \
TEST_TUNNEL_CLIENT_BINARY=/absolute/path/to/tunnel-client \
go test ./internal/tunnels -run 'TestTunnelPayloadRealStorageAndClient|TestOfficialTunnelClientIntegration' -count=1 -v
```

此专用测试要求 S3 测试凭据 `payload-test` / `payload-test-secret`，使用独立 `tunnel-payload-test` bucket。不要指向业务环境。它验证 3 MiB 请求、响应及 SSE 通知，沿用测试凭据查询 fixture；清理登记使用真实 PostgreSQL，测试会验证清理不会提前执行，再主动提前测试任务并检查所有对象版本消失。生产清理仍按原 deadline 加 5 分钟调度。

`TestTunnelReducedStorageBudget` 使用每节点 600 MiB 的独立三副本 JetStream 验证新 Tunnel 预算可初始化、旧预算被拒绝。它仅验证 Tunnel，不代表完整应用可在 600 MiB 下运行。测试和验收结束后停止自行启动的 PostgreSQL、S3、Redis 与 connector 进程。

### 2026-09-23 大报文实现验证记录

- 真实 PostgreSQL、版本化 S3 与未修改的 tunnel-client v0.0.14：3 MiB 请求、JSON 响应、SSE 通知/最终响应通过；对象清理未提前执行，提前测试任务后所有版本和删除标记均消失。既有 HTTP JSON、HTTP SSE、stdio、server-info v2 客户端回归通过。
- Tunnel/config 定向测试与 Tunnel race 检查通过，覆盖 16 MiB 正文、NATS 完整消息边界、跨实例、读取失败、过期重复响应、三节点每节点 600 MiB 的预算准入。
- `just test` 的全部 internal 包通过；`tests` 仅 `TestCodeSessionAskUserQuestionUsesCustomToolResult` 因缺少 `session.status_idle` 失败。在 `ca41d7a` 基线源码和独立数据库中重新复现相同失败，未修改该功能。
- `just lint`、`just dead-code`、`just duplicates`、`just complexity`、`just large-files` 通过。此次未执行真实 Claude/Managed Agent Sandbox 验收。

### Redis 领取绑定与无请求数量准入验收

1. 使用专用 Redis 8 运行 `TestRequestBindingsRedis8`；通过 `TEST_TUNNEL_REDIS_ADDR` 指定可短暂停顿的验收实例，不能使用共享开发 Redis。验证并发 NX、独立 TTL、读取不续期及暂停时失败。
2. 验证不存在旧 Request KV，Commands 为 R3、MaxMsgs=-1、MaxBytes=537919488；超过 256 个排队命令、绑定和等待者仍可处理。
3. 绑定缺失/过期为 404；Redis 故障为 503。领取写入失败不交付，不后台重投。已有成功领取的旧 token 在轮换后仍可完成响应。
4. 设置真实存储与官方 client 验收所需环境变量，同时指定上述 Redis 地址；运行 `TestTunnelPayloadRealStorageAndClient` 与 `TestOfficialTunnelClientIntegration`，验证跨实例回传、普通正文、超 2 MiB 正文及 SSE 顺序。
5. Redis 丢失绑定导致在途请求失败是已接受的行为；绑定读取不查询 PostgreSQL 当前凭据、不回退 NATS KV。检查 readiness 分别显示 tunnel_nats/tunnel_redis。
6. 本次不迁移或自动删除旧资源；验收结束停止本次启动的 Redis、PostgreSQL、S3 和 connector，不停止已有开发服务。

### 2026-09-24 Redis 领取绑定本地验证记录

- 专用 Redis 8 验证 NX 并发唯一创建、TTL 不续期、独立过期、暂停超时，以及三个独立 Broker/Redis 客户端的领取与响应回传，均通过。
- 未修改的 tunnel-client v0.0.14 完成 HTTP JSON、HTTP SSE、stdio、server-info v2 验收；真实 PostgreSQL、版本化 S3 与 Redis 8 完成 3 MiB 请求、响应和 SSE 通知，以及临时对象清理验收。Token 查询仍使用该集成测试原有 fixture，此结果不等同于真实 Managed Agent/Sandbox 验收。
- Tunnel 包全量测试和定向并发 race 检查通过；300 个排队命令、绑定和等待者无数量准入，固定字节预算及无 Request KV 检查通过。
- 全仓库 just test 的其他包通过，tests 包仍有 TestCodeSessionAskUserQuestionUsesCustomToolResult 缺少 session.status_idle 的失败；变更前验收日志也有同样失败，本次没有修改该逻辑。
- lint、dead-code、duplicates、complexity、large-files 均通过；E2E build tag 编译通过。
