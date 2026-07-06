# GET /v1/code/sessions/{id}/worker API 文档

## 概述

此接口用于 worker 重启后恢复前一个 worker 写入的外部元数据。成功响应只包含 non-empty 的 `worker.external_metadata`；metadata 为空时省略该字段并返回 `{ "worker": {} }`。GET 是纯读接口，不返回或刷新 connected/activity 等运行时状态字段。

**配套接口**：`PUT /v1/code/sessions/{id}/worker` - 更新工作进程状态

---

## 接口详情

### 请求

| 属性 | 值 |
|------|-----|
| **方法** | `GET` |
| **路径** | `/v1/code/sessions/{id}/worker` |

### 路径参数

| 参数 | 类型 | 描述 |
|------|------|------|
| `id` | string | 会话 ID（Session ID） |

### 请求头

```http
Authorization: Bearer <session_ingress_jwt>
anthropic-version: 2023-06-01
User-Agent: <claude-code-user-agent>
```

---

## 响应

### 成功响应 (200 OK)

```typescript
interface WorkerStateResponse {
  worker?: {
    external_metadata?: Record<string, unknown>
  }
}
```

**字段说明：**

- **`worker`**: 外部 metadata 容器对象（可能为空）
  - **`external_metadata`**: 外部元数据，包含以下可能的字段：
    - **`pending_action`**: 待处理的操作详情（当状态为 `requires_action` 时）
    - **`task_summary`**: 当前任务的摘要信息
    - **`post_turn_summary`**: 回合结束后的摘要
    - 其他自定义元数据字段

### external_metadata 结构

```typescript
interface SessionExternalMetadata {
  // 待处理操作详情（当会话等待用户操作时）
  pending_action?: RequiresActionDetails | null

  // 回合结束摘要
  post_turn_summary?: unknown

  // 任务摘要（长期运行任务的进度描述）
  task_summary?: string | null

  // 其他可能的元数据字段
  [key: string]: unknown
}

interface RequiresActionDetails {
  tool_name: string                    // 工具名称
  action_description: string            // 操作描述（如 "Editing src/foo.ts"）
  tool_use_id: string                   // 工具使用 ID
  request_id: string                   // 请求 ID
  input?: Record<string, unknown>      // 工具输入参数
}
```

注意：后端对 `external_metadata` 使用一层 merge patch 语义；PUT 中 value 为 JSON `null` 表示删除该 key，因此 GET 不会为了表达“无状态”而读回 `pending_action: null` / `task_summary: null` 这类空 key。

### 错误响应

| 状态码 | 描述 |
|--------|------|
| **401 Unauthorized** | 身份验证失败 |
| **403 Forbidden** | 无权访问此会话 |
| **404 Not Found** | 会话不存在 |
| **409 Conflict** | Epoch 不匹配 |
| **500 Internal Server Error** | 服务器内部错误 |

---

## 客户端实现

### 获取工作状态

```typescript
// 外部客户端伪代码/示例；实际调用方位于 superduck-code 的 ccrClient.ts。

/**
 * 获取工作状态
 * Control_requests 被标记为已处理且在重启时不会重新传递，
 * 因此需要读取之前工作进程写入的内容
 */
private async getWorkerState(): Promise<{
  metadata: Record<string, unknown> | null
  durationMs: number
}> {
  const startMs = Date.now()
  const authHeaders = this.getAuthHeaders()

  // 如果没有认证头，返回空
  if (Object.keys(authHeaders).length === 0) {
    return { metadata: null, durationMs: 0 }
  }

  // 带重试的 GET 请求
  const data = await this.getWithRetry<WorkerStateResponse>(
    `${this.sessionBaseUrl}/worker`,
    authHeaders,
    'worker_state',
  )

  return {
    metadata: data?.worker?.external_metadata ?? null,
    durationMs: Date.now() - startMs,
  }
}
```

### 初始化时恢复状态

```typescript
/**
 * 初始化工作进程
 */
async initialize(epoch?: number): Promise<Record<string, unknown> | null> {
  // ...

  // 与初始化 PUT 并发执行 —— 两者互不依赖
  const restoredPromise = this.getWorkerState()

  // 执行初始化 PUT
  const result = await this.request(
    'put',
    '/worker',
    {
      worker_status: 'idle',
      worker_epoch: this.workerEpoch,
      // 清除之前工作进程崩溃留下的陈旧 pending_action/task_summary
      // — 内存中的清理在进程重启后会丢失
      external_metadata: {
        pending_action: null,
        task_summary: null,
      },
    },
    'PUT worker (init)',
  )

  // 等待并发 GET 完成并在此记录 state_restored
  // 这样确保日志在 PUT 成功后记录
  const { metadata, durationMs } = await restoredPromise

  if (!this.closed) {
    logForDiagnosticsNoPII('info', 'cli_worker_state_restored', {
      duration_ms: durationMs,
      had_state: metadata !== null,
    })
  }

  return metadata  // 返回恢复的元数据
}
```

### 重试逻辑（与 GET /internal-events 共享）

```typescript
/**
 * 单次 GET 请求重试
 * 最多重试 10 次，指数退避 + 抖动
 */
private async getWithRetry<T>(
  url: string,
  authHeaders: Record<string, string>,
  context: string,
): Promise<T | null> {
  for (let attempt = 1; attempt <= 10; attempt++) {
    let response
    try {
      response = await this.http.get<T>(url, {
        headers: {
          ...authHeaders,
          'anthropic-version': '2023-06-01',
          'User-Agent': getClaudeCodeUserAgent(),
        },
        validateStatus: alwaysValidStatus,
        timeout: 30_000,  // 30 秒超时
      })
    } catch (error) {
      // 网络错误
      if (attempt < 10) {
        const delay = Math.min(500 * 2 ** (attempt - 1), 30_000) + Math.random() * 500
        await sleep(delay)
      }
      continue
    }

    if (response.status >= 200 && response.status < 300) {
      return response.data
    }
    if (response.status === 409) {
      this.handleEpochMismatch()
    }

    if (attempt < 10) {
      const delay = Math.min(500 * 2 ** (attempt - 1), 30_000) + Math.random() * 500
      await sleep(delay)
    }
  }

  // 所有重试耗尽
  logForDebugging('CCRClient: GET retries exhausted', { level: 'error' })
  return null
}
```

---

## 配套接口：PUT /v1/code/sessions/{id}/worker

### 请求

| 属性 | 值 |
|------|-----|
| **方法** | `PUT` |
| **路径** | `/v1/code/sessions/{id}/worker` |
| **Content-Type** | `application/json` |

### 请求体

```typescript
interface WorkerStateUpdate {
  worker_epoch: number                    // 工作进程纪元号
  worker_status?: SessionState            // 工作进程状态（可选）
  requires_action_details?: RequiresActionDetails | null  // 待处理操作详情（可选）
  external_metadata?: Record<string, unknown>  // 外部元数据（可选）
}

type SessionState = 'idle' | 'running' | 'requires_action'
```

### 使用场景

1. **初始化注册** - 工作进程启动时注册并报告初始状态
2. **状态更新** - 运行过程中状态变化时更新
3. **元数据更新** - 更新任务摘要或其他元数据

---

## 使用场景

### 场景 1：工作进程重启后恢复状态

```typescript
// 工作进程初始化时
const ccrClient = new CCRClient(transport, sessionUrl)

// 初始化会恢复之前的状态
const priorMetadata = await ccrClient.initialize(epoch)

if (priorMetadata?.pending_action) {
  // 恢复待处理操作
  const action = priorMetadata.pending_action as RequiresActionDetails
  console.log(`恢复待处理操作: ${action.tool_name} - ${action.action_description}`)
}

if (priorMetadata?.task_summary) {
  // 恢复任务摘要
  console.log(`恢复任务摘要: ${priorMetadata.task_summary}`)
}
```

### 场景 2：清除陈旧状态

```typescript
// 初始化时清除之前崩溃留下的陈旧状态
await ccrClient.request(
  'put',
  '/worker',
  {
    worker_status: 'idle',
    worker_epoch: this.workerEpoch,
    // 清除之前 worker 崩溃留下的陈旧 pending_action/task_summary
    // — 内存中的清理在进程重启后会丢失
    external_metadata: {
      pending_action: null,
      task_summary: null,
    },
  },
  'PUT worker (init)',
)
```

### 场景 3：报告状态变化

```typescript
// 报告工作进程状态
ccrClient.reportState('running')
ccrClient.reportState('requires_action', {
  tool_name: 'bash',
  action_description: 'Running tests',
  tool_use_id: 'tool_123',
  request_id: 'req_456',
})
```

### 场景 4：更新元数据

```typescript
// 更新任务摘要
ccrClient.reportMetadata({
  task_summary: '正在分析 TypeScript 代码...',
})
```

---

## 请求示例

### 示例 1：成功响应（有状态）

```http
GET /v1/code/sessions/sess_abc123/worker HTTP/1.1
Host: api.anthropic.com
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
anthropic-version: 2023-06-01
User-Agent: claude-code/1.0.0
```

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "worker": {
    "external_metadata": {
      "pending_action": {
        "tool_name": "AskUserQuestion",
        "action_description": "选择部署环境",
        "tool_use_id": "tool_abc123",
        "request_id": "req_def456",
        "input": {
          "question": "选择部署环境",
          "options": ["staging", "production"]
        }
      },
      "task_summary": "正在部署应用到生产环境..."
    }
  }
}
```

### 示例 2：成功响应（无状态 / 空 metadata）

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "worker": {}
}
```

### 示例 4：会话不存在

```http
HTTP/1.1 404 Not Found
Content-Type: application/json

{
  "error": {
    "type": "session_not_found",
    "message": "Session not found or has expired"
  }
}
```

---

## 状态流转

```
工作进程状态流转：

    ┌─────────┐
    │  idle   │  初始状态 / 空闲
    └────┬────┘
         │
         │ 用户消息到达
         ▼
    ┌─────────┐
    │ running │  处理中
    └────┬────┘
         │
         │ 需要用户操作
         ▼
┌──────────────────┐
│ requires_action  │  等待用户响应
└────────┬─────────┘
         │
         │ 用户响应完成
         ▼
    ┌─────────┐
    │ running │
    └────┬────┘
         │
         │ 处理完成
         ▼
    ┌─────────┐
    │  idle   │
    └─────────┘
```

---

## 并发执行

```typescript
// 初始化时的并发模式
async initialize(epoch?: number): Promise<Record<string, unknown> | null> {
  // ...

  // 1. 启动 GET（恢复状态）- 不阻塞后续 PUT
  const restoredPromise = this.getWorkerState()

  // 2. 执行 PUT（注册新 worker）- 不等待 GET 完成
  const result = await this.request('put', '/worker', {...})

  // 3. PUT 成功后，等待 GET 完成
  const { metadata, durationMs } = await restoredPromise

  // 4. 记录恢复日志
  logForDiagnosticsNoPII('info', 'cli_worker_state_restored', {
    duration_ms: durationMs,
    had_state: metadata !== null,
  })

  return metadata
}
```

**为什么这样设计：**

- GET 和 PUT 互不依赖，可以并行执行
- PUT 是关键路径（必须成功才能启动心跳）
- GET 结果用于日志和恢复，可以稍等
- 避免日志竞态：确保 `state_restored` 只在 PUT 成功后记录

---

## 性能考虑

### 重试策略

| 尝试次数 | 延迟范围 | 说明 |
|----------|----------|------|
| 1 | 0ms | 首次尝试 |
| 2 | 500 ~ 1000ms | 第一次重试 |
| 3 | 1000 ~ 1500ms | 第二次重试 |
| 4 | 2000 ~ 2500ms | 第三次重试 |
| 5 | 4000 ~ 4500ms | 第四次重试 |
| 6+ | 最大 30000ms | 后续重试 |

### 超时配置

| 参数 | 值 |
|------|-----|
| 单次请求超时 | 30 秒 |
| 最大重试次数 | 10 次 |
| 最大重试延迟 | 30 秒 |

### 诊断日志

| 事件 | 日志级别 | 事件名称 |
|------|----------|----------|
| 状态恢复成功 | Info | `cli_worker_state_restored` |
| GET 失败 | Warn | `cli_worker_request_failed` |
| 网络错误 | Warn | `cli_worker_request_error` |
| 重试耗尽 | Error | `cli_worker_get_retries_exhausted` |

---

## 配套方法

### CCRClient 方法

| 方法 | 用途 | 内部接口 |
|------|------|----------|
| `getWorkerState()` | 获取工作状态 | GET /worker |
| `reportState()` | 报告状态变化 | PUT /worker |
| `reportMetadata()` | 报告元数据更新 | PUT /worker |

### WorkerStateUploader 配置

```typescript
this.workerState = new WorkerStateUploader({
  send: body =>
    this.request(
      'put',
      '/worker',
      { worker_epoch: this.workerEpoch, ...body },
      'PUT worker',
    ).then(r => r.ok),
  baseDelayMs: 500,
  maxDelayMs: 30_000,
  jitterMs: 500,
})
```

---

## 相关接口

| 接口 | 方法 | 用途 |
|------|------|------|
| `/worker` | GET | 获取工作状态 |
| `/worker` | PUT | 更新工作状态 |
| `/worker/heartbeat` | POST | 发送心跳 |
| `/worker/internal-events` | GET | 读取内部事件 |
| `/worker/internal-events` | POST | 写入内部事件 |
| `/worker/events` | POST | 发送客户端事件 |

---

## 服务端实现建议

### 存储设计

```typescript
// 推荐的数据结构
interface WorkerState {
  session_id: string
  worker_epoch: number
  worker_status: 'idle' | 'running' | 'requires_action'
  external_metadata: {
    pending_action?: RequiresActionDetails | null
    task_summary?: string | null
    post_turn_summary?: unknown
    [key: string]: unknown
  }
  updated_at: Date
}

// 数据库表（PostgreSQL）
CREATE TABLE worker_states (
  session_id VARCHAR(64) PRIMARY KEY,
  worker_epoch BIGINT NOT NULL,
  worker_status VARCHAR(32) NOT NULL,
  external_metadata JSONB,
  updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

// Redis 存储（推荐）
const workerKey = (sessionId: string) => `worker:${sessionId}`
```

### GET 处理逻辑

```typescript
async function handleGetWorker(sessionId: string): Promise<WorkerStateResponse> {
  // 从 Redis 或数据库获取
  const state = await redis.hgetall(`worker:${sessionId}`)

  if (!state.worker_epoch) {
    return { worker: {} }  // 或返回 404
  }

  return {
    worker: {
      external_metadata: JSON.parse(state.external_metadata || '{}')
    }
  }
}
```

### PUT 处理逻辑

```typescript
async function handlePutWorker(
  sessionId: string,
  request: WorkerStateUpdate
): Promise<{ status: number }> {
  // 验证 epoch
  const current = await redis.hget(`worker:${sessionId}`, 'worker_epoch')
  if (current && parseInt(current) > request.worker_epoch) {
    return { status: 409 }  // Conflict
  }

  // 更新状态
  await redis.hset(`worker:${sessionId}`, {
    worker_epoch: request.worker_epoch,
    worker_status: request.worker_status || 'idle',
    external_metadata: JSON.stringify(
      request.external_metadata || {}
    ),
    updated_at: new Date().toISOString(),
  })

  // 设置 TTL（与会话 TTL 一致）
  await redis.expire(`worker:${sessionId}`, 60)

  return { status: 200 }
}
```

---

## 总结

### 关键要点

1. **用途**：获取工作进程状态和外部元数据
2. **初始化恢复**：工作进程重启后恢复之前的上下文
3. **并发执行**：与初始化 PUT 并发执行提高效率
4. **幂等性**：GET 请求天然幂等
5. **重试机制**：最多 10 次重试，指数退避
6. **超时**：单次请求 30 秒超时

### external_metadata 字段

| 字段 | 类型 | 描述 |
|------|------|------|
| `pending_action` | RequiresActionDetails \| null | 待处理操作详情 |
| `task_summary` | string \| null | 任务摘要 |
| `post_turn_summary` | unknown | 回合摘要 |
| 其他 | 任意 | 自定义元数据 |

### 配置常量

| 常量 | 值 |
|------|-----|
| 请求超时 | 30 秒 |
| 最大重试次数 | 10 |
| 基础重试延迟 | 500ms |
| 最大重试延迟 | 30秒 |

---

*文档生成时间: 2026-07-01*
*基于代码版本: Claude Code CLI / CCR v2*
