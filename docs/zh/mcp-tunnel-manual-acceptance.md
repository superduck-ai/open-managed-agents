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
4. 观察 client 请求，确认 metadata、poll 和 response 路径都使用同一个 `tunnel_<32 位>` ID。声明 process-affinity channel 时还必须携带稳定、无首尾空白的 instance ID。

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

## 7. 生命周期与常规收尾

1. 再次 rotate token，确认旧 client 不能继续 metadata/poll；用新 token 重启 client 后恢复 connected（亲和 channel 的进程身份变更另见下文恢复限制）。不要把
   已绑定请求的响应排空误判成旧 token 仍可领取新请求。
2. archive 本次 Tunnel，确认 retrieve/list 的归档语义正确、Connector 不能继续 metadata/poll、MCP Ingress
   和 Runtime Gateway 不再可调用，certificate 归档 SQL 不报错。
3. 查询 PostgreSQL，确认 Tunnel、所有 token version 和归档时间一致。NATS 控制 KV 应显示 active version 已暂停、presence 清除；请求/终态在有界 TTL 内暂留，控制记录不自动过期。
4. 等待 `request_timeout + tombstone_ttl` 后确认请求记录过期；停止验收期间启动的 Server、Web、Private MCP 和 tunnel-client，并清除 shell 敏感变量。

亲和 channel 使用代理会话；重启原版 tunnel-client 后，旧会话应返回 404，新的 initialize 在同一 Tunnel 绑定新进程并恢复实际工具调用。v2 完全自包含的无 session 路径采用固定 channel owner；owner 永久退出后需重新配置 Tunnel。

## 8. 可选的破坏性重置

只有在明确要求物理清理测试数据时才执行本节，并把破坏性步骤放在全部验收之后：

1. 停止 OMA Server 和所有 `tunnel-client`，防止清理期间继续创建或领取请求。
2. 按本次 `OMA_TUNNEL_ID` 查询并再次确认唯一的 organization、workspace、Tunnel UUID、token version 和
   certificate 行；确认没有把其他环境或历史 Tunnel 纳入范围。
3. 备份待删除行，按精确内部 Tunnel UUID 和 `brokerKey` 的摘要算法盘点控制 key、请求 key 和 consumer。命令 subject 只匹配该 Tunnel 的摘要。不要清空共享 NATS stream、KV bucket 或整个账号。
4. 获得明确确认后，才按已盘点的范围清理测试资源。请求和命令优先等待 TTL；控制记录删除会丢失撤销屏障与亲和归属，必须确认所有关联 OMA/Connector 已停止。Certificate 是独立资源，另行确认是否需要删除。
5. 删除后复核目标 PostgreSQL 数据及精确 NATS 状态，确认其他 Tunnel、Agent、Environment 和 Session 仍然存在且可读。

## 9. 自动化验收

Connector 验收依赖为 OpenAI `tunnel-client` 官方源码版本 `9f77746a5498289f04e1ae6d3e0c830f3871af52`。

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
