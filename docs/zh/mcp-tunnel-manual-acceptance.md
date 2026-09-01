# MCP Tunnel 手动验收

本文用于基于当前 checkout 验证管理 API、Connector、MCP Ingress、Console 和 Managed Agent 的完整链路。
默认流程不会清理现有 Tunnel、Agent、Environment 或 Session。不要把 workspace API key、Tunnel token 或
Private MCP 凭据写入 shell history、日志或文档。

## 1. 非破坏性预检

1. 确认当前分支和工作区符合预期，并记录 `internal/db/migrations/` 中当前最大 migration 文件。不要在文档中
   固定断言最大版本必须等于某个历史编号；数据库应应用当前 checkout 所包含的全部 migration。
2. 启动前检查 `goose_db_version`，确认没有失败或乱序 migration。若启用 `database.auto_migrate`，应在 Server
   启动日志中确认迁移成功；生产式配置关闭自动迁移时，应先通过受控发布流程完成迁移。
3. 确认 `mcp_tunnels`、`mcp_tunnel_token_versions`、`mcp_tunnel_certificates` 存在且无 PostgreSQL 外键；
   `mcp_tunnels.external_id` 应受 `^tunnel_[0-9a-f]{32}$` 格式约束，`workspace_uuid` 必须非空。
4. 记录验收前已有 Tunnel 的精确 external ID 和 UUID，后续只操作本次新建的 Tunnel。不要为了运行验收而清空
   现有表或扫描删除宽泛的 Redis key pattern。

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

## 3. 管理 API 与兼容路由

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

1. 使用未修改的 OpenAI `tunnel-client`，不要应用 OMA patch。准备 Console 展示的 YAML：

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
4. 观察 client 请求，确认 metadata、poll 和 response 路径都使用同一个 `tunnel_<32 位>`，没有 `tnl_`
   映射。声明 process-affinity channel 时还必须携带稳定、无首尾空白的 instance ID。

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
3. 选择 Tunnel 并保存，检查 Agent 的 `mcp_servers[].name` 直接等于 Tunnel ID、没有首尾空白，也不得出现
   `tunnel_tunnel_...`。
4. 启动真实 Managed Agent Session，让模型先列出工具，再调用刚才验证过的无副作用工具。成功标准是
   sandbox → Runtime Gateway → TunnelInvoker → Redis Broker → 原版 tunnel-client → Private MCP Server
   全链路返回结果。
5. Console probe、presence 或 `/readyz` 成功只能作为辅助证据，不能替代真实 Managed Agent 工具调用。

## 7. 生命周期与常规收尾

1. 再次 rotate token，确认旧 client 不能继续 metadata/poll；用新 token 重启 client 后恢复 connected。不要把
   已 claim response 可排空的兼容行为误判成旧 token 仍可领取新请求。
2. archive 本次 Tunnel，确认 retrieve/list 的归档语义正确、Connector 不能继续 metadata/poll、MCP Ingress
   和 Runtime Gateway 不再可调用，certificate 归档 SQL 不报错。
3. 查询 PostgreSQL，确认 Tunnel、所有 token version 和归档时间一致。查询 Redis 时应确认 active token version
   已暂停且 presence 被清除；request state、budget 或 terminal/canceled tombstone 可能在受控 TTL 内暂留，
   不要求 archive 后所有 `oma:tunnel:{uuid}:*` key 立即消失。
4. 等待配置的 tombstone/presence TTL 后，可再次确认临时状态被惰性清理。停止验收期间启动的 Server、Web、
   Private MCP 和 tunnel-client 进程，并清除 shell 中的敏感变量。

## 8. 可选的破坏性重置

只有在明确要求物理清理测试数据时才执行本节，并把破坏性步骤放在全部验收之后：

1. 停止 OMA Server 和所有 `tunnel-client`，防止清理期间继续创建或领取请求。
2. 按本次 `OMA_TUNNEL_ID` 查询并再次确认唯一的 organization、workspace、Tunnel UUID、token version 和
   certificate 行；确认没有把其他环境或历史 Tunnel 纳入范围。
3. 备份待删除行，并按精确 Tunnel UUID 盘点 Redis `oma:tunnel:{uuid}:*` key。禁止使用未解析变量、宽泛 glob
   或整个 Redis database 作为删除目标。
4. 获得明确确认后，才在事务中物理删除该 Tunnel 的 certificate、token version 和 Tunnel 行，再删除精确 UUID
   namespace 下的 Redis key。
5. 删除后复核 PostgreSQL 依赖计数为零、精确 Redis namespace 为空，同时确认其他 Tunnel、Agent、Environment
   和 Session 仍然存在且可读。
