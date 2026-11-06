# 工作区概览

概览展示真实的工作区运行摘要、会话趋势与智能体运行情况。页面与快速开始、智能体和会话管理保持独立。失败数为普通数字，不显示提醒条或失败详情弹层。设计底稿保留在本地交互稿目录，不随生产代码引入演示数据或状态开关。

## 唯一新增接口

`GET /api/organizations/{orgUuid}/workspaces/{workspaceId}/overview?days=30&timezone=Asia/Shanghai`

`days` 仅支持 7、30，默认 30；`timezone` 为 IANA 时区，默认 UTC。范围从本地第一个自然日零点开始，到请求时刻结束，采用左闭右开区间。返回 `start_time`、`end_time`、`timezone`、`agent_count`、`session_count`、`failed_session_count`、`active_agent_count`、`daily` 和 `agents`。

- `agent_count`：工作区中未删除、未归档的智能体数，用于区分首次使用与当前时段无会话。
- `session_count`：所选范围内创建且未删除的会话数，包含已归档会话，不等同于消息数或执行轮次数。
- `failed_session_count`：上述会话在同一范围内出现过明确失败结果的去重数量。结果为 `session.status_idle` 的 `stop_reason.type=retries_exhausted`，或 `session.error` 的 `error.retry_status.type` 为 `exhausted`、`terminal`。普通终止、取消、待确认、预算限制以及自动重试中的错误均不计入。该值表示历史失败，不表示当前仍需处理，后续恢复不抹去历史。
- `active_agent_count`：上述会话中不同智能体的数量。
- `daily`：按时区日期与智能体分组的会话数，每行包含 `date`、`agent_id`、`session_count`。
- `agents`：按会话数降序、ID 升序排列，包含 `agent_id`、`name`、`session_count`、`failed_session_count`、`last_run_at`。`last_run_at` 为范围内最近一个会话的创建时间，不用 `updated_at` 冒充执行时间。

组织可见性与工作区资源权限沿用现有 Console 工作区解析。SQL 使用组织、工作区 UUID 显式隔离；不接受客户端传入 UUID 来覆盖鉴权结果。响应 `Cache-Control: no-store`，无数据返回空数组和零，查询失败返回错误，不能伪装成空数据。

## 实现

两条只读 Yourbatis 查询：按日期和智能体聚合会话，以及统计当前可用智能体数。摘要与智能体表格从聚合结果生成，不新增表、迁移、缓存、后台任务或可观测存储依赖。失败判断直接读取既有持久化会话事件；不解析工具文本或模型文本推测失败。

前端通过 TanStack Query 按账号、组织、工作区、时间范围和时区隔离请求。时间范围控制整页；智能体选择只筛选趋势，不改变工作区摘要和全量表格。缺失的日期补零。摘要与表格来源一致，错误率为失败会话数除以会话数，无会话时显示「—」。

首次使用复用已有模型列表接口判断是否需要配置模型，沿用配置权限与快速开始返回地址。已有智能体但当前时段无会话时，展示当前时段暂无运行记录，提供查看智能体入口。接口失败显示不可用与真实重试，加载过程中不展示伪造统计或首次使用结论。

保留既有 Lucide、Table、Select、Chart 和共享主题。文档降为底部链接，窄屏隐藏最近会话时间列。生产页面不读取 `overviewScene` 参数。

## 验证

普通单测覆盖参数与时区、查询失败、组织与工作区拒绝访问、聚合一致性、前端请求隔离、错误重试、空状态、趋势筛选和补零。PostgreSQL 定向测试使用独立临时 schema，覆盖 JSON 失败结果、去重、自动重试、普通终止、删除、边界和租户隔离；测试结束删除临时 schema。不需要启动 Worker、执行模型任务或运行 verify-be。
