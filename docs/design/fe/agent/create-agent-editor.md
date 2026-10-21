# Create Agent 双模式配置编辑器

## 目标

Create Agent 独立页面同时提供结构化 `Rendered` 表单和 YAML/JSON `Raw` 编辑器。两种视图只编辑同一份 Draft，避免切换视图、切换格式或使用模板时出现字段丢失。

本期沿用既有 `model: string | {id, speed}` 合同，不展示或接受 `model.effort`。Agents API 继续接收内嵌的 `mcp_servers` 快照；可复用自定义 MCP 由独立的工作区资源提供候选。

## 状态流

```mermaid
flowchart LR
  Template["模板或 Describe 生成"] -->|整体替换| Draft["CreateAgentDraft"]
  Rendered["Rendered 表单"] -->|纯函数更新| Draft
  Draft -->|序列化| Raw["Raw YAML / JSON"]
  Raw -->|严格校验成功后原子替换| Draft
  Draft -->|校验通过| Create["POST /v1/agents"]
```

- Draft 是唯一合法配置来源；Rendered 不保存字段副本。
- 进入 Raw 时由 Draft 重新序列化。格式切换也从 Draft 生成，不做文本级 YAML/JSON 转换。
- Raw 输入无效时保留最后一份合法 Draft，禁止切回 Rendered、切换格式、创建，以及通过模板或 Describe 生成覆盖这段未保存文本；生成请求完成时也会重新检查 Raw 状态。
- Raw 合法时，模板选择与 Describe 生成成功会整体替换 Draft、清除错误并折叠 Starting point。
- 取消或离开创建页直接丢弃 Draft；生成请求会随组件卸载中止。
- Escape 不退出独立创建页。取消返回列表；创建成功直接进入新 Agent 的配置页，不先导航到列表页。

## 配置不变量

### Multiagent 与 Skills

- Multiagent 最多包含 20 个引用（`self` 也计入上限）；Agent 引用不得重复，并以 `{type:"agent", id, version}` 固定引用选择时的当前版本。
- Rendered 不主动提供 `self`，但能够展示、保留和移除合法 Raw 中的 `{type:"self"}`。
- Agent 选择器初始加载当前工作区前 20 个候选；输入后以 300ms 防抖按名称调用服务端搜索，符合完整 Agent ID 规则时直接精确读取，不受首屏候选数量限制。
- Skills 最多 20 个；新增选择写入 `{type, skill_id, version:"latest"}`，切换其他 Skill 时保留既有条目原始的可选 `version` 字段。
- 新建 Agent 或 Skill 使用新标签页；原创建页保留 Draft，并在重新获得焦点时刷新候选项。

### MCP 与 Tools

- “添加 MCP 服务器”沿用 Popover，并提供 Directory 与“自定义 MCP”两个候选页签。自定义页签只列出当前工作区未删除的 MCP 配置目录项，不在 Agent 表单内编辑名称或 URL；“创建 MCP 服务器”会在新标签页打开 `/workspaces/{workspaceId}/mcp-servers/new`，原 Draft 保留，窗口重新获得焦点时刷新候选。
- 选择 Directory 或工作区 MCP 配置时，前端把当时的 `{name,type:"url",url}` 复制进 Agent Draft，并原子添加同名 `mcp_toolset`；Agent 不保存工作区 MCP 目录项 ID。删除 Agent Draft 中的 MCP 时原子删除 server 与 toolset。管理目录项后续修改或删除不会改写已有 Agent 版本，删除目录项也不检查 Agent 使用情况。
- Agent 名称与 URL 的 Draft 校验保持不变：名称 trim 后必填、最长 255 个字符、只允许字母、数字、下划线、连字符和句点、不得包含 `__`，并在当前 Agent 内大小写敏感唯一；URL trim 后必填、最长 2048 个字符，必须是不含内嵌凭据或 fragment 的 HTTP/HTTPS 绝对地址；每个 Agent 最多 20 个 MCP Server。
- 创建阶段只使用 Directory `tool_names`，不调用依赖已创建 Agent ID 的动态 catalog API；工作区自定义 MCP 不探测工具列表，只提供 Toolset 级权限。
- MCP 候选项优先加载 Directory 明确提供的 HTTP/HTTPS 图片 `icon_url`；若该字段是网页或图片加载失败，则依次尝试其同源 `/favicon.ico` 和基于该 Directory 公开主机名的公共 favicon 服务，仍不可用时回退到 Server 图标。自定义 MCP 不向图标组件提供 URL，因此前端不会探测 Agent 配置的 MCP 主机，也不会把自定义主机名发送给第三方。
- Directory 与工作区 MCP 查询相互独立；任一来源失败都不会阻止另一页签选择，并分别提供重试。
- 工作区候选与当前 Agent 已有 MCP 同名但 URL 不同时，不允许静默覆盖；候选保持不可添加并展示包含冲突名称的明确提示。
- 合法 Raw 或既有 Agent 中未出现在工作区资源列表里的历史 MCP 仍在 Rendered 中按 Agent 快照回显，可调整权限、保存或移除，不会被强制迁移或丢弃。
- Tunnel 在 Picker 中仍以一个 Tunnel 一个候选项展示。选择后在 MCP 工具卡中固定展示 Channel Combobox 和连接状态，
  Channel 下方以无独立边框的弱提示行展示解析后的 canonical MCP URL，再展示工具权限；不使用独立
  Channel Dialog，也不按实时 Channel 拆成多个 Picker 候选。
- Tunnel 恰好一个实时 Channel 时自动选择实际值；多于一个或没有实时 Channel 时生成不进入 Draft 的待确认卡片，
  Channel 初始为空并自动聚焦。`main` 在没有实时 Channel 时仅作为 placeholder/建议，不作为默认值。
- 待确认 Tunnel Channel 会阻止创建和切换 Raw；取消不修改 Draft，模板或 Describe 整体替换 Draft 时清除待确认状态。
  同一 Tunnel 可以配置多个不同 Channel，同一 Tunnel + Channel 不得重复。
- 已配置 Tunnel 的 Channel 使用本地编辑缓冲；Apply 或选择有效建议后，原子更新 `mcp_server` 的名称和 URL 以及
  `mcp_toolset.mcp_server_name`，保留权限与顺序，并在已连接时重新发现工具。
- 内置工具展示当前 23 项可选工具。列表优先展示 8 项：`bash`、`read`、`write`、`edit`、`glob`、`grep`、`web_fetch`、`web_search`，随后展示 `task`、`ask_user_question`、`cron_create`、`cron_delete`、`cron_list`、`enter_plan_mode`、`enter_worktree`、`exit_plan_mode`、`exit_worktree`、`notebook_edit`、`schedule_wakeup`、`skill`、`task_output`、`task_stop`、`todo_write`。除 `ask_user_question` 默认关闭并要求用户主动开启外，其余工具默认 `always_allow`；这样纯 API 调用不会进入无人处理的 HITL 等待。Brain 的 `web_search` / `web_fetch` 请求在 oma-server 执行，外部 Claude Code 的 `WebFetch` 仍按客户端工具处理，不表示 Messages API 的模型服务端工具；Rendered 与 Raw 均支持内置 `web_search`；Web 工具域名策略和 Fetch 内容预算保留在配置中；新 MCP 默认 `always_ask`。
- 内置 Toolset 可以整体移除，并可通过“添加内置工具”恢复；恢复操作不会复制已存在的 Toolset。
- Toolset 级权限写入 `default_config` 并重置逐工具权限；Web 工具的域名策略和内容预算保留；内置工具集会额外保留一条与分组权限一致的 `ask_user_question` 显式配置，避免提交规范化把它静默改回 deny。逐工具权限与默认值一致时不保留冗余覆盖，但 `ask_user_question` 即使选 `always_allow` 也保留显式配置。
- `always_deny` 规范化为 `enabled:false`；`custom` 只是聚合展示状态，不写入 API。
- Rendered 不再提供新增 Custom Tool 的入口；Raw、模板或既有 Agent 中合法的 Custom Tool 仍可在 Rendered 中编辑和移除，并在视图往返时保留。
- Custom Tool 名称必须唯一且符合后端命名规则，描述与 JSON object `input_schema` 必须有效；Schema 输入框保留用户原始文本与光标，仅把合法 JSON 解析结果发布到 Draft。
- Raw codec 使用与 Agents API 一致的 Tool 判别联合：MCP Server 仅接受不含凭据或 fragment 的 HTTP/HTTPS `type:"url"` 地址，权限策略仅接受 `always_allow`/`always_ask`，Custom Tool 的 `input_schema.type` 必须为 `object`；内置 toolset 与同一 MCP Server 的 toolset 均不得重复。

## 数据与组件边界

- 页面入口负责模型映射、模型目录加载、创建提交和导航。
- Draft hook 负责 Rendered/Raw/格式状态及严格 codec。
- Rendered 各区块只调用 Draft 纯函数，不直接构造请求体。
- Models、Agents、Skills、Directory 查询经 feature API 适配；工作区 MCP 候选通过 Console API 加载。Models 通过 Anthropic `/v1` SDK client 加载，与 `/api` Console client 保持边界分离。
- 工具权限读语义复用 Agent 详情页模型，写语义集中在创建 Draft 模型中。
- 创建入口为 `/workspaces/{workspaceId}/agents/new`，也支持 `/agents/new`；旧的 `?create_agent=1` 入口规范化到工作区创建路径，静态 `new` 路由优先于 Agent 详情路由。列表点击创建、MCP/多智能体选择器中的新建 Agent 链接与直接刷新都进入同一页面。
- 创建页使用 shadcn Breadcrumb、Collapsible、Tabs、Input Group 和表单控件。顶部面包屑位于控制台内容左侧，主表单居中且最大宽度为 824px；General 区域采用 244px 说明列与自适应字段列，窄屏自动单列。
- Starting point 去除嵌套卡片外框，描述输入为紧凑文本框，生成按钮独立置于右下。模板网格在手机、平板、桌面分别为一、二、三列。
- 创建页的 Tools、Skills、Multiagent 默认折叠，按 Tools、Skills、Multiagent 顺序展示；折叠时显示配置摘要。Collapsible Content 使用 `keepMounted`，保留工具编辑缓冲和未确认的 Tunnel Channel，折叠不能绕过待确认校验。详情页编辑继续使用原来的展开式布局。
- Raw/Rendered 仍共用 Draft 与现有 CodeMirror；未支持的模型 effort 字段不因参考截图而加入 API 合同。底部取消与创建按钮在页面滚动时保持可访问，创建失败保留 Draft，并显示完整错误。

## 验收

- YAML 与 JSON 可往返全部支持字段，未知顶层字段和 `model.effort` 被拒绝。
- Rendered 可完成 General、Multiagent、Skills、内置工具与 Directory/工作区 MCP 选择；表单中不再出现自定义 MCP 名称或 URL 输入。既有 Custom Tool 与历史 MCP 可继续编辑权限、保存或移除。
- MCP 与 toolset 始终成对；Tunnel Channel 的待确认状态不会污染 Draft，Channel 迁移不会丢失权限；权限聚合和 deny 序列化与运行时一致。
- 模型、候选 Agent、Skills 和 Directory 加载失败都有可重试状态。
- 页面支持键盘导航、浅深主题和窄屏单列布局。
