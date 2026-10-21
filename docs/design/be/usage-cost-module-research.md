# 用量与成本模块核对

核对时间：2026-10-14，结论补记于同日。本文对照 Anthropic 公开文档、Claude Console 费用页空态，以及本仓库现有实现，说明 OMA 成本模块要对齐的两套合同、CMA 的计费口径，以及现有数据为什么还不能直接出报表。结论分已证实、可推断但未证实、未知。本次没有调用 Anthropic 线上 Admin API，也没有抓到 `/usage_cost` 的真实 JSON。费用页布局来自同日对 `https://platform.claude.com/cost` 的空数据截图。

## 结论

后面实现以这一节为准。

**已证实：Managed Agents 会话不再另收代码执行的容器小时费。** [定价页](https://platform.claude.com/docs/en/about-claude/pricing) 写明，session runtime 替代代码执行的容器小时计费；使用 Claude Managed Agents 时，不再在会话运行费之外加收容器小时。会话运行费只在状态为 `running` 时计提，单价每 session-hour 0.08 美元，按毫秒计量。`idle`、`rescheduling`、`terminated` 不计。[会话用量](https://platform.claude.com/docs/en/managed-agents/session-observability) 写明计价时长是 `usage.active_seconds`：至少一个线程处于运行的累计时间，并发线程的重叠只计一次。

这笔替代只覆盖容器小时。会话里跑代码产生的 token 仍进 Token 费用。会话内 web search 仍按每 1000 次 10 美元另计，失败的搜索不计。定价示例把一小时会话拆成 token 加 session runtime，没有代码执行容器行。

**已证实：同一组织的「代码执行总费用」卡仍可以有数。** Messages API 的代码执行工具不走 Managed Agents 会话时，继续按容器小时计，和 token 分开：[定价页](https://platform.claude.com/docs/en/about-claude/pricing) 与 [代码执行工具](https://platform.claude.com/docs/en/agents-and-tools/tool-use/code-execution-tool) 写的是单次最少 5 分钟，每组织每月 1,550 小时免费，超出后每容器小时 0.05 美元。请求里带了文件时，即使没调用该工具也会计时。请求同时包含 `web_search_20260209`（或更新版本）或 `web_fetch_20260209`（或更新版本）时，代码执行不再另收费，只收 token。因此「CMA 会话不收容器小时」不等于这张组织级卡片永远是 0。

**已证实：费用页同时展示这两项，但文档没有定义卡片怎么加总。** 2026-10-14 的 Console 空态截图上，`/cost` 有五张卡：总费用、Token 总费用、网页搜索总费用、代码执行总费用、会话运行总成本，当时都是 `US$0.00`。筛选是工作区、API 密钥、模型、范围（最近 30 天）、分组方式（模型）。图标题是「每日 Token 费用」，说明包含来自 API 和 Console 的 token 费用，日期均为 UTC。空态只能说明该组织当时没有用量，不能证明代码执行已经并进会话运行费。

文档没有写「代码执行总费用」等于哪些 `cost_type=code_execution` 行的和，也没有写「会话运行总成本」等于 `active_seconds × 0.08` 或 `cost_type=session_usage` 的和，更没有写「总费用」是其余四张卡之和。[Cost API](https://platform.claude.com/docs/en/api/admin/cost_report/retrieve) 只把类型列成 `tokens`、`web_search`、`code_execution`、`session_usage`，说明是 “Type of cost”。[Usage and Cost API](https://platform.claude.com/docs/en/manage-claude/usage-cost-api) 的成本类型一句只提到 token、网页搜索和代码执行，并说代码执行出现在成本接口的 `description` 里，文案为 `Code Execution Usage`，用量接口不包含它。该指南没有提到会话运行。会话上的 `list_cost.amount` 是按公开标价累计的整美分数字符串，文档没有把它拆成这五张卡。

## Qoder Cloud Agents 的成本

核对时间：2026-10-14。来源是 Qoder 公开文档，没有 Qoder 服务端源码，也没有调用其线上接口。能确定的是对外合同和记账时序；token 折成 Credits 的单价、沙箱每秒单价，以及内部账本表结构都没有公布。

Qoder Cloud Agents 与 CMA、OMA 同属托管 Agent：Agent、Environment、Session、Event。它的成本单位是 Credits，不是美元。一个 Session 只有两笔消耗，从同一 Credits 余额扣：[计费](https://docs.qoder.com/zh/cloud-agents/billing)。

| 计费项 | 计量 | 对外字段 |
| --- | --- | --- |
| 模型调用 | 按该次任务的 token 折算。失败的模型调用不扣该次 Credits | `usage.model_credits` |
| 沙箱运行 | 只在 Session 状态为 `running` 时按秒计量。`pause`、`idle`、`terminated` 不计。2026-08-10 起正式收费 | `usage.sandbox_runtime_credits` |

`usage.total_credits` 是这两项以及文档点名的其他已计入分项之和。三项都向下取整，最多两位小数，例如 `7.6681` 返回 `7.66`。它们是同一张快照，按 Session ID 覆盖，不能把多次查询相加。[Session 数据结构](https://docs.qoder.com/zh/cloud-agents/api/sessions/schemas)。

文档没有给 Cloud Agents 单独列出网页搜索费或代码执行容器小时费。工具调用带来的 token 进入模型 Credits。沙箱时长是容器费，角色接近 CMA 的 session runtime。存储（对话、产出文件、附件、私有 Skill 仓库、个人持久化数据）在文档撰写时限时免费。沙箱约占单次 Session 总 Credits 的 5%–10% 只是参考，不是费率。

个人空间预付 Credits 资源包，余额不足时任务可能无法发起或继续。企业空间绑定服务账号，从该账号专用资源包扣减，并可按账号设置 Credits 上限；未设置时显示不限额。

会话上的实现合同：

- 单次模型请求结束时发 `span.model_request_end`。有数据时 `model_usage` 只有 `credits`，这是本次增量，可以为 `0`。事件不返回 token 数。
- 上下文压缩若上游返回可计费 credits，写在 `agent.thread_context_compacted` 的 `usage.credits`，并且已经计入 `model_credits`。客户端不能再加一次。
- 进入 `session.status_idle` 之前，若已有用量，先发一条 `session.usage`，字段仍是三项累计值。轮次进行中不发用量事件。
- 沙箱按秒计量、按小时结算。本轮 idle 之后才结算的沙箱费不在当次 `session.usage` 里；结算后要从 GET Session 读，并会出现在下一次 idle 前的快照中。
- 预算是 `{"type":"limit","max_credit_cost":"100.00"}`，正十进制字符串，最多 12 位整数、2 位小数。检查的是 `total_credits`，包含所有线程、Advisor、Grader、上下文压缩和沙箱。文档写明由 CAS 在收到新用户消息时检查。进行中的轮次超限仍会跑完，下一条用户消息被拒绝。把上限改到已用额度以上不会自动恢复，需要再发一条用户消息。新上限低于或等于已用 credits 返回 400。
- Thread 对象当前不返回 `usage`。`stats.active_seconds` 是活跃处理时长，`stats.duration_seconds` 是 Session 持续时长，两者分开。

组织汇总是另一套接口，不按天补空桶：

- `GET /api/v1/forward/usage/identities` 按 Identity 汇总。
- `GET /api/v1/forward/usage/templates` 按 Template 汇总。

窗口是整小时。返回 `session_count`、`active_seconds` 和合并后的 `credits`。`active_seconds` 先把毫秒加总再除以 1000，不再四舍五入。`credits` 是模型 Credit 与运行 Credit 之和，向下取整到两位；没采集到的按 0。没有活动的 Identity 或 Template 不返回，也不补 0 行。分组按 credits 降序，相同则按 ID 升序。这里不再拆 `model_credits` 和 `sandbox_runtime_credits`。

和 CMA 并排放：CMA 用美元，组织报表把 token、网页搜索、代码执行、会话运行分成四类；Qoder 用 Credits，会话上只拆模型和沙箱，组织接口再合成一个数。OMA 目前只有会话级 token 计数，没有 Credits，也没有沙箱运行费。

## 1. 两套合同，不能混成一个接口

**已证实：** Claude 把“用量和成本”拆成对外 Admin API 和对内 Console API。公开指南写明，Usage & Cost Admin API 提供的数据类似于 Claude Console 用量和成本页面，但不是同一个 URL 和响应形状。

| 表面 | 路径 | 调用方 | OMA 现状 |
| --- | --- | --- | --- |
| Admin Usage | `GET /v1/organizations/usage_report/messages` | Admin 凭证 | 按时间桶返回 token 与 `active_milliseconds` 增量 |
| Admin Cost | `GET /v1/organizations/cost_report` | 组织管理员 | 按日返回运行时成本增量，只接受 `bucket_width=1d` |
| Admin Claude Code | `GET /v1/organizations/usage_report/claude_code` | 组织管理员 | 只校验 `YYYY-MM-DD`，返回空 `data` |
| Console 成本 | `GET /api/organizations/{org}/usage_cost` 和 `.../workspaces/{workspaceId}/usage_cost` | 控制台页面 | 固定返回五个空对象 |
| Console 页面 | `/cost`、`/workspaces/{id}/cost` | 浏览器 | 请求 `/runtime_cost`。运行时总成本来自接口；Token 与网页搜索卡仍是 `USD 0.00`。不带工作区时只允许组织管理员 |

来源：

- [Usage and Cost API](https://platform.claude.com/docs/zh-CN/manage-claude/usage-cost-api)
- [Get Messages Usage Report](https://platform.claude.com/docs/en/api/admin/usage_report/retrieve_messages)
- [Get Cost Report](https://platform.claude.com/docs/en/api/admin/cost_report/retrieve)
- `internal/admin/handler.go` 第 80–85 行，`internal/admin/service.go` 第 529–547 行
- `internal/platformapi/platform_backend_routes.go` 第 74–75、127–134 行
- `web/src/features/analytics/AnalyticsPages.tsx` 的 `CostPage`

Admin API 需要 Admin API 密钥（`sk-ant-admin01-...`）、带 `org:admin` 的 OAuth 令牌，或未限定工作区的个人/服务账号密钥。工作区 API 密钥不能访问。Claude Enterprise（claude.ai）走另一套 Analytics API，不在本页范围内。AWS 上的 Claude Platform 没有这些编程端点。

OMA 的 `/v1/organizations/cost_report` 已挂在组织管理员中间件上，`internal/api/workspace_actions.go` 第 33–34 行把路径中含 `/cost_report` 的动作限制为组织管理权限。这和“工作区密钥不能看组织账单”的方向一致。`reportQuery` 目前只读取 `starting_at`、`ending_at`、`bucket_width`、`limit`、`page`，不解析 `group_by[]` 和各类过滤器。`limit` 解析失败时静默变成 0，不返回 400。

## 2. Usage API：按时间桶统计 token，不算美元

`GET /v1/organizations/usage_report/messages`

**已证实的查询参数：**

- `starting_at`：必填，RFC 3339。桶起点按 `bucket_width` 对齐到 UTC 的分钟、小时或日。
- `ending_at`：可选。返回“结束时间早于该时刻”的桶，终点是开区间。
- `bucket_width`：`1d`（默认）、`1h`、`1m`。
- `group_by[]`：`account_id`、`api_key_id`、`context_window`、`inference_geo`、`model`、`service_account_id`、`service_tier`、`speed`、`workspace_id`。`speed` 需要请求头 `anthropic-beta: fast-mode-2026-02-01`。
- 过滤：`models[]`、`workspace_ids[]`、`api_key_ids[]`、`account_ids[]`、`service_account_ids[]`、`service_tiers[]`、`context_window[]`、`inference_geos[]`、`speeds[]`。
- `service_tiers[]` 取值：`standard`、`batch`、`flex`、`flex_discount`、`priority`、`priority_on_demand`。
- `context_window[]`：`0-200k`、`200k-1M`。
- `inference_geos[]`：`global`、`us`、`not_available`。2026 年 2 月之前发布的模型不支持 `inference_geo` 请求参数，该维度报 `not_available`。
- `limit` 与 `page`：按桶分页，不是按结果行分页。

粒度上限：

| 粒度 | 默认桶数 | 最大桶数 |
| --- | --- | --- |
| `1m` | 60 | 1440 |
| `1h` | 24 | 168 |
| `1d` | 7 | 31 |

**已证实的响应：** `data[]` 按时间升序，每个 `bucket_width` 一个桶，没有用量的桶也返回，`results` 为空。每页最多 `limit` 个桶。桶含 `starting_at`（含）和 `ending_at`（不含）。顶层 `has_more`、`next_page`；下一页把 `next_page` 原样放进 `page`。

未分组时，分组维度字段为 `null`。分组后每个 `results[]` 元素包含：

- token：`uncached_input_tokens`、`cache_read_input_tokens`、`output_tokens`
- `cache_creation.ephemeral_5m_input_tokens`、`cache_creation.ephemeral_1h_input_tokens`
- `server_tool_use.web_search_requests`
- 维度：`model`、`workspace_id`、`api_key_id`、`account_id`、`service_account_id`、`service_tier`、`context_window`、`inference_geo`

指南里的特殊空值：

- 默认工作区的 `workspace_id` 是 `null`。
- Console playground（以及旧 Workbench）用量不挂 API key，按 `api_key_id` 分组时该字段仍是 `null`。
- 代码执行不出现在 Usage 端点，只出现在 Cost 端点。
- Priority Tier 要在 Usage 里按 `service_tier=priority` 看；Cost 端点不包含 Priority Tier 金额。

数据通常在请求完成后 5 分钟内可见，延迟可能更长。持续轮询建议不超过每分钟一次；短时翻页可以更频繁。仪表板应缓存结果。

## 3. Cost API：美元金额，按日，单位是美分的十进制字符串

`GET /v1/organizations/cost_report`

**已证实：**

- 只支持 `bucket_width=1d`，默认也是 `1d`。
- `limit` 默认 7，最小 1，最大 31。
- `group_by[]` 只有 `workspace_id` 和 `description`。没有 `model`、`api_key_id`。
- 按 `description` 分组时，响应才带解析字段：`model`、`inference_geo`、`context_window`、`service_tier`、`token_type`、`cost_type`。不按 description 分组，或不是 token 成本时，这些字段为 `null`。
- `amount` 是字符串，单位是货币最小单位。`"123.45"` 且 `currency=USD` 表示 **1.23 美元**，不是 123.45 美元。示例里出现过 `"123.78912"`，所以允许小数美分，不能收成整数。
- `currency` 目前恒为 `USD`。
- `cost_type`：`tokens`、`web_search`、`code_execution`、`session_usage`。不按 description 分组时为 `null`。
- `token_type`：`uncached_input_tokens`、`cache_creation.ephemeral_5m_input_tokens`、`cache_creation.ephemeral_1h_input_tokens`、`cache_read_input_tokens`、`output_tokens`。
- Cost 的 `service_tier` 只有 `standard` 和 `batch`。Usage 里的 `priority`、`flex` 不会出现在这里。
- 空桶同样返回，`results` 为空。分页语义与 Usage 相同。

官方示例中，同一条结果可以同时有 `description: "Claude Opus 5.5 Usage - Input Tokens"` 和 `token_type: "cache_creation.ephemeral_1h_input_tokens"`。`description` 是展示文案，`token_type` 才是稳定分类。实现时应以 `token_type` / `cost_type` 做聚合，不要解析英文 description。

## 4. CMA 的计费口径

定价页的 Claude Managed Agents 一节是成本类型 `session_usage` 的依据：[Pricing](https://platform.claude.com/docs/en/about-claude/pricing)。

**已证实：CMA 会话的标价是 token、web search 和 session runtime。容器小时不另收。** 单价、计量和「组织级代码执行卡仍可能有数」见上文「结论」。补充：`model.speed=fast` 使用 fast mode 加价。`model.inference_geo=us` 对全部 token 类别乘 1.1。Batch 折扣不适用于会话，因为会话是有状态的交互，没有 batch 模式。

缓存相对基础输入价的倍率：5 分钟写入 1.25 倍，1 小时写入 2 倍，缓存读取默认 0.1 倍。Fable 5.1 / Mythos 5.1 的读取是 0.025 倍；Opus 5.5 / Sonnet 5.5 的读取是 0.05 倍。这些倍率与 Batch、数据驻留叠加。Fast mode 目前列了 Opus 5.5（输入 8 / 输出 40 美元每百万 token）和 Opus 5、Opus 4.8（输入 10 / 输出 50）。Fast mode 不能和 Batch 一起用。

代码执行（非 CMA 会话）按容器小时计：每次最少 5 分钟，每组织每月 1550 小时免费，超出后每容器小时 0.05 美元。请求里带了文件时，即使没调用工具也会计执行时间。它和 token 分开记账，所以 Cost 里是独立的 `code_execution`，Usage 的 messages 端点不包含它。

Web search 每次搜索计 1 次，与返回条数无关；搜索出错不计费。这对应 Usage 的 `server_tool_use.web_search_requests` 和 Cost 的 `web_search`。

Claude Code Analytics API（`/usage_report/claude_code`）是另一条按用户、按日的报表，带 `estimated_cost.amount`（美分数字，不是 Cost API 那种十进制字符串）和生产力指标。指南明确说：密钥很多时不要用它做通用 API 成本拆分，通用用量应走 Usage API，把 token 当成本代理。

## 5. OMA 控制台已经摆出的 CMA 形状

**已证实：页面是空壳，后端合同比页面多两个字段。**

`CostPage` 读取 `GET /api/organizations/{org}/runtime_cost`。三张指标卡是 token 成本、网页搜索成本、运行时总成本。前两张仍是 `USD 0.00`。运行时总成本与每日图按 `session.usage` 的 `runtime_cost_usd` 增量汇总。金额在一段运行结束时按当时的 `runtime_cost.usd_per_hour` 写入。没写这项时不写入新金额，成本页提示未配置。之后改单价不改变已写入的金额。没有该字段的旧快照在配置了单价之后，按当前配置折算。展示使用美元符号。筛选是工作区、分组（模型、工作区、服务账号）、模型、时间范围（近 7 天、近 30 天、本月至今）。工作区路由会锁住工作区筛选。

测试固定的 Console 查询是：

- `/usage_cost?starting_on=2026-06-01&ending_before=2026-07-01&group_by=model`
- `/workspaces/default/usage_cost?...&group_by=api_key_id`

响应必须含五个对象：`costs`、`web_search_costs`、`code_execution_costs`、`session_usage_costs`、`claude_code_savings`。测试只断言它们是空对象（`tests/platform_console_backend_api_test.go`）。

这里和 Admin Cost API 的差别已经能确定：

- 时间参数是 `starting_on` / `ending_before` 的日期，不是 RFC 3339 的 `starting_at` / `ending_at`。
- `group_by` 用了 `model` 和 `api_key_id`。Admin Cost 不允许这两个值，模型只在 `group_by[]=description` 的解析字段里出现。
- 响应是按成本类型分开的对象，不是 `data[].results[]` 时间桶。
- 页面三张卡没有 session runtime，也没有 Claude Code 节省额；后端却预留了 `session_usage_costs` 和 `claude_code_savings`。

**未知：** 这五个对象内部的键、序列点、金额单位。仓库里没有 CMA 的真实 payload，空对象测试不能外推字段。实现前需要一份 Console 响应样本，或者先把 Admin API 的桶作为唯一稳定合同，Console 再做投影。

同文件的用量页、缓存页、日志页同样是静态空态。`/usage` 的分组是模型、工作区、服务账号；日志表头含 Time、ID、Model、Input Tokens、Output Tokens、Type、Service Tier、Request。这些属于用量模块，不是本次 Cost 端点的响应字段。

## 6. 现有账本不够填这两套合同

**已证实：** 会话用量是累加计数，不是可分桶的流水。

`internal/db/sessions.go` 的 `SessionUsageIncrement` 只有五个计数：`InputTokens`、`OutputTokens`、`CacheReadInputTokens`、`CacheCreation5mInputTokens`、`CacheCreation1hInputTokens`。`nil` 表示该字段未知，不是零。`session_mapper.xml` 的 `usageIncrement` 把它们加进 `sessions.usage` 和 `session_threads.usage` 的 JSON。没有模型、服务层级、上下文窗口、推理地域、速度、API key、web search 次数，也没有金额。

写入点在 `internal/db/session_event_state.go`：事件带 `UsageIncrement` 时调用 `AddUsage`。前端检查器会读 `usage.list_cost.amount`（单位按美分解释，见 `web/src/features/managed-agents/sessions/sessionInspectorModel.ts`），但 `internal/` 里没有写入 `list_cost` 的代码。因此会话详情的成本曲线现在也不会有点。

设计文档 `docs/design/fe/sessions/session-conversation-workspace.md` 写明：后端提供线程级费用之前，不展示 Cost 占位，也不伪造费用。

由此推不出按日、按模型、按 description 的 Cost 报表。把会话累计 JSON 再求和，会丢失：

- 请求发生时间，无法对齐 UTC 日桶，也无法保留空桶。
- 模型。一次会话可以换模型，累计 JSON 没有模型维度。
- token 类型以外的成本：web search、代码执行、session runtime。
- Admin Cost 要求的 `description` 分组解析字段。
- 同一计数被加总后，不能再按 `service_tier` 把 Priority 从 Cost 里剔除。

**可推断但未证实：** 单次模型请求的 usage 已经出现在会话事件 payload 里（`span.model_request_end` 的 `model_usage`，以及 Messages 代理观测到的 `message_start` / `message_delta` usage）。这些是事件事实，不是为报表建的账本。直接对事件表做分析查询会把账单读路径绑到事件存储和归档策略上。是否足以回放历史，要另查事件保留和 payload 是否稳定包含模型 ID，本次没有逐条核对事件 schema。

OMA 的模型调用走组织自己的 LLM Provider（见 `docs/design/be/messages-proxy.md` 与 `llm_providers`）。Anthropic 账单对账在这里不成立：OMA 看不到供应商发票。Admin API 的 `amount` 若要填，只能是本地价目算出的估计值，并在合同上仍使用美分十进制字符串。供应商真实单价未进入 `llm_providers` 表，该表只存 endpoint、密钥和 `model_ids`。

## 7. 实现时应对齐的边界

这些是从上述合同直接推出的约束，不是新的产品方案。

- Admin Usage 与 Admin Cost 分开实现。Cost 不从 Usage 响应换算后再当 Usage 返回；Usage 不返回美元。
- Cost 的 `amount` 用十进制字符串，单位是美分。`$1.23` 写成 `"123"` 或带小数的 `"123.00"` 一类字符串，禁止用 JSON number。
- 按日对齐到 UTC 零点。`ending_at` / `ending_before` 是开区间。范围内没有数据的桶也要返回。
- Priority、flex 只进 Usage。Cost 的 `service_tier` 只产出 `standard` 和 `batch`。
- CMA 会话的运行时费用进 `cost_type=session_usage`。同一会话不要再记一笔容器小时 `code_execution`。Messages API 的代码执行容器小时才记 `code_execution`。控制台仍保留「代码执行总费用」卡，供后一种用量显示。
- Web search 次数进 Usage 的 `server_tool_use.web_search_requests`，金额进 Cost 的 `web_search`。搜索失败不计。
- 默认工作区对外 `workspace_id` 为 `null`。分组字段在未按该维度分组时为 `null`，不要省略字段。
- Console `/usage_cost` 的查询名和五个顶层键保持测试合同。它的内层结构在拿到样本前不要发明。页面上的“按模型分组”不能直接转发成 Admin 的 `group_by[]=model`，Admin Cost 没有这个参数。
- 报表查询必须带 `organization_id`。工作区范围再加 `workspace_id`。不要按 external id 全局扫描。
- 新鲜度目标沿用文档：写入后约 5 分钟可见即可，不必做请求路径上的同步汇总。

会话累计 `usage` JSON 继续服务会话详情。组织报表需要另一份按请求或按桶写入的流水，维度至少包括组织、工作区、时间桶、模型、token 类型、服务层级，以及 web search 次数和 session running 时长。API key、账号、上下文窗口、推理地域、速度只在要支持 Usage 的对应 `group_by` 时才有必要落库。

## 8. 本次未覆盖

- 未请求 Anthropic 线上接口，示例 JSON 以文档页面为准。
- 已看到 Claude Console `/cost` 的空数据页面。未观察 `/usage_cost` 的真实响应，五个对象的内层未知。也没有一份同时含 Managed Agents 会话和 Messages API 代码执行的账单，不能从页面上读出两张卡的加总规则。
- 未核对事件 payload 是否长期保存模型 ID、服务层级和 web search 次数，因此不能判断历史能否回填。
- 未把模型价目抄成可执行价表。实现计价时应以定价页当时的表为准，并单独处理 Opus 5.5 / Sonnet 5.5 的 0.05 倍缓存读取，以及 Haiku 5.5 超过 10 万 token 的另一套价格。
