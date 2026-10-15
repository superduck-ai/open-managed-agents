# Files API 协议兼容

`/v1/files` 的上传、列表、元数据读取、删除及 `/v1/files/{file_id}/content` 下载统一接受正式 Files API 请求，不要求 `anthropic-beta` 或 `beta=true`。列表的 `scope_id` 筛选也不设置 beta 头门禁。旧客户端仍可发送 `files-api-2025-04-14`、`managed-agents-2026-04-01` 和 `beta=true`，这些值不影响 Files 路由访问。

CMA 文档 `claude-managed-agents/files.md` 的上传与下载示例使用正式 `client.files`。其中 `scope_id` 示例仍要求 Managed Agents beta 头；OMA 为兼容现有客户端，对该筛选也不强制请求头。

取消 beta 门禁的调整保留了既有分页。列表现进一步兼容正式 SDK 的 `next_page/page`，同时保留 `first_id/last_id/has_more` 与 `after_id/before_id`。API 鉴权、workspace 隔离、上传大小与存储配额、文件引用保护以及上传文件不可下载的语义保持不变。Session 文件挂载与生成文件下载继续遵循现有 Filestore 合同。

列表响应始终包含 `next_page`；有后续记录时返回以 `page_` 开头的不透明游标，末页和空列表返回显式 `null`。调用方将其原样传入 `page`，并保留 `scope_id` 等筛选参数。游标封装文件 external ID、scope 和方向，复用现有 workspace/scope 限定的 Yourbatis 键集分页，不新增 SQL 或偏移分页。普通续页使用最后一条记录，`before_id` 的续页使用第一条记录并保留向前方向。旧客户端仍可直接传 `after_id` 或 `before_id`，两者同时出现时维持 `after_id` 优先。

非空 `page` 不得与非空 `after_id/before_id` 混用。格式错误、缺少文件 ID 或与请求 `scope_id` 不匹配的 `page` 返回 Anthropic 结构的 `400 invalid_request_error`，不会回退到首页或账户列表。游标不提供权限，文件定位和结果查询仍使用当前请求的 workspace 与 scope；不可见或已删除的锚点沿用旧合同返回空页。

`internal/files/pagination_test.go` 覆盖游标解析、格式和 scope 校验、混用拒绝及旧参数保留；`tests/files_pagination_test.go` 使用真实 PostgreSQL 与正式 Go SDK 自动遍历，覆盖逐页无重漏、末页、空页、旧/新正向游标等价、反向续页与跨 workspace 隔离。FILE-API-008 的 OMA 单例用于真实上传和正式分页复测，保留旧失败报告并输出新报告。FILE-API-009 的事件差异与严格比较规则不受本调整影响。

上传文件名接受双引号和百分号，例如 `quote"name.txt` 与 `percent%25.txt`，与 FILE-UP-010 的 Qoder 实测接受行为对齐。multipart 解析后的文件名原样保存并返回元数据；对象存储键继续通过 `sanitizeForKey` 单独转换，双引号转换为下划线。正式 Files API 与平台上传共用此校验。空文件名、超过 255 字节、无效 UTF-8、控制字符以及 `<>:|?*`、正反斜杠仍被校验拒绝；上传入口既有的空文件名回退保持不变。

`internal/files/handler_test.go` 覆盖双引号与百分号接受、既有非法名称拒绝，以及 multipart 双引号转义解析和对象键转换。双端上传、Session 挂载读取及 SSE/历史一致性需单独复测 FILE-UP-010；此兼容调整不把发现性观察升级为正式 CMA 契约。

`tests/files_api_test.go` 的 `TestFilesAPI` 覆盖无 beta 头和 query 的上传、列表、元数据、生成文件下载及删除，并以无 beta 头的列表请求覆盖 `scope_id` 与游标分页。既有旧 beta 上传等用例继续覆盖兼容性。回归入口为 `just test`；完整真实上传、挂载、模型读取及 SSE/历史一致性需另行执行 FILE-UP-001。

## Session scope 与账户上传

2026-10-15 核对最新 [CMA Adding files](https://platform.claude.com/docs/en/managed-agents/files) 与 [Beta Files list](https://platform.claude.com/docs/en/api/beta/files/list)。前者在 Mounting files in a session 中明确挂载会创建新的 Session 文件实例 ID；后者将 `scope_id` 限定为指定作用域的文件，并把 metadata 的 `scope` 定义为文件创建时的上下文。原上传 ID 不因 attach 自动变成 Session scope 的文件，不能直接复用该 ID 并在列表响应中改写其 scope。

本次修正 FILE-API-010 涉及的列表差异：Session Catalog 只投影活动 `/outputs/` Owned File，不再投影 `/uploads/` Source File 引用。未分页列表、分页列表和游标定位共用 `catalogResourceCTE`，因此 Source File ID 不会在任何 Session 分页方向泄漏，也不能作为 Session 的分页锚点。账户列表、原 ID 的 metadata/download 策略及活动 Resource 的删除保护保持原合同。测试器保留“账户列表包含原上传 ID、Session 列表包含输出 ID且排除原上传 ID”的独立断言。

兼容范围必须区分：当前 OMA Input attach 仍返回 Source File ID，尚未实现 CMA 的独立 Session 输入实例 ID；本次只修正列表错误投影，不代表输入实例生命周期、metadata/download/delete 等已完全兼容 CMA。官方文档没有逐项列举 Session 列表必须包含哪些输入实例，不能把本次输出投影视为“所有 Session 列表只能包含输出”的官方保证。

`tests/files_session_scope_test.go` 在真实 PostgreSQL 上覆盖 Input-only 空列表、账户 Source 可见性、挂载输入和多个输出共存时的正式 SDK 分页、未分页读取、上传 ID 游标隔离及原 metadata 不变。`internal/db/file_mapper_test.go` 覆盖三个查询共用的租户绑定和可见性 SQL。现有 Session 输入测试同步改为检查账户上传不进入 Session Catalog，保留挂载身份、只读、配额及活动引用保护检查。
