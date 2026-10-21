# LLM 模型页

## 页面模型

LLM 模型页是工作区的模型目录，而不是 Provider 卡片墙。

页面只请求 `GET /api/console/organizations/{orgUuid}/workspaces/{workspaceId}/llm_providers`。Provider 配置及其 `model_ids` 是目录的唯一数据源，页面不再额外请求 `/v1/models`，避免同一页面出现两份模型集合和无法归属 Provider 的模型。

填写 Provider 的 Base URL 和 API Key 后，可在表单里点「获取模型列表」，请求 `POST .../llm_providers/preview_models`，用上游 `{base_url}/v1/models` 填入模型 ID。获取结果与表单内已有的非空模型 ID 合并、去重，不覆盖手工输入；上游返回空数组时只提示，不清空表单。编辑 Provider 时，读取接口不会返回旧 Key，因此只有用户输入新 Key 后才显示「获取模型列表」。

保存只提交表单中的模型 ID；点击页面刷新时才请求 `POST .../llm_providers/{id}/models/sync`，把上游列表合并进该 Provider 的 `model_ids`，随后重拉 Provider 列表。不同 Provider 可以配置同名模型，同步不会跳过其他 Provider 已有的模型。模型 ID 可以为空：Provider 仍保留在轨道和目录中，`/v1/models` 返回空数组，页面显示明确的“尚未配置模型”状态。获取上游列表或刷新失败时，用可关闭、数秒后自动消失的 toast 提示，不挡住目录和表单。

toast 由 App 根部唯一的共享 Toaster 承载，各功能页面只调用全局 `toast` API，不再重复挂载通知宿主。Toaster 通过 Portal 直接挂到 `body` 并统一显示在右下角，使通知脱离 `#root` 的隔离层叠上下文；Dialog 遮罩打开时，toast 仍位于遮罩和弹窗之上，不会被 backdrop blur 模糊。

```mermaid
flowchart LR
  Page[LLM models page] --> Providers[Console providers API]
  Providers --> Catalog[Grouped catalog]
  Catalog --> Rail[Provider rail]
  Catalog --> List[Searchable model rows]
```

## 布局

- 左侧是 Provider 轨道：`全部 Provider` 加上每个网关。默认看全部模型；点某个网关后显示其详情和编辑/删除。只有当前选中的网关被删除时，才回退到仅剩的那一个或全部。
- 右侧是模型目录：搜索框占位为「添加或搜索模型」；刷新会向每个可见 Provider 同步上游 `/v1/models`，再重拉本地目录。
- 「全部 Provider」和单个 Provider 使用同一套 Provider 标头：弱化背景上展示名称、完整 Base URL、脱敏 Key 和模型数量；单个 Provider 只额外显示编辑/删除操作。全部视图使用 shadcn Accordion（Base UI、`multiple`），可独立展开/收起多个 Provider，搜索时自动展开命中分组；Provider 分组之间仅保留低对比度的语义边框，避免出现继承文字颜色的粗重实线。
- Provider 标头在名称旁用等宽 outline Badge 展示 `slug`。
- 模型行是嵌套在 Provider 标头下的独立列表项：相对标头向右缩进，使用最小行高、行间分割线、可换行的等宽完整模型 ID `{slug}/{model_id}` 和复制按钮；复制内容即完整 ID，可直接用于 Agent、Workbench 或 Messages API。搜索同时匹配完整 ID 与 Provider 名称。Provider 名称和完整 URL 在目录标头中允许换行，左侧轨道继续截断以保持紧凑。右侧目录不设置与内容无关的固定高度，少量或零个模型时随内容自然收拢；主机名仍同时出现在左侧轨道，便于快速区分同名 Provider。

## 交互

- 模型配置只允许当前组织的 `admin` 管理。非管理员不显示侧栏入口；直接访问页面时展示无权限状态，且不请求 Provider API。Workbench、Quickstart 和 Agent 创建页缺少模型时，管理员看到「配置模型」，非管理员只看到「请联系组织管理员配置模型」。前端判断只用于避免无效入口，后端 RBAC 仍是最终权限边界。
- 搜索没有精确模型 ID 命中且已选中 Provider 时，可以把当前输入作为新的上游模型 ID 追加到该 Provider；精确命中只在当前选中 Provider 内判断，因此其他 Provider 已有同名模型时仍显示添加入口。即使已有模糊匹配也仍显示添加入口。
- Provider 表单在名称下方提供 `Slug` 字段。创建时在用户手动修改前随名称自动生成（小写，非法字符替换为 `-`，最长 32 字符；中文等无法转换的名称得到空值），留空时由服务端分配 `default`/`default-N`；提交前按 `^[a-z0-9][a-z0-9._-]{0,31}$` 本地校验。编辑时只读展示，局部更新仍回传原 slug。
- 添加模型的请求进行中时禁用添加按钮，防止重复提交。
- 添加、编辑、删除 Provider 仍走原有 Dialog；编辑时留空 API Key 表示保留原密钥，且不会使用不可见的旧 Key 发起模型发现。
- Provider API 的稳定校验错误在前端映射为中英文文案，动态模型 ID 作为插值保留；未知或网络错误使用本地化的通用失败提示，不直接展示后端英文原文。
- 模型 ID 是可选配置；从 Provider 移除全部模型后，Provider 和加密 Key 仍保留，可通过获取列表、搜索添加或再次编辑恢复模型。
- 没有 Provider 时展示精简空状态：隐藏无意义的模型数 `0` 和顶部重复操作，只保留中央的「添加 Provider」入口。

## 不变边界

- Provider 的 `model_ids` 仍保存并编辑上游真实 ID；页面只在展示与复制时拼接 `{slug}/`，提交时不带前缀。
- 明文 Key 不进入前端存储；读取接口只展示 `api_key_last4`。
- 不新增独立 Model 表；启用集合仍然是 Provider 上的 `model_ids`。

## API backend

Provider 表单使用共享 shadcn Select 选择 `messages` 或 `chat_completions`。已有配置默认为
Messages，编辑时保留当前值；Responses 暂不展示也不接受保存。选择 Chat Completions 时显示仅支持
Brain 模式，并显示服务端追加的 `/v1/chat/completions` 路径，Messages 显示 `/v1/messages`。
Base URL 仍填写 API 根地址，允许路径前缀，不重复填写 `/v1`。

创建、编辑和模型预览请求均携带 `api_backend`；模型同步由后端使用已保存的 backend。
模型增删等局部操作的完整 Provider 更新也必须保留该字段，避免将 Chat Completions 配置重置为 Messages。
组件测试覆盖选择 backend、模型发现、创建与编辑保留，并断言 Responses 选项不可用。
