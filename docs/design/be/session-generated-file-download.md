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

同时 `sessionResourceColumns`（`session_resource_mapper.xml:3-7`）**不含 `path`、`file_uuid` 字段**，`SessionResource` 行结构也没有 `FileUUID`（`session_resource_mapper.go:75-128` 仅有写入参数）。因此现有 `responseFromResource`（`service_helpers.go:490-509`）对输出文件只能返回 `{id, type, created_at, updated_at}`，无法回填 `file_id`/`mount_path`。

`resource_type` 语义（`session_resource_mapper.xml:147,190,321`）：**Output Resource 的 `resource_type='file'`，payload=NULL**；目录、skill archive 各有独立类型。这为「去掉 payload 过滤后区分输出文件」提供了依据。

## 方案

### 核心思路

**不在 Files API 新增端点、不修改上传文件语义、不复制对象**。利用已存在的 `files` 表 Owned File 记录，打通下载，并让 Get Session 真正返回输出文件（含 `file_id`/`mount_path`）。

### 改动 1（#261 主体）：验证并修正下载端点

- **验证**：生成文件 `GET /v1/files/{id}/content` 是否 200（真实 sandbox E2E，见「验收」）
- 若验证失败，补丁点在 `GetFile` 或 `download()` 的查询范围，**不是新端点**（`visibleFilePredicate` 已保证可见性）
- 上传文件下载保持 400，`handler.go:155` 不动

### 改动 2（#258 主体）：Get Session 返回输出文件并回填 `file_id`/`mount_path`

**性能前提（已分析确认）**：逐条 `GetFileByUUID` 回填会产生 **N+1 查询**（每个输出文件 1 次 DB 往返）。典型 Session 生成文件数十~上百 → 数十次往返。必须**批量加载**：`List` + 1 次 `IN` 查询，总成本 O(1) 次往返，无 N+1。实现如下：

1. **`ListSessionResources` 的过滤从 `AND payload IS NOT NULL` 改为「用户资源 + `/outputs/` 下 Output 文件」**（`session_resource_mapper.xml:49-56`）：
   ```sql
   AND (
       payload IS NOT NULL
       OR (
           resource_type = 'file'
           AND path IS NOT NULL
           AND left(path, char_length('/outputs/')) = '/outputs/'
       )
   )
   ```
   - 目录、skill archive 等其他资源**仍被排除**（不进入返回集合），无需在响应层二次区分
   - `SessionResource` 行结构**新增 `FileUUID`、`Path` 字段**（`sessionResourceListColumns` 含 `file_uuid, path`，RETURNING 语句保持原列集避免「引用未插入列」错误），供回填使用

2. **批量加载 Owned File**：`loadOwnedFileMapping` 中一次性加载该 session 全部 Output Resource（`type=file` 且 path 在 `/outputs/` 下）的 `file_uuid → FileRecord`（`files` 表，`WHERE file_uuid IN (...)` 单条查询，`ListFilesByUUIDs`），构建映射传给 `responseFromResource`

3. **`responseFromResource` 回填**（`service_helpers.go`）：
   - `isOutputResource` 判断：**path 前缀 `/outputs/`**（`strings.HasPrefix(resource.Path, sandboxmount.OutputsRoot+"/")`，与 SQL `List` 的 `/outputs/` 判断一致）
   - Output Resource：从映射回填 `file_id`（Owned File 的 `external_id`，`file_...`）+ `mount_path`（`/outputs/<relative-path>`，来自 session_resources 的 `path` 字段）
   - Input Resource（payload 非空，`file_id` 在 payload）：保持现状
   - Output Resource 的 payload 为空，**跳过 `json.Unmarshal`**（空 payload 必然失败，白做）；直接建空 map

4. **`responseFromResource` 保持纯函数**，通过 `resourcesToResponses(resources, ownedFiles)` 传入已加载映射（不改为方法）

**性能结论**：`List`（去掉过滤）+ 1 次 `IN` 批量查询，共 2 次 DB 往返，O(session 资源数) 内存；相比逐条回填（N+1）节省大量往返。`MaxFileResources`（500）限制下内存可控。

### 改动 3（前端配套，本期范围外 → sub-issue/sub-PR）

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
| **N+1 风险** | Get Session 资源回填采用批量加载（1 次 `IN` 查询），避免逐条查询；`resourcesToResponses` 传入已加载映射 |
| **Output 识别** | `isOutputResource` 用 **path 前缀 `/outputs/`** 判断（`sandboxmount.OutputsRoot` 常量），与 SQL `List` 的 `/outputs/` 判断一致，不依赖 payload 空这个伴随特征 |
| **`OutputsRoot` 常量** | 新增 `sandboxmount.OutputsRoot = "/outputs"`（与 `FileSource = "/uploads"` 并列，挂载路径合同归属地） |
| **改过滤的回归风险** | `ListSessionResources` 过滤改为「用户资源 + `/outputs/` 下 Output 文件」后，目录、skill archive **仍被排除**（SQL 层保证）；前端 `ResourceTable` 依赖 `file_id`/`mount_path`，为 NULL 时显示 `—`（现状兜底，`SessionEntityPanels.tsx:428-430`） |
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
  - `ListSessionResources` 改过滤后，Output Resource 进入返回集合；**目录、skill archive 不进入**（负向）
  - `responseFromResource` 对 Output Resource 回填 `file_id`/`mount_path`；Input Resource、目录、archive 保持现状
  - 批量加载映射（`IN` 查询）正确性；`resourcesToResponses` 传入映射后无 N+1（用 mock DB 断言查询次数）
  - `visibleFilePredicate`：`/outputs` 下活动 Owned File 可见、已删除不可见、移出 `/outputs` 不可见（若现有 `db` 测试未覆盖则补）
  - 下载端点：`Downloadable=true` 放行、`false` 400
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
- **单资源接口返回 Output Resource**（已随本 PR 修复）：`GET /v1/sessions/{id}/resources/{rid}` 曾因 `FindByExternalID` 的 `payload IS NOT NULL` 过滤查不到 Output Resource——本次已改为与 `List` 相同的过滤（含 `expires_at`），`retrieveResourceRoute` 也加载 ownedFiles 映射回填 `file_id`（原 sub-issue #266 已关闭）
