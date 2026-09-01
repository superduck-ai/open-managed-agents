# OTLP Metrics 接口设计文档

## 概述

OTLP (OpenTelemetry Protocol) Metrics 是 Claude Code 客户端用于向遥测后端推送指标数据的协议。客户端使用 OpenTelemetry SDK 定期将指标数据导出到配置的端点。

**注意**：这不是客户端直接调用的 `/worker/otlp/metrics` REST API，而是通过 OpenTelemetry SDK 自动推送指标到配置的 OTLP 端点。

### 当前后端实现状态

当前 `claude-api-server` 已实现 code session 维度的 OTLP HTTP 接收端点：

```http
POST /v1/code/sessions/{code_session_id}/worker/otlp/metrics
POST /v1/code/sessions/{code_session_id}/worker/otlp/logs
POST /v1/code/sessions/{code_session_id}/worker/otlp/v1/logs
POST /v1/code/sessions/{code_session_id}/worker/otlp/v1/traces
```

当前实现目标是提供可信 OTLP ingress，并把规范化后的信号直接转发到 OpenObserve：

1. 验证 `Authorization: Bearer sk-ant-si-<JWT>` 的固定算法、`kid`、签名、issuer、audience，并要求 JWT `session_id` 与 path 中的 `code_session_id` 一致。
2. 同时限制压缩前和解压后的请求体大小，严格解码 OTLP JSON/protobuf body。
3. 要求当前 worker lease 有效；OTLP 不读取或校验 worker epoch。
4. 租约过期返回 `410` 和 OTLP `google.rpc.Status(FAILED_PRECONDITION)`。
5. 删除客户端提供的所有 `oma.*` 属性，以及经 OpenObserve 字段归一化后落入 `oma_*` 命名空间的别名，再从受信 Session 状态注入租户、Session、Agent 和 Version。
6. 使用服务端凭据转发到 OpenObserve，并保留 partial success、`Retry-After` 和 OTLP 错误语义；OMA 不在本地落盘原始遥测。

实现文件：

- `internal/codesessions/ingress.go`
- `internal/codesessions/otlp_ingress.go`
- `internal/codesessions/otlp_codec.go`
- `internal/codesessions/otlp_forward.go`
- `internal/environments/environment_manager.go`

---

## OTLP Metrics 端点规范

### 标准 OTLP 端点路径

根据 OpenTelemetry 规范，OTLP Metrics HTTP 端点的标准路径为：

| 协议 | 端点路径 |
|------|----------|
| **HTTP/JSON** | `/v1/metrics` |
| **HTTP/Protobuf** | `/v1/metrics` |

gRPC OTLP Metrics 的标准服务同样存在，但当前 `claude-api-server` 的 session-scoped worker 端点暂不支持 gRPC。gRPC 接入应作为后续 collector 扩展单独设计。

对于会话特定的指标推送，本项目使用 session-scoped worker 端点：

```http
POST /v1/code/sessions/{code_session_id}/worker/otlp/metrics
```

同一个处理器也服务 logs 端点：

```http
POST /v1/code/sessions/{code_session_id}/worker/otlp/logs
```

### 当前支持矩阵

| 协议 | 当前支持 | 请求 Content-Type | 默认成功响应 |
|------|----------|-------------------|----------|
| **HTTP/Protobuf** | 支持 | `application/x-protobuf` | `200 OK`，空 body |
| **HTTP/JSON** | 支持 | `application/json` | `200 OK`，`{}` |
| **gRPC** | 暂不支持 | - | - |

### 请求头

| Header | 必需 | 描述 |
|--------|------|------|
| `Authorization: Bearer sk-ant-si-<JWT>` | 是 | session-ingress JWT，必须通过签名和标准 claims 校验，`session_id` 必须与 path 一致；managed-agent JWT 的 worker epoch 必须仍匹配 active Code Session |
| `Content-Type` | 是 | `application/x-protobuf` 或 `application/json` |
| `Accept` | 否 | 当前实现忽略该字段；响应编码始终跟随请求 `Content-Type` |

成功响应编码与请求 `Content-Type` 一致：JSON 请求返回 OTLP JSON 响应，Protobuf 请求返回 OTLP Protobuf 响应。`Accept` 不改变响应编码。

OMA 在启动 payload 中直接注入 signal-specific endpoint 与 SessionIngress Authorization header，旧版 Environment Manager 只需按既有合同把环境变量传给 Claude Code。Environment Manager 仍需先完成 `/worker/register` 并维持 heartbeat lease，但 OTLP 请求不携带 worker epoch。

这里的“不携带 worker epoch”是指 OTLP header 不再单独发送 `X-Worker-Epoch`；managed-agent 的 session-ingress JWT 本身仍绑定启动时计算的 worker epoch，通用鉴权会拒绝已失效 epoch，OTLP handler 还会独立校验 active lease。

---

## 客户端实现

### 配置文件位置
`src/utils/telemetry/instrumentation.ts`

### 支持的导出器

```typescript
// 导出器类型
type ExporterType = 'otlp' | 'console' | 'prometheus'

// 支持的 OTLP 协议
type OTLPProtocol = 'grpc' | 'http/json' | 'http/protobuf'
```

### 导出器配置

```typescript
async function getOtlpReaders() {
  const exporterTypes = parseExporterTypes(process.env.OTEL_METRICS_EXPORTER)
  const exportInterval = parseInt(
    process.env.OTEL_METRIC_EXPORT_INTERVAL || '60000'  // 默认 60 秒
  )

  const exporters = []
  for (const exporterType of exporterTypes) {
    if (exporterType === 'otlp') {
      const protocol =
        process.env.OTEL_EXPORTER_OTLP_METRICS_PROTOCOL?.trim() ||
        process.env.OTEL_EXPORTER_OTLP_PROTOCOL?.trim()

      const httpConfig = getOTLPExporterConfig()

      switch (protocol) {
        case 'grpc': {
          const { OTLPMetricExporter } = await import(
            '@opentelemetry/exporter-metrics-otlp-grpc'
          )
          exporters.push(new OTLPMetricExporter())
          break
        }
        case 'http/json': {
          const { OTLPMetricExporter } = await import(
            '@opentelemetry/exporter-metrics-otlp-http'
          )
          exporters.push(new OTLPMetricExporter(httpConfig))
          break
        }
        case 'http/protobuf': {
          const { OTLPMetricExporter } = await import(
            '@opentelemetry/exporter-metrics-otlp-proto'
          )
          exporters.push(new OTLPMetricExporter(httpConfig))
          break
        }
      }
    }
  }

  return exporters.map(exporter => {
    if ('export' in exporter) {
      return new PeriodicExportingMetricReader({
        exporter,
        exportIntervalMillis: exportInterval,
      })
    }
    return exporter
  })
}
```

### HTTP 配置

```typescript
function getOTLPExporterConfig() {
  const proxyUrl = getProxyUrl()
  const mtlsConfig = getMTLSConfig()
  const settings = getSettings_DEPRECATED()

  const config: Record<string, unknown> = {}

  // 解析静态 headers
  const staticHeaders = parseOtelHeadersEnvVar()

  // 动态 headers（如果配置了 helper）
  if (settings?.otelHeadersHelper) {
    config.headers = async (): Promise<Record<string, string>> => {
      const dynamicHeaders = getOtelHeadersFromHelper()
      return { ...staticHeaders, ...dynamicHeaders }
    }
  } else if (Object.keys(staticHeaders).length > 0) {
    config.headers = async (): Promise<Record<string, string>> => staticHeaders
  }

  // 代理和 mTLS 配置
  // ...

  return config
}
```

---

## 环境变量配置

### 导出器选择

| 环境变量 | 默认值 | 描述 |
|----------|--------|------|
| `OTEL_METRICS_EXPORTER` | `otlp` | 指标导出器类型（otlp/console/prometheus/none） |
| `OTEL_LOGS_EXPORTER` | `otlp` | 日志导出器类型（otlp/console/none） |
| `OTEL_METRIC_EXPORT_INTERVAL` | `60000` | 导出间隔（毫秒） |
| `OTEL_LOGS_EXPORT_INTERVAL` | `5000` | 日志导出间隔（毫秒） |

### OTLP 端点配置

| 环境变量 | 默认值 | 描述 |
|----------|--------|------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | - | 通用 OTLP 端点 |
| `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` | 当前后端默认注入 session metrics endpoint | Metrics 专用端点 |
| `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` | 当前后端默认注入 session logs endpoint | Logs 专用端点 |
| `OTEL_EXPORTER_OTLP_HEADERS` | - | 通用 OTLP 请求头；后端不会向该变量注入 session token，避免 token 被自定义 signal endpoint 通过 fallback header 发送到外部 collector |
| `OTEL_EXPORTER_OTLP_METRICS_HEADERS` | 当前后端默认注入 auth | Metrics 专用请求头；供标准 OpenTelemetry signal-scoped 配置使用 |
| `OTEL_EXPORTER_OTLP_LOGS_HEADERS` | 当前后端默认注入 auth | Logs 专用请求头；供标准 OpenTelemetry signal-scoped 配置使用 |

### 协议配置

| 环境变量 | 默认值 | 描述 |
|----------|--------|------|
| `OTEL_EXPORTER_OTLP_PROTOCOL` | - | 通用 OTLP 协议 |
| `OTEL_EXPORTER_OTLP_METRICS_PROTOCOL` | - | Metrics 专用协议 |
| `OTEL_EXPORTER_OTLP_LOGS_PROTOCOL` | - | Logs 专用协议 |
| `OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE` | `delta` | 指标时间聚合类型 |

### 值选项

**Exporter Type:**
- `otlp` - 使用 OTLP 协议导出
- `console` - 输出到控制台（调试用）
- `prometheus` - Prometheus 格式导出
- `none` - 禁用导出

**OTLP Protocol:**
- `grpc` - gRPC 协议（二进制）
- `http/json` - HTTP JSON 格式
- `http/protobuf` - HTTP Protobuf 格式

**Temporality Preference:**
- `delta` - 增量值（默认）
- `cumulative` - 累积值

### Code Session 环境变量注入

OMA 在 `startup_context.environment_variables` 写入 Claude Code 的 telemetry、exporter、protocol、temporality，以及 signal-specific endpoint 和 SessionIngress Authorization header。平台连接变量覆盖同名用户值，使不会动态配置 OTLP 的旧版 Environment Manager 也能按既有启动合同工作；worker epoch 不进入 OTLP 配置。

```bash
CLAUDE_CODE_POST_FOR_SESSION_INGRESS_V2=1
CLAUDE_CODE_USE_CCR_V2=1
CLAUDE_CODE_WORKER_EPOCH={next_worker_epoch}
CLAUDE_CODE_INCLUDE_PARTIAL_MESSAGES=true
```

`observability.enabled=true` 时，OMA 无条件注入 metrics、logs 和 detailed tracing 的全套静态变量（含 `ENABLE_BETA_TRACING_DETAILED=1`），不再按信号拆分开关。内容采集由 `observability.content_capture_enabled`（默认 true）单独控制：开启时 OMA 默认补齐 `OTEL_LOG_USER_PROMPTS=1`、`OTEL_LOG_TOOL_DETAILS=1`、`OTEL_LOG_TOOL_CONTENT=1`，授权 prompt 原文、工具输入/输出正文上报（可能包含源码、命令输出和密钥）；关闭时平台不补这些变量。Runner 启动 environment-manager 时仍会默认带上 `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1` 以跳过 marketplace 和自动更新；可观测开启时 OMA 会在 Claude 的 startup env 里把它覆盖成空字符串，否则 Claude Code 会把非空值当成 essential-traffic，从而不导出 OTEL。

首次启动时 `{next_worker_epoch}` 为 `1`。Sandbox 丢失后的替换启动复用原 Code Session，并注入 `current_worker_epoch + 1`；environment-manager 注册成功后使用同一递增 epoch，避免恢复 worker 被旧 epoch fencing。该变量服务于 CCRv2 worker 生命周期，不会拼入 OTLP header。

```bash
OTEL_METRICS_EXPORTER=otlp
OTEL_EXPORTER_OTLP_METRICS_PROTOCOL=http/protobuf
OTEL_EXPORTER_OTLP_METRICS_ENDPOINT={api_base_url}/v1/code/sessions/{code_session_id}/worker/otlp/metrics
OTEL_EXPORTER_OTLP_METRICS_HEADERS=Authorization=Bearer {session_ingress_token}
```

Console Agent 可观测不再从 PostgreSQL 读取 Active Time / Token 看板。写入端仍把 OTLP 转发到 OpenObserve（凭据键为 `observability.openobserve.ingestion.*`）；查询走 `observability.openobserve.query.*` 与 `POST /api/organizations/{org}/observability/panels/query`。默认 Claude Code 版本为 `2.1.251`。

Console 查询 API 只在 `observability.enabled=true` 时注册，统一使用平台 session 中的 organization/workspace scope；浏览器不能覆盖租户字段，也不会获得 OpenObserve SQL 或凭据。接口为：

| 方法与路径 | 合同 |
| --- | --- |
| `GET /api/organizations/{org}/observability/dashboard` | 返回不含 SQL/stream 的看板投影。 |
| `POST /api/organizations/{org}/observability/panels/query` | 请求体上限 64 KiB；只执行内嵌 `query_ref`。 |
| `GET /api/organizations/{org}/observability/traces` | `start_time/end_time` 必须成对提供且最长 30 天；支持 Agent、Session、Version、Trace ID 和状态过滤。 |
| `GET /api/organizations/{org}/observability/traces/{trace_id}` | 可选时间窗遵循同一校验；省略时查最近 30 天，并在结束端保留 1 小时时钟偏差。最多返回 2000 个 spans，超出时响应 `truncated=true`。 |

旧的 `/analytics/sessions/overview` 与 `/analytics/sessions/timeseries` 只返回零值且没有真实数据来源，已随新查询 API 删除；关闭 observability 时新旧接口均为 404。

### 当前 environment-manager 指标与展示建议

当前 environment-manager exporter 每 60 秒重复导出同一组 cumulative/gauge 点。产品查询层必须先按 `code_session_id + metric.name + point.attributes` 识别 series，并取最新点；不能把 OpenObserve 中每个 sample 直接求和，否则会把同一次启动或安装重复计算。

| Metric | 当前语义 | Session Detail 展示 | 聚合规则 |
|--------|----------|---------------------|----------|
| `env_manager.start.count` | environment-manager 累计启动次数；点属性包含 `session_mode` | “Environment manager started” 状态和 `resume` / `resume-cached` 等 mode badge | 每个 session 取最新值；跨 session 统计 started session 数和 mode 分布 |
| `env_manager.claude_install.count` | Claude Code 安装/升级累计次数 | “Install / upgrade completed” 生命周期步骤 | 每个 session 取最新值；不要按 60 秒 export 次数累加 |
| `env_manager.claude_install.latency_ms` | 本次安装/升级耗时 gauge | 安装步骤的 duration | 每个 session 取最新值；跨 session 计算 p50/p95 |
| `claude_code.start.count` | Claude Code 累计启动次数 | “Claude Code started” 生命周期步骤 | 每个 session 取最新值；可与 environment-manager started session 数计算 start conversion |

推荐在现有 Session Detail 增加 `Observability` 区域，而不是把 OTLP 原始结构直接塞入 transcript：

1. 顶部生命周期 stepper：environment manager started → install/upgrade → Claude Code started。
2. 摘要字段：session mode、install latency、telemetry last received、service/SDK version。
3. 调试表：time、metric、value、unit、attributes、scope，并允许查看原始 point。
4. Agent / environment 聚合页：started sessions、Claude Code start conversion、install latency p50/p95、session mode 分布。

当前 payload 没有明确的 install failure、Claude Code start failure、exit code 或错误原因，因此不能仅凭这四个指标展示“失败率”。失败状态应与 session error events / logs 关联，或由 exporter 增加带低基数 reason 的 failure counter。

生命周期信号和 Claude Code 信号仍统一写入 OpenObserve，作为原始遥测的存储与调试数据。Console 可观测查询 OpenObserve（经中立 query API），浏览器不获得 OpenObserve 凭据或任意 SQL 能力。

---

## 协议详情

### 1. 标准 gRPC 协议（当前 worker 端点暂不支持）

**端点**: 标准 OTLP gRPC collector 服务；不是当前 session-scoped worker HTTP 端点。

**请求格式**: Protocol Buffers (binary)

**示例**:
```protobuf
// OpenTelemetry Proto 定义
service MetricService {
  rpc Export(ExportMetricsServiceRequest) returns (ExportMetricsServiceResponse);
}

message ExportMetricsServiceRequest {
  // 资源属性（服务名、版本等）
  opentelemetry.proto.resource.v1.Resource resource = 1;

  // 指标数据
  repeated opentelemetry.proto.metrics.v1.ResourceMetrics resource_metrics = 2;
}
```

### 2. HTTP/JSON 协议

**端点路径**: `/v1/metrics`

**Content-Type**: `application/json`

**请求体结构**:
```json
{
  "resourceMetrics": [
    {
      "resource": {
        "attributes": [
          { "key": "service.name", "value": { "stringValue": "claude-code" } },
          { "key": "service.version", "value": { "stringValue": "1.0.0" } }
        ]
      },
      "scopeMetrics": [
        {
          "scope": { "name": "claude-code" },
          "metrics": [
            {
              "name": "tool_calls_total",
              "description": "Total number of tool calls",
              "unit": "1",
              "data": {
                "dataType": "sum",
                "sum": {
                  "isMonotonic": true,
                  "aggregationTemporality": "DELTA",
                  "dataPoints": [
                    {
                      "asInt": 10,
                      "startTimeUnixNano": "1625097600000000000",
                      "timeUnixNano": "1625097660000000000",
                      "attributes": [
                        { "key": "tool_name", "value": { "stringValue": "bash" } }
                      ]
                    }
                  ]
                }
              }
            }
          ]
        }
      ]
    }
  ]
}
```

### 3. HTTP/Protobuf 协议

**端点路径**: `/v1/metrics`

**Content-Type**: `application/x-protobuf`

**请求格式**: 二进制 Protobuf（与 gRPC 相同的消息格式）

---

## 指标数据结构

### 资源属性 (Resource Attributes)

每个指标携带以下资源属性：

```typescript
{
  "service.name": "claude-code",
  "service.version": "<version>",
  "host.arch": "<x86_64/arm64>",
  "telemetry.sdk.name": "opentelemetry",
  "telemetry.sdk.language": "nodejs",
  // ... 其他属性
}
```

### 指标类型

OpenTelemetry 支持的指标数据类型：

| 类型 | 描述 | 示例 |
|------|------|------|
| **Sum** | 单调递增或变化的值 | 工具调用总数 |
| **Gauge** | 任意上下变化的值 | 当前内存使用 |
| **Histogram** | 分布统计 | 请求延迟分布 |
| **ExponentialHistogram** | 对数桶分布 | 高基数延迟 |

### 时间聚合类型

| 类型 | 描述 | 适用场景 |
|------|------|----------|
| **DELTA** | 自上次导出以来的变化 | 计数器、速率 |
| **CUMULATIVE** | 自进程启动以来的累积 | 总量 |

---

## 指标推送流程

```
┌─────────────────┐
│  Claude Code    │
│                 │
│  ┌───────────┐  │
│  │ Meter     │  │  创建指标
│  └─────┬─────┘  │
│        │        │
│        ▼        │
│  ┌───────────┐  │  累积指标数据
│  │ Metric    │  │
│  │ Reader    │  │
│  └─────┬─────┘  │
│        │        │
│        ▼        │  定期导出（默认 60s）
│  ┌───────────┐  │
│  │Exporter   │  │  OTLP 协议编码
│  └─────┬─────┘  │
│        │        │
│        ▼        │
│  ┌───────────┐  │  HTTP 请求（gRPC 后续扩展）
│  │  OTLP     │──┼──────────────────┐
│  │ Endpoint  │  │                  │
│  └───────────┘  │                  │
└─────────────────┘                  │
                                     │
                    ┌────────────────▼────────────────┐
                    │   OTLP Metrics Receiver         │
                    │   /v1/metrics (future)           │
                    │   /worker/otlp/metrics          │
                    │                                  │
                    │  当前实现：                      │
                    │  - 验证 Bearer token             │
                    │  - 校验 worker epoch             │
                    │  - 读取 body 并更新 activity     │
                    │  - 解码 Protobuf/JSON            │
                    │  - 注入可信 oma.* 属性           │
                    │  - 转发 OpenObserve              │
                    │                                  │
                    │  后续扩展：                      │
                    │  - 验证格式                      │
                    │  - 存储到时序数据库              │
                    └──────────────────────────────────┘
```

---

## 导出间隔和批处理

### 导出间隔

```typescript
const DEFAULT_METRICS_EXPORT_INTERVAL_MS = 60000  // 60 秒
```

可通过环境变量配置：
```bash
export OTEL_METRIC_EXPORT_INTERVAL=30000  # 30 秒
```

### PeriodicExportingMetricReader

```typescript
new PeriodicExportingMetricReader({
  exporter: OTLPMetricExporter,
  exportIntervalMillis: 60000,  // 每 60 秒导出一次
})
```

---

## 认证和安全

### mTLS 配置

```typescript
const mtlsConfig = getMTLSConfig()  // 读取证书配置

config.httpAgentOptions = {
  cert: mtlsConfig.cert,
  key: mtlsConfig.key,
  passphrase: mtlsConfig.passphrase,
  ca: caCerts,
}
```

### 请求头

```typescript
// 静态 headers（从环境变量读取）
{
  "Authorization": "Bearer <token>",
  "X-Custom-Header": "value"
}

// 动态 headers（从 helper 函数获取）
config.headers = async () => {
  const dynamicHeaders = await getOtelHeadersFromHelper()
  return { ...staticHeaders, ...dynamicHeaders }
}
```

Code session OTLP 端点运行时必须携带 `Authorization: Bearer sk-ant-si-<JWT>`，并要求签名 JWT 的 `session_id` 与 OTLP URL path 一致。服务端根据 code session 当前状态校验 active lease，不读取 worker epoch。

---

## 服务端实现指南

### 当前 Go 后端行为

当前实现位于 `internal/codesessions/otlp_ingress.go`、`otlp_codec.go` 和 `otlp_forward.go`：

1. 校验 Observability 全局开关、SessionIngress JWT、请求 path、可信凭证上下文和 active lease。
2. 上述授权在读取请求 body 前完成；失效 worker 不进入解压和 OTLP 解码。
3. 同时限制压缩前和解压后 body 大小，再按请求 `Content-Type` 严格解码 JSON/Protobuf。
4. 删除客户端各层级提供的 `oma.*` 属性及其 OpenObserve `oma_*` / `service_oma_*` 归一化别名（后者对应 traces 侧租户列的 `service_` 前缀），注入服务端可信 Organization、Workspace、Session、Agent 和 Version。
5. payload 校验成功后用服务端凭据转发到 OpenObserve；OTLP 不写 worker 状态，liveness 完全由 heartbeat 维护。
6. 保留 OpenObserve partial success 和 `Retry-After`；不在 OMA 本地落盘 OTLP body，也不把上游错误正文写入应用日志。

错误语义：

| 场景 | 状态码 | error type |
|------|--------|------------|
| token 缺失或不匹配 | 401 | OTLP typed error |
| session 不再 active | 410 | OTLP typed error |
| 当前 worker lease 已过期 | 410 | OTLP typed error |
| body 超过限制 | 413 | OTLP typed error |
| OpenObserve 暂时不可用 | 503 | OTLP typed error，可重试 |

应用日志只记录 code session ID、signal 和归一化状态等白名单元数据；不记录 query、body、Authorization、完整 headers 或 OpenObserve 错误响应正文。

### 当前成功响应

OMA 原样保留 OpenObserve 返回的 OTLP success/partial-success message，并使用与请求相同的 JSON 或 Protobuf 编码返回；空成功响应按对应 OTLP message 编码生成，不根据 `Accept` 切换协议。

### 后续完整 Collector 扩展

当前服务已经在可信 Session/worker 边界之后解码 OTLP JSON/protobuf，并直接写入 OpenObserve。下面内容仅作为未来增加独立 Collector 或 gRPC receiver 时的参考，不描述当前数据路径。

### gRPC 服务端

```protobuf
// proto/opentelemetry/proto/collector/metrics/v1/metrics_service.proto
service MetricsService {
  rpc Export(ExportMetricsServiceRequest) returns (ExportMetricsServiceResponse);
}

message ExportMetricsServiceRequest {
  opentelemetry.proto.resource.v1.Resource resource = 1;
  repeated opentelemetry.proto.metrics.v1.ResourceMetrics resource_metrics = 2;
}

message ExportMetricsServiceResponse {}
```

### HTTP/JSON 服务端

```typescript
// 接收端点实现
async function handleOTLPMetrics(
  request: ExportMetricsServiceRequest
): Promise<{ status: number }> {
  try {
    // 1. 验证请求格式
    if (!request.resourceMetrics || request.resourceMetrics.length === 0) {
      return { status: 400 }
    }

    // 2. 提取资源属性
    const resource = request.resourceMetrics[0].resource.attributes
    const sessionId = resource.find(a => a.key === 'session_id')?.value.stringValue

    // 3. 处理每个指标
    for (const rm of request.resourceMetrics) {
      for (const sm of rm.scopeMetrics) {
        for (const metric of sm.metrics) {
          await writeMetricToTimeseriesDB({
            sessionId,
            metricName: metric.name,
            description: metric.description,
            unit: metric.unit,
            dataPoints: extractDataPoints(metric.data),
          })
        }
      }
    }

    return { status: 200 }  // OTLP 规范要求返回空响应
  } catch (error) {
    return { status: 500 }
  }
}
```

### 数据点提取

```typescript
function extractDataPoints(data: any): MetricDataPoint[] {
  switch (data.dataType) {
    case 'sum':
      return data.sum.dataPoints.map((dp: any) => ({
        value: dp.asInt ?? dp.asDouble,
        timestamp: new Date(Number(dp.timeUnixNano) / 1e6),
        attributes: attributesToObject(dp.attributes),
        startTime: new Date(Number(dp.startTimeUnixNano) / 1e6),
      }))

    case 'gauge':
      return data.gauge.dataPoints.map((dp: any) => ({
        value: dp.asInt ?? dp.asDouble,
        timestamp: new Date(Number(dp.timeUnixNano) / 1e6),
        attributes: attributesToObject(dp.attributes),
      }))

    case 'histogram':
      return data.histogram.dataPoints.map((dp: any) => ({
        count: dp.count,
        sum: dp.sum,
        min: dp.min,
        max: dp.max,
        buckets: dp.bucketCounts,
        explicitBounds: dp.explicitBounds,
        timestamp: new Date(Number(dp.timeUnixNano) / 1e6),
        attributes: attributesToObject(dp.attributes),
      }))

    default:
      return []
  }
}

function attributesToObject(attributes: any[]): Record<string, string | number> {
  const result: Record<string, any> = {}
  for (const attr of attributes) {
    const value = attr.value.stringValue ??
                  attr.value.intValue ??
                  attr.value.doubleValue
    if (value !== undefined) {
      result[attr.key] = value
    }
  }
  return result
}
```

---

## 时序数据库存储

### 推荐数据库

| 数据库 | 适用场景 |
|--------|----------|
| **Prometheus** | 开源、广泛使用、Pull 模式 |
| **InfluxDB** | 高性能、Push 模式、时序优化 |
| **TimescaleDB** | PostgreSQL 扩展、SQL 支持 |
| **Mimir** | Prometheus 兼容、高可用 |

### 存储示例 (TimescaleDB)

该表结构仅用于后续完整 collector 方案。为避免丢失 OTLP histogram、exponential histogram，以及同一时间/指标名下 attributes 不同的 series，需要把 metric identity、series dimensions 与 samples 分开存储。

```sql
-- 指标 series 维度表
CREATE TABLE metric_series (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  session_id TEXT NOT NULL,
  metric_name TEXT NOT NULL,
  metric_description TEXT,
  unit TEXT,
  data_type TEXT NOT NULL,
  aggregation_temporality TEXT,
  is_monotonic BOOLEAN,
  resource_attributes JSONB NOT NULL DEFAULT '{}'::jsonb,
  scope_name TEXT,
  scope_version TEXT,
  point_attributes JSONB NOT NULL DEFAULT '{}'::jsonb,
  attributes_hash BYTEA NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (session_id, metric_name, data_type, attributes_hash)
);

-- 指标样本表。遵循本项目数据库约束：不创建 PostgreSQL foreign key。
CREATE TABLE metrics (
  time TIMESTAMPTZ NOT NULL,
  series_id BIGINT NOT NULL,
  start_time TIMESTAMPTZ,
  as_int BIGINT,
  as_double DOUBLE PRECISION,
  histogram_count BIGINT,
  histogram_sum DOUBLE PRECISION,
  histogram_min DOUBLE PRECISION,
  histogram_max DOUBLE PRECISION,
  histogram_bucket_counts JSONB,
  histogram_explicit_bounds JSONB,
  exponential_histogram JSONB,
  raw_data_point JSONB NOT NULL DEFAULT '{}'::jsonb,
  PRIMARY KEY (time, series_id)
);

-- 创建 hypertable
SELECT create_hypertable('metrics', 'time');

-- 索引
CREATE INDEX ON metric_series (session_id, metric_name);
CREATE INDEX ON metric_series USING GIN (resource_attributes);
CREATE INDEX ON metric_series USING GIN (point_attributes);
CREATE INDEX ON metrics (series_id, time DESC);
CREATE INDEX ON metrics USING GIN (raw_data_point);

-- 数值型指标查询示例
SELECT
  time_bucket('5 minutes', m.time) AS bucket,
  avg(COALESCE(m.as_double, m.as_int::double precision)) AS avg_value,
  max(COALESCE(m.as_double, m.as_int::double precision)) AS max_value
FROM metrics m
JOIN metric_series s ON s.id = m.series_id
WHERE s.session_id = 'cse_abc123'
  AND s.metric_name = 'tool_calls_total'
  AND s.point_attributes @> '{"tool_name":"bash"}'::jsonb
  AND m.time > NOW() - INTERVAL '1 hour'
GROUP BY bucket
ORDER BY bucket;

-- Histogram 指标查询示例
SELECT
  m.time,
  m.histogram_count,
  m.histogram_sum,
  m.histogram_bucket_counts,
  m.histogram_explicit_bounds
FROM metrics m
JOIN metric_series s ON s.id = m.series_id
WHERE s.session_id = 'cse_abc123'
  AND s.metric_name = 'api_request_duration'
ORDER BY m.time DESC
LIMIT 20;
```

---

## 示例请求

### HTTP/JSON 请求示例

```http
POST /v1/code/sessions/cse_abc123/worker/otlp/metrics HTTP/1.1
Host: telemetry.example.com
Content-Type: application/json
Authorization: Bearer sk-ant-si-<signed-session-ingress-jwt>

{
  "resourceMetrics": [
    {
      "resource": {
        "attributes": [
          { "key": "service.name", "value": { "stringValue": "claude-code" } },
          { "key": "service.version", "value": { "stringValue": "1.0.0" } },
          { "key": "session_id", "value": { "stringValue": "cse_abc123" } }
        ]
      },
      "scopeMetrics": [
        {
          "scope": { "name": "claude-code" },
          "metrics": [
            {
              "name": "tool_calls_total",
              "description": "Total number of tool calls",
              "unit": "1",
              "data": {
                "dataType": "sum",
                "sum": {
                  "isMonotonic": true,
                  "aggregationTemporality": "DELTA",
                  "dataPoints": [
                    {
                      "asInt": 5,
                      "startTimeUnixNano": "1625097600000000000",
                      "timeUnixNano": "1625097660000000000",
                      "attributes": [
                        { "key": "tool_name", "value": { "stringValue": "bash" } },
                        { "key": "status", "value": { "stringValue": "success" } }
                      ]
                    }
                  ]
                }
              }
            },
            {
              "name": "api_request_duration",
              "description": "API request duration",
              "unit": "ms",
              "data": {
                "dataType": "histogram",
                "histogram": {
                  "aggregationTemporality": "DELTA",
                  "dataPoints": [
                    {
                      "count": 100,
                      "sum": 15234.5,
                      "min": 45.2,
                      "max": 892.1,
                      "bucketCounts": [0, 5, 23, 67, 100],
                      "explicitBounds": [0, 100, 500, 1000],
                      "startTimeUnixNano": "1625097600000000000",
                      "timeUnixNano": "1625097660000000000",
                      "attributes": [
                        { "key": "endpoint", "value": { "stringValue": "/v1/messages" } }
                      ]
                    }
                  ]
                }
              }
            }
          ]
        }
      ]
    }
  ]
}
```

### 响应

HTTP/JSON 请求成功响应：

```http
HTTP/1.1 200 OK
Content-Type: application/json

{}
```

HTTP/Protobuf 请求成功响应：

```http
HTTP/1.1 200 OK
Content-Type: application/x-protobuf
Content-Length: 0
```

**注意**：当前后端成功响应选择规则与前文一致：请求 `Content-Type` 包含 `json` 时返回 JSON `{}`；否则返回 protobuf 空响应。`Accept` 不改变响应编码，客户端当前只要求 2xx 成功状态，不解析成功响应体。

---

## 常见指标

### Claude Code 可能发送的指标

| 指标名称 | 类型 | 描述 |
|----------|------|------|
| `tool_calls_total` | Sum | 工具调用总数 |
| `tool_calls_duration` | Histogram | 工具调用延迟 |
| `api_requests_total` | Sum | API 请求总数 |
| `api_request_duration` | Histogram | API 请求延迟 |
| `session_active` | Gauge | 活跃会话数 |
| `agent_tasks_total` | Sum | 代理任务总数 |
| `memory_usage_bytes` | Gauge | 内存使用量 |
| `cpu_usage_percent` | Gauge | CPU 使用率 |

---

## 故障排查

### 检查配置

```bash
# 查看当前环境变量
echo $OTEL_METRICS_EXPORTER
echo $OTEL_EXPORTER_OTLP_ENDPOINT
echo $OTEL_EXPORTER_OTLP_METRICS_PROTOCOL
```

### 调试模式

```bash
# 启用控制台导出器
export OTEL_METRICS_EXPORTER=console

# 运行 Claude Code
claude
```

### 验证端点

```bash
# 使用 curl 测试 HTTP/JSON 端点
curl -X POST http://127.0.0.1:38080/v1/code/sessions/cse_abc123/worker/otlp/metrics \
	-H "Content-Type: application/json" \
	-H "Authorization: Bearer ${SESSION_INGRESS_TOKEN}" \
	-d '{"resourceMetrics": []}'
```

---

## 相关文件

- **后端 OTLP handler**: `internal/codesessions/ingress.go`
- **后端 environment-manager payload**: `internal/environments/environment_manager.go`
- **后端测试**: `tests/sessions_api_test.go`
- **客户端遥测配置**: `superduck-code/src/utils/telemetry/instrumentation.ts`（外部客户端仓库）
- **客户端环境变量白名单**: `superduck-code/src/utils/managedEnvConstants.ts`（外部客户端仓库）
- **客户端 bridge runner**: `superduck-code/src/bridge/sessionRunner.ts`（外部客户端仓库）

---

## 独立 E2E 验收

`go run ./cmd/observability-e2e` 同时验证 `/v1` 资源链路和 Console 可观测查询链路，因此必须显式提供两套身份：

| 环境变量 | 用途 |
| --- | --- |
| `OMA_API_KEY` | 调用 `/v1/agents`、`/v1/sessions` 和 Session events |
| `OMA_PLATFORM_SESSION_KEY` | 作为 `sessionKey` cookie 调用 Platform/Console API |
| `OMA_ORGANIZATION_ID` | 限定 observability organization |
| `OMA_WORKSPACE_ID` | 通过 `X-Workspace-ID` 限定 Platform workspace |
| `OMA_ENVIRONMENT_ID` | 创建测试 Session |

API key、Platform session、organization 和 workspace 必须属于同一租户上下文。命令会先只读请求 `GET /api/organizations/{org}/`，确认 Platform session、organization 和 workspace 有效，再创建 Agent。可观测 Panel 查询的网络错误或非 2xx 响应会立即终止；只有成功返回但 `data.current` 尚未大于 0 时才继续轮询。

```bash
OMA_API_KEY=... \
OMA_PLATFORM_SESSION_KEY=... \
OMA_ORGANIZATION_ID=... \
OMA_WORKSPACE_ID=... \
OMA_ENVIRONMENT_ID=... \
go run ./cmd/observability-e2e
```

---

## 总结

### 关键要点

1. **当前协议**：支持 session-scoped HTTP/JSON 和 HTTP/Protobuf；暂不支持该端点的 gRPC。
2. **端点**：`/v1/code/sessions/{code_session_id}/worker/otlp/metrics` 与 `/v1/code/sessions/{code_session_id}/worker/otlp/logs`
3. **导出间隔**：默认 60 秒
4. **认证**：`Authorization: Bearer sk-ant-si-<JWT>`，校验签名和 claims，并把 `session_id` 绑定到请求 path
5. **时间聚合**：默认 DELTA（增量）
6. **worker 防护**：服务端要求当前 worker lease 有效；OTLP 不参与 worker epoch fencing

### 配置常量

| 常量 | 值 |
|------|-----|
| 导出间隔 | 60000ms (60秒) |
| 默认协议 | http/protobuf |
| 时间聚合 | DELTA |

### 当前服务端要求

1. 校验 session-ingress JWT，并要求 JWT `session_id` 与 path 中的 `code_session_id` 一致。
2. 在读取 body 前校验当前 worker lease；不读取 worker epoch。
3. 请求体同时受压缩前和解压后大小限制，并严格解码 OTLP JSON/Protobuf。
4. 删除客户端 `oma.*` 及其 OpenObserve `oma_*` / `service_oma_*` 归一化别名后注入服务端可信上下文；OTLP 是只读校验加转发，不刷新 worker activity。
5. 使用服务端 OpenObserve 凭据转发，保留 partial success 与可重试错误语义。
6. 未注册或 lease 过期的 worker 返回 410。
7. 应用日志只记录白名单元数据，不记录请求或上游响应正文；OMA 不保存本地 JSONL 副本。

### 后续扩展要求

1. 评估独立 Collector 或 gRPC receiver 的必要性。
2. 增加数据质量、采样、限流和高基数标签保护。

---

*文档生成时间: 2026-07-01*
*基于代码版本: Claude Code CLI / OpenTelemetry SDK*
