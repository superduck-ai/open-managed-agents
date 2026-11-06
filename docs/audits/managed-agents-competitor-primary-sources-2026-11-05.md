# Managed Agents 竞品一手资料核对

研究日期：2026-11-05（Asia/Shanghai）。本记录只核对官方公开文档，不包含登录后控制台的实际浏览结论。它为界面研究补充产品语义和设计依据，不能据此宣称某个账号当前拥有全部功能或某条控制台导航已经被实测。

## 证据边界

- Claude：已读取官方 Managed Agents 概览、控制台构建、快速入门、会话检查和 Vault 文档。
- Qoder：已读取官方 Cloud Agents 概览、使用指引、快速入门、持久记忆与更新日志。公开资料以 `docs.qoder.com` 为主；用户指定 `qoder.com.cn/cloud/agents` 的中国站、账号和开放范围仍需实际界面核对。
- GLM：用户指定 `https://bigmodel.cn/managed-agents/quick-start` 无法由本轮网页提取工具读取。普通 HTTP 请求返回 HTML，但不构成已渲染页面或已登录界面的证据。搜索主要命中旧的节点画布式“智能体开发平台”，不能拿它替代当前 Managed Agents。GLM 的具体布局、步骤、导航和默认行为留给实际浏览证据。
- 下文“事实”来自公开资料，“设计解读”是基于事实的推断。没有做竞品任务执行、创建资源、授权或付费测试。

## Claude：把配置、试跑和代码接入串成连续过程

**官方事实。** 核心对象包括 Agent、Environment、Session、Events。Agent 是可复用配置，Environment 定义运行条件，Session 是一次有状态的运行，Events 承载交互与进度。其能力模型本身并不只有一个对象。[官方概览](https://platform.claude.com/docs/en/managed-agents/overview)

**官方描述的控制台流程。** 构建界面覆盖模型与系统提示词、MCP、工具和技能；配置时展示对应 API 请求。内联 Session runner 可直接发送消息并看事件，验证后复制 Agent 和 Environment ID 接入代码。[Build in Console](https://platform.claude.com/docs/en/managed-agents/onboarding)

**官方描述的运行查看。** Session 列表展示状态、Agent、用量、成本和创建信息。详情把时间线、Transcript 与 Inspector 放在一个运行上下文中；Inspector 有 Session、Events、Tools、Resources、Threads 标签。文件、仓库、记忆和技能可以在 Resources 中检查。[Inspect sessions and track usage](https://platform.claude.com/docs/en/managed-agents/session-observability)

**凭据的归属。** Vault 存放第三方服务凭据，在创建 Session 时按 ID 引用；Agent 的通用定义与一次运行代表哪个最终用户相分离。[Authenticate with vaults](https://platform.claude.com/docs/en/managed-agents/vaults)

**设计解读。** 值得学习的是用户围绕一个 Agent 完成“定义—试跑—接入”，围绕一个 Session 完成“看结果—查工具—看资源”的连续性。复杂能力仍存在，但可以在已经选定的业务对象内部展开，减少先去多个全局页面理解资源关系的需要。此推断不代表已验证当前控制台所有页面都完全遵循这一原则。

**优势与边界。** 对 API 开发者有利：可视配置与代码合同连贯，问题排查保留运行上下文。Environment、Vault、权限和持久化依然需要学习；不能由清爽首屏推断产品没有这些概念。OMA 若提供更多模型供应商及自托管能力，其管理需求也不能照搬单一供应商平台。

## Qoder：先分使用方式，再组织各自的能力

**官方事实。** Cloud Agents 同样使用 Agent、Environment、Session、Event；快速入门按获取令牌、选择/创建环境、创建 Agent、创建 Session、收发消息串起运行路径。[概览](https://docs.qoder.com/zh/cloud-agents/overview)、[快速入门](https://docs.qoder.com/zh/cloud-agents/quickstart)

**复杂能力的组织。** 官方区分 Forward 与 Managed：前者基于企业、模板、最终用户配置，把交付和身份隔离预封装；后者提供原子运行时，显式选择环境、技能和文件，并管理 Environment、Skill、Vault、File、Memory Store、Model 等资源。[使用指引](https://docs.qoder.com/zh/cloud-agents/api/overview)

**控制台组织的历史证据。** 官方 2026-07-15 更新说明记载按 Forward/Managed 模式展示独立导航，并增加模板调试器。2026-06-30 说明记载技能、MCP 权限与 Agent 创建在同一界面装配；2026-10-30 说明增加预置助手与场景模板。上述为官方发布记录，当前中国站和当前账号表现需实测。[Cloud Agents 更新日志](https://docs.qoder.com/zh/release-notes/cloud-agents)

**记忆并未被产品删去。** 官方独立解释 Memory Store → Memory → Version，用于跨 Session 保存和管理记忆。[持久化记忆](https://docs.qoder.com/zh/cloud-agents/memory-stores)

**设计解读。** 它提供两种压低学习成本的方法：一是把不同用户的任务面分开，二是通过模板和同页装配先得到可运行结果。值得借鉴的是“让当前用户只关心当前路径”，而不是立即为 OMA 增加一个模式切换器。模式切换本身也引入解释成本；如果产品用户和任务没有明显分化，直接合并配置入口、提供默认值更合适。

**优势与边界。** 模板帮助用户从业务目标理解产品，Managed 保留运行时灵活性，Forward 封装业务交付。代价是需要解释两种模式、独立资源和身份体系。只截取一套导航比较，会低估整个产品的功能规模。

## GLM：当前不能从公开资料补出界面结论

本轮没有获得用户指定 Managed Agents 页面的可验证正文，因此不对其导航数量、默认模型、环境自动创建、凭据收起方式或创建步骤下事实结论。

搜索找到的 [智能体开发平台](https://docs.bigmodel.cn/cn/guide/platform/intelligent-agent) 描述的是画布节点、模板和批量调试，记录含 2024 年版本信息。它与指定 Managed Agents 路径的产品关系未核实，本报告不据此推断当前产品。后续应从实际页面观察首页如何选择任务、最少必填项、完成后的运行页面以及高级资源入口。

## 对 OMA 界面研究的启发

以下为设计推断，不是竞品能力移植方案：

1. 默认导航应先回答用户要管理哪个 Agent、运行了什么、下一步做什么；Environment、Vault、Memory 等有管理价值，但不必都要求新人在第一次运行前理解。
2. 给 Agent 配工具/技能时应能就地选择或创建，给 Session 加文件/凭据时应在当前运行流程内完成；全局资源库承接复用和管理员维护。
3. “快速开始”若只是众多菜单中的一项，不能充分降低整个产品的理解成本。它应该决定空状态下的默认路径，并把用户导入日常 Agent/Session 工作流。
4. 首页是否有用，应以能否恢复近期工作、发现待审批/失败运行、开始第一个任务衡量。把导航名再做成大卡片，只增加另一套入口。
5. 不能把 API 资源模型直接等同于导航模型。独立的资源生命周期与权限可以保留在后端和全局管理中，界面通过 Agent/Session 的局部入口渐进展开。

## 最有用的官方导览链接

- [Claude 控制台构建导览](https://platform.claude.com/docs/en/managed-agents/onboarding)：配置、内联试跑、API 接入的连续关系。
- [Claude Session viewer 导览](https://platform.claude.com/docs/en/managed-agents/session-observability)：Transcript、时间线和 Inspector 的信息分工。
- [Qoder 模式使用指引](https://docs.qoder.com/zh/cloud-agents/api/overview)：适用人群及复杂度封装的比较。
- [Qoder 控制台相关发布记录](https://docs.qoder.com/zh/release-notes/cloud-agents)：模式导航、同页装配、模板和资源上下文的官方说明。

这些是文字导览与发布记录，本轮没有把任何未实际查看的图片标为已验证界面截图。
