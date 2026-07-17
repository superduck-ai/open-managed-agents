# CCRv2 子进程 HTTPS 代理与模型运行时

## 目标

Managed Agent 启动 Claude Code 后，Claude 创建的 Bash、MCP、LSP 和 hook 子进程需要继承 `HTTPS_PROXY`。当前 Claude Code 内置的 CCRv2 relay 已经负责在容器内监听本地 CONNECT 代理，但只有以下条件同时成立时才会启用：

- `CLAUDE_CODE_REMOTE=true`
- `CLAUDE_CODE_REMOTE_SESSION_ID` 是当前 code session ID
- `CCR_UPSTREAM_PROXY_ENABLED=1`
- `/run/ccr/session_token` 存在且非空
- `GET {ANTHROPIC_BASE_URL}/v1/code/upstreamproxy/ca-cert` 成功
- `WS {ANTHROPIC_BASE_URL}/v1/code/upstreamproxy/ws` 可以完成鉴权和 CONNECT 隧道

本实现不修改 Claude Code/SuperDuck。`environment-manager` 和 API server 共同补齐上述启动与服务端协议。

## 关键约束

Claude Code 当前使用同一个 `ANTHROPIC_BASE_URL` 完成两类请求：

1. 模型请求 `/v1/messages`。
2. CCRv2 CA 与 WebSocket upstream proxy 请求。

因此不能让 Claude 继续直接指向 Kimi/Anthropic，同时再为 CCRv2 指定另一个地址。运行时统一指向 Open Managed Agents API server；API server 再持有真正的上游地址和密钥，并代理模型请求。

这也建立了明确的凭证边界：上游模型密钥只存在于 API server，不能进入 sandbox 环境变量、environment-manager stdin 或 Claude 子进程。

## 启动数据流

```mermaid
sequenceDiagram
    participant API as Open Managed Agents API
    participant EM as environment-manager
    participant Claude as Claude Code
    participant Relay as Local CONNECT relay
    participant Child as Claude child process

    API->>EM: v0 payload<br/>api_base_url + OAuth token + ingress JWT
    Note over API,EM: CLAUDE_CODE_REMOTE=true<br/>CCR_UPSTREAM_PROXY_ENABLED=1
    EM->>EM: write /run/ccr/session_token (0600)
    EM->>Claude: ANTHROPIC_BASE_URL=api_base_url<br/>REMOTE_SESSION_ID=code-session ID<br/>OAuth FD + WebSocket auth FD
    Claude->>API: GET /v1/code/upstreamproxy/ca-cert
    Claude->>Relay: start 127.0.0.1 ephemeral CONNECT listener
    Claude->>Claude: unlink /run/ccr/session_token after relay is ready
    Claude->>Child: HTTPS_PROXY=http://127.0.0.1:{port}
    Child->>Relay: CONNECT public-host:443
    Relay->>API: WebSocket /v1/code/upstreamproxy/ws
    alt MITM disabled
        API->>Child: bidirectional raw TLS tunnel
    else MITM enabled
        API->>Child: dynamically signed leaf certificate
        API->>API: decrypt and validate HTTP/1.1
        API->>API: establish independently verified upstream TLS
    end
```

### environment-manager payload

`buildEnvironmentManagerV0Payload()` 注入：

```text
CLAUDE_CODE_REMOTE=true
CCR_UPSTREAM_PROXY_ENABLED=1
CLAUDE_CODE_POST_FOR_SESSION_INGRESS_V2=1
CLAUDE_CODE_USE_CCR_V2=1
CLAUDE_CODE_WORKER_EPOCH=1
```

`startup_context.api_base_url` 是 sandbox 可访问的 Open Managed Agents API 地址。payload 不再注入上游 `ANTHROPIC_BASE_URL` 或 `ANTHROPIC_API_KEY`；environment-manager 使用 `api_base_url` 作为 Claude 的 `ANTHROPIC_BASE_URL` fallback。

environment-manager 启动 Claude 时还会根据 executor 的 session ID 设置 `CLAUDE_CODE_SESSION_ID` 与 `CLAUDE_CODE_REMOTE_SESSION_ID`。后者是 relay 的必要条件；缺失时 Claude 会记录 `CLAUDE_CODE_REMOTE_SESSION_ID unset; proxy disabled`，即使 token 文件和其他开关都存在也不会注入代理环境。

payload 同时提供两种用途独立的 auth：

- `session_ingress`：使用 Ed25519 签名的 `sk-ant-si-<JWT>`，通过 `CLAUDE_CODE_WEBSOCKET_AUTH_FILE_DESCRIPTOR` 供 CCR worker 与 upstream proxy relay 鉴权。
- `anthropic_oauth`：使用只保存 hash、由 CCR worker lease 决定生命周期的 `sk-ant-oat01-...` token，通过 `CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR` 访问本地 `/v1/messages` 模型代理。

payload 不再包含 `anthropic_api` 或 `CLAUDE_CODE_SESSION_ACCESS_TOKEN`。后者会优先于 WebSocket FD，被删除是为了保证 Claude 实际读取签名 ingress JWT。`cse_...` 只作为 URL 和 session 标识，不再作为 OTLP Bearer 凭证。

Runner 把 environment-manager 作为 E2B 后台进程启动。包含双凭证的 payload 通过进程 PID 直接写入 stdin，随后显式关闭 EOF；payload 不写入沙箱文件系统。stdin 发送或关闭失败时，Runner 终止尚未完整初始化的后台进程并按沙箱启动失败处理。

environment-manager 只在 remote CCRv2 proxy 开关开启时，原子写入 `/run/ccr/session_token`。目录权限为 `0700`，文件权限为 `0600`；Claude relay 启动成功后删除文件，executor 销毁时也执行兜底清理。

## HTTP 接口

Claude worker 与 upstream proxy 端点由长生命周期的 `codesessions.Handler.RegisterV1Routes` 注册到统一的 `/v1` chi 子路由并执行 code-session 鉴权；`/v1/messages` 由 `internal/messages` 注册在通用凭据感知中间件内。`internal/api` 只负责版本路由组装、依赖注入和鉴权入口选择。

`codesessions.Handler` 持有 WebSocket、MITM CA/leaf cache 与 OTLP 文件锁等协议状态；不参与 HTTP 的 `codesessions.Service` 只持有数据库与公开事件 sink。API server 创建一个 Service，并同时注入 code-session Handler 与 sessions Handler，保证 worker 输出仍能发布到公开 session stream。environment runner 也只依赖 Service，因此不会耦合 HTTP 或 MITM 生命周期。

### `POST /v1/messages`

这是普通 SDK、platform session 与 Claude runtime 共用的模型代理，经过统一的凭据感知中间件。

1. workspace API key、platform `sessionKey` cookie 按原鉴权链处理；lifecycle-bound code-session token 只在此 `POST` 路径被接受。
2. code-session token 按 hash 查询，且 code session 必须 active、public session 未 terminated、`worker_lease_expires_at > now()`；失败返回 `401 authentication_error`。
3. 请求体通过 `http.MaxBytesReader` 边计数边流式转发，超过 32 MiB 返回 `413`；不预读、不落盘，也不解析或校验 `model`。
4. 目标为 `{ANTHROPIC_UPSTREAM_BASE_URL}/v1/messages`。
5. 删除下游 `Authorization`、`X-Api-Key` 和所有 hop-by-hop headers。
6. 设置服务端 `ANTHROPIC_UPSTREAM_API_KEY` 为上游 `X-Api-Key`。
7. 原样转发上游状态、end-to-end headers 和响应流；提交状态后立即 flush，之后每次写入继续 flush，以支持 SSE。响应一旦提交，流错误只记录并终止连接，不再尝试改写 HTTP 状态。

### `GET /v1/code/upstreamproxy/ca-cert`

接口不鉴权，因为当前 Claude relay 下载 CA 时不会携带 token；CA 证书是公开材料，不包含私钥。

- MITM 关闭时，服务端忽略 `CODE_SESSION_UPSTREAM_PROXY_CA_KEY_FILE`，生成进程生命周期内稳定的临时 ECDSA P-256 CA，仅用于兼容 relay 初始化合同。
- MITM 开启并配置生产 CA 时，部署侧只提供长期稳定的 private key；`codesessions.Handler` 每次构造都使用该 key 重新自签一张一年期根证书，并仅保存在内存中。接口返回本次启动生成的 certificate。
- 根证书固定使用 `O=Open Managed Agents, CN=Open Managed Agents CCRv2 MITM CA`，SKI 从同一公钥稳定派生；不同启动的随机 serial number、有效期和 certificate 原始字节可以不同。
- 所有 API server 实例共享同一把只读 private key；各实例在内存中独立持有本次启动生成的 certificate。private key 不得进入数据库 API 响应、environment-manager stdin、sandbox 环境变量或 Claude 子进程。

启用配置：

```text
CODE_SESSION_UPSTREAM_PROXY_MITM_ENABLED=true
CODE_SESSION_UPSTREAM_PROXY_CA_KEY_FILE=/run/secrets/ccrv2-mitm-ca-key.pem
```

MITM 开启时，private key 必须已经存在并以只读 Secret 挂载。Handler 构造阶段立即解析 key、自签根证书，并把 certificate、PEM 和 signer 保存在进程内存；任一解析或签名错误都会在启动期拒绝启动。MITM 关闭时不会检查或读取该路径。私钥文件永远不会由 HTTP 接口返回，根证书也不会写入本地文件。

使用相同 key、完全相同的 `RawSubject` DER、SKI 和 CA/`CertSign` 约束后，已运行 Claude 只信任旧根证书时仍可以验证服务重启或其他实例新签发的 leaf，但只能持续到旧根证书自身过期。SKI 只是链候选匹配辅助，不能替代 issuer/subject、签名、有效期和 CA 约束校验。服务端重新签发不会延长已下载旧根的 `NotAfter`，因此 code session 生命周期必须远小于一年，API server 需要至少每年、并在当前根到期前计划重启。本方案只处理证书有效期更新，不处理 private key 泄露、更换 key 或双根过渡。

### `GET /v1/code/upstreamproxy/ws`

升级前要求：

- `Authorization: Bearer sk-ant-si-<JWT>` 或同等 `X-Api-Key`。
- JWT 必须通过 EdDSA、`kid`、issuer、audience 校验，并将签名 `session_id` 绑定到 CONNECT Basic username；当前不回查数据库 session 状态或 CCR worker lease。新 JWT 不设置独立墙钟 `exp`。
- `Content-Type: application/proto`。

WebSocket payload 使用 Claude relay 的单字段 protobuf wire format：

```protobuf
message UpstreamProxyChunk {
  bytes data = 1;
}
```

首个 chunk 必须是完整 CONNECT head：

```http
CONNECT example.com:443 HTTP/1.1
Proxy-Authorization: Basic base64(code_session_id:session_ingress_jwt)
```

服务端要求 Basic username 等于 JWT 的 `session_id`，并使用常量时间比较确认 Basic password 与 WebSocket Bearer 是完全相同的 ingress JWT。空 chunk 是 keepalive，最大 chunk 为 512 KiB。

- MITM 关闭：拨号成功后返回 framed `HTTP/1.1 200 Connection Established`，后续 chunk 作为原始 TCP bytes 双向转发。
- MITM 开启：先加载 CA、动态签发目标 leaf certificate，并完成真实上游 TLS 握手；全部成功后才返回 `200`。服务端随后把 framed WebSocket 适配为 `net.Conn`，作为 TLS server 解密客户端 HTTP，并使用独立 TLS client 验证真实目标证书。

## HTTPS MITM 边界

根 CA 生命周期、动态 leaf 签发、缓存并发和有效期续签限制的完整设计见 [《CCRv2 MITM 证书签发设计》](./mitm-certificate-issuance.md)。

MITM 模式当前只协商 HTTP/1.1：

1. 动态 leaf 使用 ECDSA P-256，包含精确 DNS/IP SAN，有效期最长 24 小时。
2. leaf 由本次启动生成的根证书身份和长期稳定 private key 签发，并按规范化主机名缓存最多 12 小时。缓存采用容量为 1024 的 LRU，满载时淘汰最久未使用的域名，避免任意域名造成无界内存增长，同时保证新域名仍可进入缓存。
3. 同一主机名的并发 cache miss 通过 `singleflight` 合并为一次签发；不同主机名可以并行生成 leaf，不使用覆盖证书生成过程的全局锁。
4. TLS ClientHello SNI、HTTP `Host` 与 CONNECT 目标必须一致；不同域名返回 TLS 错误或 `421 Misdirected Request`。
5. 真实上游连接使用系统根证书、TLS 1.2+、精确 `ServerName` 并只拨号 SSRF 校验后锁定的 IP，避免 DNS rebinding。
6. HTTP 使用 `httputil.ReverseProxy` 流式转发；删除 `Proxy-Authorization` 与 `Proxy-Connection`，不把 CCR 凭证传给目标网站；请求写完后最多等待 15 秒接收真实上游响应头，收到响应头后不限制 SSE 等流式响应体时长。
7. 当前不记录请求/响应 header 或 body。后续策略引擎可以在解密后的 handler 边界按 method/path/header 决策，但必须单独定义脱敏、审计和凭证注入规则。

不兼容边界：只接受 HTTP/2 的客户端、证书固定（certificate pinning）以及客户端证书认证（mTLS）目标可能失败。此类域名后续应增加显式 pass-through 策略，而不是降低上游证书校验强度。

## 网络安全边界

upstream proxy 是 code-session 级公开网络出口，不是任意 SSRF 转发器：

- 只允许 `CONNECT host:443`。
- 拒绝 loopback、RFC1918、link-local、CGNAT、benchmark、documentation、multicast 和保留地址。
- 域名先解析，服务端只拨号已验证的 public IP，避免校验后再次解析造成 DNS rebinding。
- 不记录 bearer token、Basic header、上游 API key 或隧道内容。
- MITM 默认关闭；开启后只解密通过 code-session 双重鉴权且通过 SSRF 校验的 CONNECT 流量。
- 即使开启 MITM，也不会信任动态 CA 作为真实上游根证书；服务端到目标网站始终使用系统信任链。

本地 fake-IP/TUN DNS 可能把公网域名解析到 `198.18.0.0/15`，从而触发上述保护。仅用于临时排障时，可以设置 `CODE_SESSION_UPSTREAM_PROXY_DISABLE_SSRF_PROTECTION=true` 关闭目标 IP 过滤；默认值为 `false`。该开关仍然只允许端口 `443`，但会允许 loopback、私网、link-local 与 fake-IP，因此不得在生产环境启用。

## 失败语义

| 场景 | 结果 |
| --- | --- |
| 模型/WS 缺少或伪造 code-session token | HTTP `401` |
| WS Content-Type 错误 | HTTP `415` |
| CONNECT/protobuf 格式错误 | framed `400` |
| Basic session/token 不匹配 | framed `407` |
| 非 443 或非公网目标 | framed `403` |
| DNS、拨号失败 | framed `502` |
| CA key 不合法、路径冲突或 certificate 无法写入 | 配置加载或 Handler 构造阶段拒绝启动 |
| 真实上游 TLS 验证失败 | framed `502` |
| TLS SNI 与 CONNECT 目标不一致 | TLS 握手失败 |
| 解密后 HTTP Host 与 CONNECT 目标不一致 | HTTP `421` |
| CA 生成失败 | HTTP `500`，Claude relay fail-open 并保持禁用 |

## 验收

- environment-manager 单元测试覆盖开关判断、token 原子替换、权限和空值拒绝。
- API 单元测试覆盖 protobuf、CONNECT、Basic 双字段鉴权、私网/端口拒绝、CA 解析和二进制隧道往返。
- MITM 单元测试覆盖稳定 private key 驱动的启动期根证书签发、旧根验证新 leaf、动态 leaf 信任链、LRU 淘汰、同域并发签发合并、异域并行签发、客户端 TLS 解密、path/query 转发和代理凭证剥离。
- API 集成测试覆盖无 token、私网 CONNECT、公开 CA，以及 `/v1/messages` 的多凭证鉴权、上游密钥替换和流式转发。
- linux/amd64 镜像验收通过真实 Claude CLI 和 Bash tool call 确认：relay 从 `/run/ccr/session_token` 读取 token 并启动，Claude 子进程同时具有指向同一 `127.0.0.1` relay 的 `HTTPS_PROXY`、`https_proxy`，以及 `NODE_EXTRA_CA_CERTS`、`SSL_CERT_FILE`。
