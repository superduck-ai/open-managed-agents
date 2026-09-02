# Session 生成文件下载（#261）与 Get Session 文件资源回填（#258）

> 状态：实现方案（已评审）
> 关联：issue #261、#258；上游 #259（session files/resources 对齐）已合并
> 前置：`docs/design/be/filestore.md`（Session Resource/File 统一持久化契约）
> 强校验：官方文档 `platform.claude.com/docs/en/managed-agents/files.md` + `managed-agents-environments.md`（claude-api skill `shared/`）

## 背景与目标

官方 Claude Managed Agents Files 契约（managed-agents/files.md + environments.md 实测）：

- **上传的文件**：`POST /v1/files` 上传 → `resources: [{type:"file", file_id, mount_path}]` 挂载，`mount_path` 映射到 sandbox `/mnt/session/uploads/<mount_path>`（OMA 用 `/mnt/user-data/uploads` 的等价实现）。官方文档**未明文** `downloadable` 字段；OMA #248 曾把上传文件改成可下载被**撤销**，说明**官方实际行为**是上传文件不可下载（下载返回 400）
- **agent 生成的输出文件**：写入 **`/mnt/session/outputs/`**（OMA 的 sandbox 内路径为 `/mnt/user-data/outputs`，公开路径统一为 `/outputs/`），通过 `GET /v1/files?scope_id=<session_id>` 列出、`GET /v1/files/{id}/content` 下载

**路径映射（OMA 与官方一致的关键）**：OMA 的 `visibleFilePredicate` 判定的是 **filestore namespace 公开路径 `/outputs/`**，与 sandbox 内路径（`/mnt/user-data/outputs`）无关；sandbox 内 `user-data` 挂载到公开 `/outputs/`，system prompt 引导写 `/mnt/user-data/outputs/<relative-path>`。这是 sandbox 内部实现差异，不是 API 契约差异。

### 路径差异溯源：为什么 OMA 用 `/mnt/user-data/outputs` 而不是官方的 `/mnt/session/outputs/`

| 项 | 官方 | OMA |
|---|---|---|
| 路径 | `/mnt/session/outputs/` | `/mnt/user-data/outputs` |
| 性质 | 纯约定（官方自管容器，无需区分卷） | E2B 沙箱**持久化用户数据卷**挂载点 |
| 写入方 | agent 工具（write/bash） | 同上 |
| 公开契约 | `/outputs/` + `files.list(scope_id)` + `download` | 与官方一致 |

**根源**：`internal/runtime/e2bruntime/runtime.go:22-23` 定义

```go
sandboxUserDataVolumeName = "user-data"
sandboxUserDataMountPath  = "/mnt/user-data"
```

`/mnt/user-data` 是 E2B 沙箱启动时挂载的**持久化用户数据卷**（`TestSandboxVolumeMountsOnlyIncludeUserData` 验证只有这个卷）。OMA 把 outputs 放在该卷下，输出文件随卷持久化。

**演进**（三处提交逐步固化）：
1. **#151（Arthur.Zhang，07-26，rclone filestore 奠基）**：引入「`/mnt/user-data/outputs` + `/mnt/session/uploads` + `/mnt/transcripts` + `/mnt/user-data/tool_results`」四挂载点，`/mnt/user-data` 为可写 volume，`/mnt/session` 为只读
2. **a45eaf5 / 88a9858（xgxgx，07-28，environment packages provisioning）**：沿用该路径
3. **7c121af（xgxgx，08-18，#259）**：`environment_manager.go:38` 明确「写入 `/mnt/user-data/outputs` 的文件投影为公开 `/outputs/`」并进入 Files API Catalog

**为什么不用官方路径**：官方路径是**纯约定**（官方自己管理容器，路径不影响持久化）；OMA 因 E2B 沙箱需要**持久化卷承载 rclone 挂载与输出文件**，才引入 `/mnt/user-data` 命名空间。**这是实现层差异，不是契约差异**——OMA 的公开 API（`/outputs/` + `scope_id` 列表 + 下载）与官方完全一致；差异只在沙箱内部 agent 被引导写入的路径名。**本方案不改变该路径**（下载侧只看公开 `/outputs/` + `files` 表，不感知沙箱内路径）。

OMA 现状：

- 上传文件：`internal/files` 硬编码 `Downloadable: false`（对齐官方实际行为 ✅）
- 生成文件：通过 rclone FUSE / filestore 路径写入，已作为 Owned File 落库（`files` 表，`Downloadable: true`）✅
- **缺口 1（#261）**：生成文件是否真正能通过 `GET /v1/files/{id}/content` 下载，未经验证
- **缺口 2（#258）**：`GET /v1/sessions/{session_id}` 的 `resources[]` 对输出文件（`payload` 为 NULL）只返回 `{id,type,created_at,updated_at}`，**缺 `file_id` 与 `mount_path`**，前端无法展示输出文件

本方案目标是**打通生成文件的下载闭环（#261，对齐官方），并补齐 Get Session 资源中的 `file_id`/`mount_path`（#258，OMA 前端展示增强——官方契约仅要求 `files.list(scope_id)` 能列出输出文件，未要求 Get Session 返回它们）**。

## 现状事实链（代码验证结论）

### 1. 生成文件已在 `files` 表，`Downloadable: true`

`InsertOwnedFileAndResource`（`internal/db/session_resource_mapper.xml:157-196`）在创建 Owned File 时直接向 `files` 表插入：

- `scope_type = 'session'`、`scope_id = session.external_id`
- `downloadable` 来自 `params.Downloadable`，而 filestore 路径默认 `fileDownloadable()` 为 true（`internal/filestore/service.go:607-612`，`nil` 时默认 true）
- `external_id` 为 `file_...` 前缀（`newFileIdentity`，`internal/db/session_resource_file_helpers.go:366-372`）

**结论**：生成文件在 Files API 侧已经是一条合法 File 记录，`file_` ID 与上传文件共用同一命名空间。

### 2. `files.list` 的可见性已由 `visibleFilePredicate` 限定（对齐官方）

`internal/db/file_mapper.xml:9-22` 的 `visibleFilePredicate`：

```sql
AND (
    NOT EXISTS (
        -- 普通文件（payload 非空）恒可见
        SELECT 1 FROM session_resources owner
        WHERE owner.file_uuid = files.uuid AND owner.workspace_uuid = files.workspace_uuid
          AND owner.payload IS NULL
    )
    OR EXISTS (
        -- Owned File（payload IS NULL）仅当：活动（未删除、未过期）且路径在 /outputs/ 下可见
        SELECT 1 FROM session_resources owner
        WHERE owner.file_uuid = files.uuid AND owner.workspace_uuid = files.workspace_uuid
          AND owner.payload IS NULL AND owner.deleted_at IS NULL
          AND (owner.expires_at IS NULL OR owner.expires_at > now())
          AND left(owner.path, char_length('/outputs/')) = '/outputs/'
    )
)
```

语义：**上传文件（payload 非空）与 `/outputs` 下活动 Owned File 在 Files API 可见**；已删除、过期、或移出 `/outputs` 的生成文件不可见。这是官方「生成文件可下载、上传文件不可下载」的 SQL 级实现。

### 3. 下载端点逻辑完整，理论上已支持生成文件

`internal/files/handler.go:294-329` `download()`：

1. `GetFile(workspaceUUID, fileID)`（带 `visibleFilePredicate`，`file_mapper.xml:67-74`）
2. `if !record.Downloadable` → 400 `File is not downloadable`
3. `store.Open(record.S3Key)` → 流式返回

生成文件满足「可见（在 `/outputs`）+ `Downloadable=true`」→ **理论上已能下载**。上传文件 `Downloadable=false` → 400（对齐官方）。

### 4. 真正的缺口：Get Session 不返回输出文件，且资源缺 `file_id`

**缺口 4a（比缺 `file_id` 更严重）**：`SessionResourceMapper.List`（`session_resource_mapper.xml:37-45`）带 **`AND payload IS NOT NULL`** 过滤——**Output Resource（payload 为 NULL）根本不会被 `ListSessionResources` 返回**。因此 `GET /v1/sessions/{id}` 的 `resources[]` **当前完全不包含输出文件**，#258 验收项 2「输出文件出现在同一资源集合」**未实现**。

**缺口 4b（缺 `file_id` 的根因）**：`SessionResourceFileMapper.resourceFileSource`（`session_resource_file_mapper.xml:5-47`）的 `source_file_uuid` 定义为：

```sql
CASE WHEN resource.payload IS NOT NULL THEN resource.file_uuid ELSE NULL END
```

- **Input Resource**（payload 非空）：`source_file_uuid` 指向 Source File（上传文件），`file_id` 在 payload 中 ✅
- **Output Resource**（payload 为 NULL）：`file_uuid` 指向 Owned File（生成文件），但该投影**未暴露 Owned File 的 `external_id`（`file_` ID）** ❌

同时 `sessionResourceColumns`（`session_resource_mapper.xml:3-7`）**不含 `path` 字段**，`SessionResource` 行结构既没有公开路径也没有 Owned File 的 `external_id`（`session_resource_mapper.go` 仅有写入参数）。因此改动前的 `responseFromResource` 对输出文件只能返回 `{id, type, created_at, updated_at}`，无法回填 `file_id`/`mount_path`。

`resource_type` 语义（`session_resource_mapper.xml:147,190,321`）：**Output Resource 的 `resource_type='file'`，payload=NULL**；目录、skill archive 各有独立类型。这为「去掉 payload 过滤后区分输出文件」提供了依据。

## 方案

### 核心思路

**不在 Files API 新增端点、不修改上传文件语义、不复制对象**。利用已存在的 `files` 表 Owned File 记录，打通下载，并让 Get Session 真正返回输出文件（含 `file_id`/`mount_path`）。

### 改动 1（#261 主体）：验证并修正下载端点

- **验证**：生成文件 `GET /v1/files/{id}/content` 是否 200（真实 sandbox E2E，见「验收」）
- 若验证失败，补丁点在 `GetFile` 或 `download()` 的查询范围，**不是新端点**（`visibleFilePredicate` 已保证可见性）
- 上传文件下载保持 400，`handler.go:155` 不动

### 改动 2（#258 主体）：Get Session 返回输出文件并回填 `file_id`/`mount_path`

**读取原子性前提（评审后修正）**：Owned File 的 `external_id` 必须与 Session Resource **同批读出**。`retireSessionResourceFileTx`（`session_resource_file_helpers.go:425-443`）在同一事务里先退休 `files` 行再退休 `session_resources` 行，因此稳态下不存在「File 已删、Resource 仍活」；但**分两次独立查询**会在事务提交的间隙返回缺少 `file_id` 的输出资源，违反 #258 验收项。同时 Session 列表逐条构造响应（`service.go:229`，limit 上限 1000），额外一次 File 查询会把既有 N 次放大到 2N。两个问题的同一个解法是 **JOIN**：

1. **`List` / `FindByExternalID` 改为 `LEFT JOIN files` 并收窄可见性**（`session_resource_mapper.xml`）：
   ```sql
   FROM session_resources resource
   LEFT JOIN files file
       ON file.uuid = resource.file_uuid
       AND file.workspace_uuid = resource.workspace_uuid
       AND file.deleted_at IS NULL
   WHERE resource.workspace_uuid = #{workspaceUUID}
   AND resource.session_external_id = #{sessionExternalID}
   AND resource.deleted_at IS NULL
   AND (
       resource.payload IS NOT NULL
       OR (
           resource.resource_type = 'file'
           AND left(resource.path, char_length('/outputs/')) = '/outputs/'
           AND (resource.expires_at IS NULL OR resource.expires_at > now())
           AND file.uuid IS NOT NULL
       )
   )
   ```
   - 目录、skill archive 等其他资源**仍被排除**（不进入返回集合），无需在响应层二次区分
   - `expires_at` 过滤与 `visibleFilePredicate`（`file_mapper.xml:9-29`）对齐，带 TTL 的过期输出文件不可见
   - `file.uuid IS NOT NULL` 保证输出资源必然带 `file_id`，不会返回残缺资源
   - `SessionResource` 新增 `Path` 与 `FileExternalID` 字段（`sessionResourceListColumns` 含 `resource.path` 与 `file.external_id AS file_external_id`）；写入语句的 RETURNING 保持原列集，避免引用未插入列
   - 两条读取语句都走 JOIN，因此单资源 GET 与列表行为一致，不需要额外加载步骤

2. **`responseFromResource` 回填**（`service_helpers.go`）：
   - `isOutputResource` 判断：**path 前缀 `/outputs/`**（`strings.HasPrefix(resource.Path, sandboxmount.OutputsRoot+"/")`，与 SQL 的 `/outputs/` 判断一致）
   - Output Resource：直接使用同批 JOIN 出的 `resource.FileExternalID` 作为 `file_id`，`resource.Path` 作为 `mount_path`（`/outputs/<relative-path>`）
   - Input Resource（payload 非空，`file_id` 在 payload）：保持现状
   - Output Resource 的 payload 为空，**跳过 `json.Unmarshal`**（空 payload 必然失败，白做）；直接建空 map
   - 保持纯函数签名 `responseFromResource(resource)`，不需要传入预加载映射

**性能结论**：每个 Session 仍是 1 次资源查询，Session 列表维持既有 N 次往返，不引入新的 N+1。JOIN 只多取 `files.external_id` 一列，且 CLAUDE.md 允许「确实读取目标表字段」的 JOIN。

**响应长度上限（评审后补齐）**：改动前 `resources[]` 被写入侧的 500 上限兜住（`sessionresource.ValidateFileSpecs` + `enforceSessionFileResourceCapacityTx`）。输出资源不吃这个配额——`CountSessionFileResources`（`session_resource_mapper.xml`）带 `payload IS NOT NULL`，Filestore 写入侧只校验 workspace 字节配额、不限文件数——所以让输出文件进入 `resources[]` 会把长度变成无上限，并被 List Sessions（limit 上限 1000）放大。`List` 因此在 SQL 层自己截断：

```sql
SELECT ... , row_number() OVER (
    PARTITION BY (resource.payload IS NULL)
    ORDER BY resource.created_at DESC, resource.uuid DESC
) AS output_rank
...
WHERE resource.payload IS NOT NULL
OR resource.output_rank <= #{maxOutputResources}
```

- `PARTITION BY (resource.payload IS NULL)` 让用户资源与输出文件各自排名，**用户资源不受截断**（它们已有写入侧上限）
- 输出文件保留 `created_at DESC` 最近的 `MaxSessionOutputFileResources` 条（`db.MaxSessionOutputFileResources = sessioncontract.MaxFileResources`，即 500），与写入侧同一水位，正常用量下行为与改动前完全一致
- 超过上限时是**静默截断**：`resources[]` 只承载最近 500 个输出文件，**完整集合以 `files.list(scope_id)` 为准**（官方契约本身也只要求 `files.list` 能列全输出文件）
- `FindByExternalID` 不需要截断（单行查询），因此被截断的输出资源仍可按 ID 单独取回

更彻底的做法是让输出文件也计入写入侧 500 配额，但那会让 agent 写满后 `/outputs/` 写入失败，失败语义要穿过 rclone FUSE 传回 agent，影响面落在运行时，需单独设计。

### 改动 3（评审补充）：输出资源在单资源路由上的语义

去掉 `payload IS NOT NULL` 后，输出资源开始能被 `FindByExternalID` 查到，三条单资源路由的行为随之变化：

| 路由 | 改动前 | 改动后 | 依据 |
|---|---|---|---|
| `GET /v1/sessions/{id}/resources/{rid}` | 404（被 payload 过滤挡住） | 200，带 `file_id`/`mount_path` | `FindByExternalID` 走同一可见性 SQL |
| `PATCH .../resources/{rid}` | 404 | 400 `only github_repository resources can be updated` | `service.go:683` 的类型检查 |
| `DELETE .../resources/{rid}` | 404 | **仍是 404** | `GetSessionResourceForMutation`（`session_resource_mapper.xml:132`）保留 `payload IS NOT NULL` |

GET 与 DELETE 不对称是**有意保留**的：输出文件的生命周期属于 Filestore/Files API（`RemoveFilestoreFile` 与清理 worker 成对退休 `files` 与 `session_resources`），不允许通过 Session Resource 路由单边删除 Resource 行而留下孤立 File 行。读取路径开放、写入路径继续拒绝，是这个所有权边界的直接体现。

### 改动 4（前端配套，本期范围外 → sub-issue/sub-PR）

对齐 #259 已合并的 `SessionEntityPanels.tsx` 资源表格：`ResourceTable` 已读取 `resource.file_id`/`mount_path`（`SessionEntityPanels.tsx:419-430`），并对 `file_id` 反查文件名（`useSessionFileNames`）。前端**已具备展示 file_id/mount_path 的能力**；本期后端补齐 `file_id` 后前端表格自动展示。**前端「可下载」标记与下载入口另行开 sub-issue/sub-PR**（本期不做）。

## 边界与安全

| 边界 | 处理 |
|---|---|
| **FUSE/rclone 写入** | 透明。sandbox 写 `/outputs` → rclone FUSE → `PutFilestoreFile` → `InsertOwnedFileAndResource`。OMA 下载侧不感知 FUSE，对象定位直接用 `record.S3Key`（`filestores/...` 前缀） |
| **上传文件不可下载** | `handler.go:155` 固定 `Downloadable=false` 不动 |
| **可见性** | `visibleFilePredicate` 已限定：普通文件恒可见、Owned File 仅 `/outputs` 下活动可见 |
| **workspace 隔离** | `GetFile`/`GetFileByUUID` 均按 `workspace_uuid` 过滤（`file_mapper.xml:70,106`）；Session 资源回填只读当前 session 的 Owned File |
| **下载不越权** | 下载走 `GetFile`（带 `visibleFilePredicate`），已删除/过期/非 outputs 的生成文件返回 404 |
| **配额/清理** | 生成文件已有 filestore 配额与清理链路，下载不新增对象、不重复计费 |
| **不复制对象** | 下载直接流式读 `record.S3Key`，不产生新对象 |
| **N+1 风险** | 输出文件的 `file_id` 由 `List`/`FindByExternalID` 的 `LEFT JOIN files` 同批取出，每个 Session 仍是 1 次资源查询；Session 列表（limit 上限 1000）维持既有 N 次往返，不叠加 File 查询 |
| **残缺输出资源** | `file.uuid IS NOT NULL` 使 File 行已退休的输出资源整条不可见，避免返回没有 `file_id` 的资源 |
| **响应长度上限** | 输出文件不吃写入侧的 `MaxFileResources` 配额，因此 `List` 在 SQL 层按 `output_rank` 截断到 `MaxSessionOutputFileResources`（500）；用户资源不受截断，完整输出集合以 `files.list(scope_id)` 为准 |
| **Output 识别** | `isOutputResource` 用 **path 前缀 `/outputs/`** 判断（`sandboxmount.OutputsRoot` 常量），与 SQL `List` 的 `/outputs/` 判断一致，不依赖 payload 空这个伴随特征 |
| **`OutputsRoot` 常量** | 新增 `sandboxmount.OutputsRoot = "/outputs"`（与 `FileSource = "/uploads"` 并列，挂载路径合同归属地） |
| **改过滤的回归风险** | `ListSessionResources` 过滤改为「用户资源 + `/outputs/` 下活动 Output 文件」后，目录、skill archive **仍被排除**（SQL 层保证）；前端 `ResourceTable` 依赖 `file_id`/`mount_path`，为 NULL 时显示 `—`（现状兜底，`SessionEntityPanels.tsx:428-430`） |
| **`visibleFilePredicate` 子查询性能** | `GetFile`（单行）场景开销可忽略；`ListSessionFilesPage`（列表）对每行做 `session_resources` 的 EXISTS 子查询，若验证发现慢，需确认 `session_resources(file_uuid, workspace_uuid, deleted_at)` 索引覆盖（filestore 既有投影依赖） |
| **file_id 一致性** | Get Session 返回的输出文件 `file_id` 必须与 `files.list(scope_id)` 的 `f.id` 一致（同一 `files` 表 `external_id`），前端 `useSessionFileNames` 反查依赖此一致性 |

## 验收

1. **生成文件可下载**：sandbox 生成文件（写入 `/mnt/user-data/outputs` ↔ 公开 `/outputs/`）→ `GET /v1/files/{file_id}/content` 返回 200 与文件内容
2. **上传文件不可下载**：`GET /v1/files/{file_id}/content` 返回 400 `File is not downloadable`（对齐官方实际行为）
3. **Get Session 资源带 file_id**：`GET /v1/sessions/{session_id}` 的 `resources[]` 中**输出文件出现**，并包含 `file_id`（`file_...`）与 `mount_path`（`/outputs/<relative-path>`）；**目录、skill archive 资源不出现**（负向断言，防去 payload 过滤回归）
4. **不越权**：其他 workspace 的 file_id 返回 404；已删除/过期的输出文件不可见
5. **上传文件语义不变**：`/uploads` 下的 Input Resource 仍返回 `file_id` + `mount_path`（现状）
6. **file_id 一致性**：Get Session 返回的输出文件 `file_id` 与 `files.list(scope_id=session.id)` 返回的 `f.id` **一致**（前端 `useSessionFileNames` 依赖此一致性反查文件名）

## 测试计划

- **单测**：
  - `List`/`FindByExternalID` 的 builder 断言覆盖 `LEFT JOIN files`、`file.external_id AS file_external_id`、`expires_at` 过滤与 `file.uuid IS NOT NULL`
  - `responseFromResource` 对 Output Resource 回填 `file_id`/`mount_path`；Input Resource、目录、archive 保持现状
  - JSON 字面量 `null` payload 不触发 nil-map panic
- **真实 PostgreSQL（`tests/session_resources_visibility_test.go`）**：同一 Session 内种入 Input 文件、活动 Output 文件、过期 Output 文件、`/outputs/` 目录、skill archive、`/tool_results/` 下 Owned File 和 `github_repository`，断言：
  - `List` 只返回用户资源与活动 Output 文件；目录、skill archive、过期 Output 不出现
  - Output 资源的 `path` 与 `file_external_id` 正确扫描；`github_repository` 的两个 nullable 列扫描为空串
  - 只退休 `files` 行后，该 Output 资源在 `List` 与 `GetSessionResource` 中都不可见（覆盖两次独立查询会返回残缺资源的场景）
  - `FindByExternalID` 对隐藏资源返回 `ErrNotFound`
  - 种入 501 个输出文件后 `List` 只返回 500 个，最旧那条被截断，Input 与 `github_repository` 资源不受影响
- **E2E（真实 sandbox，遵循 `docs/design/be/filestore.md` 的验收方式）**：
  - 起独立服务（`ADDR=127.0.0.1:18080`）+ minio + E2B sandbox
  - 创建 Session → 写入 `/mnt/user-data/outputs/` 生成文件 → `GET /v1/sessions/{id}` 确认输出文件出现在 `resources[]` 且带 `file_id`/`mount_path` → `GET /v1/files/{file_id}/content` 确认 200 → `files.list(scope_id=session.id)` 确认 `f.id` 一致
  - 上传文件 → 下载 400
- **前端**（sub-issue/sub-PR 范畴）：`bun run build` + 窄范围功能测试（Session 详情资源表格展示输出文件）

## 明确不做（本期范围外）

- 不新增 Files API 端点（复用 `/v1/files/{id}/content`）
- 不修改上传文件的 `Downloadable` 语义
- 不做 `/transcripts`、`/tool_results`、`/skills` 的下载（filestore.md 明确不进入 Files Catalog）
- 不做前端「可下载」标记与下载入口（另行 sub-issue/sub-PR；前端表格已能展示 `file_id`/`mount_path`）
- 不做文件预览、分片下载等增强（仅打通下载闭环）
- **单资源接口返回 Output Resource**（已随本 PR 修复）：`GET /v1/sessions/{id}/resources/{rid}` 曾因 `FindByExternalID` 的 `payload IS NOT NULL` 过滤查不到 Output Resource——本次已改为与 `List` 相同的 JOIN 与可见性条件，`file_id` 同批取出（原 sub-issue #266 已关闭）。`PATCH`/`DELETE` 的语义见「改动 3」
- 不让输出文件计入写入侧的 `MaxFileResources` 配额（会让 agent 写满后 `/outputs/` 写入失败，失败语义需穿过 rclone FUSE，另行设计）；本期改为读取侧截断，见「改动 2」
- 不给 `resources[]` 加分页（官方 session 对象里 `resources` 是裸数组，分页会偏离官方形状）
