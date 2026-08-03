# Console Workspace API Key 创建

控制台的 Workspace Settings 页面可以手动创建 API key。创建后的 key 与 magic-link 登录时自动
provisioning 的默认 key 走同一条认证路径，区别只在于来源是用户主动操作。本文记录从 HTTP 入口
到数据库写入的完整链路，重点说明密钥的安全存储设计。

## HTTP 契约

```
POST /api/console/organizations/{orgUuid}/workspaces/{workspaceId}/api_keys
```

路由注册在 `internal/api/server.go` 的 `registerPlatformConsoleRoutes` 中，挂在
`platformAuthMiddleware` 保护的 route group 下。具体的子路由在
`internal/platformapi/console_api_keys.go` 的 `registerConsoleOrganizationAPIKeyRoutes`
里声明。

请求体：

```json
{
  "name": "my-key",
  "expires_at": "2026-12-31T00:00:00Z"
}
```

`expires_at` 可以为 `null` 或省略，表示永不过期。格式必须是 RFC 3339。

成功响应（200）：

```json
{
  "type": "api_key",
  "id": "apikey_...",
  "workspace_id": "wrkspc_...",
  "name": "my-key",
  "key_prefix": "sk-ant-api03-...",
  "key_suffix": "...abc123",
  "partial_key_hint": "sk-ant-api03-......abc123",
  "created_by": { "id": "user-uuid", "type": "user" },
  "status": "active",
  "expires_at": "2026-12-31T00:00:00Z",
  "created_at": "2026-08-01T...",
  "updated_at": "2026-08-01T...",
  "raw_key": "sk-ant-api03-<完整密钥>"
}
```

`raw_key` 只在这一次响应中出现。数据库不保存明文，之后无法再次获取。

## 认证与权限

`platformAuthMiddleware` 从 `sessionKey` cookie 解析平台会话，构造 `auth.Principal` 注入
`request.Context`。会话解析逻辑在 `internal/api/server.go` 的 `authenticatePlatformSession`
中，详见 [auth-credential-routing.md](./auth-credential-routing.md)。

Handler 内部依次做两层校验：

1. **组织归属** — `visibleOrgUUID` 从 URL 取 `orgUuid`，与 principal 持有的
   `OrganizationUUID` 比对。不匹配返回 404（而非 403，避免泄露组织存在性）。

2. **Workspace 解析** — `consoleWorkspaceScopeFromRequest` 从 URL 取 `workspaceId`
   （可以是 external_id 如 `wrkspc_...`，也可以是 UUID，或字面量 `default`），查库列出该
   组织下所有未归档 workspace，再通过 `ResolveWorkspaceScope` 匹配出 `WorkspaceScope`。
   匹配失败返回 404。

通过校验后，handler 直接读取 `auth.Principal.UserUUID` 作为 `createdByUserUUID`，连同 org UUID、
workspace UUID、name 和 expires_at 一起传入 DB 层。这里不接受前端提交的创建者，也不使用
`UserExternalID` 或 `user_...` 派生值。HTTP/resource 边界负责清理和校验这些 UUID 字符串，DB 层
按调用契约直接绑定到 PostgreSQL 的 `uuid` 列，不再重复调用 `TrimSpace`、`parseDBUUID`，也不添加
`CAST(... AS uuid)`；PostgreSQL 会从列比较或写入位置推断参数类型。`CreatorExists` 按
`organization_uuid + user UUID + deleted_at IS NULL` 验证创建者仍属于当前组织。

## 密钥生成与存储

DB 层入口是 `internal/db/console_api_keys.go` 的 `CreateConsoleAPIKey`。

### 明文密钥只存在于内存

调用时生成一个随机明文密钥：

```
rawKey = "sk-ant-api03-" + base64url(crypto/rand 32 bytes)
```

这个值在方法结束时随返回值交给 handler，handler 写进 HTTP 响应的 `raw_key` 字段，然后就被
丢弃了。数据库的任何一张表都不保存它。

### key_hash 是认证的依据

对明文做一次 SHA-256，把 hex 编码的结果存进数据库：

```go
// internal/auth/auth.go
func HashAPIKey(key string) string {
    sum := sha256.Sum256([]byte(key))
    return hex.EncodeToString(sum[:])
}
```

后续 `/v1/*` 请求带着明文 key 来时，认证链（`authenticateWorkspaceAPIKey`）对请求中的 key
做同样的 SHA-256，拿 hash 去 `api_keys` 表查 `WHERE key_hash = $1`。命中就通过，查不到就
拒绝。整个过程等价于密码比对——存储侧只有 hash，没有明文。

SHA-256 是单向函数，即便数据库整库泄露，攻击者也拿不到可用的 key。两张表的 `key_hash` 列
都有 `UNIQUE` 约束，保证同一个明文 key 不会重复插入。

### 为什么写两张表

创建时在一个 yourbatis 事务里同时写两张表：

| 表 | 角色 | 关键列 |
|----|------|--------|
| `console_api_keys` | 控制台展示层，存储 prefix/suffix/display 信息 | `external_id`, `key_prefix`, `key_suffix`, `workspace_display_id`, `name` |
| `api_keys` | 核心认证表，被 `/v1/*` 鉴权链查询 | `uuid`, `key_hash`, `workspace_uuid`, `status` |

`api_keys` 是 `/v1/*` 鉴权链唯一会查的表。`console_api_keys` 不参与运行时认证，只服务于
控制台的列表、计数和状态管理。两张表通过共享 `key_hash`（而非外键，遵循项目的 no-FK 约定）
和 `external_id` 关联。事务保证两者要么同时写入成功，要么一起回滚，不会出现控制台显示了
key 但实际无法用于认证的中间状态。

### prefix/suffix/hint 的用途

这三者都是为了在**不暴露完整密钥**的前提下让用户能在控制台识别自己的 key：

- `key_prefix` — 前 16 个字符，如 `sk-ant-api03-Ab`
- `key_suffix` — 后 6 个字符，如 `cDeFgH`
- `partial_key_hint` — 存在 `api_keys` 表，格式 `前8...后4`

单独泄露 prefix 或 suffix 都无法还原完整 key。用户在控制台看到的是
`partial_key_hint`（如 `sk-ant-a...3456`），据此辨认是哪个 key。

```
明文:  sk-ant-api03-XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXcDeFgH
         ├── key_prefix (16) ──┘                                ├── key_suffix (6) ──┘
         └── partial_key_hint: 前8...后4 ──────────────────────┘
```

## 创建后如何被使用

key 创建后，SDK/CLI 调用 `/v1/*` 时在请求头携带 `X-Api-Key: sk-ant-api03-...` 或
`Authorization: Bearer sk-ant-api03-...`。认证流程：

```
请求 X-Api-Key: sk-ant-api03-xxx
        │
        ▼
auth.HashAPIKey(apiKey)  →  SHA-256 hex
        │
        ▼
db.GetAPIKey(keyHash)  →  SELECT ... FROM api_keys WHERE key_hash = $1
        │
        ├─ 命中 → auth.Principal { APIKeyUUID, OrganizationUUID, WorkspaceUUID, ... }
        └─ 未命中 → 尝试 environment key / code-session token / 拒绝
```

这一步查的是 `api_keys` 表，不碰 `console_api_keys`。控制台的状态更新（archived/inactive）
会同步写入两张表，保持一致。

## 实现文件

| 文件 | 职责 |
|------|------|
| `internal/platformapi/console_api_keys.go` | 路由注册、请求解析与校验、响应格式化 |
| `internal/db/console_api_keys.go` | 密钥生成、hash 计算、双表事务写入、列表/更新/计数 |
| `internal/auth/auth.go` | `HashAPIKey`（SHA-256）、`ExtractAPIKey`（从请求头提取） |
| `internal/api/service_auth.go` | `authenticateWorkspaceAPIKey`（运行时认证，查 `api_keys`） |
| `internal/api/server.go` | 路由挂载、`platformAuthMiddleware`、`authenticatePlatformSession` |
