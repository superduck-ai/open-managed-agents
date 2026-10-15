# Files API 协议兼容

`/v1/files` 的上传、列表、元数据读取、删除及 `/v1/files/{file_id}/content` 下载统一接受正式 Files API 请求，不要求 `anthropic-beta` 或 `beta=true`。列表的 `scope_id` 筛选也不设置 beta 头门禁。旧客户端仍可发送 `files-api-2025-04-14`、`managed-agents-2026-04-01` 和 `beta=true`，这些值不影响 Files 路由访问。

CMA 文档 `claude-managed-agents/files.md` 的上传与下载示例使用正式 `client.files`。其中 `scope_id` 示例仍要求 Managed Agents beta 头；OMA 为兼容现有客户端，对该筛选也不强制请求头。

此调整仅移除协议入口限制。API 鉴权、workspace 隔离、分页、上传大小与存储配额、文件引用保护以及上传文件不可下载的语义保持不变。Session 文件挂载与生成文件下载继续遵循现有 Filestore 合同。

`tests/files_api_test.go` 的 `TestFilesAPI` 覆盖无 beta 头和 query 的上传、列表、元数据、生成文件下载及删除，并以无 beta 头的列表请求覆盖 `scope_id` 与游标分页。既有旧 beta 上传等用例继续覆盖兼容性。回归入口为 `just test`；完整真实上传、挂载、模型读取及 SSE/历史一致性需另行执行 FILE-UP-001。
