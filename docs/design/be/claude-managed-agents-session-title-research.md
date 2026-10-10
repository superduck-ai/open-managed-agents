# Claude Managed Agents Session 标题契约核对

核对时间：2026-09-29。本文记录 Anthropic 公开文档中的合同及 OMA 本地复现，用于区分 Session 标题更新与 Agent 对话事件。

## 文档明确规定的行为

- 创建 Session 的 `POST /v1/sessions` 请求可带 `title: string | null`，字段可省略；文档称其为“Human-readable session title”，最大长度 500。[Create Session](https://platform.claude.com/docs/en/api/beta/sessions/create)
- 更新 Session 的 `POST /v1/sessions/{session_id}` 请求也可带 `title: string | null`；字符串长度为 1–500。官方示例通过该接口把标题设为 `Order #1234 inquiry`。[Update Session](https://platform.claude.com/docs/en/api/beta/sessions/update)
- 获取与列举 Session 的响应均包含 `title: string | null`。因此标题属于 Session 资源字段，而非从 `agent.message` 文本推断的字段。[Get Session](https://platform.claude.com/docs/en/api/beta/sessions/retrieve) · [List Sessions](https://platform.claude.com/docs/en/api/beta/sessions/list)
- `session.updated` 在 UpdateSession 请求实际改变至少一个字段时发出，只包含发生变化的字段；它的 `title: string | null` 仅在标题变化时出现。[List Events：`BetaManagedAgentsSessionUpdatedEvent`](https://platform.claude.com/docs/en/api/beta/sessions/events/list) · [事件类型参考](https://platform.claude.com/docs/en/managed-agents/reference)
- `agent.message` 的定义是 Agent 响应内容块。正常情况下，每个模型请求完成后才发布缓冲的 `agent.message`，它是对话响应的权威记录，增量只是预览。[事件类型参考](https://platform.claude.com/docs/en/managed-agents/reference) · [事件流与增量](https://platform.claude.com/docs/en/managed-agents/events-and-streaming#event-deltas)

## 自动生成标题：证据分级

**已证实：** 官方 Go SDK v1.68.0 的 `BetaSessionNewParams.Title` 是可选字段，`BetaSessionService.New` 只是把参数提交到 `POST /v1/sessions`，没有在 SDK 调用路径生成标题的代码。此结论只涵盖该版本 Go SDK 的客户端实现，不能推及服务端。[Go SDK `New` 方法](https://github.com/anthropics/anthropic-sdk-go/blob/v1.68.0/betasession.go#L51-L61) · [Go SDK 请求参数](https://github.com/anthropics/anthropic-sdk-go/blob/v1.68.0/betasession.go#L2454-L2462)

**可推断但未证实：** [创建指南](https://platform.claude.com/docs/en/managed-agents/sessions)以不带 `title` 的请求演示创建 Session；[创建 API schema](https://platform.claude.com/docs/en/api/beta/sessions/create)将 `title` 定为可选，响应标题允许 `null`。公开合同允许没有标题的 Session，但这不能证明服务端一定不会稍后自动补标题。

**未知：** 在已核对的 [Session 操作指南](https://platform.claude.com/docs/en/managed-agents/session-operations)、[事件流指南的 Console 说明](https://platform.claude.com/docs/en/managed-agents/events-and-streaming#console-observability)、[Claude Platform 发布说明](https://platform.claude.com/docs/en/release-notes/overview)、Session API schema 和上述 Go SDK 中，未找到“省略标题后按首条用户消息自动生成标题”的正面说明，也未找到明确否定此行为的声明。服务端是否有未公开的自动命名逻辑，单凭这些公开材料无法判定。

据已公开合同，`{"title":"..."}` 若作为 `agent.message` 的文本出现，仍是一次 Agent 消息事件，不能当作 Session 标题更新事件；标题变更应体现为 Session 资源的 `title`，由显式更新触发时还会有 `session.updated.title`。

## OMA 对齐决策

当前能作为兼容目标的行为是：创建 Session 时接受可选 `title`，未提供时允许返回 `null`；客户端可通过 Update Session 设置或清除标题，并从 Session 资源及 `session.updated` 观察变化。OMA 已有创建、更新与页面空标题回退路径；更新路由目前要求 Session 为 `idle`，这一额外限制应另行核对官方 Update Session 状态约束。不能因为 Claude Code 内部产生了标题辅助响应，就将其文本提升为 Managed Agents 的 Session 标题，也不能把它作为 Agent 对用户的回答。

如果产品要提供“首条消息后自动命名”，应把它定义为 OMA 自己的可选策略，并另行验证 Claude Managed Agents 的真实服务端行为后再声明兼容。该策略应更新 Session 资源字段，并遵守显式标题优先、只填充空标题和并发条件更新；产生的标题不能以 `agent.message` 进入对话历史。

## OMA 本地复现与请求归属

- 隔离测试通过与 Claude Code 标题辅助请求同类的非流式 JSON schema 模型请求，让假上游返回 `{"title":"展示一下markdown的能力"}`。没有上报任何 Worker `assistant` 事件，Session 历史仍持久化了对应 `agent.message`。测试连续运行三次，均在相同断言失败；这证明模型代理路径本身足以产生所见多余消息。测试使用独立 PostgreSQL schema，结束时清理。
- `internal/messages/model_request.go` 对所有 code-session 模型请求调用 `BeginModelRequest`，并将成功响应的 text block 交给 `EndModelRequest`；后者在 `internal/codesessions/model_requests.go` 发布为 `agent.message`。当前请求入口没有可用的标题辅助请求标志。
- Claude Code 的 `generateSessionTitle` 调用 `queryHaiku`，内部 `querySource` 为 `generate_session_title`，返回 JSON 后解析 title。SDK 控制请求路径返回的是 `control_response`，不是 Worker `assistant`。`querySource` 用于本地请求选项和日志，当前没有作为可供 OMA 判定的稳定请求字段发送。
- Worker 真正的 `assistant` 输出有 `type`、`uuid`、`message.id/content`，可能有 `request_id` 和 `parent_tool_use_id`；OMA 将 text block 映射为 `agent.message`，thinking 映射为 `agent.thinking`。`request_id` 关联模型请求，不能表明请求用途。代理生成的正常回答也没有 Worker `uuid`，因此仅按 `uuid` 存在与否过滤会误删正常回答。
- 真实 Worker 的 `chat.roundtrip` 基线通过：1 条用户输入、1 条最终回答、1 对模型请求 span，SSE 预览与最终消息一致，历史可恢复。该场景没有触发标题辅助请求，不能代替上面的定向复现。

## 处理边界与验证

**已采用的最小方案：关闭运行时的标题辅助请求。** [Claude Code 环境变量文档](https://code.claude.com/docs/en/env-vars)明确说明 `CLAUDE_CODE_DISABLE_TERMINAL_TITLE=1` 在 Agent SDK 和 `claude -p` 模式下还会跳过后台生成 Session 标题的小模型请求；[2.1.110 更新日志](https://code.claude.com/docs/en/changelog)记录了该模式遵守此开关的修复。OMA 在 `buildEnvironmentManagerV0Payload` 中固定注入该变量。Claude Code 2.1.251 的隔离 `chat.public` 验证经过真实 Runner 和 environment-manager，创建无标题 Session 并完成两轮对话；每轮历史只有正常回答和一对模型请求 span，没有标题 `agent.message` 或额外模型请求。`chat.roundtrip` 也通过。模型上游为脚本响应；测试不覆盖所有时序或外部云沙箱。

OMA 启动脚本已有 `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC` 默认值，但启用 OTLP 时会在启动 payload 中将它设为空值，以允许导出 telemetry。因此它不能作为持续关闭标题生成的单一开关，独立的 `CLAUDE_CODE_DISABLE_TERMINAL_TITLE` 更合适。

**仅当开关在实际 CCR Worker 路径无效时，再考虑请求用途标记。** 由发起模型请求的运行时显式传递用途，例如将 `generate_session_title` 标为 auxiliary；OMA 在模型代理入口识别后保留必要的请求生命周期与用量，但不把辅助响应发布到公开 Session 对话。若 Worker 也上报该请求的 `assistant`，须使用 `request_id` 关联同一用途并保持过滤一致。普通主线程和子线程请求继续发布真实 `agent.message`。当前安装的是官方 Claude Code 二进制，无法仅靠 OMA 后端直接给其内部 `querySource` 增加 HTTP 标记。

不要按响应文本是否是 `{"title":...}`、是否非流式、是否采用 JSON schema、`request_id` 前缀或 Worker `uuid` 过滤。这些字段都不能稳定表达“面向用户的 Agent 回复”。
