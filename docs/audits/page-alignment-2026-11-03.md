# 文档逐页对照：OMA 功能与 Claude 参考

基线：本地代码 `a651454803c2928e803e42eb789cc1ec7694f45a`，工作树含既有文档改动；2026-11-03 审计。结论基于源码和官方文档的静态对照，代码存在不等于目标部署已经上线或完成真实运行验证。逐页记录问题与证据，不把上游能力视为 OMA 已支持。当前记录随审计推进更新。

## 智能体设置

- 模型 `effort`：文档称省略时填入 high，实际规范化仅默认 `speed`，省略的 `effort` 不会自动输出（`internal/agents/handler.go:774`，`internal/agents/model_effort_test.go:51`）。同模型更新时文档称保留旧 effort，当前更新重新规范化并替换 model（`internal/agents/handler.go:482`）。会话覆盖接受并保留 effort（`internal/sessions/agent_model_override.go:34`），实际运行是否使用仍需端到端核实。
- `multiagent` 更新并非一律整体替换：新类型按字段合并，旧 coordinator 才整体替换（`internal/agents/handler.go:560`、`internal/agents/cma_multiagent.go:131`）。advisor、workflow 执行不能从配置接受推定已实现（`docs/design/be/subagent-delegation.md:132`）。
- 地域 pin 的物理路由、工作区允许列表及每轮合规保证缺实现证据；设计说明只保留兼容字段及部分一致性校验（`docs/design/be/subagent-delegation.md:85`）。
- 缺控制台创建、模型前置配置、工具/MCP/技能选择和版本管理说明（`web/src/features/managed-agents/agents/create-dialog-rendered.tsx:55`）。建议提供脱敏空白表单与版本管理截图。页面示例固定本地地址和模型，中文锚点也需修正。对照 [Claude 智能体设置](https://platform.claude.com/docs/zh-CN/managed-agents/agent-setup)。

## 工具

- 工具表遗漏 `ask_user_question`：默认添加为禁用，界面将禁用展示为拒绝；不能宣称所有工具默认开启（`internal/agents/handler.go:1099`，`web/src/features/managed-agents/agents/tools/model.ts:405`）。
- 文档声称所有超过 100,000 字符的工具输出均落盘；现有证据仅覆盖 MCP 文本和 web search 等路径，web fetch 另有处理方式（`oma-brain-dsh/src/external-mcp/output.ts:6`，`oma-brain-dsh/src/web-tools/client.ts:17`）。
- 补 Web 搜索 Provider 配置、域名允许/阻止规则和受限网络等前提（`internal/webtools/providers.go:10`，`internal/webtools/config.go:35`）。建议展示工具选择、权限及 Web 域名配置的空表单和提问卡片。示例地址和链接需核对。[Claude 工具页](https://platform.claude.com/docs/en/managed-agents/tools) 的统一描述不能证明 OMA 覆盖所有工具。

## MCP 连接器

- 页面遗漏 OMA 控制台 MCP 目录、Agent 选择及 Tunnel/Connector 配置与状态路径（`web/src/features/mcp-servers/MCPServerDialogs.tsx:171`，`docs/design/mcp-tunnel-developer-guide.md:12`）。可补空表单、选择器和脱敏状态截图。
- 会话创建虽不验证 MCP 端点连通性，受限网络策略仍可让创建直接失败（`internal/sessions/service.go:61`，`internal/networkpolicy/session_mcp.go:10`）。
- MCP 输出落盘针对文本，非文本和写入失败情况不同，成功文件在临时目录且不能保证沙箱替换后仍可用（`oma-brain-dsh/src/external-mcp/output.ts:12`）。凭据 URL 匹配还要求路径和 query 对应，不能仅解释主机及尾斜杠（`internal/vaults/inject_match.go:53`）。
- HTTP(S) URL 限制、最多 20 个服务器和服务器与工具集双向引用约束可根据 `internal/agents/handler.go:846`、`:992` 补齐。对照 [Claude MCP](https://platform.claude.com/docs/zh-CN/managed-agents/mcp-connector)。

## 权限策略

- API 接受 `auto`，但当前实现对内置工具和 MCP 均降级为询问，不能照搬 Claude 的自动风险评估承诺（`internal/agents/handler.go:1164`，`internal/codesessions/auto_permission_test.go:15`）。控制台编辑器仅列 allow/ask，已有 auto 的展示也有兼容风险（`web/src/features/managed-agents/agents/create-dialog-model.ts:79`）。
- 普通内置工具默认 allow、MCP 默认 ask，但 `ask_user_question` 默认禁用是例外（`internal/codesessions/tool_permissions.go:300`）。禁用属于 enabled 状态，不应写成 API 的独立 `always_deny` 类型。
- 需核实多线程审批时序，修复中文锚点和示例前提；建议增加工具权限配置和会话审批卡截图。对照 [Claude 权限策略](https://platform.claude.com/docs/en/managed-agents/permission-policies)。

## 智能体技能

- 每个 Agent 的 skills 数组上限为 20，而页面照搬上游每会话 500 的说法（`internal/agents/handler.go:921`）；跨 Agent 的合计限制需另核。
- 内置技能从数据库解析，未导入时可能缺失；不能保证每个工作区均有 pptx、xlsx、docx、pdf（`internal/skills/resolver.go:146`，`docs/design/be/skills-builtin-seed-and-storage.md:7`）。自动选用属于模型行为，需运行验证。
- 仓库自动发现和自托管限制等上游陈述尚无充分实现证据；先标待核。补工作区技能列表、上传、版本和 Agent 选择的截图与说明（`web/src/features/managed-agents/agents/create-dialog-api.ts:31`）。对照 [Claude 技能](https://platform.claude.com/docs/en/managed-agents/skills)。

## 会话预算与页面组织

Claude 当前确实有独立的 [Session budgets](https://platform.claude.com/docs/en/managed-agents/budgets) 页面，启动会话中保留简短入口。OMA 预算涉及创建、更新、计量、触顶和恢复，建议也保留独立参考页，在会话入门及操作页各用简短说明链接过去；先以 OMA 的实际合同重写，避免重复长篇。

当前 `docs/zh/budgets.mdx:8` 称功能未实现，与本地预算门禁和触顶代码（`internal/sessions/budget.go:67`、`:150`）矛盾。正文继承 Claude 的未舍入计价说法，而 OMA 设计说明模型费用按请求舍入美分（`docs/design/be/session-budget.md:3`），实际累计及活跃时间计算见 `internal/billing/billing.go:173`、`:213`。部署版本、完整计价和恢复链路仍需另行验证，不能仅凭源码宣称生产已支持或等同账单金额。

## 云环境设置

- 文档称网络不影响 web search/fetch，但受限网络的 allowed hosts 会用于搜索结果过滤、请求及重定向校验（`internal/webtools/policy.go:24`、`internal/webtools/tools.go:33`、`internal/webtools/fetch.go:35`）。Web 工具域名配置与环境允许列表还有创建时约束（`internal/webtools/config.go:94`）；MCP 也受到约束（`internal/networkpolicy/session_mcp.go:10`）。
- `allowed_hosts` 并非不支持端口：代码接受 host:port 和 IP（`internal/networkpolicy/hosts.go:67`、`:95`）。控制台与后端创建默认网络为 unrestricted（`web/src/features/managed-agents/environments/model.ts:17`，`internal/environments/handler.go:987`），与 [Claude 环境页](https://platform.claude.com/docs/zh-CN/managed-agents/environments)不同。
- 预构建需补状态、阶段、日志与操作入口（`web/src/features/managed-agents/environments/prebuild-dialog.tsx:29`）；初始化脚本截图应标注来源并重新核实版本。软件包来源由部署配置，包规格受校验和大小限制（`internal/environments/package_config.go:104`）。沙箱替换后不同路径和进程的保留范围应核实（`internal/environments/environment_prompt.tmpl:33`），不能笼统保证持久。

## 云沙箱参考

- 页面固定操作系统、架构、资源规格和 limited 默认值不能作为所有 OMA 部署的合同；后端和 UI 默认 unrestricted，资源由环境配置与部署限制决定（`internal/environments/handler.go:991`，`internal/environments/template_spec.go:229`）。
- 本机独立沙箱镜像仓库当前工作提交含 Ubuntu 24.04 系列、双架构与多种语言及文档/媒体工具，但处于未合并的工作版本；仓库中的示例部署引用 latest（`deploy/docker-compose/oma-server.yaml:41`）不能证明目标环境运行的镜像及 digest。应以部署的实际镜像为准，按基础镜像、可选 toolkit、自定义镜像区分，并提供版本和工具自查方法。
- [Claude 云沙箱参考](https://platform.claude.com/docs/en/managed-agents/cloud-sandboxes-reference)描述其自身运行环境，不能直接迁移其版本、规格或默认策略。建议脱敏环境配置与模板状态截图；线上镜像和预装工具尚未现场核实。

## 启动会话

- API 对 model override 的 effort 进行规范化并保留，实际模型是否使用仍需运行验证（`internal/sessions/agent_model_override.go:34`）；文档声称不生效过于确定。地域控制同样缺物理执行证据（`docs/design/be/subagent-delegation.md:85`）。
- 创建会话会排队工作，实际启动取决于环境 runner/worker，不能照搬无需等待的承诺（`internal/sessions/service.go:113`）。创建前还检查环境、网络、Vault、预算/价格与资源（`internal/sessions/service.go:40`、`:94`）。
- `initial_events` 的类型和数量等限制有代码依据（`internal/sessions/initial_events.go:28`），但不证明结果评估执行闭环。应把最短路径放在页面前部，并补控制台创建对话框和脱敏交互/审批截图（`web/src/features/managed-agents/sessions/SessionCreateDialog.tsx:114`）。对照 [Claude 启动会话](https://platform.claude.com/docs/en/managed-agents/sessions)。

## 会话操作

- 会话列表返回 `data` 和 `next_page`，文档声称 `prev_page`、游标绑定排序及过滤条件错误不能直接套用（`internal/sessions/handler.go:47`，`internal/sessions/event_payload.go:304`，`internal/sessions/service.go:209`）。事件列表游标是另一套规则，不应混淆。
- 归档和删除是否允许取决于状态、worker 是否开始等条件，并非所有 running 状态都相同；操作进行中任务应先中断并等待安全状态（`internal/db/session_removal.go:31`）。删除涉及软删及异步回收，不等于响应时物理数据已全部清除（`internal/db/sessions.go:468`，`internal/sessions/service.go:477`）。
- 漏 title、metadata 更新，`vault_ids` 更新被拒（`internal/sessions/service.go:314`）；预算变更后的自动恢复需要端到端验证（`:393`）。补详情、归档/删除确认和预算调整截图。对照 [Claude 会话操作](https://platform.claude.com/docs/en/managed-agents/session-operations)。

## 会话事件流

- 本地实现支持 `Last-Event-ID` 回放游标之后的公开持久事件；无效或不匹配的游标会报错，临时 delta 不在历史里（`internal/sessions/stream_hub.go:287`，`internal/sessions/stream_replay.go:11`）。原先首轮审计中“不回放”的判断已更正，目标部署仍待验证。
- 空闲会话不能保证完整沙箱 checkpoint 或固定 30 天：本地生命周期允许临时文件、工作目录和进程丢失（`docs/design/be/sandbox-lifecycle.md:3`，`internal/environments/environment_prompt.tmpl:35`）。实际回收由部署配置决定。
- thinking 是状态信号而非思维正文；delta 是短暂帧（`internal/sessions/stream_hub.go:331`，`internal/codesessions/brain_model_events_test.go:115`）。工具确认接受 allow/deny，拒绝 `updated_input`/`answers`（`internal/sessions/service_helpers.go:389`）；来源应根据 `source.kind` 而非仅看 role 判定（`oma-brain-dsh/src/ccrv2/session.ts:253`）。
- 每次 idle 前必有使用量事件、特定模型系统消息限制及多线程审批时序仍需验证。建议拆分入门与 delta 专题，补事件→流→持久历史→重连图和脱敏审批截图；参照 [Claude 事件流](https://platform.claude.com/docs/en/managed-agents/events-and-streaming)。

## 自托管沙箱

- 环境创建、统计和 stop 的 cURL 漏 `?beta=true`，当前环境路由强制要求（`internal/environments/handler.go:153`）。SDK/CLI 是否自动补参数和指向 OMA 仍需实测。
- 页面声称控制台可生成环境密钥，但尚未找到与持久环境密钥鉴权贯通的生产调用；需核实 token 路径后才能写可操作步骤（`internal/platformapi/platform_environment_tokens.go:69`，`internal/api/service_auth.go:113`）。
- `pending` 是未停止的 work，不只是已认领未确认（`internal/db/environment_work_mapper.xml:386`）；执行发生在自托管环境并不意味着工具输入输出不会流经控制面。Memory 等资源支持和 Worker 完整挂载链要区分（`internal/sessions/environment_resources.go:14`）。
- 1800 行页面可按最短部署、凭据、运行、监控、安全任务拆分；第三方 worker/SDK 示例需单独验证，不凭上游文档承诺兼容。参照 [Claude 自托管沙箱](https://platform.claude.com/docs/en/managed-agents/self-hosted-sandboxes)。

## 自托管沙箱安全

- Environment key 的权限不仅是工作队列，还包括同环境会话和事件访问（`internal/sessions/access.go:19`）；环境隔离不能自动等同个人隔离。异常使用检测及可操作的签发/撤销/轮换入口尚无充分证据，需实测（`internal/api/service_auth.go:113`）。
- 自托管且挂载 Memory 时才生成的 work token 绑定工作、会话及租约，并限制附加 Memory 的访问和写权限（`internal/sessions/service.go:101`，`internal/api/work_auth.go:13`）；不可笼统称每个 work 都具备同样凭据。
- 应图示控制面、worker、模型和 MCP 的数据流与信任边界，补 Memory 本地副本、只读及残留清理责任；异常清理需真实 worker 核验。对照 [Claude 安全页](https://platform.claude.com/docs/en/managed-agents/self-hosted-sandboxes-security)。

## Webhook

- `session.budget_reached` 专用通知尚未由会话 bridge 映射，不代表预算控制未实现（`internal/sessions/webhook_bridge.go:99`，`internal/sessions/budget.go:67`）。[Claude Webhook](https://platform.claude.com/docs/en/managed-agents/webhooks) 的专用通知不能直接当作 OMA 合同。
- 3xx 触发尝试禁用，数据库写入仍可能失败（`internal/webhooks/webhooks.go:98`）；事件类型还可含线程和 Vault ID，事件 ID 示例前缀应与实际生成一致（`internal/webhooks/webhooks.go:19`，`internal/webhooks/enqueuer.go:72`）。
- 管理 API 的 `webhooks-2026-03-01` Beta 头、自包含客户端和验签前置需要补齐（`internal/webhooks/handler.go:29`）。把内部队列细节移至部署运维，补空白订阅、事件选择和遮挡密钥的截图；验签与投递未端到端验证。

## 定义结果

- 当前 DSH brain 模式不执行 outcomes，兼容性页面仅称部分支持；接受事件不能推定评分器和迭代闭环已运行（`docs/design/be/brain-runtime.md:142`，`docs/zh/compatibility.mdx:19`）。[Claude 定义结果](https://platform.claude.com/docs/en/managed-agents/define-outcomes)是上游合同。
- 本地 API 可接受事件、生成 ID 和 pending 条目，但示例读取的 `result` 字段并非新增条目的状态字段（`internal/sessions/service_helpers.go:300`、`:525`）。评分与投影需真实运行验证；常规事件校验与 initial events 约束也不同（`:416`，`internal/sessions/initial_events.go:49`）。
- Files 路由并不要求页面所称的 Beta 头（`docs/design/be/files-api-compatibility.md:3`）。先保留明确能力状态与已验证最小示例；闭环验证前不制作虚构结果截图。

## 密钥库

- 创建 Vault 和凭据等 cURL 多处漏 `?beta=true`，但 Vault 路由强制要求（`internal/vaults/handler.go:173`）。
- 自动注入受部署的 MITM、网络策略、目标主机和注入位置等约束；Tunnel 走独立路径，不能笼统承诺所有出站协议都自动注入，也不能保证智能体永远无法看到凭据（`docs/design/be/vault-runtime.md:375`）。写入加密依赖部署的 secret service/KMS（`internal/vaults/handler.go:439`）。
- 候选凭据按路径尝试，失败或 401 后可能继续；普通代理与 MCP 精确匹配规则不同（`internal/vaults/injector.go:225`，`internal/vaults/inject_match.go:53`）。补控制台空表单、MCP 选择、授权和会话 Vault 选择说明，截图不得展示密钥。对照 [Claude Vault](https://platform.claude.com/docs/en/managed-agents/vaults)。

## 访问 GitHub

- 更新 token 的 cURL 在续行后放注释，导致请求被截断，且缺鉴权与内容类型；不能盲取列表第一个资源。只有 Git 资源支持 token 更新（`internal/sessions/service.go:799`）。
- Git 克隆/推送 token 与 GitHub MCP 的 Vault 凭据是不同路径，示例不能据一个 token 推断 MCP 可以创建 PR（`internal/environments/managed_agent_runtime_resources.go:36`）。
- OMA 支持符合条件的通用 HTTPS Git、branch 和完整 commit SHA，有限制；自托管不接受 Git 资源（`internal/sessionresource/git_repository.go:54`，`internal/sessions/environment_resources.go:14`）。仓库缓存、技能自动发现和沙箱替换后的持续挂载应实测，不能复制 [Claude GitHub 页](https://platform.claude.com/docs/en/managed-agents/github)作为保证。补空白资源与 token 更新表单截图。

## 文件

- 上传文件 `downloadable=false`，不能对会话文件列表的任意第一项直接下载；区分上传输入、会话实例和可下载输出（`internal/files/handler.go:140`、`:294`，`docs/design/be/session-generated-file-download.md:103`）。
- 自托管不支持 file resource（`internal/sessions/environment_resources.go:14`）。输入文件默认只读挂载，生成输出的路径、实例生命周期和 500 个输入资源的范围见 `docs/design/be/filestore.md:136`、`:199`；真实 FUSE 与输出下载仍待验证。
- 本页 curl 中 `auth`、`base_url` 等变量未定义；Files 路由不强制 Beta（`docs/design/be/files-api-compatibility.md:3`）。应补自包含前置、错误说明及空白资源选择/脱敏下载截图，区分 [Claude 文件页](https://platform.claude.com/docs/en/managed-agents/files)的上游行为。

## 记忆存储

- 大多数 cURL 缺 `?beta=true`，而 Memory 路由要求 query（`internal/memory/handler.go:169`）。上游 Beta header 的互斥和过渡说明不能直接当作该路由的校验行为；SDK 自动加参数与指向 OMA 尚待验证。
- 文档称 `/mnt/memory/` 非挂载目录写入临时空间，当前运行提示要求父目录只读、挂载外写入拒绝（`internal/environments/memory_catalog.go:56`）；云和自托管实际挂载需真实验证。
- 30 天版本保留等承诺没有足够清理证据；删除可能异步重试，不能等同对象即时物理清除（`docs/design/be/memory-store.md:401`）。每 store 2000 项、单项 100 KiB、每会话 8 个挂载有代码依据（`internal/db/memory.go:18`，`internal/sessioncontract/resource.go:18`）。补 store 创建、内容浏览和挂载权限截图。对照 [Claude Memory](https://platform.claude.com/docs/en/managed-agents/memory)。

## 梦境

- 页首提示未完成，后面却仍以可执行语气描述创建、轮询、取消、计费与配额。当前服务未挂载 Dreams 路由，设计列为非目标（`internal/api/server.go:389`，`docs/design/be/memory-store.md:79`）；应将上游合同移至兼容说明或链接 [Claude Dreams](https://platform.claude.com/docs/en/managed-agents/dreams)，不放看似可在 OMA 运行的示例。
- 记忆存储页推荐 Dream 整理也需同步修正。界面是未接入空态（`web/src/features/managed-agents/resources/ManagedResources.tsx:212`）；不制作虚构运行截图。目标环境版本尚未验证。

## 多智能体编排

- 页面把 advisor 放入旧 coordinator 名单并承诺咨询，但旧类型只接受 agent/self（`internal/agents/handler.go:621`）。新类型 advisor 为独立配置字段；当前设计说明仅配置兼容，DSH 不执行 advisor consultation 或 workflow run（`internal/agents/cma_multiagent.go:110`，`docs/design/be/subagent-delegation.md:120`）。
- 控制台开放子智能体，隐藏 workflow/advisor（`web/src/features/managed-agents/agents/subagents-editor.test.tsx:31`）。应以已确认的子智能体为主，区分配置存储和实际执行；参考 [Claude 多智能体](https://platform.claude.com/docs/en/managed-agents/multiagent-orchestration)最新结构，不将其执行能力等同 OMA。
- 子线程权限和 MCP 作用域随运行路径不同，不能无条件保证子 Agent 的独立配置生效；线程上限包括保留的 idle 子线程，审批需要原始事件和线程身份（`docs/design/be/subagent-delegation.md:169`、`:215`、`:292`）。建议子智能体空表单、脱敏线程/审批截图。

## 计划部署

- OMA 不添加页面所述的 Claude 定时 jitter（`docs/design/be/deployments-api-contract.md:77`）；实际排队启动时间仍受运行环境影响。支持无 schedule 的手动部署，以及定时触发（`internal/deployments/cron.go:15`，`web/src/features/managed-agents/resources/deployment-schedule-fields.tsx:113`）。
- 手动 run 在依赖验证失败时可能记录错误且没有 session，成功也仅排队 work（`internal/deployments/handler.go:625`，`internal/deploymentjobs/execution.go:76`）。自动归档、无变化更新和重试按不同路径处理，不保证每次都有 Run 或通知（`internal/deploymentjobs/worker.go:53`，`docs/design/be/deployments-api-contract.md:136`）。
- 补初始事件约束、手动/定时设置、运行历史和失败处理；建议空表单与脱敏运行截图。时区边界、River 调度与真实运行尚未验收。对照 [Claude 计划部署](https://platform.claude.com/docs/en/managed-agents/scheduled-deployments)。

## 托管智能体参考

- 组织级 300/1200 每分钟限额来自上游，本地入口未找到相同限流依据；线上网关仍可能另有限制，宜按实际部署说明（`docs/zh/reference.mdx:114`）。系统消息模型白名单、outcome 自动评估和 redacted 占位等上游承诺也需核实（`internal/sessions/service_helpers.go:347`，`docs/design/be/brain-runtime.md:142`）。
- `ant beta:worker` 属第三方 CLI，版本、OMA 地址和鉴权兼容性待实测；MCP SSE 回退只在特定 HTTP 响应条件触发（`oma-brain-dsh/src/external-mcp/client.ts:55`）。Beta 条件需按 OMA 路由区分。对照 [Claude 参考](https://platform.claude.com/docs/en/managed-agents/reference)。

## 概览

- 统一 managed-agents Beta 头的说法不适合 OMA：Environment 和 Memory 需要 query，Session 线程还区分身份类型（`internal/environments/handler.go:153`，`internal/memory/handler.go:169`，`internal/sessions/thread_beta.go:12`）。各版本 SDK 自动处理行为尚未实测。
- 对话历史与特定挂载可持久，不意味着沙箱工作目录、临时文件和进程均可恢复（`docs/design/be/sandbox-lifecycle.md:5`）。删除受运行状态约束且回收异步（`internal/db/session_removal.go:31`，`internal/sessions/service.go:477`）。
- 搜索 Provider、镜像和网络均依赖部署；MCP 是外部集成，不能笼统列为内置工具。已有快速开始截图，建议补 Agent→环境→会话→事件概念图和控制台/API 入口。对照 [Claude 概览](https://platform.claude.com/docs/zh-CN/managed-agents/overview)。

## 控制台入门

- 创建 Agent 从工作区 MCP 目录或 Tunnel 选择，会话再通过 Vault 提供认证，服务声明不等于认证完成（`web/src/features/managed-agents/agents/create-dialog-rendered.tsx:97`）。补模型 Provider、可用环境、权限和预构建前置与排障。
- 对话构建包含保存 Agent、环境、测试及可选部署/集成，文档需解释草稿、确认和测试边界（`web/src/features/managed-agents/quickstart/builder/model.ts:7`，`web/src/features/managed-agents/quickstart/builder/api.ts:191`）。已有起始和创建截图，建议补脱敏测试、审批和集成页面；示例仍需自包含地址、密钥和 ID。对照 [Claude 控制台入门](https://platform.claude.com/docs/en/managed-agents/onboarding)。

## 迁移

- 完整 SDK 示例未配置 OMA 地址，环境 SDK 是否自动附 `beta=true` 待验证；cURL 固定 localhost 且 SSE 后台订阅与发消息有竞态，任何 idle 均退出会误判预算触顶或其他停止原因（`docs/zh/migration.mdx:253`、`:687`，`internal/sessions/budget.go:150`）。
- 工具可能禁用或等待审批；自托管不支持 file/Git 资源，显式固定 Agent 版本的会话不会自动使用新版本（`internal/sessions/environment_resources.go:14`，`internal/sessions/service_helpers.go:27`）。`always_ask` 不等于完整的执行前后 hook。
- 用一条最短迁移路径和事件流程图替代大量重复示例，补 OMA Web Provider 和网络前置（`internal/webtools/config.go:29`）。参照 [Claude 迁移](https://platform.claude.com/docs/en/managed-agents/migration)，SDK、Worker 和线上运行仍待实测。

## 快速入门

- 本轮环境 cURL 已加 query、Python/TS 显式配置 OMA；但环境 SDK 的 query、CLI 和其他语言客户端的地址与密钥仍未实测，部分示例可能指向上游（`docs/zh/quickstart.mdx:128`、`:164`、`:222`）。CLI 版本亦须先验证 OMA 兼容性再更新。
- cURL 发送消息/流步骤仅注释，无法完成入门流程；SDK 把任意 idle 当完成也会误判预算触顶（`internal/sessions/budget.go:222`）。需补可复制请求并处理审批、错误和 stop reason。
- 完整工具集并非全部默认启用，搜索依赖 Provider（`internal/agents/handler.go:1099`，`internal/webtools/providers.go:10`）；部分应用卡片无实际链接。补脱敏密钥入口、环境和运行结果截图。对照 [Claude 快速入门](https://platform.claude.com/docs/en/managed-agents/quickstart)。

## API：认证

- 普通资源通常使用工作区 API key，但 Filestore、环境、work 和 code-session 有独立受限凭据，不能笼统称所有 `/v1` 都相同（`internal/api/server.go:270`，`internal/api/service_auth.go:61`，`internal/api/work_auth.go:13`）。
- 补工作区 API 密钥入口、明文仅显示一次、禁用/删除和轮换；截图不要展示生成值（`web/src/features/settings/WorkspaceApiKeysPage.tsx:182`、`:364`）。非空 `X-Api-Key` 优先于 Bearer，冲突时不回退（`internal/auth/auth.go:67`）。SDK 和首次请求仍需实际验证。参照 [Claude 认证](https://platform.claude.com/docs/en/manage-claude/authentication)，不引入未经证实的上游身份能力。

## API：概览

- 首次请求和 SDK 示例硬编码本地默认密钥和地址，应链接认证并使用实际部署 URL 与工作区 key；默认种子可被配置覆盖（`internal/config/defaults.go:126`）。建议空白密钥创建表单截图，不显示生成值。
- 成功响应不全是 JSON：SSE、批处理 JSONL、文件原始 MIME 和上游代理均为例外（`internal/sessions/stream_hub.go:293`，`internal/batches/handler.go:446`，`internal/files/handler.go:318`，`internal/messages/handler.go:250`）。错误体、request ID 与鉴权需说明代理、流和 Filestore 的边界。
- SDK 兼容应限定版本和测试覆盖；模型、Provider、数据库、对象存储及 runner 是不同角色的部署前置。对照 [Claude API 概览](https://platform.claude.com/docs/en/api/overview)，目标部署和真实 SDK 尚未测试。

## API：版本与 Beta

- Agent 和普通 Session 不应与 Environment/Memory/Vault 统一写成必须 `beta=true`；后三者资源路由要求 query，Session 线程 API key 要特定 Beta 头，控制台身份另有例外（`internal/agents/handler_test.go:10`，`internal/sessions/thread_beta.go:12`，`internal/environments/handler.go:153`，`internal/memory/handler.go:169`，`internal/vaults/handler.go:173`）。
- Files 旧头仅兼容，Skills 的头决定旧版响应等兼容行为（`docs/design/be/files-api-compatibility.md:3`，`internal/skills/handler.go:168`）。部署和 Webhook 分别有自己的头及身份条件（`internal/deployments/handler.go:1531`，`internal/webhooks/handler.go:131`）；Tunnel 管理 API 也有专用头（`internal/tunnels/management_handler.go:22`）。
- 应按资源、身份、query/header 和错误列矩阵，再逐 SDK 验证；[Claude Beta 头](https://platform.claude.com/docs/en/api/beta-headers)不等于 OMA 合同。

## API：分页

- 不能统一以 `next_page` 缺失判断结束：Batches 使用 `has_more` 和 `after_id`/`before_id`，Models 当前返回全量及 `has_more=false`（`internal/batches/handler.go:79`，`internal/models/handler.go:46`）。
- Files 支持 page、after_id、before_id；page 不能与 ID 游标组合，且绑定 scope_id（`internal/files/pagination.go:18`）。事件游标绑定排序，会话列表游标不同（`internal/sessions/event_payload.go:304`）。每个资源应单列参数、响应、限制，保持原过滤条件并勿承诺快照一致性。
- 示例使用默认密钥和固定地址，需改为认证页的环境变量；未知筛选键不一定报错（`internal/agents/handler.go:192`）。参照 [Claude API 概览](https://platform.claude.com/docs/en/api/overview)的资源差异，不能照搬具体 Models 行为。

## API：错误

- OMA 本地错误通常使用统一信封，Messages 代理可透传上游 body，Filestore 也有独立形状（`internal/messages/handler.go:228`，`internal/filestore/errors.go:78`）。`request-id` 可由调用方提供，缺省才生成，不能保证每次都是服务端新建（`internal/api/server.go:412`）。
- 状态码与 error type 并非一一对应；429 也不保证可重试或有 Retry-After（`internal/httpapi/app_error.go:85`）。流建立后断线不能用普通 HTTP 错误体判断，写请求超时应先核查结果以避免重复。
- 补错误体、请求 ID 和 SSE 分支示例；上游与 SDK 行为未实测。对照 [Claude 错误](https://platform.claude.com/docs/en/api/errors)。
