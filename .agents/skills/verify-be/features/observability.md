# Brain 与 OpenObserve

运行 `just verify-be chat doctor brain-observability`，然后运行 `just verify-be chat brain-observability`。先构建 oma-brain-dsh 并设置 `OMA_BRAIN_BINARY`，本地需有固定镜像 `public.ecr.aws/zinclabs/openobserve:v1.1.2`；CLI 不自动拉取镜像。

场景启动独立 OpenObserve 及隔离 PostgreSQL/Redis/NATS/MinIO。Brain 使用真实官方 SessionTelemetryCoordinator、dsh-otel exporter；真实 OMA 校验 worker 凭据/lease 并通过 OTLP logs 转发。仅上游模型响应固定，遥测写入和查询不 mock。

`brain_observability_roundtrip` 检查公开 SSE、最终回复和历史。`brain_observability_native_history` 从真实 OpenObserve 查询 header 和连续事件，与 CCR 私有 canonical dsh_log 逐条核对，并验证 after_seq 不重复。`brain_observability_isolation` 验证不同组织和工作区读不到本次轨迹。`brain_observability_session_lanes` 为 Agent 开启内联子 agent，脚本模型调用 `send_to_agent`，验证主线程（含主线程 ID）映射到 code session、子 agent 线程映射到 `sthr_*` 且 header 带 parentSession、其他会话的线程和越界分段返回 404、列表按 OMA Session 聚合且子 agent 数为 1。脚本模型让该会话第一次根请求返回 500，Brain 退避后重试。`brain_observability_overview` 在 Agent 范围读取总览、模型、工具、子 agent 四个看板 tab，验证活跃会话与交互数、交互耗时、Token 总量（OMA 入口从 usage 解析的列）、模型请求数/重试率/TTFT、模型统计表恰有一次失败、`send_to_agent` 计入工具表、子 agent 按名称 observer 汇总、版本表与结束原因非空。`brain_observability_deleted_session` 删除该会话后验证轨迹列表与主线程/子线程详情都不可见。`brain_observability_trace_export` 创建一个 metadata_only 与一个 content 导出目标（加密 header，指向本地 OTLP 接收器，场景配置 `telemetry_export.allow_insecure`），以 `traceparent` 发送消息，验证 span 经 JetStream 以 gzip protobuf 送达两个目标：turn 挂在调用方 span 下、子 agent turn 嵌套、失败的模型请求为 ERROR、委派工具无 span、Agent 改为工具自动允许后根 agent 调用一次 `Write`，其 span 的 `gen_ai.tool.call.id` 等于公共 `agent.tool_use` 事件 ID、内容只出现在 content 目标、资源属性为可信租户，并更新投递健康。

不覆盖真实模型质量、图片附件、本机图片 URL、retention、长期断网、崩溃重传、生产容量或并发吞吐。截图/UI 渲染需另用真实平台浏览器检查。测试后回收所有 run-owned 依赖和 Brain 进程；报告仅在清理成功、所有 proof 存在且无 skip 时通过。
