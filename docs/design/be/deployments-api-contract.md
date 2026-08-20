# Deployments API 合同

OMA 按照 Anthropic 文档中的规范 `/v1` 路径提供 Claude beta Deployments 和 Deployment Runs HTTP 接口：

- `POST /v1/deployments`
- `GET /v1/deployments`
- `GET|POST /v1/deployments/{deployment_id}`
- `POST /v1/deployments/{deployment_id}/archive`
- `POST /v1/deployments/{deployment_id}/pause`
- `POST /v1/deployments/{deployment_id}/unpause`
- `POST /v1/deployments/{deployment_id}/run`
- `GET /v1/deployment_runs`
- `GET /v1/deployment_runs/{deployment_run_id}`

API 密钥请求必须携带 `anthropic-version: 2023-06-01`，并在 `anthropic-beta` 中包含 `managed-agents-2026-04-01`。官方 SDK 额外发送的 `?beta=true` 查询参数仍然兼容，但不是必需项。供内部 Web 应用使用的平台会话请求可以继续通过 `?beta=true` 启用该接口。

## 请求与响应边界

- 新建 Deployment 的 ID 使用官方 `depl_` 前缀；Deployment Run 的 ID 使用 `drun_` 前缀。
- 响应中的 `description` 始终为字符串；未设置或清空后返回空字符串。
- 创建请求中的 `metadata`、`resources` 或 `vault_ids` 整体为 `null` 时返回拒绝。
- 字符串形式的 Agent 引用固定到最新版本；对象形式必须包含 `type: "agent"` 和 `id`，省略 `version` 时同样固定到最新版本。
- 元数据最多包含 16 个键；每个键最多 64 个字符，每个值最多 512 个字符。
- `user.message.content` 是至少包含一个受支持内容块的数组；`system.message` 只接受至少一个文本块，最多出现一次，并且必须作为最后一个事件紧跟在 `user.message` 之后；`user.define_outcome.rubric` 必须是文件或文本评分标准对象。
- GitHub 资源必须提供只写的 `authorization_token`；Memory Store 的 `instructions` 必须是最多 4096 个字符的字符串。
- File 资源响应会省略内部字段 `source`，并把 `mount_path` 统一映射到 `/uploads` 命名空间；请求未传 `mount_path` 时默认使用 `/uploads/<filename>`（文件名缺失时回退到 `file_id`），显式传入的挂载路径也映射为 `/uploads/<相对路径>`，与 Session 资源响应一致。
- 创建或更新 Deployment 时引用不存在的 File 返回 `404 not_found_error`。
- Deployment Run 将已规范化 resources 一次解码为命名结构，并在创建 Session 前重新查询 File、Memory Store 等可变引用；FileSpec 与查询到的文件记录直接携带到物化阶段，不再经由松散 map 或重复路径校验。
- Deployment 更新请求中的元数据使用字符串（包括空字符串）新增或覆盖键，使用键级别的 `null` 删除键。
- Deployment 更新请求拒绝整个字段为 `metadata: null`；省略 `metadata` 时保留原值，传入 `{}` 时不做修改。
- 列表请求同时出现 `status` 和 `include_archived` 时返回拒绝，包括 `include_archived=false`。
- 已暂停的 Deployment 仍可手动运行。手动运行不会解除暂停；已归档的 Deployment 不能运行。

资源数量限制遵循官方的聚合合同，共享常量位于 `internal/sessioncontract`：

| 限制 | 常量 | Claude 合同 | OMA |
| --- | --- | ---: | ---: |
| 资源总数 | `MaxResources` | 500 | 500 |
| File 资源数量 | `MaxFileResources`（等于 `MaxResources`） | 没有单独的更低上限 | 最多 500 |

该限制作用于顶层混合类型的 `resources` 数组。OMA 不额外设置更低的 File 资源上限，Session 实体化也使用相同上限，因此包含 500 个 File 资源的 Deployment 在创建成功后仍然可以运行。File 资源的响应配置可以直接作为更新输入；替换 GitHub 资源时必须重新提供只写的 `authorization_token`。

更新请求会区分省略字段和显式清空：

| 字段 | 省略 | `null` | 空值形式 |
| --- | --- | --- | --- |
| `description` | 保留 | 清空 | `""` 清空 |
| `resources` | 保留 | 清空 | `[]` 清空 |
| `vault_ids` | 保留 | 清空 | `[]` 清空 |
| `metadata` | 保留 | 拒绝 | `{}` 不做修改 |
| `schedule` | 保留 | OMA 清空；官方未说明 | 不适用 |

公开合同没有说明的模糊行为保持不变，包括 `limit=0`、`schedule:null`、默认列表顺序，以及未记录的错误状态和幂等行为。

本次对齐仅涉及 HTTP/API 边界和手动运行的状态检查，不新增调度器、数据库迁移、Filestore 或 Sandbox 投影变更、自动暂停行为及其他运行时功能。

主要参考资料：

- <https://platform.claude.com/docs/en/api/beta/deployments>
- <https://platform.claude.com/docs/en/api/beta/deployment_runs>
