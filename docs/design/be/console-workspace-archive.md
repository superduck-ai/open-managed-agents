# Console Workspace 归档

本文记录工作区归档（软删除）的数据模型、级联行为、禁归档边界与 API 契约，对齐 Anthropic Console workspace 的 archive 语义：归档即停用工作区并立即吊销其下所有 API key，且不可恢复。

## 背景与语义

Anthropic Console 中，归档工作区是不可逆的软删除：工作区立即停用，其下所有 API key 立即吊销，且无法恢复。本实现对齐该语义：

- `workspaces.archived_at` 由空变为 `now()` 表示归档；归档后的工作区不再出现在工作区列表与切换器中。
- `default` 工作区是兼容 Anthropic 的回退工作区，永远不可归档。
- 归档操作在单个数据库事务内级联吊销该工作区下的所有 `console_api_keys`。

## 数据模型

归档只写两个时间戳列，不删除任何行：

- `workspaces.archived_at timestamptz`：非空即归档。
- `console_api_keys.archived_at timestamptz`：随工作区归档一并置位。

`workspaces.uuid` 是内部稳定工作区标识，`external_id` 是兼容 API 的展示标识，`organization_uuid` 是租户边界。`console_api_keys.organization_uuid` + `workspace_uuid` 和 `api_keys.workspace_uuid` 分别定位控制台密钥与实际鉴权密钥。归档写使用 `coalesce(archived_at, now())`，因此对已归档工作区重复归档是幂等的：`archived_at` 不会被改写。

## 状态机与级联

```mermaid
stateDiagram-v2
    [*] --> Active: 创建工作区
    Active --> Archived: POST /archive (name != 'default' 且 != current)
    Archived --> Archived: POST /archive (幂等)
    note right of Archived
        archived_at 非空；console_api_keys 全部 archived_at 非空
        不可恢复
    end note
```

```mermaid
sequenceDiagram
    participant UI as Console 设置页
    participant H as handleArchiveConsoleWorkspace
    participant DB as DB.ArchiveConsoleWorkspace
    participant WS as workspaces
    participant KEY as console_api_keys
    UI->>H: POST /workspaces/{id}/archive
    H->>H: 校验 default / current workspace
    H->>DB: ArchiveConsoleWorkspace(orgUUID, id)
    DB->>DB: BEGIN sqlx tx
    DB->>WS: UPDATE archived_at = coalesce(archived_at, now())
    WS-->>DB: returning workspace
    DB->>KEY: UPDATE console_api_keys WHERE organization_uuid AND workspace_uuid
    DB->>KEY: UPDATE api_keys status = archived WHERE workspace_uuid
    DB->>DB: COMMIT
    DB-->>H: ConsoleWorkspace(archived_at)
    H-->>UI: 200 OK
```

## 禁归档边界

归档端点对默认工作区与当前会话工作区做了多层防护：

| 场景 | 后端 | 前端 |
|------|------|------|
| `default` 别名 | handler `DisplayID == "default"` → 409 `cannot_archive_default_workspace` | DropdownMenu 归档项 disabled |
| 默认工作区（真实 external_id） | DB 层 `WHERE name <> 'default'` 排除，0 行 → `ErrNotFound` → 404 | 列表过滤默认工作区，前端不可达 |
| 当前会话绑定的工作区 | handler 比较 principal 的 workspace UUID 或 external ID → 409 `cannot_archive_current_workspace` | `workspace.id === activeWorkspaceId` 时归档项 disabled |

默认工作区是兼容 Anthropic 的回退工作区，永远不可归档。`workspaces` 表的 `(organization_uuid, name)` 唯一约束保证每个组织只有一个 `name = 'default'` 的工作区，因此 DB 层 `WHERE lower(coalesce(name, '')) <> 'default'` 精确兜底：即便调用方绕过 handler 的别名校验、直接用默认工作区的真实 `external_id` 或 UUID 调用，UPDATE 也命中 0 行而返回 `ErrNotFound`（404）。handler 的别名校验仅用于对最常见的 `"default"` 调用给出明确的 409 语义。

前端禁用是为了避免用户触发必败请求；后端校验是权威防线，防止绕过 UI 直接调用 API 造成自锁——归档当前工作区后，会话绑定的 API key 会立即被吊销。

## 包职责

- `internal/db/console_workspaces.go`
  - `ArchiveConsoleWorkspace(ctx, orgUUID, workspaceID)` 在单个 `sqlx` 事务内按 `organization_uuid` 定位工作区，兼容 workspace UUID 与 external ID 输入。
  - UPDATE 的 WHERE 子句带 `lower(coalesce(name, '')) <> 'default'`，把"默认工作区不可归档"作为写入路径不变量；随后按稳定 workspace UUID 级联更新 `console_api_keys` 和实际鉴权用的 `api_keys`。
  - `returning` 结果复用已有命名 SQLX row mapping，所有事务 SQL 使用命名参数和 UUID 类型边界。
- `internal/platformapi/console_workspaces.go`
  - `handleArchiveConsoleWorkspace` 先通过 `WorkspaceScope` 将路由中的 display ID 或 UUID 解析为稳定 UUID，负责禁归档校验（default / current）、`404`/`409` 映射与响应。
- `internal/platformapi/platform_backend_routes.go`
  - 归档路由挂在已有的 `RegisterConsoleOrganizationWorkspaceRoutes`（workspace 路由的独立入口，`server.go` 已在调用），与 `r.Get("/workspaces", ...)` 同处。
- `internal/platformapi/console_api_keys.go` 与 `internal/db/console_api_keys.go`
  - 提供 `WorkspaceScope`、console workspace row mapping 和 console API key schema；归档复用这些边界，不重新引入旧的 identity 列。

## API 契约

```
POST /api/console/organizations/{orgUuid}/workspaces/{workspaceId}/archive
```

| 状态码 | 含义 |
|--------|------|
| 200 | 归档成功（含幂等重试），返回带 `archived_at` 的 workspace |
| 404 | 工作区不存在、不属于该组织（组织隔离），或默认工作区按真实 external_id/UUID 归档（DB 层不变量） |
| 409 | 目标是 `default` 别名，或当前会话绑定的工作区 |

归档不要求请求体。

## 前端入口

`WorkspacesSettingsPage` 每行的操作列改为 `DropdownMenu`（API keys / Webhooks / 归档），归档项打开 `ArchiveWorkspaceDialog` 二次确认。`default` 工作区与当前激活工作区的归档项被禁用，并通过 `title` 给出切换提示。归档成功后 `WorkspaceProvider` 通过 `queryClient.setQueryData` 从工作区列表移除该工作区；由于当前激活工作区已被前端禁用归档，正常路径下不会触发激活工作区收敛。

## 测试

`tests/console_workspace_archive_api_test.go` 覆盖：

- `default` 别名归档 → 409。
- 默认工作区按真实 external_id 归档 → `ErrNotFound` 且 `archived_at` 保持空（直接调用 `ArchiveConsoleWorkspace` 验证 DB 层不变量，避开 HTTP 会话的自锁校验）。
- 未知工作区 → 404。
- 组织隔离（用 A 组织凭证归档 B 组织工作区 → 404）。
- 归档成功并级联吊销 `console_api_keys`（断言 key 的 `archived_at` 非空）。
- 归档同时吊销 `api_keys`，使已归档工作区的实际鉴权立即失效。
- 重复归档幂等（`archived_at` 不变）。

验证命令：

- `go test ./tests -run TestConsoleWorkspaceArchive -count=1 -v`
- `go test ./internal/db ./internal/platformapi -count=1`
- 前端：`bun run build`、`eslint --config eslint.complexity.config.js src`。
