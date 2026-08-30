# DB 与 Platform Auth 边界

本文记录 DB 包和平台登录 provisioning 的职责边界。

## 包职责

- `internal/db`
  - 保留数据库连接、迁移、seed、事务开启、SQL 查询和写入原语。
  - `internal/db/files.go` 暴露 file record、file CRUD、workspace storage 统计和 object cleanup job 数据访问；具体 SQL 按表拆分到 Mapper XML，统一使用生成参数绑定、静态行映射和 Yourbatis 事务。
  - `internal/db/platform_auth.go` 只暴露 platform auth 所需的事务内数据访问原语，例如按邮箱查询用户上下文、插入 organization/user/workspace/workspace_member/api_key，以及 session identity 查询。
- `internal/platformauth`
  - 当前只实现邮箱验证码登录，并通过 `auth.smtp` 配置 SMTP 发信。
  - 负责 email 归一化和校验、验证码签发/验证、默认用户名/组织名、外部 ID 生成、默认 workspace/member/API key 创建流程、API key raw token/hash/hint 生成。
  - `LoginCodeSender` 保留为测试 seam，使验证码流程可以在测试中避开真实发信。
  - Redis challenge 只保存 HMAC 摘要，不保存邮箱或验证码明文；HMAC key 由 SMTP 密码做域隔离派生，不需要额外配置 secret。验证码固定 10 分钟有效，邮件使用相对有效期文案；同一邮箱每小时最多发送 5 次、1 分钟重发冷却、最多错误 5 次，并在验证成功后原子删除。
  - 当前不按客户端 IP 限流，避免默认反向代理把所有用户折叠到同一额度；只有建立可信代理边界后才可重新引入客户端 IP 限流。
  - 通过 `db.WithPlatformAuthTx` 保持默认 organization、user、workspace、workspace member 和 API key 的事务一致性。
- `internal/platformapi`
  - 只负责 HTTP 请求解析、cookie/session 响应和路由注册。
  - `/send_magic_link` 请求真实发信；`/verify_magic_link` 只有在验证码验证成功后才从邮箱认证服务获取用户上下文，再直接从 store 读取 account 和 session identity。

## 依赖方向

- `internal/platformauth` 可以依赖 `internal/db` 的数据访问接口和事务原语。
- `internal/db` 不依赖 `internal/platformauth`、`internal/platformapi` 或 HTTP handler 包。
- `main` 只把 `auth` 配置和共享依赖交给 `platformauth.New`；SMTP、Redis challenge 和 HMAC 组装全部留在邮箱认证模块内部。

## 兼容与测试

邮箱验证码登录保持原有路由和成功响应形状，但不再接受任意验证码：

- email 必须是有效地址，空 email 不再回退为测试账号。
- 验证码为 6 位随机数字、默认 10 分钟有效、只能成功使用一次；SMTP 失败时撤销本次 challenge。
- 发码限流返回 `429`，但不提供无法区分一分钟冷却和小时额度的 `Retry-After`。
- 默认 user/org 名称仍来自 email local part，且沿用 lower-case 行为。
- 找不到用户时仍创建默认 organization、admin user、default workspace、workspace_admin membership 和 active default API key。
- 创建链路仍在单个 DB transaction 内完成。
- challenge 全部保存在 Redis，本次变更不需要数据库 migration。

验证重点：

- `go test ./internal/db ./internal/platformauth ./internal/platformapi -count=1`
- `go test ./internal/api -count=1`
- `go test ./... -count=1`
