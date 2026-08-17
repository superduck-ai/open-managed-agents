# Dreams 能力面后端实现

> Issue: #245
> 日期: 2026-08-16
> 分支: `feat/dreams-backend`

## 背景

官方 Claude Managed Agents 的 Dreams 能力（`dreams.md`，research preview 但已完整文档化）：**Dream 是一个异步 job**，输入一个已有 memory store + 1~100 个 sessions（历史转录），Claude 验证、去重、重组后产出一个**新的 output memory store**。

OMA 当前**后端零实现**（`internal/` 对 "dream" 全零命中），仅前端有 placeholder 页面。

## 官方契约（dreams.md）

- 端点：`/v1/dreams`，需要 `dreaming-2026-04-21` beta header（`managed-agents-2026-04-01` 不单独授权）
- 输入：`[{type: "memory_store", memory_store_id}, {type: "sessions", session_ids: [...]}]`（1 个 store + 1~100 个 sessions）
- 输出：另一个 output memory store（`outputs[]` 字段，dream 进入 `running` 后出现；运行中可能短暂为空）
- Lifecycle：`running` → 完成 / `cancelled` / `archived`
- 支持：steer with instructions、track progress、watch pipeline、cancel、archive、list
- 错误契约 / 计费 / 上限（1~100 sessions）

## 方案（API + 数据模型层，精炼工作流后续）

### 1. DB：dreams 表（migration 00053）

```sql
CREATE TABLE dreams (
    id bigint generated always as identity primary key,
    uuid uuid default gen_random_uuid(),
    external_id text,
    organization_uuid uuid,
    workspace_uuid uuid,
    created_by_api_key_uuid uuid,
    input_store_uuid uuid,
    session_ids jsonb,        -- 1~100 sessions
    instructions text,        -- steer with instructions
    status text,              -- pending/running/succeeded/failed/cancelled/archived
    output_store_uuid uuid,   -- 完成后填充
    error text,
    created_at timestamptz,
    updated_at timestamptz,
    archived_at timestamptz
);
```

### 2. API 层（internal/dreams/ 新包）

- `POST /v1/dreams`：创建（校验输入 store 存在、sessions 1~100、`dreaming-2026-04-21` beta header 门控）
- `GET /v1/dreams/{id}`：查询（含 outputs[]、status）
- `GET /v1/dreams`：列表
- `POST /v1/dreams/{id}/cancel`：取消
- `POST /v1/dreams/{id}/archive`：归档

### 3. 异步 job（占位）

- 创建后状态 `pending`，**精炼工作流（验证→去重→重组→写输出 store）作为后续**（依赖模型调用 + memory store 写入）
- 本实现提供 `markDreamRunning`/`markDreamSucceeded` 等状态推进方法（供后续 worker 调用）

### 明确不做（依赖后续）
- 精炼工作流本身（AI 逻辑，需模型调用）
- output store 的实际生成
- steer with instructions 的深度语义

## 测试

- 创建校验：输入 store 不存在、sessions 超 100、缺 beta header
- CRUD + cancel/archive 状态流转
- mapper contract 测试

## 验收

- 官方 SDK 创建 dream 全流程可用（创建 → 查询 → cancel/archive）
- `dreaming-2026-04-21` beta header 门控正确
- 精炼工作流作为后续（design 文档标注）
