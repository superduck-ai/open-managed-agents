# Issue #67：Environment 创建、详情与编辑页面国际化规格

## Problem Statement

当前 Environment 创建、详情与编辑流程仍包含硬编码英文文案，Networking、Packages、Metadata、Work queue、状态、校验与错误反馈未完整接入现有 i18n Module。中英文用户因此会看到不一致的字段含义、状态表达与失败反馈；编辑表单也缺少可靠的未保存修改保护，Metadata 的客户端说明和序列化行为与后端合同不一致。

本任务以当前 `main` 的 Environment 能力为边界，只完善既有页面和交互。Environment 更新继续在同一 `environment_id` 上原地生效，并只影响此后创建的 Sandbox；不提前引入 Runtime Version、Environment Version、Snapshot、History、Diff 或 Rollback。

## Solution

- 将 Environment 创建对话框、列表、详情、编辑器、归档只读提示、删除确认、Work queue、加载/空/成功/失败状态的用户可见文案接入现有 `useI18n` 和 message catalogs。
- 通用动作与字段使用 `common.*`，Managed Agents 跨资源语义使用 `managedAgents.common.*`，Environment 专属文案使用 `managedAgents.environments.*`；English 与 `zh-CN` catalogs 保持 key parity，不引入第二套 i18n API。
- 中文界面统一采用：Environment「环境」、Networking「网络访问」、Packages「软件包」、Package manager「包管理器」、Metadata「元数据」、Work queue「工作队列」、Work「工作项」、Sandbox「沙箱」、Cloud「云端」、Unrestricted「不受限」、Limited「受限」、Workspace「工作区」。API 字段名、枚举值、ID、`pip`、`npm` 等保持原文。
- 创建能力保持不变：对话框只展示 Name、Hosting type 和 Description，仍提交默认 unrestricted Networking、空 Packages 与空 Metadata。
- Environment 列表、详情和 Work queue 的相对时间使用现有基于 `Intl.RelativeTimeFormat` 的 locale-aware formatter，不扩展为全应用时间重构。
- 编辑表单保存初始基线并计算 dirty 状态。未修改时允许直接关闭；有修改时，应用内取消、关闭和可拦截导航显示本地化 `AlertDialog`，刷新、关闭标签页或离站使用原生 `beforeunload`。保存成功后解除保护；保存期间禁止关闭。
- 提交时显示本地化 Saving 状态，并在 handler 入口使用 `submitting` guard。连续提交只产生一个请求和一个成功 Toast，不额外显示“重复提交”错误；请求失败保留输入并允许重试。
- 客户端校验与当前 Go 后端 `len` 语义一致：Name trim 后非空；单个 Package token 经 UTF-8 编码后最长 255 字节；Metadata 最多 16 项；key 必填且经 UTF-8 编码后最长 64 字节；value 经 UTF-8 编码后最长 512 字节；重复 key 拒绝提交，避免 map 序列化时静默覆盖。不限制 lowercase，并替换现有不准确的 lowercase 帮助文案。
- 已知 Environment 校验、冲突和资源错误提供完整中英文映射。未知后端错误显示本地化的操作摘要，同时保留安全的服务端错误详情用于诊断。
- 归档 Environment 显示明确的本地化只读 Alert，Edit 按钮保留但禁用，配置和 Work queue 继续可见；Delete 保持现有 API 与确认行为。
- Work queue 本地化 `queued`、`starting`、`active`、`stopping`、`stopped`、`failed`。未知状态保留原值并安全 humanize，避免新状态显示为空。

## User Stories

- 作为中文用户，我可以在 Environment 创建、列表、详情和编辑流程中看到含义一致、术语统一的中文文案。
- 作为英文用户，我可以在相同流程中看到完整英文文案，不会混入硬编码的另一种语言。
- 作为创建 Environment 的用户，我仍然只需填写 Name、Hosting type 和 Description，既有默认配置与 API payload 不发生变化。
- 作为编辑 Environment 的用户，我会在提交前看到与后端合同一致的字段校验，并且重复 Metadata key 不会静默丢失数据。
- 作为正在编辑的用户，我在关闭、取消或离开 dirty 表单时会收到确认，保存成功或未修改时不会收到多余提示。
- 作为网络较慢时重复点击保存的用户，我只会触发一次请求和一次成功反馈；失败后仍可保留输入重试。
- 作为查看归档 Environment 的用户，我能明确知道它是只读的，同时仍可查看 Networking、Packages、Metadata 和 Work queue。
- 作为排查 Environment 工作项的用户，我能按当前语言理解已知 Work 状态；遇到后端新增状态时仍能看到可辨认的原始状态。
- 作为遇到失败的用户，我能看到本地化的操作上下文；若错误未知，服务端详情仍被保留以便诊断。

## Implementation Decisions

1. 保持当前 API、路由、鉴权、删除语义和 Environment 原地更新语义，不修改 Go 后端合同。
2. 在现有 Managed Agents feature slice 内完成 i18n key、文案映射、校验、错误转换和 dirty/submitting 状态；页面继续通过已有公共入口组合这些模块。
3. 已知错误按 Environment 操作和后端错误内容映射到稳定的本地化消息；未知错误由“本地化摘要 + 服务端详情”组成，不吞掉诊断信息。
4. Metadata 在转为 API object 前先校验条数、key/value 长度和 key 唯一性；更新时按后端 PATCH 合同为从初始基线移除的 key 发送 `null` 删除哨兵，新增和保留项发送当前字符串值；后端仍是最终校验权威。
5. dirty 判定基于规范化的可提交表单值与打开编辑器时的基线比较；保存成功先重置基线/dirty，再关闭编辑器。
6. 应用内放弃修改使用项目已有 shadcn/Base UI `AlertDialog`；浏览器级离开只使用标准 `beforeunload`，不承诺浏览器自定义提示文本。
7. Work 状态由受控的已知状态映射与未知状态 fallback 共同处理；不根据 Sandbox 字段推导额外诊断状态。
8. 列表、详情和 Work queue 复用现有 locale-aware formatter。此次不改造 Managed Agents 之外的相对时间实现。
9. English 与 `zh-CN` 同步增加相同 key；既有 `I18nProvider.test.tsx` parity guard 继续作为目录级合同。

## Testing Decisions

主要测试接缝为 `web/src/features/managed-agents/ManagedAgentsPage.resources.suite.tsx`，使用 `renderManagedAgentsPage('environments', locale)` 和 `mockManagedResourceApi()` 从用户可观察页面行为验证功能。只有无法从页面接缝可靠观察的纯转换逻辑才增加更低层测试。

测试按失败场景优先、成功场景随后组织，并覆盖：

- English 与 `zh-CN` 下的创建、列表、详情、编辑、Networking、Packages、Metadata、Work queue 和归档只读文案。
- Name、Package 长度、Metadata 条数、key/value 长度和重复 key 校验；确认客户端不限制 lowercase。
- 已知后端错误的完整本地化，以及未知错误的本地化摘要与原始详情保留。
- 未修改直接关闭、dirty 时继续编辑/放弃修改、`beforeunload` 注册与移除、保存成功后解除保护、保存期间不可关闭。
- 连续提交只发出一个请求并只显示一次成功 Toast；失败后输入保留且可重试。
- 所有已知 Work 状态、未知状态 fallback，以及 English/Chinese locale-aware 相对时间。
- 归档 Environment 的只读 Alert、禁用 Edit、配置与 Work queue 仍可见，并且页面不存在 Version、Snapshot、Diff 或 Rollback 入口。
- 复用 `web/src/shared/i18n/I18nProvider.test.tsx` 的 English/Chinese key-parity 守卫。

完成后运行相关前端窄范围测试、`just web-format-check`、`bun run build`、`just duplicates` 和 `just complexity`。随后执行 `just restart-web`，在浏览器中验证中英文创建、详情、编辑、归档与 Work queue 关键流程。

## Out of Scope

- Issue #68 的 Runtime Version、`env_vars`、`init_script`，以及用 Runtime Version 替换 Packages。
- 将 Networking、Packages 或 Metadata 加入现有创建对话框。
- Environment Version、Snapshot、History、Diff、Rollback 或 restore。
- Issue #63 的 Sandbox 诊断字段、Heartbeat、Kill、replay、手工改状态或队列重排。
- 修改 Environment 后端运行语义、数据库 schema、API payload、路由或鉴权。
- 全应用相对时间或全应用 i18n 架构重构。
- 对不可由浏览器可靠拦截的页面退出方式作额外承诺。

## Further Notes

- 本规格来自 `/grill-with-docs` 的逐项确认，并按 `/to-spec` 固定实施与测试边界。
- 相关边界来自父 Issue #21；Runtime Version 后续工作由 #68 负责，Sandbox 诊断由 #63 负责。
- 这些决定局限于单个前端功能、可逆且未形成跨系统长期架构约束，因此不创建 ADR。
- 用户明确要求本规格写入 `docs/agents/`，因此该文件是本次对仓库 `docs/design/` 默认位置规则的任务级例外；PR 中不再创建第二份重复设计文档。
- 实现完成后在 Issue #67 追加验收结果、测试命令与范围说明；规格评论和完成评论均写入既有 Issue，不创建重复 Issue。
