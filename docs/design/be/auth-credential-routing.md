# `/v1/*` 认证路由：基于凭证而非 Host 头

> 目标：让 `/v1/*` 入口路由根据客户端实际携带的凭证类型（API key / session cookie）做分发，而不是依赖 Host 头猜测调用方身份，从而让反向代理和任意端口部署都能正确工作。

---

## 1. 问题

### 1.1 原有路由逻辑

`apiEntrypointRouter.ServeHTTP` 在 `internal/api/server.go` 中决定 `/v1/*` 请求走 service 路由还是 platform 路由：

```go
// 原有实现
func (r apiEntrypointRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
    if isPlatformHost(req.Host) && auth.ExtractAPIKey(req) == "" {
        r.platform.ServeHTTP(w, req)  // session cookie 鉴权
        return
    }
    r.service.ServeHTTP(w, req)        // x-api-key 鉴权
}
```

`isPlatformHost` 只识别以下 host：

- `localhost:5173` / `127.0.0.1:5173` / `[::1]:5173` — Vite 前端开发服务器
- `oma.duck.ai` — 生产域名

### 1.2 触发场景

当通过以下方式访问时，Host 头不在白名单内，`/v1/*` 请求被错误路由到 service 路径（要求 `x-api-key`），返回 401：

| 访问方式 | Host 头 | 路由结果 | 预期 |
|----------|---------|----------|------|
| `http://localhost` (Caddy :80) | `localhost` | → service (401) | platform |
| `http://localhost:38080` (直连) | `localhost:38080` | → service (401) | platform |
| 任意反向代理后 | 代理域名 | → service (401) | platform |

这个问题在 docker-compose 部署中尤其突出：Caddy 监听 `:80`，前端通过 `http://localhost` 访问，所有 `/v1/*` 请求都带 session cookie 但被路由到 service auth middleware，直接返回 401。

---

## 2. 方案

### 2.1 核心思路

**不看 Host，看凭证。** `/v1` 资源只注册一次，请求携带什么凭证，就使用对应的鉴权链：

```go
func (s *Server) v1AuthMiddleware(next http.Handler) http.Handler {
    service := s.serviceAuthMiddleware(next)
    platform := s.platformAuthMiddleware(next)
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if auth.ExtractAPIKey(r) != "" {
            service.ServeHTTP(w, r)
            return
        }
        platform.ServeHTTP(w, r)
    })
}
```

`registerVersionedAPIRoutes` 在一个 `/v1` chi 子路由中完成组装：`codesessions.Handler` 注册 runtime 路由并执行各自的鉴权策略，privacy consent 保持开放，其余资源统一放入 `v1AuthMiddleware` 路由组。实现不再创建结构相同的 service/platform 两套路由。子路由的 `NotFound` 与 `MethodNotAllowed` fallback 也通过 `v1AuthMiddleware`，保证未知路径和已知路径上的错误 method 不会绕过 API 级鉴权边界。

### 2.2 凭证提取

三个核心函数均在 `internal/auth/auth.go` 中：

```go
// ExtractAPIKey — 从 X-Api-Key header 或 Authorization: Bearer <token> 提取
func ExtractAPIKey(r *http.Request) string

// ExtractBearerToken — 只从 Authorization: Bearer <token> 提取
func ExtractBearerToken(r *http.Request) string

// ExtractPlatformSessionKey — 从 sessionKey cookie 提取
func ExtractPlatformSessionKey(r *http.Request) string
```

`ExtractAPIKey` 只负责通用 service 入口分流。service auth 首先按 workspace API key 校验；普通 API key 未命中时，只有 `POST /v1/messages` 可以继续校验 `sk-ant-oat01-...` OAuth-compatible token。该 token 不能访问其他 `/v1/*` 资源，具体见 [messages-proxy.md](./messages-proxy.md)。Filestore 不进入这条 fallback。

worker、session ingress、OTLP 与 upstream proxy 不走通用 OAuth-compatible fallback。它们统一校验 `sk-ant-si-<JWT>` 的固定 EdDSA 算法、`kid`、签名、issuer、audience，以及 JWT `session_id` 与请求路径的绑定。新 JWT 不设置独立 `exp`，也不携带 repository/resource sources。大多数 ingress handler 把 claims 作为签名身份快照，并由对应 handler 执行 worker epoch、heartbeat grace 等状态约束；OTLP 还要求当前 worker epoch 与未过期 lease。这条 code-session ingress 合同未因 Filestore 而改动。

Filestore 是显式例外：`/v1/filestore` 通过独立中间件验证 `Authorization: Bearer` 中的原始 compact JWT，只接受 Filestore 专用凭证；`X-Api-Key`、workspace API key、`sk-ant-oat01-` OAuth-compatible token 和 `sk-ant-si-` session-ingress JWT 都不会进入 Filestore。Filestore JWT 使用独立 claims 与验证器，并回查活跃的 organization、account、workspace 和单个 filesystem；`org_taints` 与 `workspace_cmek_enabled` 还必须与当前策略一致。鉴权结果被映射为资源专用的 `filestore.Principal`，通过独立 context key 传入 handler；全局 `auth.Principal` 不承载 Filestore 专属字段。Filestore 鉴权失败使用其 wire contract 要求的扁平 `{code,message}`，不使用 Anthropic error envelope；鉴权函数是 status、code 和 message 的唯一所有者，中间件只把该专用错误原样写入协议响应，不再按 HTTP status 二次生成错误语义。通过鉴权后的未知操作或错误方法也由 Filestore handler 输出同一错误外观。

### 2.3 路由决策表

| API Key | Session Cookie | 鉴权链 | 原因 |
|---------|---------------|------|------|
| ✓ | — | service | SDK/CLI 调用，token 鉴权 |
| ✓ | ✓ | **service** | API key 优先，明确的服务调用意图 |
| — | ✓ | platform | 浏览器控制台，session 鉴权 |
| — | — | platform | 默认走 platform，保留 `/v1/privacy-consents` 等无需鉴权的开放路由 |

### 2.4 向后兼容分析

| 场景 | 修复前 | 修复后 | 变化 |
|------|--------|--------|------|
| `curl -H 'x-api-key: ...' localhost:38080/v1/models` | service | service | 无 |
| 浏览器 `localhost:5173` 带 session cookie | platform | platform | 无 |
| 浏览器 `localhost:38080` 带 session cookie | service (401) | **platform** | ✅ 修复 |
| 浏览器 `localhost` (Caddy) 带 session cookie | service (401) | **platform** | ✅ 修复 |
| 无凭证请求 | host 猜测 | platform | 无开放路由影响 |

唯一的语义变化是：**session cookie 现在在任意端口/域名上都生效**，这正是本次修复的目标。

### 2.5 为什么 API key + session cookie 同时存在时选 service

当两个凭证都存在时（例如开发者用 curl 带 API key 调试，但浏览器也留下了 cookie），API key 是更强的调用意图信号 — 客户端明确选择了 service 调用方式。选择 service 鉴权链也符合最小惊讶原则。

---

## 3. 同步清理

### 3.1 认证中间件中的 `isPlatformHost` 残留

入口鉴权改为凭证驱动前，认证中间件内部仍按 `isPlatformHost(r.Host)` 判断是否清除无效 session cookie。清理这些 host 分支后，无效 session 在任意入口都返回 `401` 并清理 cookie；原有 mirror recovery 也一并删除，避免把未知 session cookie 映射到组织的 bootstrap 用户。

处理了4处相关鉴权逻辑：

| 函数 | 变更 |
|------|------|
| `platformAuthMiddleware` | 移除 `isPlatformHost`，只要有 `sessionKey` cookie 就清理 |
| `authenticated` | 同上 |
| `recoverPlatformMirrorSession` | 删除；session store 中不存在或已过期的 cookie 必须重新登录，不再按组织 bootstrap 用户重建 session |
| `platformMirrorOrganizationAlias` | 移除 `!isPlatformHost(r.Host)` 前置条件 |

### 3.2 死代码删除

`isPlatformHost` 及其依赖函数在 `/v1/*` 路由和中间件中均不再使用，全部删除：

```go
// 删除的函数
func isPlatformHost(host string) bool
func isExternalPlatformHost(host string) bool
func isLocalFrontendPlatformHost(host string) bool
func normalizedRequestHost(host string) string
func normalizedRequestHostParts(host string) (string, string)
func platformSessionRecoveryOrgID(r *http.Request) string
func isPlatformAPIRequestPath(path string) bool
```

同时移除 `net` 包导入（`normalizedRequestHostParts` 中 `net.SplitHostPort` 的唯一使用者）。

### 3.3 `sessionKey` cookie 安全加固

此前 `sessionKey` cookie 没有 `HttpOnly` 和 `SameSite` 属性。改为凭证驱动路由后，任何 `Host` 都可能携带 session cookie 访问 `/v1/*`，CSRF 与 XSS 窃取面扩大。

在 `internal/platformapi/platform_auth_routes.go` 的 `setSessionCookies` 中：

```go
// 修复后
http.SetCookie(w, &http.Cookie{
    Name:     "sessionKey",
    Value:    sessionKey,
    Path:     "/",
    MaxAge:   maxAge,
    HttpOnly: true,
    Secure:   false,          // 本地部署无 HTTPS
    SameSite: http.SameSiteLaxMode,
})
```

`lastActiveOrg` cookie 保持 `HttpOnly: false`（前端需要读取组织上下文）。

---

## 4. 不影响的范围

1. **`/v1/*` 以外的路由** — 不受影响。
2. **workspace API key 逻辑** — 原验证、权限和 scope 不变；`POST /v1/messages` 额外接受受路径、active session 与 CCR worker lease 约束的 OAuth-compatible token。`/v1/filestore` 不接受上述凭证，只接受绑定单个 filesystem 的 Filestore JWT；Code Session Ingress 与 `/v1/messages` 的既有鉴权不受影响。
3. **platform session 持久化结构** — 不变。session cookie 仍保存登录时解析出的默认 workspace 身份，不写回请求级 workspace。请求携带 `X-Workspace-ID` 或 `workspace_id` 时，鉴权层把客户端值解析为本次请求的 workspace scope：组织管理员可以访问组织内 workspace，普通用户必须具有有效的 `workspace_members` 记录；授权通过后再使用现有 API-key 查询选择目标 workspace 的 active、未过期 key，并只更新本次请求的 principal。目标 workspace 不存在、已归档、用户无权访问或没有可用 key 时返回 `403`。该流程不新增表、列或 Mapper，也不使用资源的 `created_by_user_uuid` 判断权限。

---

## 5. 测试

### 5.1 单元测试

`internal/api/auth_test.go` — `TestV1AuthenticationSelection`（12个用例）和 `TestV1FallbacksRequireAuthentication`：

覆盖：

- API key 在任何 host 上都进 service
- Bearer token 在任何 host 上都进 service
- session cookie 在 `localhost:5173`、`localhost:38080`、`oma.duck.ai`、`api.anthropic.com` 上都进 platform
- API key + session cookie 同时存在 → API key 胜出，进 service
- 无凭证时默认进 platform（保留开放路由）
- 未知 `/v1` 路径和已知路径上的错误 method 都先鉴权，未携带凭证时返回 `401`

### 5.2 集成测试

`tests/files_api_test.go` — `TestV1AuthModes`（8个用例），已更新以匹配新的凭证路由语义：

- `success api key works on any host` — API key 在 platform host 上也返回 200（旧语义下预期 401）
- `success session cookie works on any host` — session cookie 在 API host 上也返回 200（旧语义下预期 401）

`tests/platform_email_login_api_test.go` — 验证受保护路由不会恢复 session store 中不存在的 cookie，并由 `TestPlatformWorkspaceHeaderScopesV1Resources` 验证普通用户在没有 workspace membership 时被拒绝；获得 membership 后，agent、environment 和 session 请求均落在目标 workspace，创建出的 session 记录使用目标 workspace 的 API key。

`tests/console_invites_api_test.go` — mirrored official organization alias 仍可配合有效 session 使用，不再依赖删除 session 后的隐式恢复。

---

## 6. 与 docker-compose 部署的关系

本次修复是 docker-compose 一键部署的前置条件。Caddy 反向代理在 `:80` 提供服务，Host 头为 `localhost`（不带端口），原路由逻辑会将其误判为 service 调用。修复后，前端控制台通过 Caddy 访问时，session cookie 被正确识别，platform 路由生效。

参见：`docs/design/docker-compose-deployment.md` 第 5 节。

---

## 7. 实现文件

| 文件 | 变更 |
|------|------|
| `internal/api/server.go` | `/v1` 资源统一注册到 `registerVersionedAPIRoutes`；持有 `codesessions.Handler`，并把同一个底层 `codesessions.Service` 注入 sessions handler；`v1AuthMiddleware` 按凭证选择鉴权链并保护 NotFound/MethodNotAllowed fallback；platform 请求从客户端 workspace ID 解析并授权 workspace scope，同时选择目标 workspace 的可用 API key；移除双 router 入口分流；移除中间件中4处 `isPlatformHost` 检查；删除 Host 判断相关死函数 |
| `internal/api/service_auth.go` | 对 `/v1/filestore` 资源命名空间启用独立 Filestore JWT，并把 claims 绑定到 organization/account/workspace/filesystem 数据库范围 |
| `internal/api/filestore_auth_test.go` | 覆盖 Filestore 路径边界、扁平鉴权错误、Bearer-only 入口、JWT/DB identity 绑定和跨凭证/跨资源拒绝 |
| `internal/api/auth_test.go` | 测试用例从 host 驱动改为凭证驱动，覆盖 API key、session cookie、双凭证、无凭证场景及两个 `/v1` 鉴权 fallback |
| `tests/files_api_test.go` | 更新2个集成测试用例：api key 在任意 host 返回 200，session cookie 在任意 host 返回 200 |
| `tests/platform_email_login_api_test.go` | 覆盖 platform session 的请求级 workspace/API-key 同步切换，并验证自定义 workspace 可以创建 session |
| `tests/console_invites_api_test.go` | 保留有效 session 下的 organization alias 兼容测试，移除 session 自动恢复预期 |
| `internal/platformapi/platform_auth_routes.go` | `sessionKey` cookie 添加 `HttpOnly: true` 和 `SameSite: Lax` |
| `docs/design/be/auth-credential-routing.md` | 本设计文档 |
