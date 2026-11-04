# OMA 文档梳理：首轮全站盘点

盘点日期：2026-11-02。范围以 `docs.json` 的中文导航为准：27 篇用户指南、5 篇 API 入门文档，以及中英文 OpenAPI 的 67 条路径、105 个操作。逐篇检查可读性、配图需求和明显的实现差异；对 API 规范做中英文结构对照和重点抽查，并非 105 个操作逐字段或端到端验收。英文指南没有逐句审校。仓库可能与目标部署版本不同；代码存在不等于线上行为已验证。

优先级：P0 影响认证、安全或首次运行；P1 影响功能承诺或主要工作流；P2 影响查找、理解或维护。下列未标“已验证”的行为需进一步确认，不能据此作生产保证。

## API 入门（按导航顺序）

| 页面 | 发现与建议 | 视觉材料 |
| --- | --- | --- |
| API 概览 | P1 首次请求缺获取密钥引导；“成功响应使用 JSON”不适用于文件内容、SSE 和 JSONL。 | 简短请求/响应示例，链接到认证。 |
| 认证 | P0 旧示例硬编码本地默认密钥，取得密钥与首次请求未形成步骤；已改为工作区密钥、环境变量和两种认证方式。`X-Api-Key` 优先级见 `internal/auth/auth.go`。 | 后续增加空白、脱敏的 API 密钥入口截图；不要展示密钥值。 |
| 版本与 Beta | P0 表格把 Agent、Session、Environment、Memory 和 Vault 合为同一规则；Session 线程 API-key 路径要求特定 `anthropic-beta`，Environment/Memory/Vault 有各自的 query 要求。须逐路由核对后修表及 OpenAPI。 | 各资源一条可复制请求和参数表，比截图更适合。 |
| 分页 | P1 仅介绍 `page`/`next_page`；Files 另支持 `before_id`/`after_id`，与 `page` 的互斥和 `scope_id` 绑定规则需说明。 | 请求→响应→下一页示例。 |
| 错误 | P2 错误信封有实现依据；各状态码、上游 `Retry-After`、流已开始后的错误须进一步核实。 | 401/400 示例和 request ID 定位说明。 |

## 用户指南（按导航顺序）

| 页面 | 清晰度、事实风险及后续动作 | 适合的图或截图 |
| --- | --- | --- |
| 概览 | 区分控制台读者与 API 读者入口；避免把部署依赖写成普遍承诺。 | 已有快速开始截图；可参考 PDF 第 4 页解释 Agent、Environment、Session、Events。 |
| 快速入门 | P0 原始 cURL 环境请求缺 `beta=true`，Python/TS 客户端未指向 OMA；本轮已修正。其他语言及 CLI 未逐一验收，模型需使用工作区真实 ID。 | 脱敏密钥入口、创建首个会话及运行结果。 |
| 在控制台中构建 | 已补快速开始和 Agent 表单截图；补模型 Provider 前置条件。 | 后续增加在线测试和结果截图。 |
| 迁移 | 文档很长，缺少从消息循环到 Agent/Session 的最小迁移路线；上游合同须逐项核实。 | 消息循环与托管 Session 的对照图。 |
| 智能体设置 | 创建/版本/API 混杂；推理地域与数据驻留承诺待核实。 | 创建表单与版本管理截图。 |
| 工具 | P1 需区分内置工具、MCP、自定义工具及审批；100,000 字符输出策略不应泛化为所有工具。 | 分类与工具调用、人工确认流程。 |
| MCP 连接器 | 缺工作区 MCP 目录、Agent 选择与 Session Vault 串联流程；已加入基于 PDF 第 20 页并按实现校核的 Tunnel 时序图。 | 后续补服务器目录和 Agent 选择的空表单截图。 |
| 权限策略 | 长文不易看出默认值和覆盖优先级；`auto` 接受与运行语义须核实。 | 决策优先级图、实际工具审批卡。 |
| 技能 | P1 文档对预构建技能全工作区可用及自动调用说法过强；当前环境列表曾只显示部分内置技能，清单以实际部署为准。 | 系统/工作区技能列表、附加及版本选择截图。 |
| 云环境设置 | 已补脚本、资源、环境变量及示例；web 工具域名限制和环境网络策略的关系与创建校验存在疑点，须核实。 | 空白创建表单、网络选项和预构建状态/日志。 |
| 云沙盒参考 | P1 固定运行时、架构、资源规格与控制台默认网络不宜当成所有部署的保证；当前创建页观察为不受限。 | 部署可配置项表；真实镜像以部署为准。 |
| 自托管沙箱 | 长文应提供最短运行路径；数据“永不离开环境”与工具输入输出流经控制面的叙述需澄清。 | 控制面、worker、模型 Provider 与数据流/信任边界图。 |
| 自托管沙箱安全 | 划分部署者、OMA、上游的责任；异常检测等承诺待核实。 | 凭据及执行边界图、责任表。 |
| 启动会话 | 已有流程图；将最短路径置前，长示例折叠。地域限制与覆盖语义待核实。 | 空白创建会话、资源选择和工作台截图。 |
| 会话操作 | 区分中断、等待、归档与删除；状态变迁需对照实现。 | 状态图和操作截图。 |
| 会话事件流 | 内容详尽但入门路径过长；thinking 为进度信号。2026-11-03 复核：当前代码支持按 Last-Event-ID 回放持久公开事件，临时 delta 不在历史中；原先“不自动回放”的判断有误，详见 `internal/sessions/stream_replay.go`，目标部署仍待验证。 | SSE 时序与事件、工具审批截图。 |
| 会话预算 | P1 顶部“未实现”与正文及当前代码相矛盾；上游的未舍入计价说明与 OMA 美分舍入口径不同，须校准。 | 配置预算、触顶状态截图及计量流程。 |
| Webhooks | 区分预算控制与 `session.budget_reached` 专用通知；专用通知入口需逐项核查。 | 空白订阅表单、事件→投递→验签/去重图。 |
| 定义结果 | 现有完整评估叙述与兼容性矩阵“部分支持”不一致；运行闭环须真实验证。 | 在能力确认前仅画定义与已确认事件，不画虚构运行。 |
| 密钥库 | 补清楚创建、引用、密钥更新及部署者责任。 | 空白表单和控制面/出站凭据边界图；不得含真实凭据。 |
| 访问 GitHub | 区分 Git 资源与 GitHub MCP；当前 URL 验证支持符合限制的通用 HTTPS Git，不代表所有主机都已验证。缓存承诺须核实。 | 资源挂载空表单、认证与网络关系图。 |
| 文件 | cURL 示例引用的 `auth` 和 `base_url` 在页内未定义，需给自包含前提。 | 上传、挂载和下载的生命周期图，空表单截图。 |
| 记忆存储 | P0 多数 cURL 示例缺服务端要求的 `beta=true`；引用 Dream 整理流程与其未完整实现提示矛盾。 | 记忆存储与会话的只读/读写挂载、版本图。 |
| 梦境 | 保留清晰的未完成提示，区分目标合同和当前可运行能力。 | 不用虚构功能截图。 |
| 多智能体编排 | P1 advisor 和 workflow 的运行承诺超过当前设计文档中已实现的范围；配置被接受不等同执行。 | 已确认的协调器/子线程关系图及配置截图；未经验证不画顾问执行。 |
| 计划部署 | 手动运行与定时运行均需解释；`initial_events` 的 `system.message` 及约束须对照实现。 | 触发器表单、部署→运行→会话关系图。 |
| 托管智能体参考 | 固定 Claude 模型及事件能力列表需要逐项核对 OMA 的 Provider 和运行时。 | 保持表格与短链接，不强制截图。 |

## 全站横切问题

- `mint broken-links --check-anchors` 对指南前半的抽查报告 8 个文件、46 个链接，后半报告 15 个文件、107 个链接；检查范围有重叠，不能相加当作唯一数量。多数涉及英文 fragment 与中文标题生成的 slug；这些是工具报告，尚未逐项人工验证。
- 当前中英文 OpenAPI 的路径与方法集合一致（67 条路径、105 个操作）；这仅证明结构对齐，不证明两种语言内容或接口合同准确。已发现线程 SSE 的响应类型等候进一步修正。
- 公开指南、API schema、设计文档和控制台文案应按功能共同更新。架构细节、部署者专属说明和 API 参数不应照搬成最终用户承诺。
- 截图使用空表单或专用演示数据，避免账户名、工作区与资源 ID、私有地址、凭据和真实会话内容；标注语言、示例模型与界面可能因部署不同而变化。图形应配文字说明，并在浅色/深色和小屏复核。

## PDF 的使用边界

用户提供的《OMA企业级智能体托管平台》共 29 页。第 4 页可用于规划核心概念图；第 10–18 页有架构、安全和文件系统图，第 20 页展示 MCP Tunnel，第 24–28 页是场景示例。PDF 包含产品介绍和示意而非当前版本的 API 合同；部分“所有写操作必经确认”“数据不出域”等绝对表述及第三方能力依赖部署，不能直接当作线上保证。已将第 20 页的核心路径重新绘制为可维护的 MCP 时序图；其他图应经代码、部署和敏感信息核对后再融入。

## 后续顺序

1. 先修认证、Beta、快速入门及可复制请求，再逐条清理高频跳转锚点。
2. 对预算、数据驻留、顾问/工作流、结果评估、网络和文件生命周期核对现有实现及线上版本，明确可用、部分支持和未验证边界。
3. 按创建 Agent→环境→会话→工具审批→资源→部署的用户旅程补充脱敏截图；为安全、数据流、SSE 等抽象概念制作图。
4. 逐资源对照 105 个 API 操作及响应，并对英文指南与所有示例进行实际校验；区分 SDK 兼容和运行时能力。

## API 操作清单及复核状态

以下为规范中的操作清单，仅按方法和路径盘点；除正文明确指出的问题，均未逐字段与实现或线上行为核验。优先按资源分批核对权限、请求、响应、错误、分页和示例。

- `GET /v1/models` — 列出模型
- `POST /v1/messages` — 创建消息
- `POST /v1/messages/batches` — 创建消息批处理
- `GET /v1/messages/batches` — 列出消息批处理
- `GET /v1/messages/batches/{message_batch_id}` — 检索消息批处理
- `DELETE /v1/messages/batches/{message_batch_id}` — 删除消息批处理
- `POST /v1/messages/batches/{message_batch_id}/cancel` — 取消消息批处理
- `GET /v1/messages/batches/{message_batch_id}/results` — 检索消息批处理结果
- `POST /v1/files` — 上传文件
- `GET /v1/files` — 列出文件
- `GET /v1/files/{file_id}` — 获取文件元数据
- `DELETE /v1/files/{file_id}` — 删除文件
- `GET /v1/files/{file_id}/content` — 下载文件
- `POST /v1/skills` — 创建技能
- `GET /v1/skills` — 列出技能
- `GET /v1/skills/{skill_id}` — 获取技能
- `DELETE /v1/skills/{skill_id}` — 删除技能
- `POST /v1/skills/{skill_id}/versions` — 创建技能版本
- `GET /v1/skills/{skill_id}/versions` — 列出技能版本
- `GET /v1/skills/{skill_id}/versions/{version}` — 获取技能版本
- `DELETE /v1/skills/{skill_id}/versions/{version}` — 删除技能版本
- `GET /v1/skills/{skill_id}/versions/{version}/content` — 下载技能版本内容
- `POST /v1/environments` — 创建环境
- `GET /v1/environments` — 列出环境
- `GET /v1/environments/{environment_id}` — 获取环境
- `POST /v1/environments/{environment_id}` — 更新环境
- `DELETE /v1/environments/{environment_id}` — 删除环境
- `POST /v1/environments/{environment_id}/archive` — 归档环境
- `GET /v1/environments/{environment_id}/work/stats` — 获取队列统计
- `GET /v1/environments/{environment_id}/work/poll` — 轮询 worker
- `POST /v1/environments/{environment_id}/work/{work_id}/ack` — 确认 worker
- `POST /v1/environments/{environment_id}/work/{work_id}/heartbeat` — 记录心跳
- `POST /v1/environments/{environment_id}/work/{work_id}/stop` — 停止 worker
- `GET /v1/environments/{environment_id}/work/{work_id}` — 获取 worker
- `POST /v1/environments/{environment_id}/work/{work_id}` — 更新 worker
- `GET /v1/environments/{environment_id}/work` — 列出 worker
- `POST /v1/sessions` — 创建会话
- `GET /v1/sessions` — 列出会话
- `GET /v1/sessions/{session_id}` — 获取会话
- `POST /v1/sessions/{session_id}` — 更新会话
- `DELETE /v1/sessions/{session_id}` — 删除会话
- `POST /v1/sessions/{session_id}/archive` — 归档会话
- `GET /v1/sessions/{session_id}/events` — 列出会话事件
- `POST /v1/sessions/{session_id}/events` — 发送会话事件
- `GET /v1/sessions/{session_id}/events/stream` — 流式接收会话事件
- `GET /v1/sessions/{session_id}/threads/{thread_id}/events` — 列出会话线程事件
- `GET /v1/sessions/{session_id}/threads/{thread_id}/stream` — 流式接收会话线程事件
- `GET /v1/sessions/{session_id}/threads` — 列出会话线程
- `GET /v1/sessions/{session_id}/threads/{thread_id}` — 获取会话线程
- `POST /v1/sessions/{session_id}/threads/{thread_id}/archive` — 归档会话线程
- `GET /v1/sessions/{session_id}/resources` — 列出会话资源
- `POST /v1/sessions/{session_id}/resources` — 添加会话资源
- `GET /v1/sessions/{session_id}/resources/{resource_id}` — 获取会话资源
- `DELETE /v1/sessions/{session_id}/resources/{resource_id}` — 删除会话资源
- `POST /v1/sessions/{session_id}/resources/{resource_id}` — 更新会话资源
- `POST /v1/agents` — 创建智能体
- `GET /v1/agents` — 列出智能体
- `GET /v1/agents/{agent_id}` — 获取智能体
- `POST /v1/agents/{agent_id}` — 更新智能体
- `POST /v1/agents/{agent_id}/archive` — 归档智能体
- `GET /v1/agents/{agent_id}/versions` — 列出智能体版本
- `POST /v1/agents:search` — 搜索智能体
- `POST /v1/deployments` — 创建部署
- `GET /v1/deployments` — 列出部署
- `GET /v1/deployments/{deployment_id}` — 获取部署
- `POST /v1/deployments/{deployment_id}` — 更新部署
- `POST /v1/deployments/{deployment_id}/archive` — 归档部署
- `POST /v1/deployments/{deployment_id}/pause` — 暂停部署
- `POST /v1/deployments/{deployment_id}/unpause` — 恢复部署
- `POST /v1/deployments/{deployment_id}/run` — 立即运行部署
- `GET /v1/deployment_runs` — 列出部署运行
- `GET /v1/deployment_runs/{deployment_run_id}` — 获取部署运行
- `POST /v1/vaults` — 创建密钥库
- `GET /v1/vaults` — 列出密钥库
- `GET /v1/vaults/{vault_id}` — 获取密钥库
- `POST /v1/vaults/{vault_id}` — 更新密钥库
- `DELETE /v1/vaults/{vault_id}` — 删除密钥库
- `POST /v1/vaults/{vault_id}/archive` — 归档密钥库
- `POST /v1/vaults/{vault_id}/credentials` — 创建凭据
- `GET /v1/vaults/{vault_id}/credentials` — 列出凭据
- `GET /v1/vaults/{vault_id}/credentials/{credential_id}` — 获取凭据
- `POST /v1/vaults/{vault_id}/credentials/{credential_id}` — 更新凭据
- `DELETE /v1/vaults/{vault_id}/credentials/{credential_id}` — 删除凭据
- `POST /v1/vaults/{vault_id}/credentials/{credential_id}/archive` — 归档凭据
- `POST /v1/vaults/{vault_id}/credentials/{credential_id}/mcp_oauth_validate` — 验证凭据
- `POST /v1/memory_stores/{memory_store_id}/memories` — 创建记忆
- `GET /v1/memory_stores/{memory_store_id}/memories` — 列出记忆
- `GET /v1/memory_stores/{memory_store_id}/memories/{memory_id}` — 检索记忆
- `POST /v1/memory_stores/{memory_store_id}/memories/{memory_id}` — 更新记忆
- `DELETE /v1/memory_stores/{memory_store_id}/memories/{memory_id}` — 删除记忆
- `GET /v1/memory_stores/{memory_store_id}/memory_versions` — 列出记忆版本
- `GET /v1/memory_stores/{memory_store_id}/memory_versions/{memory_version_id}` — 检索记忆版本
- `POST /v1/memory_stores/{memory_store_id}/memory_versions/{memory_version_id}/redact` — 编辑记忆版本
- `POST /v1/memory_stores` — 创建记忆存储
- `GET /v1/memory_stores` — 列出记忆存储
- `GET /v1/memory_stores/{memory_store_id}` — 检索记忆存储
- `POST /v1/memory_stores/{memory_store_id}` — 更新记忆存储
- `DELETE /v1/memory_stores/{memory_store_id}` — 删除记忆存储
- `POST /v1/memory_stores/{memory_store_id}/archive` — 归档记忆存储
- `POST /v1/webhooks` — 创建 Webhook
- `GET /v1/webhooks` — 列出 Webhook
- `GET /v1/webhooks/{webhook_id}` — 获取 Webhook
- `POST /v1/webhooks/{webhook_id}` — 更新 Webhook
- `DELETE /v1/webhooks/{webhook_id}` — 删除 Webhook
- `POST /v1/webhooks/{webhook_id}/regenerate_signing_secret` — 重新生成 Webhook 签名密钥
