# Memory 限额与 access 校验

> Issue: #252
> 日期: 2026-08-16
> 分支: `feat/memory-limits-access`

## 背景

官方 Claude Managed Agents 的 Memory Stores 有明确的限额与访问控制契约：
- 每 session 最多 **8 个** memory store
- 每 store 最多 **2,000** 条 memory（单条 ≤100KB）
- memory 版本保留 **30 天**（近期版本永留）
- `access`：`read_write` / `read_only`，文件系统级强制

OMA 的 Memory API 主体齐全（14 端点 + 版本审计 + redact），但**限额与 access 校验不完整**：
- session 创建/追加 memory_store 资源时 **access 不校验枚举**（`internal/sessions/service_helpers.go:193-213` 只透传；deployments 路径 `resources.go:238-252` 有校验——两条路径行为不一致）
- **每 session 8 个 store 上限未实现**（无计数检查）
- **每 store 2000 条未实现**（`CreateMemory` 无计数）
- **30 天版本保留未实现**（无清理逻辑）

## 方案（聚焦校验层，沙箱挂载另行）

### 1. session 路径 access 校验（对齐 deployments）

`internal/sessions/service_helpers.go` 的 memory_store 分支：
- `access` 非空时校验必须为 `read_write` / `read_only`，非法值报错
- 缺省时默认 `read_write`（对齐官方默认）

### 2. 每 session 8 个 store 上限

- `addResource` 路径（session 追加 memory_store）时，用 `ListSessionResources` 数已有 memory_store 数量，≥8 拒绝
- create 路径（initial resources 含 memory_store）同样校验

### 3. 每 store 2000 条上限（DB 层）

`internal/db/memory.go` 的 `CreateMemory`：
- 先 `CountMemoriesByStore`（新方法：`SELECT count(*) WHERE store AND NOT deleted`）
- ≥2000 时返回明确的 limit 错误

### 4. 版本 30 天保留（DB 层）

- 新增 `DeleteMemoryVersionsOlderThan`（清理 30 天前且非「近期版本」的版本）
- 调用方：memory store 操作时惰性清理，或独立 worker（暂不做 worker，先提供方法 + 在 store 写入路径调用）

> 注：`/mnt/memory/<slug>` 挂载与 read_only 文件系统级强制属于沙箱集成（大工程），本 issue 只做 **API 校验层**；沙箱挂载见 #117 盘点结论后另行。

## 测试

- access 非法值：session 路径报错（对齐 deployments）
- 8 store 上限：第 9 个 memory_store 拒绝
- 2000 条上限：CreateMemory 达上限报错
- 30 天版本清理：方法正确删除过期版本

## 验收

- session 路径 access 校验与 deployments 一致
- 超过 8 个 store / 2000 条 / 30 天保留均被正确限制
- 官方 SDK 创建带 access 的 memory_store 全流程可用

## Review 修正（2026-08-16）

**已补**：
- `DeleteMemoryVersionsOlderThan` 接入 `CreateMemory` 事务（30 天版本保留落地，之前零调用）
- `ErrMemoryStoreLimit` 映射为 409 `memory_store_limit_error`（之前落 500）
