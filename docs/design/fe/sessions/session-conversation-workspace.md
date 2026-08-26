# Session 会话工作台设计

## 目标

Session 详情页同时承担两个职责：继续与运行中的智能体对话，以及检查这次运行实际绑定的上下文。页面沿用 OMA 的 shadcn/new-york 组件与主题令牌；外部产品仅用于验证信息架构，不作为视觉样式来源。

## 页面结构

会话摘要下方提供五个一级页签：

1. **事件**：复用现有 transcript/debug、事件筛选、搜索、线程 lane、minimap 和事件详情面板。
2. **资源**：展示 Session retrieve 响应中 `resources` 返回的挂载项。
3. **智能体**：展示会话固定的 Agent 版本、模型、系统提示词和工具类型。
4. **环境**：展示 Session 引用的 Environment、状态和网络模式。
5. **凭据**：展示 Session 关联的凭据保险库和凭据元数据。页面不得读取或展示 credential 的 secret 值。

一级页签保持一行平铺，不提供下拉菜单；内容使用全宽卡片和现有语义色，必须同时支持深色和浅色主题。

## 数据来源

| 区域     | 数据来源                         | 说明                                                         |
| -------- | -------------------------------- | ------------------------------------------------------------ |
| 会话摘要 | Session retrieve                 | 状态、Agent 引用、Environment ID、Vault IDs、用量和时间      |
| 事件     | Session/Thread events + SSE      | 保留现有缓存、补帧和实时状态机                               |
| 资源     | Session retrieve + File metadata | Session 返回挂载关系；文件名按 `file_id` 获取 Files metadata |
| 智能体   | Agent retrieve                   | 按 `session.agent` 中的 ID 和固定版本读取                    |
| 环境     | Environment retrieve             | 按 `environment_id` 读取                                     |
| 凭据     | Vault + credential list          | 按 `vault_ids` 读取元数据，禁止请求 credential secret        |

关联实体并行加载；一类关联实体失败时保留其余可用数据并显示不可用状态，不影响事件与对话。

进入资源页签时重新请求 Session retrieve，以获取后端最新的 `resources`。文件资源再按 `file_id` 请求 File metadata 获取文件名；不从 `mount_path` 推断名称，也不调用 Session resources list 或 Files list。事件流不触发资源刷新。

## 对话、停止与权限审批行为

- 输入框位于事件列表底部；选中事件后仍与右侧详情面板并存。
- 事件区在桌面宽度下固定为左侧事件列表、右侧详情；未选中事件时右侧保留空状态，避免布局跳变。
- `Enter` 发送，`Shift+Enter` 换行；输入法合成中和键盘长按不会触发发送。
- 空消息、发送中、已归档、已终止或已删除的 Session 不能发送。
- idle Session 仍允许发送新消息；running、queued 或 rescheduled Session 同时显示停止按钮。
- 发送普通消息使用既有 `user.message` 事件合同，停止使用 `user.interrupt`；当最新 `session.status_idle.stop_reason` 为 `requires_action` 时，按 `event_ids` 在右侧详情区域渲染可滚动的 Action Card。普通工具 Allow/Deny 发送 `user.tool_confirmation`，AskUserQuestion 问卷答案发送 `user.custom_tool_result`；等待期间保留并禁用左侧普通消息输入框。
- 成功发送后清空草稿，将响应中的已创建事件合并进现有缓存并恢复 SSE；响应未返回事件时才回退到刷新事件历史。失败保留草稿并使用详情页现有错误提示。
- Session 状态最终以 SSE/后端响应为准；发送后的临时 running 状态只用于恢复实时订阅，前端禁用状态只用于交互反馈，不替代后端权限检查。
- 流式预览继承 SSE envelope 的事件时间；Session 或 thread 进入 idle/terminated 时清理未完成预览，避免临时消息残留。
- 转录视图不重复展示与上一条 Agent 消息文本完全相同的 `session.status_idle.result`；Debug 视图仍保留两条原始事件。

## 非目标

- 不重写事件归一化、SSE 重连、thread lane 或 minimap 算法。
- 不在本功能中新增资源挂载、Agent 编辑、Environment 编辑或 Vault credential 编辑能力。
- 不展示密钥明文，不根据资源名称推断不存在的后端能力。
