# Memory Store Attach 合同

- 状态：已实现（Session / Deployment attach + 控制台表单）
- 日期：2026-09-07
- 官方 HTTP 合同：[Using agent memory](https://platform.claude.com/docs/en/managed-agents/memory)
- 后续：Filestore `/memory/{slug}` 写回（#330）；Sandbox 挂载与 `MEMORY.md`（#331）

CRUD、三张表、S3 正文、控制台列表/详情已落地。本文只覆盖 **attach 合同**：把 Memory Store 挂到 Session / Deployment，校验 slug / access / instructions，并写入挂载快照。运行时挂载与写回不在本层。

控制台表单见 [memory-store-attach-form.md](../fe/memory-store-attach-form.md)。

---

## 1. Attach 合同

- 最多 8 个 store，不得重复。Create Session 请求体与 `POST /resources` 都强制这两条；`CreateSessionResource` 在锁定 Session 行后的同一事务里再次检查，并发附加不会写出重复 `memory_store_id` 或第 9 个 store。冲突返回 `invalid_request_error`。
- `access` 缺省 `read_write`；仅 `read_write` / `read_only`。这是本次挂载权限，不是 store 属性。
- `instructions` ≤ 500 个 Unicode 码点，允许空。Session 与 Deployment 共用该上限。超限返回 `400`，不截断。
- 请求携带 `mount_path` / `name` / `description` → 400。
- 不存在 → 404；已归档 → 400。
- slug = name 小写，非字母数字折叠为 `-`，去首尾；空则回退 external id；同 Session 冲突追加 `-2`。
- 错误集中 `internal/sessions/errors.go`。
- 资源顺序不影响落盘。
- Deployment 创建/运行时按同一合同解析 memory store，并把快照写入即将创建的 Session。
- 运行准备阶段收集并去重 memory store ID，按 workspace 使用一次 `IN` 查询加载；无 ID 时不查询。查询排除已删除记录但保留已归档记录，再按资源原顺序检查缺失和归档，以保持错误分类及优先顺序。

## 2. 响应快照

服务端写入并回显：

- `memory_store_id`
- `access`
- `instructions`（可空）
- `name` / `description`（来自 store 当时的值）
- `mount_path`（`/mnt/memory/{slug}`，本层只快照，不挂盘）

客户端不得回写 `mount_path` / `name` / `description`。

## 3. 验收

1. 响应含服务端 `mount_path`；请求携带 `mount_path` / `name` / `description` → 400。
2. 控制台能选 store、Access、Instructions；提交体不含 `mount_path` / `name` / `description`。
3. `instructions` 501 个码点 → 400；500 个码点通过。
4. 同一 Session 重复 `memory_store_id` 或第 9 个 store → 400；并发 `POST /resources` 不会写出重复或超限行。
5. Deployment 批量加载保持缺失/归档错误的原资源顺序；`internal/deployments/memory_store_batch_test.go` 覆盖错误优先级，`internal/db/memory_store_batch_test.go` 覆盖 SQL 绑定及单次查询，`TestMemoryStoreBatchPostgres` 使用 `TEST_MIGRATION_DATABASE_URL` 在独立 schema 验证租户隔离、删除过滤及 JSON/nullable 扫描。

测试先失败再成功。sessions / deployments / 控制台覆盖上表。

合入时已同步：[deployments-api-contract.md](./deployments-api-contract.md)（instructions 4096→500）。
