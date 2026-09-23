# MCP Tunnel 实现与端到端流程

本文面向 OMA 开发者，解释 MCP Tunnel 为什么存在、各组件如何协作、每一层如何鉴权，以及 OMA 如何与原版
OpenAI `tunnel-client` 整合。

如果只需要查某个模块的约束，可继续阅读：

- [MCP Tunnel 后端设计](be/mcp-tunnels.md)：持久化、Broker 原子性、Header、超时和协议合同；
- [MCP Tunnel Console 前端设计](fe/mcp-tunnels.md)：Console 页面、Secret 展示和 Agent 选择器；
- [MCP Tunnel 手动验收](../zh/mcp-tunnel-manual-acceptance.md)：可复制的本地端到端验收步骤。

## 1. 一句话理解 MCP Tunnel

MCP Tunnel 把一个只能在企业私网中访问的 MCP Server，投影成 OMA 可访问的稳定 MCP URL：

```text
https://<oma-public-origin>/v1/mcp/tunnel_<32位小写十六进制>
```

私网不需要向 OMA 开放入站端口。运行在私网中的 `tunnel-client` 主动向 OMA 发起 HTTPS 长轮询，领取
MCP 请求，在本地调用私有 MCP Server，再把响应提交回 OMA。

它不是网络层 VPN，也不是透明 TCP 隧道。它转发的是受约束的 MCP HTTP/JSON-RPC 命令、协议 Header、
通知和最终响应。

## 2. 整体架构

```mermaid
flowchart LR
    subgraph OMAClients[OMA 调用方]
        Direct[直接 MCP 调用方]
        Sandbox[Managed Agent Sandbox]
        Console[OMA Console]
        SDK[Claude SDK / CLI]
    end

    subgraph OMA[OMA Server]
        Management[Tunnel Management]
        ConsoleAPI[Console Tunnel API]
        RuntimeGateway[Code Session Runtime Gateway]
        Ingress[MCP Ingress / TunnelInvoker]
        ConnectorAPI[Connector API]
        Broker[NATS Broker]
        TunnelDB[(PostgreSQL)]
        NATS[(JetStream / Core NATS)]
    end

    subgraph PrivateNetwork[企业私网]
        Client[tunnel-client]
        PrivateMCP[Private MCP Server]
    end

    SDK -->|workspace API key| Management
    Console -->|Session + CSRF| ConsoleAPI
    Direct -->|workspace X-Api-Key| Ingress
    Sandbox -->|SessionIngressToken| RuntimeGateway
    RuntimeGateway -->|进程内 TunnelInvoker| Ingress
    Management --> TunnelDB
    ConsoleAPI --> TunnelDB
    ConsoleAPI --> Broker
    Ingress --> TunnelDB
    Ingress --> Broker
    ConnectorAPI --> TunnelDB
    ConnectorAPI --> Broker
    Broker --> NATS
    Client -->|Bearer tunnel token; outbound poll/response| ConnectorAPI
    Client --> PrivateMCP
```

组件职责如下：

| 组件               | 主要职责                                                                            |
| ------------------ | ----------------------------------------------------------------------------------- |
| Tunnel Management  | 创建、查询、分页、reveal token、rotate token、archive；采用 Claude Tunnels API 合同 |
| Console Tunnel API | 复用平台 Session/CSRF，向 Web 提供管理、连接快照和 MCP probe                        |
| MCP Ingress        | 接收直接 MCP 请求，完成租户授权、Header 清理、请求入队和响应重建                    |
| Connector API      | 校验 Tunnel token，处理 metadata、poll 和 response wire                             |
| Runtime Gateway    | 只允许 Sandbox 访问当前 Code Session Snapshot 中按名称配置的 MCP Server             |
| TunnelInvoker      | 识别 canonical Tunnel URL，在 OMA 进程内直接进入 Broker，避免 HTTP 回环             |
| NATS Broker        | presence、排队、原子 claim、响应状态、通知、超时、预算和进程亲和                    |
| PostgreSQL         | 保存 Tunnel、租户归属、归档状态和加密的 token version                               |
| `tunnel-client`    | 出站长轮询、并发与背压、本地 MCP 转发、通知与终态响应回传                           |
| Private MCP Server | 真正执行 `initialize`、`tools/list`、`tools/call` 等 MCP 请求                       |

## 3. 标识符与稳定 URL

### 3.1 Tunnel ID

所有边界统一使用：

```text
tunnel_<32 位小写十六进制>
```

ID 由 16 字节密码学随机数生成。管理 API、Console、数据库、Agent Snapshot、MCP URL 和 `tunnel-client`
配置都直接使用同一个值，不做 `tnl_...` 或其他前缀转换。

canonical URL 识别边界会再次校验 ID 必须严格匹配 `^tunnel_[0-9a-f]{32}$`，channel 必须匹配
`[a-z0-9_-]{1,64}`。URL 已指向受控 public origin 或 hostname suffix、但 ID/channel 不合法时仍被视为
“声明为本系统 Tunnel 的畸形 URL”并 fail-closed，不会降级成普通 remote MCP。

数据库内部另有 `uuid`。NATS subject 和 KV key 使用内部 UUID 的摘要；公开 Tunnel ID 不作为租户隔离依据。
每 Tunnel 的令牌许可、presence、亲和归属与 pending 额度保存在同一个控制记录，通过 CAS 更新。

### 3.2 MCP URL

推荐且稳定的入口是：

```text
POST   /v1/mcp/{tunnel_id}             # main channel
POST   /v1/mcp/{tunnel_id}/{channel}   # 指定 channel
DELETE /v1/mcp/{tunnel_id}[/{channel}] # 终止 MCP session
```

生产环境必须配置 `tunnel.public_base_url`，使 Agent Snapshot 和 Console 生成调用方真正可达的 HTTPS URL。
开发环境未配置时，Console 可以基于当前请求 origin 回退，但该回退不是生产部署合同。

Tunnel 还拥有一个创建后永不复用的 hostname alias，后缀由 `tunnel.domain_suffix` 决定，默认
`tunnel.invalid`。canonical path 是默认入口；只有部署侧另行提供 wildcard DNS/TLS 时，真实 hostname
入口才有意义。

### 3.3 API 路由图

| 方法与路由                                                                       | 用途                              |
| -------------------------------------------------------------------------------- | --------------------------------- |
| `POST /v1/tunnels`                                                               | 创建 Tunnel                       |
| `GET /v1/tunnels`                                                                | 分页列出当前 workspace 的 Tunnel  |
| `GET /v1/tunnels/{id}`                                                           | 获取单个 Tunnel                   |
| `POST /v1/tunnels/{id}/reveal_token`                                             | 解密并返回当前 active token       |
| `POST /v1/tunnels/{id}/rotate_token`                                             | 轮换 token version                |
| `POST /v1/tunnels/{id}/archive`                                                  | 归档 Tunnel                       |
| `POST /v1/tunnels/{id}/certificates`                                             | 保存 CA Certificate               |
| `GET /v1/tunnels/{id}/certificates`                                              | 分页列出 Certificate              |
| `GET /v1/tunnels/{id}/certificates/{certificate_id}`                             | 获取单个 Certificate              |
| `POST /v1/tunnels/{id}/certificates/{certificate_id}/archive`                    | 归档 Certificate                  |
| `GET /connector/v1/tunnels/{id}`                                                 | `tunnel-client` 启动 metadata     |
| `GET /connector/v1/tunnels/{id}/poll`                                            | Connector 长轮询领取命令          |
| `POST /connector/v1/tunnels/{id}/response`                                       | Connector 提交通知或终态响应      |
| `POST/DELETE /v1/mcp/{id}[/{channel}]`                                           | 直接 MCP 数据面                   |
| `GET /.well-known/oauth-protected-resource/v1/mcp/{id}[/{channel}]`              | 直接调用方的 RFC 9728 discovery   |
| `POST/DELETE /v2/ccr-sessions/{session}/mcp/{name}`                              | Managed Agent Runtime Gateway     |
| `GET /.well-known/oauth-protected-resource/v2/ccr-sessions/{session}/mcp/{name}` | Sandbox 可达的 RFC 9728 discovery |

Console 路由统一位于
`/api/console/organizations/{orgUuid}/workspaces/{workspaceId}/mcp_tunnels`。集合路由提供列表和创建；
`GET .../mcp_tunnels/{tunnelId}` 返回单条 Tunnel 及其实时 connection snapshot；资源子路由提供 reveal、
rotate、archive 和 probe。它与 `/v1/tunnels` 共享 Service/DB，但使用不同的鉴权和错误响应合同。

## 4. HTTP 边界与鉴权

MCP Tunnel 有多套凭据，因为每套凭据保护的是不同的信任边界。它们不能互相替代。

| 边界                    | 路由                                                                           | 凭据                                                               | 绑定范围                                           |
| ----------------------- | ------------------------------------------------------------------------------ | ------------------------------------------------------------------ | -------------------------------------------------- |
| Tunnel 管理面           | `/v1/tunnels...`                                                               | workspace API key，支持 `X-Api-Key` 或 Bearer 入口                 | organization + workspace                           |
| Console 管理面          | `/api/console/organizations/{orgUuid}/workspaces/{workspaceId}/mcp_tunnels...` | 平台 cookie Session；非安全方法再校验 `X-CSRF-Token`               | 当前可见 organization + workspace                  |
| 直接 MCP Ingress        | `/v1/mcp/{tunnel_id}[/{channel}]`                                              | 必须显式提供 workspace `X-Api-Key`                                 | organization + workspace + Tunnel                  |
| Connector metadata/poll | `/connector/v1/tunnels/{tunnel_id}...`                                         | `Authorization: Bearer <tunnel token>`                             | 精确 Tunnel + active token version                 |
| Connector response      | `/connector/v1/tunnels/{tunnel_id}/response`                                   | Bearer tunnel token；允许已 retired 的领取版本完成在途请求         | 精确 Tunnel + claim 绑定                           |
| Agent Runtime Gateway   | `/v2/ccr-sessions/{code_session_id}/mcp/{server_name}`                         | `sk-ant-si-` 前缀的 session-scoped Ed25519 JWT                     | Code Session + worker epoch + Snapshot server name |
| Private MCP Server      | `tunnel-client` 配置的私网 URL/stdio                                           | 下游 `Authorization`、client 本地静态 Header、mTLS 或 MCP 自身方案 | Private MCP Server 自己的权限模型                  |

### 4.1 Workspace API key

管理面要求 `anthropic-beta: mcp-tunnels-2026-06-22`。API key 解析成 `Principal` 后，所有 Tunnel PostgreSQL 查询都同时绑定
`organization_uuid`、`workspace_uuid` 和 Tunnel 标识；跨 workspace 的资源按不可见处理。

直接 MCP Ingress 只把 `X-Api-Key` 当作 OMA 凭据。它不会接受 Tunnel token，也不会把 workspace key
转发到私网。

### 4.2 Console Session 与 CSRF

Console 不调用 `/v1/tunnels`，而是调用 `/api/console/.../mcp_tunnels`。读请求依赖平台 cookie Session；
create、reveal、rotate、archive、probe 等非安全方法还必须通过 session-bound CSRF 校验。CSRF 中间件只
挂载在 MCP Tunnel Console 子路由，不改变其他既有 Console API 的行为。

列表不返回明文 token。只有 reveal 和 rotate 响应返回明文，并带 `Cache-Control: no-store`；前端只把它
保存在 Secret 弹窗组件的内存状态，不写 URL、local storage、session storage 或 Query cache。

### 4.3 Tunnel token

Tunnel token 只用于 `tunnel-client` 到 Connector API 的控制面认证：

1. 创建或轮换时生成 32 字节随机值，并编码为 URL-safe base64；
2. PostgreSQL 保存 SHA-256 hash，用于每次 Connector 请求的精确校验；
3. active token 的明文通过 `internal/secrets` envelope encryption 保存，支持受控 reveal；
4. token version 单调递增；一个 Tunnel 同时只能有一个 active version；
5. rotate 后旧 token 不能继续 metadata 或 poll；旧 envelope 字段立即清空；
6. 已由旧 version claim 的请求仍可凭 instance、shard token 和 version 提交终态响应；
7. archive 后 Tunnel 和所有 token 都拒绝 Connector 请求。

Tunnel token 不是 workspace key，不是 SessionIngressToken，也不是 Private MCP Server 的凭据。

### 4.4 Managed Agent Session 凭据

Session 创建或恢复时，OMA 签发 `sk-ant-si-` Ed25519 JWT，使用 issuer `session-ingress`、audience
`anthropic-api` 和 role `worker`，绑定 Code Session、organization、workspace、Agent version 和 worker epoch。
同一个 SessionIngressToken 用于 worker/relay 与命名 MCP Gateway；各入口保留自己的授权检查。
整个 Managed Agent 运行环境是同一个 Session 执行主体，MCP 配置中的 Token 也拥有该 Session 的 ingress
身份权限，配置文件必须按运行凭据保护，不能写回 Agent Snapshot 或进入日志。

命名 Gateway 及其 discovery 入口要求正数 worker epoch，并回查活动 Session 的当前 epoch；缺失或过期的
worker epoch 均被拒绝。Gateway 精确匹配路径中的 Code Session ID，再根据 server name 查找 Session 固定
的 Agent Snapshot，执行租户、exact URL 和 Environment network policy 授权。请求不能通过 query 指定目标。

转发前删除入口 `Authorization`，避免 SessionIngressToken 泄漏到 Broker、`tunnel-client` 或 Private MCP Server。

### 4.5 Private MCP 凭据边界

这里需要区分两条调用链：

- 直接调用 `/v1/mcp/...` 时，OMA 会删除 `X-Api-Key`、Cookie、hop-by-hop 和 Tunnel 内部 Header，但允许
  下游 `Authorization` 进入队列；`tunnel-client` 最终把它用于 Private MCP HTTP 请求。
- Managed Agent 调用时，Runtime Gateway 会先删除自己的 SessionIngressToken。当前 TunnelInvoker 分支直接进入
  NATS Broker，不经过普通 remote MCP 使用的 OMA Vault HTTP transport。因此 Tunnel 私网凭据应在
  `tunnel-client` 侧通过本地静态额外 Header、mTLS 或 Private MCP 自身支持的方式提供。

也就是说，不能把 OMA workspace key、Tunnel token 或 SessionIngressToken 当作下游 MCP token。任何允许穿越
Tunnel 的下游 `Authorization` 都会经过 OMA 请求处理和 JetStream 队列；若凭据要求严格只留在私网，应使用
`tunnel-client` 的本地凭据注入能力，而不是从调用方转发。

原版 `tunnel-client` 的本地静态 Header 只适用于 HTTP MCP，并按目标 origin 限定；stdio 没有 HTTP Header
注入。若一个直接调用请求同时携带同名转发 Header，客户端当前以后应用的转发值为准，因此它可能覆盖本地
静态 `Authorization`。Managed Agent 路径不会转发 SessionIngressToken，所以不会发生 Session Token 覆盖本地凭据。

## 5. Tunnel 生命周期与 `tunnel-client` 启动

### 5.1 创建

Tunnel 可以通过 Console 或 Tunnel 管理面创建。创建事务同时写入：

- `mcp_tunnels`：公开 ID、内部 UUID、organization/workspace、名称、domain 和时间；
- `mcp_tunnel_token_versions`：version 1、token hash 和加密 envelope。

创建响应不返回明文 token。Console 创建成功后进入独立详情页，路由 state 只传递“刚创建”标记；详情页再调用
reveal，并只把明文保存在组件内存中。Token 不进入 URL、history state、Query cache 或 storage。

### 5.2 Certificate API-only 生命周期

Claude SDK 的 Certificate 子资源支持 Create、Get、List 和 Archive。OMA 保存完整 CA PEM，并从唯一的
X.509 CA block 计算 DER SHA-256 fingerprint 和 `expires_at`。这些数据目前只供管理 API 往返；不会被
Connector、Broker、Ingress、Agent Picker 或 MCP Runtime Gateway 消费。

Certificate 路径中的 Tunnel ID 只用来确认资源归属和 workspace 范围。归档 Tunnel 不改变它的
Certificate 记录；只有 Certificate Archive API 会设置它自身的 `archived_at`。

### 5.3 配置原版 `tunnel-client`

OpenAI `tunnel-client` 通过 metadata/poll/response HTTP 协议连接 OMA。
客户端将 OMA 的 Connector 路由前缀配置为 `/connector`：

```yaml
config_version: 1
control_plane:
  base_url: https://oma.example.com
  url_path: /connector
  tunnel_id: tunnel_0123456789abcdef0123456789abcdef
  api_key: env:OMA_TUNNEL_TOKEN
mcp:
  server_urls:
    - channel: main
      url: http://private-mcp.internal:39091/mcp
```

客户端最终访问：

```text
GET  https://oma.example.com/connector/v1/tunnels/{tunnel_id}
GET  https://oma.example.com/connector/v1/tunnels/{tunnel_id}/poll
POST https://oma.example.com/connector/v1/tunnels/{tunnel_id}/response
```

`control_plane.api_key` 必须引用 Tunnel token。不要把 token 固化进受版本控制的 YAML。

OMA 当前实现的是原版客户端所需的 metadata、poll、response 子集，不实现
`/cloudflare/runtime`。管理面的 Certificate create、retrieve、list、archive API 会独立持久化 CA 证书及其元数据，
但不会把证书注入 Connector、Broker、Ingress 或 MCP 请求链路。启用原版客户端的其他托管服务能力前，
应先确认 OMA 是否实现对应 Connector endpoint；不能把 Certificate API 的 2xx 响应理解为运行时证书认证已启用。

### 5.4 启动与 presence

客户端启动后先读取 metadata 做配置诊断，然后持续 long-poll。每次 poll 携带：

- Bearer Tunnel token；
- 稳定的 `X-Tunnel-Client-Instance-Id`；
- `X-Tunnel-MCP-Server-Info`，声明 1 到 32 个 channel 及其 `proc_affinity`；
- `limit` 和 `timeout_ms`。

Broker 把成功 poll 视为短期 Connector presence。presence 默认 TTL 为 60 秒，因此 Console 的
`connected` 是一个 NATS KV 快照，不是永久状态，也不是一次完整 MCP 调用已经成功的证明。

## 6. OMA 与 `tunnel-client` 的 wire 整合

### 6.1 长轮询

典型请求：

```http
GET /connector/v1/tunnels/{tunnel_id}/poll?limit=25&timeout_ms=30000
Authorization: Bearer <tunnel-token>
X-Tunnel-Client-Instance-Id: <stable-instance-id>
X-Tunnel-MCP-Server-Info: {"version":1,"channels":[{"name":"main"}]}
```

无任务时 OMA 返回 `204 No Content`。有任务时返回：

```json
{
  "commands": [
    {
      "request_id": "req_<opaque>",
      "shard_token": "<opaque>",
      "command_type": "jsonrpc",
      "channel": "main",
      "created_at": "2026-08-25T00:00:00Z",
      "headers": {
        "Accept": ["application/json, text/event-stream"],
        "Content-Type": ["application/json"]
      },
      "response_timeout": "119000ms",
      "jsonrpc": {
        "jsonrpc": "2.0",
        "id": "1",
        "method": "tools/list",
        "params": {}
      }
    }
  ]
}
```

`response_timeout` 是 OMA 统一 deadline 的剩余时间，不是客户端可以重新开始的一段完整超时。

### 6.2 客户端调度

原版客户端内部把职责分成：

1. control-plane poller 根据本地 prefetch queue 剩余容量计算 `limit`，最大 25；
2. bounded queue 提供背压，满队列时暂停继续 poll；
3. dispatcher 按 `mcp.max-concurrent-requests` 控制真正访问 MCP Server 的并发；
4. MCP client 根据 channel 把命令路由到 Streamable HTTP、stdio 或内存实现；
5. JSON-RPC notification 作为非终态响应立即回传；
6. 最终 JSON-RPC、OAuth discovery 或 session termination 响应结束命令。

OMA 不需要了解 Private MCP URL、stdio command 或本地证书路径。这些都留在 `tunnel-client` 配置中。

### 6.3 响应提交

客户端把 poll 返回的 shard token 放在 Header，而不是响应 JSON 中：

```http
POST /connector/v1/tunnels/{tunnel_id}/response
Authorization: Bearer <tunnel-token>
X-Tunnel-Client-Instance-Id: <same-instance-id>
X-Tunnel-Shard-Token: <shard-token-from-command>
Content-Type: application/json
```

```json
{
  "request_id": "req_<opaque>",
  "channel": "main",
  "resp_json": {
    "jsonrpc": "2.0",
    "id": "1",
    "result": { "tools": [] }
  },
  "resp_headers": {
    "Content-Type": ["application/json"]
  },
  "resp_code": 200,
  "resp_type": "jsonrpc_response"
}
```

Broker 会同时校验 request ID、Tunnel UUID、channel、instance ID、shard token、token version 和 command
type。绑定不匹配、已取消、已过期或未知请求按不可见请求处理。

`tunnel-client` 会对适合重试的 response POST 做有限重试。因为 response 是潜在的重复写入，OMA 通过
terminal tombstone 让同一个正确绑定的重复终态响应幂等成功。

## 7. 一次直接 MCP 调用的完整流程

```mermaid
sequenceDiagram
    autonumber
    participant Caller as MCP Caller
    participant Ingress as OMA MCP Ingress
    participant Connector as OMA Connector API
    participant DB as PostgreSQL
    participant Broker as NATS Broker
    participant Client as tunnel-client
    participant MCP as Private MCP Server

    Client->>Connector: Bearer token long-poll
    Connector->>DB: 校验 tunnel + token hash/version
    Connector->>Broker: 注册 channel presence，等待任务
    Caller->>Ingress: POST /v1/mcp/{id} + X-Api-Key
    Ingress->>DB: 按 org/workspace/id 查 active Tunnel
    Ingress->>Ingress: 限流、读 body、清理 headers、生成 request_id/deadline
    Ingress->>Broker: 先订阅 response，再 enqueue
    Broker-->>Connector: 原子 claim，返回 command + shard token
    Connector-->>Client: poll 返回 command
    Client->>MCP: 转发 MCP 请求
    MCP-->>Client: notification 或 final response
    Client->>Connector: POST response + instance/shard/token version
    Connector->>Broker: 原子校验并写通知或终态
    Broker-->>Ingress: Core NATS 唤醒；终态仍保存在请求 KV
    Ingress-->>Caller: JSON 或重建后的 SSE
```

几个关键顺序不能交换：

- Ingress 先建立 response subscription，再 enqueue，避免极快 Connector 的首条通知丢失；
- enqueue 前必须看到目标 channel 的 live presence，否则快速返回 503，不把请求放进无人消费的队列；
- claim 先以请求记录 CAS 绑定 instance、shard token 和 token version，再用控制记录 CAS 取得交付许可；
- terminal response 先以 CAS 保存 JetStream KV 终态，再通过 Core NATS 唤醒原等待进程；丢失唤醒时通过有界读取恢复；
- Core NATS 信号丢失时，等待方在 deadline 内每 250 ms 有界读取持久终态；
- 调用方断开或 deadline 到期会主动 cancel；迟到响应不能重新唤醒已结束的调用方。

## 8. Managed Agent 调用的额外流程

Managed Agent 不直接拿 workspace API key 调 `/v1/mcp/...`。它多一层 Runtime Gateway：

```mermaid
sequenceDiagram
    autonumber
    participant Start as Session Start
    participant Sandbox as Claude Code in Sandbox
    participant Gateway as Runtime Gateway
    participant Snapshot as Agent Snapshot + Environment Policy
    participant Invoker as TunnelInvoker
    participant Broker as NATS Broker
    participant Connector as Connector API
    participant Client as tunnel-client
    participant MCP as Private MCP Server

    Start->>Start: 签发 SessionIngressToken
    Start->>Sandbox: 写入 0600 MCP config，普通原始 URL + Tunnel named Gateway
    Sandbox->>Gateway: POST /v2/ccr-sessions/{session}/mcp/{server_name}
    Gateway->>Gateway: 校验 SessionIngressToken、session path、正数 worker epoch
    Gateway->>Snapshot: 按 server_name 解析固定原始 URL并执行 network policy
    Gateway->>Gateway: 删除入口 Authorization
    Gateway->>Invoker: canonical Tunnel URL 走进程内分发
    Invoker->>Broker: 与直接 Ingress 相同的 enqueue/wait 流程
    Client->>Connector: Bearer token long-poll
    Connector->>Broker: register/claim
    Broker-->>Connector: command + shard token
    Connector-->>Client: poll 返回命令
    Client->>MCP: 私网调用
    MCP-->>Client: MCP 响应
    Client->>Connector: response + instance/shard/token version
    Connector->>Broker: 校验并提交响应
    Broker-->>Invoker: terminal response
    Invoker-->>Gateway: MCP HTTP/SSE response
    Gateway-->>Sandbox: 重写为 Sandbox 可访问的 resource metadata
```

Agent Snapshot 与 Code Session metadata 保存原始 MCP 源声明。Session 启动或恢复取得当前身份后，
构建器先识别目标类型，再分别构建普通 MCP 和 Tunnel MCP。Tunnel 条目固定使用 `type: "http"`，地址为：

```text
{code_session.sandbox_api_base_url}/v2/ccr-sessions/{code_session_id}/mcp/{server_name}
```

并只在该 Tunnel 条目中放入当前 SessionIngressToken。Channel 名为 `sse` 或 `stdio` 也不会改变该 HTTP 传输类型。
普通 Directory/自定义 MCP 使用自己的传输规则，URL、Header 和工具配置保持
原样，通过 Sandbox HTTP(S) Proxy/MITM 与 Vault 注入；启动 payload 顶层 `mcp_servers` 也完整保留。
最终文档用于生成权限为 `0600` 的配置文件和启动参数。`session_context` 通过鉴权后使用同一构建器和当前
Session 凭据返回文档，并禁止缓存；持久化源声明不包含运行时 Gateway 地址或 Token。

Console Agent Picker 允许同一个 Tunnel 绑定多个 Channel，但不扩展 Agent API。所有 Channel 统一使用
`tunnel_<32hex>.<channel>` 作为 server name，因此 main 为 `tunnel_<32hex>.main`。URL 分别保存为
`/v1/mcp/{tunnelId}` 和 `/v1/mcp/{tunnelId}/{channel}`；名称后缀中的 `_` 转为 `.` 以避免保留分隔符 `__`，URL 中的 Channel 保持原值。每个 server name 都有同名
`mcp_toolset.mcp_server_name`。Runtime Gateway 按 Snapshot 中的 name 找到原始 URL，因此多个 Channel 可以各自
配置工具权限。

Gateway 只在 URL origin 与 `tunnel.public_base_url` 精确一致且 path 是 canonical Tunnel path，或 hostname
匹配受控 domain suffix 时，才交给 TunnelInvoker。named Gateway 解析到普通 remote MCP 时返回 404，不提供
普通 MCP outbound 路径。带 `mcp_url` 参数的显式 Session MCP proxy 优先识别 Tunnel，普通 MCP 使用 outbound 路径。
TunnelInvoker 在进程内调用 Broker；受控 origin 和 suffix 校验防止第三方 host 被误认为 Tunnel。

`server_name` 使用 Snapshot 中的原值做精确索引，不接受首尾空白。畸形 URL、非规范名称或重复名称都会让
Snapshot policy 编译失败；运行时构建非空 MCP URL 时也不会吞掉 URL 解析错误。上述失败都不会回退成
unrestricted、普通 MCP 或部分放行。

## 9. 命令类型与 MCP 传输语义

OMA 与客户端当前使用三类 command：

| command type          | 来源                                  | 客户端动作                               | terminal response type         |
| --------------------- | ------------------------------------- | ---------------------------------------- | ------------------------------ |
| `jsonrpc`             | MCP POST                              | 调用 MCP Server；可产生多条 notification | `jsonrpc_response`             |
| `oauth_discovery`     | RFC 9728 protected-resource discovery | 从私网侧读取 metadata                    | `oauth_discovery_response`     |
| `session_termination` | MCP DELETE                            | 终止对应 MCP session                     | `session_termination_response` |

`jsonrpc_notify` 是非终态响应。它通过 Core NATS request/reply 到达正在等待的 Ingress；接收方先确认有界缓冲接纳，并被编码成标准
`event: message` / `data: ...` SSE frame。最终 JSON 值关闭这次 HTTP 响应。

Connector wire 中只传规范化后的 `resp_json`，不保留 Private MCP 的原始 SSE framing。Ingress 根据最终
`Content-Type` 恢复 JSON 或 SSE 语义。独立的 `GET /v1/mcp/...` SSE 连接不受支持，明确返回 405。

OAuth discovery 的 `resource` 和 MCP 401 `WWW-Authenticate` 中的 `resource_metadata` 会被重写成调用方
实际可达的 canonical URL 或 Runtime Gateway URL。成功和失败状态都执行相同改写，不能因为 Connector
返回 4xx 而把 Private MCP 地址原样暴露给调用方。

## 10. NATS Broker 状态机

```mermaid
stateDiagram-v2
    [*] --> queued: enqueue
    queued --> dispatched: Connector claim
    queued --> canceled: caller disconnects
    queued --> expired: deadline
    dispatched --> completed: first terminal response
    dispatched --> canceled: caller disconnects
    dispatched --> expired: deadline
    completed --> [*]: tombstone TTL
    canceled --> [*]: tombstone TTL
    expired --> [*]: tombstone TTL
```

重要语义：

- `dispatched` 后不自动重执行。OMA 在绑定、交付许可和 JetStream ACK 后写 poll HTTP；该窗口内崩溃会失败或超时，不能承诺外部工具 exactly-once。
- notification 不终结请求。原等待进程确认缓冲接纳后返回成功；每请求最多 16 条 / 2 MiB，全进程最多 64 MiB，慢 HTTP 写入仍占用预算。满载返回 429。
- 终态与完成状态在同一请求 KV 中 CAS 保存；重复提交不延长保留期，取消不能覆盖完成。Core 唤醒丢失时每 250 ms 有界读取终态。
- 请求 KV 的 `MaxMsgs` 默认 4096，限制排队、执行、保留终态与亲和会话的合计数量。每 key 保留一个版本；TTL 为 request timeout 加 tombstone TTL。空间按每条最大 2 MiB 结果预留，新增请求在准入时背压。
- 每 Tunnel 另有 pending 限制，默认 256 个请求、32 MiB；完成或取消释放，崩溃未释放的额度按原 deadline 清理。控制记录还限制最多 32 个 channel、每 channel 64 个实例。
- 命令是 R3 file WorkQueue，过期或确认交付后删除。有效命令的 KV 暂时 missing 时 NAK 保留，不能推断为已取消。
- 每个 HTTP poll 并行消费声明的 channel，使用请求独立的 JetStream `Consume()` 消费过程和共享 durable Consumer；每个 channel 一次拉取一条，交给领取逻辑后才继续拉取。OMA 不设置实例级等待 poll 数量上限，实际消息仍受单条大小、批次、pending 与存储预算约束。
- 首条有效命令就绪后，只合并已经可接收的命令，不额外等待；达到 limit、约 2 MiB 批次上限或没有就绪命令时返回。`timeout_ms=0` 使用 `FetchNoWait`，只查询已有命令，没有则返回 204；查询仍需要网络往返。
- poll 返回、超时或连接断开时取消本次消费并在后台归还未绑定消息，HTTP 返回不等待退订的网络确认；已绑定命令不重新派发。无活动 durable Consumer 按保留期限回收。

### 10.1 Channel 与进程亲和

channel 名必须匹配 `[a-z0-9_-]{1,64}`。server-info 支持 v1 和 v2，v2 的 `stateless` 与 `proc_affinity` 独立。
未声明 channel 时使用 `main`。声明亲和 channel 时必须携带合法 instance ID。

亲和 channel 的 initialize 在执行前预留代理会话，绑定一个 live instance。成功响应先保存下游 session ID 与 ready，再暴露终态。
OMA 给调用方返回不透明 `Mcp-Session-Id`；后续请求按映射进入同一实例，发给 Connector 时还原下游 ID，stdio 没有下游 ID 时移除 header。
普通 HTTP 非亲和 channel 保持透传。stdio 的 DELETE 仅关闭 OMA 代理会话，避免发送原版 Connector 无法解析的无下游 ID 命令。

进程退出后旧会话返回 404；调用方可在同一 Tunnel 重新 initialize，绑定新实例。旧会话不迁移，已交付工具不自动重执行。
会话占用全局 KV slot，空闲 `request_timeout + tombstone_ttl` 后过期；后续活动刷新期限。初始化失败或崩溃遗留记录由相同 TTL 收敛。

v2 自包含且完全没有 initialize/session header 的路径使用固定 channel owner；owner 永久退出后需重新配置 Tunnel。

## 11. rotate 与 archive

### 11.1 Token rotation

rotation 同时协调 PostgreSQL 与 NATS 控制 KV：

1. 先用数据库 active version 对齐 Broker；
2. 以同一控制 key 的 CAS 暂停当前 token version 并清 presence；后续交付许可必须拒绝；
3. PostgreSQL 事务锁定 Tunnel 和 active token，以 expected version 防止并发覆盖；
4. retire 旧 token、清空旧 envelope、创建 version + 1；
5. NATS 控制 KV 单调激活新 version；
6. 新 token 可以 poll，旧 token 只能为已经 claim 的请求提交匹配响应。

若数据库事务失败，服务重新读取数据库 active version 再恢复 Broker，不会盲目恢复请求开始时看到的旧值。
非空 poll 批次在写出前再次验证数据库凭据；长轮询期间失效的 token 不会收到命令，已领取但未交付的请求会被有界取消。

### 11.2 Archive

archive 是资源终止操作：

- Tunnel、所有 token version 的归档与 River 控制记录清理任务在同一数据库事务中提交；Certificate 保持独立，只能通过自身的 Archive API 归档；
- NATS 控制 KV 中当前 token version 被暂停，presence 被清除；提交后 River Worker 精确清除该控制记录，释放 4096 个全局名额中的一个；
- 管理面 retrieve/list 按归档语义返回；
- Connector metadata/poll/response 不再接受归档 Tunnel；
- MCP Ingress 把归档 Tunnel 视为不可见资源；
- Agent Snapshot 即使仍保留旧 URL，Runtime Gateway 的实时 Tunnel 查询仍会拒绝。

归档成功表示资源与清理任务已持久化，NATS 容量在后台清理成功后释放。Worker 在清理前核对租户归属、
Tunnel UUID 和归档状态；清理幂等，失败一分钟后再执行，不消耗普通重试次数。进程退出不会丢失已提交任务。
普通 Poll、派发与 pending 释放不能创建缺失的控制记录；只有在数据库行锁下确认 Tunnel 与 Token 仍有效后
才能恢复记录，因此迟到 Poll 不会复活已归档资源。数据库资源记录继续保留供详情和历史查询使用。

## 12. Header、安全与数据边界

Ingress 请求 denylist 至少包括：

- hop-by-hop Header 和 `Connection` 指定的扩展 Header；
- Cookie、`X-Api-Key`、proxy credentials；
- shard token、client instance、server info 等 Tunnel 内部 Header；
- `Content-Length` 等需要由新传输重新计算的字段。

允许的 MCP 协议 Header 和调用方下游 `Authorization` 可以转发。Connector 响应只接受显式白名单，例如
`Content-Type`、`Mcp-Session-Id`、`Mcp-Protocol-Version`、`WWW-Authenticate`；`Content-Length` 和 `Date`
会被删除，因为 body 已经被 wire 规范化。

默认限制：

| 项目                                      | 默认值 |
| ----------------------------------------- | ------ |
| 请求或 terminal response 的 MCP JSON body | 1 MiB  |
| Connector response 外层协议包             | 2 MiB  |
| Header 总量                               | 32 KiB |
| 单个 Header value                         | 8 KiB  |
| 每 Tunnel pending 请求                    | 256    |
| 每 Tunnel pending payload                 | 32 MiB |
| poll timeout                              | 30 秒  |
| MCP 总 deadline                           | 2 分钟 |
| presence TTL                              | 60 秒  |
| terminal tombstone                        | 5 分钟 |

`max_pending_requests` 允许配置为 `1..512`。Tunnel OAuth protected-resource discovery 走 Connector 的
`oauth_discovery` command，并受 Tunnel 请求总 deadline 约束；named Gateway 不为普通 MCP 发起 metadata 请求。

运行日志禁止记录 workspace key、Tunnel token、下游 Authorization、Cookie、shard token 和原始 body。
MCP payload、tool argument、response 及被转发的 Authorization 会经过 OMA 和 NATS；“无需私网入站端口”
不等于“业务数据不经过 OMA”。

## 13. 错误与故障语义

| 现象                                                         | 对外状态             | 含义                                  |
| ------------------------------------------------------------ | -------------------- | ------------------------------------- |
| workspace、Console、SessionIngressToken 或 Tunnel token 无效 | 401/403              | 对应信任边界鉴权失败                  |
| Tunnel 不属于 scope、已归档或 response 绑定不匹配            | 404 或资源不可见语义 | 防止跨租户和内部状态泄漏              |
| GET MCP SSE                                                  | 405                  | Tunnel 只支持同请求内 SSE             |
| body/Header 超限                                             | 413                  | 请求未入队或响应被拒绝                |
| pending 数量或 payload 预算超限                              | 429                  | Broker 背压                           |
| 没有 live Connector                                          | 503                  | 快速失败，不排队等待客户端上线        |
| NATS / JetStream 不可用                                      | 503                  | 不回退到进程内队列                    |
| 统一 deadline 到期                                           | 504                  | 请求已被 cancel/expired，迟到响应无效 |

`/healthz` 只表示 OMA 进程存活；`/readyz` 同时检查 PostgreSQL 和 Tunnel NATS（`tunnel_nats`）；Console presence 只说明近期
有 poll。要证明 Tunnel 可用，应至少执行 Console probe 或真实 `initialize` + `tools/list`。要证明 Managed
Agent 链路可用，必须执行真实 Sandbox Session 工具调用。

`tunnel-client` 自己的 `/readyz` 可能在旧 token 被 rotate 后仍为 200，因此排障时应以 Connector metadata、
poll 和实际 MCP 请求为准。

## 14. 开发定位图

OMA 主要代码：

| 路径                                                                                           | 内容                                                    |
| ---------------------------------------------------------------------------------------------- | ------------------------------------------------------- |
| `internal/tunnels/management_handler.go`                                                       | Tunnel 管理 API 与 beta gate                            |
| `internal/tunnels/management_service.go`                                                       | create/reveal/rotate/archive 编排                       |
| `internal/tunnels/certificate_handler.go`                                                      | Certificate Create/Get/List/Archive HTTP 合同           |
| `internal/tunnels/certificate_service.go`                                                      | X.509 校验、fingerprint 和独立持久化编排                |
| `internal/tunnels/connector_handler.go`                                                        | Tunnel token、metadata、poll、response                  |
| `internal/tunnels/ingress_handler.go`                                                          | direct ingress、TunnelInvoker、SSE 和 OAuth rewrite     |
| `internal/tunnels/broker_nats.go`、`broker_control.go`、`broker_poll.go`、`broker_sessions.go` | NATS 状态机、控制记录 CAS、presence/affinity            |
| `internal/tunnels/protocol.go`                                                                 | command、response 和 channel wire 类型                  |
| `internal/tunnels/probe.go`                                                                    | Console/catalog initialize/tools/list Broker 探测       |
| `internal/mcpcatalogs/handler.go`                                                              | Agent 工具 catalog、Tunnel scope 校验与 last-good 保存  |
| `internal/db/mcp_tunnels.go` 及 Mapper/XML                                                     | Tunnel 与 token version 持久化                          |
| `internal/db/mcp_tunnel_certificates.go` 及 Mapper/XML                                         | API-only Certificate 持久化                             |
| `internal/db/migrations/00060_rebuild_mcp_tunnels.sql`                                         | 当前 Tunnel schema                                      |
| `internal/codesessions/mcp_proxy.go`                                                           | Runtime Gateway、Snapshot URL 解析和 TunnelInvoker 分支 |
| `internal/codesessions/session_credentials.go`                                                 | SessionIngressToken 的签发与身份校验                    |
| `internal/codesessions/mcp_runtime_config.go`                                                  | 目标识别、普通 MCP 与 Tunnel MCP 独立构建               |
| `internal/codesessions/mcp_runtime_tools.go`                                                   | MCP 工具权限映射                                        |
| `internal/environments/managed_agent_mcp_config.go`                                            | MCP 配置文件与启动字段封装                              |
| `internal/environments/environment_manager.go`                                                 | environment-manager 启动 payload 与命令封装             |
| `web/src/features/mcp-tunnels/`                                                                | Console 列表、详情、共享动作与 canonical 路由 UI        |

独立 `tunnel-client` 仓库主要代码：

| 路径                                                 | 内容                                            |
| ---------------------------------------------------- | ----------------------------------------------- |
| `pkg/controlplane/internal/tunnel_service_client.go` | URL 拼接、metadata、poll、response 和重试       |
| `pkg/controlplane/internal/poller.go`                | 长轮询与 prefetch queue 背压                    |
| `pkg/controlplane/wiretypes/`                        | 与 OMA 对接的 JSON wire                         |
| `pkg/dispatcher/internal/`                           | command 调度、并发和响应提交                    |
| `pkg/mcpclient/`                                     | HTTP/stdio MCP 调用、Header 与 notification     |
| `pkg/runtimeconfig/`                                 | `base_url`、`url_path`、Tunnel ID 和 token 配置 |

## 15. 修改时必须保持的关键不变量

开发者修改 Tunnel 时，应优先确认以下不变量没有被破坏：

1. 所有资源读写都同时绑定 organization、workspace 和 Tunnel；
2. workspace key、Tunnel token、SessionIngressToken、Private MCP 凭据不可互换；
3. response subscription 必须先于 enqueue；
4. 交付许可与撤销必须在同一控制 key 上排序，terminal/cancel 必须竞争同一请求 revision；
5. dispatched 请求不能自动重投；
6. response 必须校验 instance、shard、token version、channel 和 command type；
7. rotate 必须允许旧 version 只排空已 claim 请求，不能继续领取新请求；
8. archive 必须在管理面、Connector、Ingress 和 Runtime Gateway 同时失效；
9. Runtime Gateway 只能按 Snapshot 中无首尾空白的精确 server name 选择目标，不能接受 Sandbox 自选 URL；
10. SessionIngressToken、workspace key、Tunnel token、Cookie 和私网 secret 不能进入日志；
11. SSE `Content-Type` 与实际 framing 必须一致；
12. Connector wire 必须满足 OpenAI `tunnel-client` 协议合同，并通过契约测试。

## 16. 验证层次

从低到高建议按以下顺序验证：

1. `internal/tunnels` 单测：鉴权、Header、wire、Broker 状态与错误映射；
2. NATS 集成测试：并发领取、撤销、响应背压、取消、missing 恢复及三副本故障；
3. 管理面与 Console handler 测试：tenant scope、CSRF、secret no-store，以及官方 Go SDK Certificate
   Create/Get/List/Archive 生命周期；
4. `tunnel-client` `go test -race ./...`：poller、dispatcher、MCP transport 和 response retry；
5. 本地原版客户端 smoke：metadata、initialize、tools/list、tools/call、DELETE；
6. Console probe：验证 Console → Broker → client → Private MCP；
7. Agent Picker/Detail catalog：验证添加 Channel 后自动发现、卡片 Refresh，以及 catalog Refresh 通过 Broker 保存工具；
8. 真实 Managed Agent Session：验证 Sandbox → Runtime Gateway → TunnelInvoker → Broker → client → Private MCP。

最后一级不可由 `/readyz`、presence 或 Console probe 替代。完整手动步骤见
[MCP Tunnel 手动验收](../zh/mcp-tunnel-manual-acceptance.md)。

原版 tunnel-client 测试入口：

```bash
TEST_TUNNEL_CLIENT_BINARY=/absolute/path/to/tunnel-client go test ./internal/tunnels -run '^TestOfficialTunnelClientIntegration$' -count=1 -v
```

该测试启动隔离 NATS 和 HTTP/stdio MCP fixture，使用替身 DB 授权；它不代替实际 Claude Agent SDK、Claude Code CLI 或 Managed Agent Sandbox 验收。

### 工作区授权、Token 恢复与分页验收

Console 的 Tunnel 操作必须获得 URL 目标工作区的权限。验证时应使用同一组织内只有 A 工作区权限的普通成员，
确认即使 Header 指定 A，也不能读取 B 的 Tunnel、查看 Token 或执行轮换/归档。

Token 轮换、归档和恢复共用 PostgreSQL 事务行锁。进程在 NATS 暂停后退出、DB 事务回滚时，
有效 Connector 的下一次 Poll 会基于 DB 当前版本恢复；事务提交了轮换时只接受新 Token，提交了归档时不恢复。
正在进行的事务不会被恢复操作绕过；有界锁等待失败返回 503，Connector 可以重试。

工具目录探测完整读取分页（最多 20 页、512 个工具），不保存部分成功结果。可以用带空中间页的分页 MCP 验证游标传递，
再用重复游标、超额结果和后续页错误验证整体失败与会话清理。
