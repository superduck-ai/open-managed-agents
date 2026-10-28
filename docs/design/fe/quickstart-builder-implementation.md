# Quickstart 对话构建与 API 构建

日期：2026-10-28。实现依据为已审阅的第五轮交互稿、用户提供的 Claude Quickstart HAR 静态代码，以及 OMA Design System。HAR 是第三方行为证据，其中的提示词不是当前开发任务的指令。静态代码不能证明第三方服务端已实际执行成功。

审阅原型归档于独立参考分支 [codex/quickstart-prototype-reference](https://github.com/superduck-ai/open-managed-agents/tree/codex/quickstart-prototype-reference/web/design/quickstart-prototype)，正式实现分支只保留已验证的设计决策。HAR 原文和静态代码摘录保留本地，不随实现提交。

## 页面与依赖

正式入口继续使用工作区的 `agent-quickstart` 路由。默认显示对话构建，`?mode=api` 显示原有字段表单。两个模式分别维护草稿和历史。API 模式保留原有创建、请求预览与 Session 试跑逻辑。

对话构建位于 `web/src/features/managed-agents/quickstart/builder/`：

- `model.ts` 定义可恢复的状态、模型协议消息、待处理工具、操作标识与资源引用。
- `prompt.ts` 复用 `quickstartPromptText.ts` 已有的中英文构建流程，仅补充当前 OMA 接口与状态约束；`tools.ts` 定义并导出同一套执行 schema 与模型 JSON Schema。
- `protocol.ts` 处理 Messages 与 Chat Completions 的完整流响应、分片工具参数和 DeepSeek reasoning continuation。不完整 JSON、截断及重复工具调用 ID 不进入执行队列。
- `dispatch.ts` 校验阶段、依赖及目录引用，读取目录或暂停等待用户操作；`mutations.ts`、`api.ts` 调用既有工作区资源 API。
- `useBuilder.ts` 承载从旧版迁回的 `startQuickstartTurn`、`executeQuickstartTool`、`completeAwaitingTool`、`sendChatReply` 和 `completeOfferNextStep` 主流程，保留持久化和当前资源接口适配。配置和资源写入成功后才向模型返回成功。
- 展示组件复用共享 Card、Button、RadioGroup、Checkbox、Accordion、MessageScroller 与现有凭据弹窗。Agent 提议默认显示名称、模型与指令；完整 JSON 按需展开。

```mermaid
flowchart TD
  Start[模板或需求描述] -->|模板点击直接创建 v1| Saved[已保存 Agent]
  Start -->|描述或空白需求| Proposal[模型提出完整配置]
  Proposal --> Confirm[用户确认]
  Confirm --> Saved
  Saved --> Environment[选择环境或对话创建环境]
  Environment --> NeedsAuth{实际 MCP 依赖需要凭据?}
  NeedsAuth -->|否| Ready[试跑就绪]
  NeedsAuth -->|是| Vault[选择或创建保险库]
  Vault --> Auth[连接、手动添加或明确跳过]
  Auth --> Ready
  Ready --> Session[独立 Session 对话]
  Session -->|用户明确结束| Review[分析实际事件]
  Review -->|有具体问题| Repair[修复多选题]
  Repair -->|确认配置修复| Proposal
  Review --> Usage[选择使用方式]
  Repair -->|继续或空选| Usage
  Usage -->|定时运行| Deployment[确认并创建 Deployment]
  Usage -->|应用调用| Integrate[接入代码]
  Deployment --> Integrate
```

## 与 Claude 对齐的行为

模板按钮直接调用 Agent 创建 API，自然语言先提出配置再确认。修改已有 Agent 时保留 ID，完整配置经既有 schema 合并和校验后按当前版本更新；409 重新读取最新配置并让模型重新提议，避免覆盖其他地方的修改。

环境创建不打开一张永久表单。模型先逐项询问必要信息，再呈现网络与软件包配置确认。选择已有环境使用真实 ID，并检查归档状态。

没有实际引用时不展示技能、凭据保险库或部署入口。技能只随已保存 Agent 的实际引用显示，不新增通用技能选择器。需要鉴权的 MCP 才进入保险库流程；仅目录明确 `authless` 的地址可以跳过凭据检查。凭据覆盖按精确服务 URL、有效状态及鉴权类型判断，完整分页读取。

授权复用 `CredentialDialog` 的真实 OAuth 与手动凭据能力。令牌和 OAuth 凭据停留在该弹窗与既有 API 请求中，不写入 Builder 快照或工具结果。OAuth 完成后重新读取凭据验证覆盖。明确跳过记录未授权事实并允许有限试跑；用户后续要求重新授权时可以重新打开该服务的授权步骤。

试跑复用 `useQuickstartConversation`、真实 Session SSE、Transcript、工具确认、停止与未知消息投递恢复。构建消息与 Session 消息分开保存和展示。首条消息被实际接受后才返回给 Builder；首轮 idle 不表示整个试跑已关闭。用户显式结束前阻止改动运行依赖；运行或等待工具确认时需先中断并等待状态收敛。

复盘使用实际 Session 事件的有限摘要：用户和 Agent 文本、事件类型、工具名称、错误标识与停止原因，不发送原始凭据或工具输入。有具体问题才提出修复多选题；空选等同继续。修复后保存新版本可以直接继续，不强制再试跑。

试跑复盘后的使用方式选择沿用旧版 `ask_user_questions`：模型询问定时运行或应用调用；选择定时后由 `flag_schedule_intent` 记录。`offer_next_step` 仅显示继续，不再附带前端写死的第二套使用方式按钮。定时意图也可以提前记录，Deployment 只能在试跑结束后确认创建。自然语言周期由模型转为 cron 和时区，并用现有 `previewSchedule` 校验 OMA 的 cron/DST 语义。确认绑定已保存 Agent 的具体版本、环境、保险库和初始消息。Deployment 创建成功后才出现在资源卡；接入分支展示 deployment runs 与运行 Session 的事件接口。

本 HAR 的 Builder 和内嵌试跑 composer 均未接附件入口，因此该实现保持文本输入。Files API 与 Session resources 的通用能力不等于本页面已有附件交互。

## 协议、保存与恢复边界

Builder 模型通过工作区配置解析实际 Provider。Messages 使用已有 Console proxy；Chat Completions 使用新增的组织 Console proxy 路径。二者均由服务端注入供应商密钥并移除浏览器凭据。未修改 `/v1/chat/completions` 的 Brain 凭据授权边界。

Console POST 流请求携带 CSRF，资源请求继续使用现有 SDK client 与服务端工作区权限。界面暂停和确认不替代服务端 RBAC。

构建历史使用账户与工作区隔离的同标签页 `sessionStorage`，不提供跨设备归档。Session 历史由既有后端保存。刷新先显示起始页，用户点击「继续上次构建」后恢复待确认工具，不自动续跑模型或重放写请求。写入前保存 `quickstart_operation_id`；结果未知时仅提供读取结果恢复，未确认结果前不再次创建。此策略不等于新增了服务端幂等键或 exactly-once 合同。

凭据目录与模型目录读取失败保留错误，不把错误当作空目录或已授权。取消、切换模式与卸载中止模型流，并阻止旧页面的异步结果写入新范围。

## 验证与已知范围

测试覆盖描述先提议、模板直接创建、同 ID 版本更新、未指定字段保留、环境对话、未知写入结果恢复、独立 Session 首轮与显式结束、真实事件复盘、空修复继续、凭据分页与精确覆盖、重新授权、部署前置条件和绑定、Messages/Chat 工具分片及截断、CSRF 和组织代理出站 header 隔离。

浏览器使用临时受控 fixture 挂载正式组件，核对明暗主题、环境问答和窄屏布局。它只验证 UI 与交互，不代表真实模型、OAuth、Worker 或调度执行已通过。

初版验证时本机 `127.0.0.1:38080` 后端未运行，正式页面返回登录信息加载失败。尝试 `just restart-server` 后确认该 worktree 缺少 `.run/config.yaml`，启动入口在创建服务前拒绝运行；未复制其他 worktree 的配置或密钥。尚未完成真实 Provider、外部 OAuth、Worker Session 与调度运行的端到端联调；未执行 `verify-be`。

本次普通 Go 全量测试有既有 `TestPackageSourcePersistScriptRewritesManagerConfigIdempotently` 失败，定向复测相同：npm 返回公共 registry 而不是该测试临时 npmrc 指定地址。未修改该不相关测试或放宽门禁。Go lint/dead-code/complexity 使用工具声明要求的 Go 1.27.2 运行。

### 本轮检查记录

- Quickstart 功能回归：65 通过、0 失败，包括新增 Builder 与原 API 流程。
- 正式前端构建、命名、复杂度、重复率与全目录 Prettier 检查通过；构建仍提示既有大 chunk。
- 初版全前端回归：1553 通过、1 失败，失败为供应商表单焦点断言 `keeps provider edits after failure and disables dismiss controls while saving`；该文件定向复测 4 通过、0 失败。全量不能报告为全部通过。末次 deployment runs 示例修正另由 Quickstart 定向回归覆盖。
- Go Console 代理相关单测通过；Go lint、dead-code、complexity 和 Go/TS duplicates 通过。Go 全量普通单测的环境包 registry 失败如上，定向复测仍失败。
- 浏览器：正式组件的受控 fixture 中完成配置确认、环境选择、新建环境问答、明暗主题、390×844 窄屏检查。窄屏 DOM 的 clientWidth 与 scrollWidth 均为 390px。未做全页逐像素对比度、原生 200% 缩放或真实后台执行认证。
- 临时浏览器 fixture 已移除；证据截图保留在本次任务的 visualizations 目录。正式功能不依赖该 fixture。

### 首屏与审阅稿对齐

两种构建方式统一命名为「对话构建 / 手动配置」，入口页共用稳定的标题、方式切换器与 880px 主容器。切换只替换页头下的内容，保留 `mode=chat/api` 路由参数兼容旧链接。API 接入仍是两种构建方式完成后的能力。

对话构建依次显示 728px 需求输入框、模板网格和空白入口；开始后进入连续聊天，隐藏入口标题与方式切换器，输入框回到会话底部。手动配置首屏只显示模板选择与紧邻模板、靠右的「开始配置」按钮，开始后隐藏入口标题与方式切换器，显示步骤进度；长表单保留底部操作区，返回模板选择时恢复入口标题与方式切换器，并保留已进入流程的步骤导航。

两种方式共用同一套模板目录、名称、图标、顺序和卡片基础样式：深度研究、结构化提取、领域追踪、客服助手、数据分析、代码评审。桌面两行三列，窄屏降为两列、一列，整体可滚动。对话模板直接进入构建，手动模板增加单选状态，选择和开始配置均只修改草稿，显式确认后才创建资源。手动方式在业务模板上方提供紧凑的「Hello World 示例」卡片，说明通过一句问候了解创建、试跑和 API 调用，保留单选与「开始配置」确认。移除手动方式的「从空白开始」入口；旧 custom 草稿仍保留恢复兼容，用户仍可在配置表单中编辑提示词。

模板通过既有快速开始配置构造函数创建基础 Agent；不会因为模板目录排序变化替换成其他场景，也不会为展示效果添加未请求的 MCP 依赖。结构化提取、客服助手纳入手动草稿及待确认操作的恢复校验。

入口布局验收：Quickstart 67 项测试通过，前端全量 1558 项测试通过。正式登录页面核对两种方式切换前后的标题与切换器坐标一致，模板区均为 880px；390px 窄屏两种方式无横向溢出，手动选择客服助手后进入对应草稿并显示步骤进度，浏览器检查未提交资源创建。

### 构建对话与配置摘要

对话历史与输入框统一为 680px 内容宽度，整体工作区限制为 1440px。用户发言使用右侧气泡，助手正文使用带标记的 Markdown 展示，创建和保存结果使用左侧资源记录，保留当次返回的名称与版本。协议中的 `tool_result` 虽然携带 `user` 角色，但不直接视为用户发言；问答答案和用户选择仍按用户消息展示。保存历史不使用最新配置覆盖旧版本。

右侧配置使用与审阅稿一致的紧凑分组：进度依据真实资源与试跑完成状态标记；智能体采用图标、名称、版本及整行配置链接；资源分组保留空态，具体资源、Skill 和凭据条目只在实际使用时出现。对话中的「继续」与助手内容对齐，保留原来的续接行为。

Chat 协议转换在助手工具调用中显式携带 `reasoning_content`，合成的模板工具记录使用空字符串，真实模型思考内容原样回传。这解决开启 thinking 的 Chat provider 在模板保存后拒绝续接的问题；不生成或展示虚构的模型回复。

对话优化验收：Quickstart 69 项测试通过，构建、命名、复杂度、重复代码检查通过。正式登录页面用已有模板完成真实 Chat 模型续接，得到助手回复；明暗主题和 390px 窄屏检查通过。前端全量回归 1559 通过、1 失败，末次失败为未修改的子智能体服务器搜索测试；它与前一轮失败的批量归档测试定向复测均通过（2 通过、0 失败），不能报告全量通过。

### 快速开始入口与继续构建

点击侧栏「快速开始」、直接打开页面或刷新时，先显示构建方式、需求输入框和模板，缓存中的 `initialized` 仅代表存在历史，不决定是否直接显示对话。通过路由导航事件重建入口视图，同一路径上的再次点击也返回起始页；切换模式保留路由的 history state。

存在同账号、同工作区的本地记录时显示「继续上次构建」及智能体名称。点击后恢复原有消息、配置与待确认操作，不发起模型续跑或资源写入。开始新的构建前明确确认替换当前本地记录，已保存的远端资源保留；取消时保留原记录和新输入的文字。写入结果或消息投递结果仍未知时，先继续原构建核对结果，禁止直接替换。

入口回归覆盖同路径重新导航、显式继续且不重放写入、取消替换保留输入，以及刷新后查询未知保存结果。

入口修复验收：Quickstart 71 项测试通过，模型配置缺失和加载失败的 3 项回归通过。正式页面验证继续旧构建、同路径点击侧栏返回起始页、新模板替换确认及取消保留原记录。前端全量 1561 通过、1 失败，失败项为未修改的分页 Session 删除测试；构建与质量检查通过。

### 资源空态、导出与构建页布局

进入对话构建后显示独立的返回按钮和智能体标题，不恢复入口的方式切换器。返回起始页保留本地构建记录。消息区按视口高度设置顶部留白，与顶栏、侧栏和底部输入框形成稳定层级，避免内容直接贴在页面顶部。

右侧始终显示「资源」分组；尚无已选环境、保险库或部署时显示图标与「暂无资源」，不添加未使用的 Skill 或凭据条目。分组下提供「导出配置」，复用现有文件下载能力生成 `agent.json`。文件包含当前 Agent 配置及实际 Session 资源引用，只在存在实际环境时导出 `environment_id`，保险库仅导出 ID，不包含构建对话、保险库凭据内容或占位资源 ID。

验收：Quickstart 73 项测试通过，正式页面完成文件下载并核对内容包含实际 Agent ID、版本及系统提示词，无环境时不伪造引用。构建和静态质量检查通过。最终全量前端回归 1564 通过、0 失败。

## 旧版对话构建复用（2026-10-28）

Git 历史确认 PR #427（`ce4786b1`）把旧版对话构建替换为手动四步向导。该提交父版本中的 `AgentQuickstartPage.tsx` 已包含模型工具循环、完整配置提议、环境问答、保险库授权、试跑与接入；并非本次新增的产品能力。

当前按能力复用旧实现：

- 提示词以仍在仓库中的 `quickstartPromptText.ts` 中英文行为文本为共同来源，移除旧静态 MCP 目录，使用实际工作区目录。当前运行约束作为明确优先的适配块，覆盖未创建的描述草稿、真实模型列表、确认后写入、授权跳过、复盘继续和定时意图等差异。不发送旧版 API 文档块，也不恢复其过时接口示例。
- `QuickstartQuestions.tsx` 从旧版 `questions/AskUserQuestionsCard.tsx` 恢复问答主体，继续使用共享 `Questionnaire` 和原有问题解析器。逐题展示、选项说明、单选/多选、自由回答、前后切换和跳过沿用旧交互；`BuilderQuestions` 只校验工具输入并接入当前工具结果。
- 模板首条消息沿用旧版需求描述语义，优先展示模板 description，而非合成“使用模板：名称”。模板仍直接保存配置，描述输入仍先提议再确认。
- 既有 `quickstartBuildAgentConfigInput`、Agent schema、SDK 资源客户端、凭据弹窗和 `useQuickstartConversation` 持续复用。未恢复旧页面内单体状态与旧资源写入代码；当前协议适配、版本冲突和结果未知恢复仍由现有模块承担，防止旧协议和无确认写入回退。

此前只复用了提示词和问答，主循环尚未迁回；本轮进一步以旧版主流程为基础迁移。来源固定为 `1033ab15bffa0c6be670195b29799290d92be458`（PR #427 的父提交），不是恢复整个旧页面或仅重命名现有函数。

| 旧代码位置与职责 | 当前接入 | 必要适配 |
| --- | --- | --- |
| `AgentQuickstartPage.tsx` 的 `startQuickstartTurn`、`executeQuickstartTool`、`completeAwaitingTool` | `builder/useBuilder.ts` | 保留“执行自动工具 → 等待用户工具 → 工具结果续轮”的递归控制流；用可恢复的 queue/gate 替代旧页面 chatItems 的运行状态；每个自动结果立即保存，多个工具遇到等待时不丢弃已完成结果 |
| 同文件 `sendChatReply`、`completeOfferNextStep` | `builder/useBuilder.ts` | 用户补充需求按旧版专门的工具结果语义返回；问答未完成时阻止从输入框绕过；按当前阶段推进，同时兼容可选 deploy 阶段 |
| `chatModel.ts` 的 `quickstartChatReplyToolResult`、`toolResultBlock` 与旧页面工具结果消息 | `quickstart/chatModel.ts`、`builder/model.ts` | 统一 tool_result ID 与 is_error；不再把补充需求一律告知模型为“取消并改变方向” |
| 旧页面 `buildCurrentQuickstartTurnContextBlock` | `quickstart/chatModel.ts` 的 `quickstartTurnMessages`，复用 `buildQuickstartTurnContextText` | 每次续轮附带最新阶段、配置与定时意图；仅进入模型请求，避免把内部状态行显示或保存成用户消息 |
| `api.ts` 的 `createQuickstartSession` | `quickstart/api.ts` | 恢复共享的 Agent 版本、环境和 vault 绑定；手动配置与对话试跑共用；增加现有 operation metadata 以保留结果未知恢复 |

原先 `advance` 的独立循环已经删除。`dispatch.ts` 只保留当前资源前置校验与暂停策略；`mutations.ts` 只保留版本冲突检查和 SDK 写入适配，它们均不再承担模型调用、用户回复或续轮控制。未为了声称复用而改名这两个适配模块。

旧版只解析 Messages，当前协议层继续兼容 Chat Completions 和 reasoning continuation。附带 turn context 时，Chat Completions 必须先返回所有 tool 消息，再附加 context 用户消息；不能将 context 插到未完成的工具结果之间。旧版未知工具直接报告完成、配置修改总是新建 Agent、写入结果未知后直接重试等路径没有恢复。新建 vault 及写入结果恢复沿用旧版累加选择的语义，不覆盖先前选择。

本轮本地验证：前端完整套件 1568 项通过；Quickstart 77 项通过；构建、格式、命名、复杂度和重复代码门禁通过。另通过已登录正式页面与真实工作区模型，以“客服工单助手，先确认系统和范围，先不保存”验证返回逐题问答；选择 Intercom 后可进入下一题，返回仍保留选择，尚未保存 Agent。此证据覆盖模型问答，不表示外部工单连接、OAuth 或 Worker 试跑已验收。

### 主流程复用验收

自动化回归覆盖：同轮多个工具在问答前后的结果顺序、刷新后恢复剩余工具而不重放模型或写入、配置确认前用文字修改提议、结构化问答阻止输入框绕过、当前阶段与完整配置的模型上下文、Chat 工具结果与上下文顺序。已有配置版本更新、授权跳过与重新授权、Session 显式结束后复盘、Deployment 依赖检查继续回归。

本轮最终验证结果：前端完整套件 **1577 通过、0 失败**；Quickstart 定向回归 **86 通过、0 失败**。构建、格式、命名、复杂度与重复代码门禁通过；既有大 bundle 提示仍在。

真实本地正式页面使用工作区 DeepSeek 模型完成了：三题字段问答 → 完整配置提议 → 确认前通过文字仅修改名称 → 保存 Agent v1 → 刷新后显式恢复 → 列举并复用已有环境 → 创建实际 Session → 发送订单 → 收到结构化 JSON。测试单号 `ACCEPT-001`，返回商品“测试键盘”、规格“白色”、数量 `2`、单价“99元”，没有提取额外字段。首轮完成后试跑保持打开，由用户动作“结束试跑”触发复盘。

可检查的本地资源：Agent `agent_rPhZEPLsIDkiybpr0LdaYQLH`（旧流程复用验收助手，v1），Session `sesn_MA2PGwgwnnz2flmaVJcoyUgy`，复用环境 `env_xRKXrm1hkAQvRYO6r0a9CQgr`。没有新建环境、vault 或 deployment。证据覆盖此次本地 Provider/Worker 的单条文本运行，不代表外部 OAuth、多服务授权或定时调度验收；未运行 `verify-be`。

环境实测中发现模型曾将已有环境选择与假设新建时的网络策略一起询问，已按旧版先后关系补足约束：第一轮仅问环境选择，确认新建后才问网络，此处补足旧版已有的提示词约束，不增加另一套环境问答流程。

实际结束试跑后，模型已依据本次返回的订单字段复盘为无需修复；用户在模型的结构化问答中选择应用调用，随后只出现一个继续操作，没有再次询问使用方式。


### PR #511 审阅修复：模型范围、工具队列与试跑恢复

Builder 同时加载工作区和组织供应商，与 `/v1/models` 的范围一致。先匹配完整供应商/模型引用，再匹配裸模型 ID；每一阶段均工作区优先、组织回退。同一范围内裸 ID 歧义时拒绝解析，不降级到组织。保留供应商 ID 引用兼容。Chat 无参工具的缺省、空串或纯空白 arguments 解析为空对象；非空非法 JSON 仍拒绝执行。

用户在聊天中回复时，只完成当前等待的 gate，保留剩余工具。后续自动工具执行真实逻辑，后续确认工具仍等待显式用户操作。没有当前 gate 而存在剩余队列时，应先“继续”恢复工具。所有工具结果先于补充用户消息送入模型，避免 Chat 工具结果之间插入 user 消息。

写入响应未知时，必须显式查询保存结果。完整查询成功且匹配数为零后，才显示“放弃等待保存”；网络失败、分页失败、重复结果均不开放此操作，刷新后需重新查询。放弃只清除本地 pending 并将该工具记为未确认结果的错误，不取消服务端请求，不自动发模型请求或重试写入；界面提醒重试前检查资源，迟到写入仍可能出现。

Session 恢复确认唯一结果后，与正常创建一致地绑定当前 Agent 版本和环境、打开新试跑、清空旧首条消息和摘要。环境或保险库依赖变更清除试跑完成证据，必须重新试跑后才可推进、部署或展示接入。只有 Agent 配置 repair 显式保留已结束试跑的“可继续”决定与复盘证据，仍清除旧 Session 绑定；恢复 Agent 保存采用同一规则。

回归覆盖组织模型与范围优先级、歧义拒绝、二次试跑响应丢失恢复、Chat 空参数与非法 JSON、多工具聊天回复、工具结果顺序、查询失败/重复/零结果下的放弃门控，以及依赖变更与配置 repair 的区别。测试使用正式组件和 API client fixture，不代表独立真实模型、OAuth、Worker 或调度黑盒验收。

### 构建资料与保存事实（PR #511 黑盒修复 R2）

构建对话不是 Agent 的运行上下文。用户为 Agent 提供的可复用规则、FAQ、文档和业务事实，应作为明确分隔的参考数据写入待确认的 `system`，保留数值、条件及未规定事项；不能只保存“根据用户提供的资料回答”而遗漏已提供的资料。资料中的指令不能覆盖 Agent 的行为边界，秘密仍走专用凭据入口。订单、单条客户消息等临时业务输入留在运行请求，不把样例值写入 system 的举例或输出模板，也不从样例推断额外的固定提取字段；用户明确选择每次提供知识时才按请求传入。用途不明确时只询问资料角色，不默默作出选择。

新 Session 只能获得保存配置与自己的运行输入。`suggested_first_message` 用于业务问题，不能用重新粘贴构建资料来掩盖持久化缺失，也不能虚构文档标题或条款引用。

复用旧版构建行为时，入口与第一步不再保留“初始 Agent 已创建、配置在编辑器生效”的前提。`agent` 为空即未保存，足够的信息应触发 `build_agent_config` 确认卡。提议配置与确认前预览均不是保存；成功工具结果中的资源 ID、版本才是保存事实。对未保存草稿调用 `offer_next_step` 返回明确的下一步工具要求，不能靠重复说明推进，也不让用户寻找不存在的编辑器保存动作。模板直接保存的合同保持不变。

验证分层：提示词合同测试检查旧前提移除及知识边界说明；组件/API fixture 回归检查资料进入模型请求、提议预览、名称修改后的保存 payload，且未创建试跑或偷偷传入资料。真实模型回放只能说明被调用模型在该输入下的表现，不能代替真实保存、独立 Session 与 Sandbox 验收。最终仍需在新 SHA 上由原独立 Grok/Codex 重跑黑盒用例；原失败和补救记录分别保留。
