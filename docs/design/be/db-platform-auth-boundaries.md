# DB 与 Platform Auth 边界

本文记录 DB 包和平台登录 provisioning 的职责边界。

## 包职责

- `internal/db`
  - 保留数据库连接、迁移、seed、事务开启、SQL 查询和写入原语。
  - `internal/db/files.go` 暴露 file record、file CRUD、workspace storage 统计和 object cleanup job 数据访问；具体 SQL 位于 `files_sqlx.go`，统一使用命名参数、结构体映射和 `sqlx.Tx`。
  - `internal/db/platform_auth.go` 只暴露 platform auth 所需的事务内数据访问原语，例如按邮箱查询用户上下文、插入 organization/user/workspace/workspace_member/api_key，以及 session identity 查询。
- `internal/platformauth`
  - 承载 magic-link 登录的领域编排。
  - 负责默认 email 归一化、默认用户名/组织名、外部 ID 生成、默认 workspace/member/API key 创建流程、API key raw token/hash/hint 生成。
  - 通过 `db.WithPlatformAuthTx` 保持默认 organization、user、workspace、workspace member 和 API key 的事务一致性。
- `internal/platformapi`
  - 只负责 HTTP 请求解析、cookie/session 响应和路由注册。
  - magic-link verify 时从 `platformauth.Service` 获取用户上下文和 session identity，从 bootstrap store 读取 account 响应数据。

## 依赖方向

- `internal/platformauth` 可以依赖 `internal/db` 的数据访问接口和事务原语。
- `internal/db` 不依赖 `internal/platformauth`、`internal/platformapi` 或 HTTP handler 包。
- `internal/api` 负责组装 `platformauth.Service` 并传入 `platformapi.RegisterPlatformEmailLoginRoutes`。

## 兼容与测试

本次拆分保持现有 magic-link 登录行为不变：

- 空 email 仍归一化为 `test@qq.com`。
- 默认 user/org 名称仍来自 email local part，且沿用 lower-case 行为。
- 找不到用户时仍创建默认 organization、admin user、default workspace、workspace_admin membership 和 active default API key。
- 创建链路仍在单个 DB transaction 内完成。

验证重点：

- `go test ./internal/db ./internal/platformauth ./internal/platformapi -count=1`
- `go test ./internal/api -count=1`
- `go test ./... -count=1`

若 `internal/api` 或全量测试失败，需要先排除既有 platform-host 分流问题，避免把行为修复混入边界拆分。
