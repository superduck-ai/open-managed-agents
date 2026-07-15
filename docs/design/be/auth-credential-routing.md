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

`registerVersionedAPIRoutes` 在一个 `/v1` chi 子路由中完成组装：`codesessions.Handler` 注册 runtime 路由并执行各自的鉴权策略，privacy consent 保持开放，其余资源统一放入 `v1AuthMiddleware` 路由组。实现不再创建结构相同的 service/platform 两套路由。

### 2.2 凭证提取

两个核心函数均在 `internal/auth/auth.go` 中：

```go
// ExtractAPIKey — 从 X-Api-Key header 或 Authorization: Bearer <token> 提取
func ExtractAPIKey(r *http.Request) string

// ExtractPlatformSessionKey — 从 sessionKey cookie 提取
func ExtractPlatformSessionKey(r *http.Request) string
```

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

入口鉴权已改为凭证驱动，但认证中间件内部仍按 `isPlatformHost(r.Host)` 判断是否清除无效 session cookie 或恢复 mirror session。这导致非 platform host 上的无效 session 不会被清理，mirror session 也无法恢复。

清理了4处残留检查：

| 函数 | 变更 |
|------|------|
| `platformAuthMiddleware` | 移除 `isPlatformHost`，只要有 `sessionKey` cookie 就清理 |
| `authenticated` | 同上 |
| `recoverPlatformMirrorSession` | 移除 `!isPlatformHost(r.Host)` 前置条件 |
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
2. **service auth middleware 逻辑** — 不变。API key 验证、权限、scope 均无变化。
3. **platform session 解析逻辑** — 不变。session 验证、组织上下文注入均无变化。

---

## 5. 测试

### 5.1 单元测试

`internal/api/auth_test.go` — `TestV1AuthenticationSelection`（12个用例）：

覆盖：

- API key 在任何 host 上都进 service
- Bearer token 在任何 host 上都进 service
- session cookie 在 `localhost:5173`、`localhost:38080`、`oma.duck.ai`、`api.anthropic.com` 上都进 platform
- API key + session cookie 同时存在 → API key 胜出，进 service
- 无凭证时默认进 platform（保留开放路由）

### 5.2 集成测试

`tests/files_api_test.go` — `TestV1AuthModes`（8个用例），已更新以匹配新的凭证路由语义：

- `success api key works on any host` — API key 在 platform host 上也返回 200（旧语义下预期 401）
- `success session cookie works on any host` — session cookie 在 API host 上也返回 200（旧语义下预期 401）

---

## 6. 与 docker-compose 部署的关系

本次修复是 docker-compose 一键部署的前置条件。Caddy 反向代理在 `:80` 提供服务，Host 头为 `localhost`（不带端口），原路由逻辑会将其误判为 service 调用。修复后，前端控制台通过 Caddy 访问时，session cookie 被正确识别，platform 路由生效。

参见：`docs/design/docker-compose-deployment.md` 第 5 节。

---

## 7. 实现文件

| 文件 | 变更 |
|------|------|
| `internal/api/server.go` | `/v1` 资源统一注册到 `registerVersionedAPIRoutes`；持有 `codesessions.Handler`，并把同一个底层 `codesessions.Service` 注入 sessions handler；`v1AuthMiddleware` 按凭证选择鉴权链；移除双 router 入口分流；移除中间件中4处 `isPlatformHost` 检查；删除 Host 判断相关死函数 |
| `internal/api/auth_test.go` | 测试用例从 host 驱动改为凭证驱动，12个用例覆盖 API key、session cookie、双凭证、无凭证场景 |
| `tests/files_api_test.go` | 更新2个集成测试用例：api key 在任意 host 返回 200，session cookie 在任意 host 返回 200 |
| `internal/platformapi/platform_auth_routes.go` | `sessionKey` cookie 添加 `HttpOnly: true` 和 `SameSite: Lax` |
| `docs/design/be/auth-credential-routing.md` | 本设计文档 |
