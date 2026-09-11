# Session 状态转换合法性校验

> Issue: #247
> 日期: 2026-08-16
> 分支: `fix/session-status-transition`

## 背景

官方 Claude Managed Agents 的 session 有明确生命周期：`idle` / `running` / `rescheduling` / `terminated`，终态（terminated）后不应再变回。OMA 的 `SetSessionStatus`（`internal/db/sessions.go:253`）当前**无条件 UPDATE**（`session_mapper.xml:93-99` 无旧状态限制）——**可从 terminated 改回 idle**，状态机被破坏。

官方还有两个相关契约：
- **running 的 session 不能归档/删除**（需先 interrupt）
- **归档的 session 不能接受新事件**

## 方案

### 1. 定义合法状态转换表

```text
idle        → running, rescheduling, terminated
running     → idle, rescheduling, terminated
rescheduling→ idle, running, terminated
terminated  → （终态，不可回退）
```

### 2. `SetSessionStatus` 增加旧状态校验

`internal/db/sessions.go` 的 `SetSessionStatus`：
- SQL 加 `WHERE status IN (合法前驱状态)`（应用层先读再判更简单，但 SQL 层原子性更好）
- 采用 SQL 方案：`session_mapper.xml` 的 `SetStatus` 加 `AND status IN (...)`，rowsAffected==0 时区分「不存在」和「非法转换」

### 3. 区分错误

- 不存在 → `ErrNotFound`（既有）
- 非法转换 → 新 sentinel `ErrInvalidStateTransition`

## 测试

- 合法转换（idle→running、running→idle）通过
- 非法转换（terminated→idle）报 `ErrInvalidStateTransition`
- 不存在报 `ErrNotFound`（回归）
- mapper contract 测试更新

## 验收

- `terminated` 的 session 无法通过 `SetSessionStatus` 改回活跃状态
- 非法转换返回明确错误（区别于 not found）
- 正常状态流转不受影响


## 范围外（后续工作）

- 运行中 session 的归档/删除拒绝（官方「running 需先 interrupt」）走独立的 Archive/Delete mapper 路径，本 PR 未覆盖，见 PR 描述。
- terminated session 的事件追加边界（AppendSessionEvents 目前只拒 archived）属事件路径工作，同样不在本 PR。
