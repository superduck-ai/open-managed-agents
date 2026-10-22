# Brain 与 OpenObserve

运行 `just verify-be chat doctor brain-observability`，然后运行 `just verify-be chat brain-observability`。先构建 oma-brain-dsh 并设置 `OMA_BRAIN_BINARY`，本地需有固定镜像 `public.ecr.aws/zinclabs/openobserve:v1.1.2`；CLI 不自动拉取镜像。

场景启动独立 OpenObserve 及隔离 PostgreSQL/Redis/NATS/MinIO。Brain 使用真实官方 SessionTelemetryCoordinator、dsh-otel exporter；真实 OMA 校验 worker 凭据/lease 并通过 OTLP logs 转发。仅上游模型响应固定，遥测写入和查询不 mock。

`brain_observability_roundtrip` 检查公开 SSE、最终回复和历史。`brain_observability_native_history` 从真实 OpenObserve 查询 header 和连续事件，与 CCR 私有 canonical dsh_log 逐条核对，并验证 after_seq 不重复。`brain_observability_isolation` 验证不同组织和工作区读不到本次轨迹。

不覆盖真实模型质量、图片附件、本机图片 URL、retention、长期断网、崩溃重传、生产容量或并发吞吐。截图/UI 渲染需另用真实平台浏览器检查。测试后回收所有 run-owned 依赖和 Brain 进程；报告仅在清理成功、所有 proof 存在且无 skip 时通过。
