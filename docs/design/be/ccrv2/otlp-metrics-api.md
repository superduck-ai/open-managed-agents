# OTLP Metrics 接口设计文档

## 概述

OTLP (OpenTelemetry Protocol) Metrics 是 Claude Code 客户端用于向遥测后端推送指标数据的协议。客户端使用 OpenTelemetry SDK 定期将指标数据导出到配置的端点。

**注意**：这不是客户端直接调用的 `/worker/otlp/metrics` REST API，而是通过 OpenTelemetry SDK 自动推送指标到配置的 OTLP 端点。

### 当前后端实现状态

当前 `claude-api-server` 已实现 code session 维度的 OTLP HTTP 接收端点：

```http
POST /v1/code/sessions/{code_session_id}/worker/otlp/metrics
POST /v1/code/sessions/{code_session_id}/worker/otlp/logs
```

当前实现目标是先打通客户端 exporter，不在该接口内解析或持久化 OTLP payload：

1. 验证 `Authorization: Bearer sk-ant-si-<JWT>` 的固定算法、`kid`、签名、issuer、audience，并要求 JWT `session_id` 与 path 中的 `code_session_id` 一致。
2. 限制请求体大小，读取并丢弃 OTLP body。
3. 强制读取 worker epoch，并要求它等于当前 epoch 且对应 worker lease 未过期。
4. 缺失或格式非法的 epoch 返回 `400 invalid_request_error`。
5. 旧 worker epoch 返回 `409 conflict_error`，租约过期返回 `410 session_expired`。
6. 请求 `Content-Type` 包含 `json`，或 `Accept` 包含 `application/json` 时成功返回 `{}`；其他成功响应返回 200 protobuf 空 body。

实现文件：

- `internal/codesessions/ingress.go`
- `internal/environments/environment_manager.go`
- `tests/sessions_api_test.go`

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

### 必需请求头

| Header | 必需 | 描述 |
|--------|------|------|
| `Authorization: Bearer sk-ant-si-<JWT>` | 是 | session-ingress JWT，必须通过签名和标准 claims 校验，且 `session_id` 必须与 path 一致 |
| `X-Worker-Epoch: {epoch}` | 是 | 当前 worker epoch，用于拒绝旧 worker 写入并检查 lease |
| `Content-Type` | 是 | `application/x-protobuf` 或 `application/json` |
| `Accept` | 否 | 如果包含 `application/json`，即使请求是 protobuf，成功响应也会返回 JSON |

成功响应选择规则以 `writeOTLPSuccess()` 为准：请求 `Content-Type` 包含 `json`，或 `Accept` 包含 `application/json` 时返回 JSON `{}`；否则返回 `application/x-protobuf` 和空 body。

`worker_epoch` 仍可从 query 参数读取，便于兼容已有调用；但不应作为 OpenTelemetry JS HTTP exporter 的配置方式。客户端代码使用的 Node HTTP transport 会从 endpoint URL 中取 `pathname`，query string 不会稳定出现在最终请求里，因此实际运行应通过 header 传 epoch。exporter 未带 epoch 时服务端返回 `400`。

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
| `OTEL_EXPORTER_OTLP_METRICS_HEADERS` | 当前后端默认注入 auth 和 epoch | Metrics 专用请求头；供标准 OpenTelemetry signal-scoped 配置使用 |
| `OTEL_EXPORTER_OTLP_LOGS_HEADERS` | 当前后端默认注入 auth 和 epoch | Logs 专用请求头；供标准 OpenTelemetry signal-scoped 配置使用 |

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

### Code Session 默认注入

`buildEnvironmentManagerV0Payload()` 会在 `startup_context.environment_variables` 中注入 code session worker 必需变量：

```bash
CLAUDE_CODE_POST_FOR_SESSION_INGRESS_V2=1
CLAUDE_CODE_USE_CCR_V2=1
CLAUDE_CODE_WORKER_EPOCH=1
```

如果没有显式配置 `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` 或 `OTEL_EXPORTER_OTLP_ENDPOINT`，并且 `OTEL_METRICS_EXPORTER` 未设置或包含 `otlp`，后端会默认注入：

```bash
OTEL_METRICS_EXPORTER=otlp
OTEL_EXPORTER_OTLP_METRICS_PROTOCOL=http/protobuf
OTEL_EXPORTER_OTLP_METRICS_ENDPOINT={api_base_url}/v1/code/sessions/{code_session_id}/worker/otlp/metrics
OTEL_EXPORTER_OTLP_METRICS_HEADERS=Authorization=Bearer {session_ingress_token},x-worker-epoch=1
```

如果没有显式配置 `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` 或 `OTEL_EXPORTER_OTLP_ENDPOINT`，并且 `OTEL_LOGS_EXPORTER` 未设置或包含 `otlp`，后端会默认注入：

```bash
OTEL_LOGS_EXPORTER=otlp
OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=http/protobuf
OTEL_EXPORTER_OTLP_LOGS_ENDPOINT={api_base_url}/v1/code/sessions/{code_session_id}/worker/otlp/logs
OTEL_EXPORTER_OTLP_LOGS_HEADERS=Authorization=Bearer {session_ingress_token},x-worker-epoch=1
```

保留用户自定义配置的规则：

1. 如果用户已设置 `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` 或 `OTEL_EXPORTER_OTLP_ENDPOINT`，不注入默认 metrics endpoint。
2. 如果用户已设置 `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` 或 `OTEL_EXPORTER_OTLP_ENDPOINT`，不注入默认 logs endpoint。
3. 如果用户将 `OTEL_METRICS_EXPORTER` 设置为不包含 `otlp` 的值，如 `console`、`prometheus` 或 `none`，不注入默认 OTLP metrics 配置。
4. 如果用户将 `OTEL_LOGS_EXPORTER` 设置为不包含 `otlp` 的值，如 `console` 或 `none`，不注入默认 OTLP logs 配置。
5. 如果默认 metrics/logs OTLP endpoint 被注入，会分别向 `OTEL_EXPORTER_OTLP_METRICS_HEADERS` / `OTEL_EXPORTER_OTLP_LOGS_HEADERS` 补缺 `Authorization` 与 `x-worker-epoch`。
6. 后端不会向通用 `OTEL_EXPORTER_OTLP_HEADERS` 补缺 session token。若用户自行设置该变量，会原样保留；用户需要自行承担通用 header 被所有 OTLP signal fallback 复用的风险。

### 服务端本地 JSONL 日志配置

后端会在成功认证并通过 activity/epoch 检查后 best-effort 解码 OTLP HTTP body，并可写入本地 JSONL 文件。该功能不改变 OTLP HTTP 响应；解码或写文件失败只打印服务端日志。

| YAML 配置 | 默认值 | 描述 |
|----------|--------|------|
| `code_session.otlp_file_log_enabled` | development 默认 `true`，production/prod 默认 `false` | 是否写本地 OTLP JSONL |
| `code_session.otlp_log_root` | `./logs` | 本地 OTLP JSONL 根目录，默认相对于配置文件目录 |
| `code_session.otlp_log_body_preview_bytes` | `262144` | `requests.jsonl` body preview 截断字节数 |

文件路径：

```text
{code_session.otlp_log_root}/{safe_code_session_id}/otlp/requests.jsonl
{code_session.otlp_log_root}/{safe_code_session_id}/otlp/metrics.jsonl
{code_session.otlp_log_root}/{safe_code_session_id}/otlp/logs.jsonl
```

`safe_code_session_id` 只保留 ASCII 字母、数字、`_` 与 `-`，其他字符统一替换为 `_`，避免路径分隔符或 `..` 影响日志根目录边界。日志目录以 `0700` 创建，JSONL 文件以 `0600` 创建。

`requests.jsonl` 每个已接受 OTLP export request 一行，包含 request metadata、worker epoch metadata、decode summary 和有界 body preview。`metrics.jsonl` 每个 metric datapoint 一行，`logs.jsonl` 每个 log record 一行。JSON/text-like body preview 以 UTF-8 保存；protobuf/binary preview 以 base64 保存，并带 `truncated` 标记。OTLP JSON 解码忽略未知字段，以兼容 SDK 增量字段。

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
                    │  - best-effort 写本地 JSONL      │
                    │  - 返回 OTLP 成功响应            │
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
  "X-Worker-Epoch": "<epoch>",
  "X-Custom-Header": "value"
}

// 动态 headers（从 helper 函数获取）
config.headers = async () => {
  const dynamicHeaders = await getOtelHeadersFromHelper()
  return { ...staticHeaders, ...dynamicHeaders }
}
```

Code session OTLP 端点运行时必须同时具备：

1. `Authorization: Bearer sk-ant-si-<JWT>`，并要求签名 JWT 的 `session_id` 与 OTLP URL path 一致。
2. `X-Worker-Epoch: {epoch}`，用于拒绝旧 worker 写入。

两者都是硬性要求；`worker_epoch` query 参数仅作为兼容入口，不应作为实际 Claude Code OTel exporter 配置。

---

## 服务端实现指南

### 当前 Go 后端行为

当前实现位于 `internal/codesessions/ingress.go` 和 `internal/codesessions/otlp_file_log.go`：

1. 校验 `Authorization: Bearer sk-ant-si-<JWT>` 的固定算法、`kid`、签名、issuer、audience，以及 `session_id` 与请求 path 的绑定。
2. 使用既有 `maxIngressBodySize` 读取 body。
3. 从 query/header 读取并解析 epoch；缺失或非法值返回 `400 invalid_request_error`。
4. 调用 `TouchCodeSessionWorkerActivityForActiveLease()`；stale epoch 返回 `409 conflict_error`，过期 lease 返回 `410 session_expired`。
5. activity/epoch 检查成功后，按 `Content-Type` 解码 OTLP JSON/protobuf body，并 best-effort 追加写入本地 JSONL。
6. 解码失败或文件写入失败不会改变 HTTP 响应；服务端日志记录失败原因。
7. JSON 请求或 `Accept: application/json` 返回 `{}`；protobuf 请求返回 200 空 body。

错误语义：

| 场景 | 状态码 | error type |
|------|--------|------------|
| token 缺失或不匹配 | 401 | `authentication_error` |
| epoch 缺失 | 400 | `invalid_request_error` |
| epoch 格式非法 | 400 | `invalid_request_error` |
| session 不存在 | 404 | `not_found_error` |
| epoch 与当前 worker 不匹配 | 409 | `conflict_error` |
| 当前 worker lease 已过期 | 410 | `session_expired` |
| body 超过限制 | 413 | `invalid_request_error` |

调试日志会在 body 读取失败、epoch 解析失败以及 DB/epoch/lease 拒绝路径打印。日志包含 request id、signal、path/query、content type、accept、user agent、content length、body byte 数、epoch presence/value/source 和 reason；不会打印 `Authorization` 或完整原始 headers。body 会按 `maxLoggedWorkerRequestBytes` 截断：JSON/text-like 请求以 UTF-8 文本打印，protobuf/binary 请求以 base64 预览打印，并记录 `body_truncated`。

### 当前成功响应

```go
func writeOTLPSuccess(w http.ResponseWriter, r *http.Request) {
	if otlpWantsJSONResponse(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}\n"))
		return
	}
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
}
```

JSON 成功响应：

```http
HTTP/1.1 200 OK
Content-Type: application/json

{}
```

Protobuf 成功响应：

```http
HTTP/1.1 200 OK
Content-Type: application/x-protobuf
Content-Length: 0
```

### 后续完整 Collector 扩展

当前服务已经在 epoch-safe ack 边界之后解码 OTLP JSON/protobuf，并写入本地 JSONL 作为 staging 数据模型。后续如果需要长期分析或告警，可以在同一边界之后增加格式校验、采样/限流、高基数标签保护，并写入时序数据库或转发到外部 collector。下面是未来完整 receiver 的参考设计。

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
X-Worker-Epoch: 1

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

**注意**：当前后端成功响应选择规则与前文一致：请求 `Content-Type` 包含 `json`，或 `Accept` 包含 `application/json` 时返回 JSON `{}`；否则返回 protobuf 空响应。客户端当前只要求 2xx 成功状态，不解析成功响应体。

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
  -H "X-Worker-Epoch: 1" \
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

## 总结

### 关键要点

1. **当前协议**：支持 session-scoped HTTP/JSON 和 HTTP/Protobuf；暂不支持该端点的 gRPC。
2. **端点**：`/v1/code/sessions/{code_session_id}/worker/otlp/metrics` 与 `/v1/code/sessions/{code_session_id}/worker/otlp/logs`
3. **导出间隔**：默认 60 秒
4. **认证**：`Authorization: Bearer sk-ant-si-<JWT>`，校验签名和 claims，并把 `session_id` 绑定到请求 path
5. **时间聚合**：默认 DELTA（增量）
6. **worker 防护**：必须通过 `X-Worker-Epoch` 传当前 epoch，query 参数仅兼容旧调用；服务端同时要求当前 lease 有效

### 配置常量

| 常量 | 值 |
|------|-----|
| 导出间隔 | 60000ms (60秒) |
| 默认协议 | http/protobuf |
| 时间聚合 | DELTA |

### 当前服务端要求

1. 校验 session-ingress JWT，并要求 JWT `session_id` 与 path 中的 `code_session_id` 一致。
2. 必须读取 `worker_epoch`，优先使用 `X-Worker-Epoch` header 校验当前 epoch。
3. 读取请求体并受 `maxIngressBodySize` 保护。
4. 调用 `TouchCodeSessionWorkerActivityForActiveLease()`，同时检查当前 epoch 与未过期 lease。
5. JSON 请求返回 `{}`；protobuf 请求返回 200 空 body。
6. stale epoch 返回 `409 conflict_error`；缺失或非法 epoch 返回 `400 invalid_request_error`；过期 lease 返回 `410 session_expired`。
7. 调试日志记录 OTLP 请求元数据和有界 body 预览；JSON/text-like body 以 UTF-8 打印，protobuf/binary body 以 base64 打印。
8. 成功通过认证与 activity/epoch 检查后，best-effort 解码 OTLP JSON/protobuf，并写入本地 JSONL；未知 OTLP JSON 字段按兼容字段忽略，解码或文件写入失败不改变 HTTP 响应。
9. 本地 JSONL 使用安全路径段、`0700` 目录和 `0600` 文件权限，避免 session id 影响日志根目录边界并降低本机敏感 telemetry 暴露面。

### 后续扩展要求

1. 验证 OpenTelemetry 格式并定义拒绝/降级策略。
2. 将 metrics/logs 写入数据库、时序数据库或转发到外部 collector。
3. 增加数据质量、采样、限流和高基数标签保护。

---

*文档生成时间: 2026-07-01*
*基于代码版本: Claude Code CLI / OpenTelemetry SDK*
