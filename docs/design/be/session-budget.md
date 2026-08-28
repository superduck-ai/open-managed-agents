# Session Budget（max_list_cost 费用上限）

> Issue: #244
> 日期: 2026-08-16
> 分支: `feat/session-budget`

## 背景

官方 Claude Managed Agents 支持 session 级费用预算：创建 session 时传 `budget: {type: "limit", max_list_cost: {amount: "125", currency: "USD"}}`（amount 是美分字符串，仅支持 USD），达到上限后暂停/拒绝新事件，避免失控烧钱。

OMA 全仓对 budget **零实现**（无字段、无 DB 列、无校验）——官方 SDK 创建带 budget 的 session 被静默忽略。

## 官方契约（budgets.md + events-and-streaming.md）

### 校验（400 场景）
1. `amount` 必须是**无前导零的整数美分字符串**（如 `"125"` = $1.25），`"25.00"`/零/负数拒绝
2. `currency` 必须是大写 `USD`
3. 达上限时发 work-starting 事件（`user.message` 等）→ 400（错误需列出可接受的 settle 事件）
4. 预算 ≤ 已消耗 list cost → 400
5. 无预算的 session 加预算、或移除后重加 → 400（**本实现范围**：OMA 尚无 list cost 概念，此场景暂以「创建时设置、更新可改/移除」简化）
6. 无定价模型 → 400（依赖模型目录，暂不实现）

### 达上限行为
- 只接受 settle 事件：`user.tool_confirmation` / `user.tool_result` / `user.custom_tool_result` / `user.interrupt`
- work-starting 事件（`user.message` 等）→ 400
- `user.interrupt` 在暂停时被接受但忽略
- 改预算（高于已消耗）或移除预算（`"budget": null`）→ 恢复

### 事件
- `session.budget_reached` webhook 事件（每次设置预算最多触发一次，改预算重新武装）
- `session.status_idle` 的 stop_reason 可为 `budget_reached`
- 暂停顺序：`session.thread_status_idle(budget_reached)` → `session.usage` → `session.status_idle(budget_reached)`（**session.usage 依赖 #249，本实现先发 idle + budget_reached**）

## 方案（最小可用实现）

### 1. DB：sessions 表加 budget 列（migration）

`internal/db/migrations/` 新增 migration：
```sql
ALTER TABLE sessions ADD COLUMN budget jsonb;
```
（存 `{"type":"limit","max_list_cost":{"amount":"125","currency":"USD"}}` 或 NULL）

### 2. 校验（sessions/handler.go + service_helpers.go）

- `sessionMutationRequest` 加 `Budget json.RawMessage`
- 新增 `normalizeBudget(raw)`：校验 type=limit、amount 整美分字符串、currency=USD
- create 时校验并存入；update 时允许改/移除（`"budget": null`）

### 3. 达上限拒绝（sendEvents 路径）

- session 带 budget 时，`user.message` 等 work-starting 事件先查「是否已达上限」（**本实现用 DB 存的 `budget_reached_at` 标记**，无真实 list cost 计算）
- 达上限 → 400，错误列出 settle 事件清单
- 改/移除预算清除标记

### 4. 事件

- `session.budget_reached` webhook 事件：注册进 `supportedEndpointEventTypes` + 默认订阅 + 映射桥（复用 #255 的模式）
- 达上限时发 `session.status_idle`（stop_reason=budget_reached）

### 明确不做（依赖后续）
- list cost 计算（依赖 #62 用量采集）
- `session.usage` 事件（#249）
- 无定价模型校验（依赖模型目录）
- 精确暂停语义（thread 级暂停）

## 测试

- `normalizeBudget`：合法/非法 amount、currency、type
- create 带 budget → 存储；update 改/移除（API 级：无预算加预算 400 / 改 200 / 移除 200）
- 达上限后 `user.message` → 400；settle 事件通过（**#271 范围**）
- `session.budget_reached` webhook 注册 + 映射（**#271 范围**）

## 验收

- 官方 SDK 创建带 budget 的 session 全流程可用（字段存储 + 响应回显）
- 达上限后 work-starting 事件 400（列出 settle 清单）
- 改/移除预算恢复
- `session.budget_reached` 事件可订阅

## 范围更新（2026-08-16 实施后）

**已实现**：
- DB migration 00055：sessions 表加 budget jsonb 列
- sessionRow/sessionWriteParams/sessionUpdateParams/Session 结构加 Budget
- session_mapper.xml 的 columns/Insert/Update 支持 budget
- `sessionMutationRequest` 加 Budget 字段 + `normalizeBudget` 校验（type=limit、美分字符串、USD）
- create 存储 budget；update 支持改/移除（`"budget": null`）
- session 响应回显 budget（无预算时 null）
- 前端：
  - **创建**：session 创建对话框新增 Budget (USD cents) 输入（`ManagedTextField`），create body 带 budget（`api.ts` `createManagedEntityBody`）
  - **编辑回填**：`entityBudgetAmount(entity)` 从 `entity.budget.max_list_cost.amount` 回填 `budgetAmount`；`budgetInitiallySet` 记录原状态
  - **更新三态**（`sessionBudgetUpdate`）：非空 → 发新 budget；清空且原本有 → 发 `budget: null` 移除；清空且原本无 → 不发（官方禁止给无预算 session 加预算）
  - i18n：`managedAgents.sessions.budget` / `budgetPlaceholder`（扁平 key，en/zh 对称）

**未实现（依赖后续）**：
- 达上限拒绝 work-starting 事件（需真实 list cost，依赖 #62 用量采集）
- `session.budget_reached` 事件（依赖达上限判定）
- `session.status_idle` 的 stop_reason=budget_reached（依赖达上限判定）
- 无定价模型校验（依赖模型目录）
- `session.usage` 事件（#249）

## Review 修正（2026-08-16）

**已补**：
- update 路径「给无预算 session 添加预算 → 400」（官方 budgets.md 契约，此前静默放宽）
- `session.updated` 事件携带 budget（改/移除预算后下游可感知）
- 补 budget 规则测试

**仍依赖后续**（不变）：达上限拒绝、budget_reached 事件、list cost 计算（#62）、session.usage 真实 budget 回显（#249 需在 #244 合并后手动读取 session.Budget）。
