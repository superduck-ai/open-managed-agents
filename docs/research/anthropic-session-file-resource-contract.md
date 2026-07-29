# Anthropic Session File Resource 合同研究

## 研究问题

核对 Anthropic 官方 Managed Agents / Sessions Resource / Files 合同，回答以下问题：

1. attach File 到 Session 时，请求与响应中的 `file_id` 是否保持原 workspace File identity，还是创建新的 session-scoped File identity。
2. `files.list(scope_id=session)` 返回什么 ID。
3. 删除与配额语义如何。

## 结论

### 1. attach 时会创建新的 session-scoped `file_id`

官方 Adding files 文档明确说明：把已上传的文件挂到 session 时，API 会为该 session 中的文件实例创建一个新的 `file_id`。也就是说：

- 请求里的 `file_id` 指向的是已存在的 workspace / Files API 文件。
- 响应里的 `file_id` 指向的是 session 内部的新文件身份，不是原始 workspace 文件身份。
- 该 session 复制件不计入存储配额。

### 2. `files.list(scope_id=session)` 返回 session 范围内的 `FileMetadata.id`

官方 Files API 文档和 SDK 都把 `scope_id` 定义为按 scope 过滤文件；`scope` 里可以是 session。返回项是 `FileMetadata`，其 `id` 是文件对象自身的标识，不是 session resource 的 `id`。

因此，`files.list(scope_id=session)` 返回的是：

- session 范围内文件对象的 `FileMetadata.id`
- 这些文件对象带有 session scope 信息

### 3. 删除语义分成两层

- `sessions.resources.delete(resource_id)` 删除的是 session resource，返回 `session_resource_deleted`。
- `files.delete(file_id)` 删除的是 Files API 的文件对象，返回 `file_deleted`。

公开文档没有把“删除 session resource”解释为“删除原 workspace 文件”。从合同层面只能确认：

- session 资源删除与 Files API 文件删除是两个不同操作。
- attach 生成的 session 复制件是 session scoped 的文件对象。
- session 复制件不计入存储限制。

### 4. 配额语义

官方文档明确给出两个限制：

- 每个 session 最多可挂载 500 个文件。
- session 中创建的文件复制件不计入存储限制。

## 公开合同 vs. 内部实现

### 公开合同可以确定的内容

- attach 请求使用原始 Files API 文件的 `file_id`。
- attach 响应生成新的 session-scoped `file_id`。
- `files.list(scope_id=session)` 返回 session scope 下的文件对象列表。
- session 资源删除和文件删除是两条不同的 API 路径。
- session 复制件不计入存储限制。

### 不能从合同直接推出的内容

- 是否必须在内部单独建一张 `files` 表。
- session 文件是否与 workspace 文件复用同一物理记录。
- 删除 session resource 时，底层存储如何实现级联。

这些都属于实现细节，只能从仓库当前代码或数据库模型判断，不能从公开合同反推为必然要求。

## 项目落地决策

本项目选择更小的内部与公开模型，明确不实现官方“attach 返回新 session-scoped
`file_id`”这一点：

- 请求 `file_id` 解析为 Source File UUID，写入 Resource 的 `file_uuid`。
- 响应继续返回 Source File ID，不生成 Alias，也不新增 File 行。
- 同一 Source 多次 attach 由不同 `sesrsc_` 和 path 区分；Catalog 按真实 File 去重。
- metadata/download 只接受真实 File ID；Source File 是唯一文件元数据与对象事实。
- Input attach 不复制对象、不计费；删除 Resource 只删除一次 Attach。
- 活动 Resource 存在时，Files delete 拒绝删除 Source File。
- `/outputs` 的真实新对象创建 Owned File，并由内部 Resource 引用。

该取舍保持 Sessions Resource 与 Files 两套删除语义清晰：
`sessions.resources.delete(sesrsc_...)` 删除挂载，`files.delete(file_...)` 删除真实文件。
代价是 attach 响应与官方当前的新 `file_id` 行为不同；这是项目当前已接受的兼容性偏差，
不应再通过隐式 Alias 模拟。

## 证据

### 官方文档

- Adding files to sessions: https://platform.claude.com/docs/en/managed-agents/files
  - 文档明确写出 attach 会创建新的 `file_id`，并说明 session 复制件不计入存储限制。
- Files API reference: https://platform.claude.com/docs/en/api/beta/files
  - 文档说明可以按 `scope_id` 列出某个 scope 下的文件，并支持 session scope。

### 官方 SDK 源码

- TypeScript SDK `src/resources/beta/sessions/resources.ts`: https://github.com/anthropics/anthropic-sdk-typescript/blob/main/src/resources/beta/sessions/resources.ts
  - `add` 的参数要求是“已上传文件”的 `file_id`。
  - 返回对象包含 session resource `id` 与 `file_id`。
  - `delete` 返回 `session_resource_deleted`。
- TypeScript SDK `src/resources/beta/files.ts`: https://github.com/anthropics/anthropic-sdk-typescript/blob/main/src/resources/beta/files.ts
  - `FileMetadata` 代表文件对象本身，包含 `id`、`scope` 等字段。
  - `list` 支持按 `scope_id` 过滤文件。
  - `delete` 返回 `file_deleted`。
- Python SDK `src/anthropic/resources/beta/files.py`: https://github.com/anthropics/anthropic-sdk-python/blob/main/src/anthropic/resources/beta/files.py
  - 同样把 `scope_id` 作为 Files 列表过滤条件。
  - 文件对象元数据与 scope 信息分离，支持 session scope。

## 版本备注

- 以上结论基于 Anthropic 官方当前公开的 beta 文档与官方 SDK 源码。
- 由于这是 beta 合同，后续版本可能调整字段名、响应对象或删除语义；如果升级 beta 版本，应重新核对上述三个链接。
