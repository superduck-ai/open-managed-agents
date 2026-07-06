# Session Detail Lane Timeline 设计指南

---

## 1. 概述

泳道图不是独立的横向主时间轴，而是**横向 minimap + 纵向事件列表**的组合：

- minimap 用多条 lane 展示 session/thread 事件在全局时间上的相对位置；
- 纵向事件列表负责显示可读详情，并通过 `[data-event-id]` 与 minimap 双向同步；
- seek 的最终落点是选中事件并滚动纵向列表，不是横向滚动主轴。

核心坐标是 `leftPct` / `widthPct`。所有 tick 都以百分比定位，因此 minimap 宽度变化时不需要重新计算像素。

---

## 2. 范围

### 2.1 包含

- 单 lane 与 multiagent 多 lane minimap。
- 主线程 lane 0 + 子线程 lanes。
- 事件 tick 的百分比定位、对数压缩、最小可见宽度。
- active/hover/filter/playhead/tooltip。
- minimap 点击、拖拽、lane 切换、纵向列表滚动同步。
- raw firehose 与 debug/归一化模式的 tick 生成差异。

### 2.2 不包含

- 事件详情面板的完整内容布局。
- 后端 session event 存储设计。
- 横向可滚动 flamegraph 主视图。
- tick DOM 虚拟化。第一版按全量 tick 渲染，后续性能不足时再评估 canvas 或分段虚拟化。

---

## 3. 数据模型

### 3.1 线程与 lane

lane 的结构不能只从事件流推断。应优先使用线程元数据：

1. 主线程固定为 lane 0，事件来自当前 session 的主事件流。
2. 子线程来自 threads API，按 `created_at` 升序排列。
3. `archived_at != null` 的子线程默认隐藏，可通过“显示归档子线程”展开。
4. 同名 agent 需要去重命名，例如 `Researcher`、`Researcher 2`。
5. `laneIndexByThreadId` 用于跨线程跳转和事件归属。

```ts
interface SessionThread {
  id: string
  parent_thread_id?: string | null
  created_at?: string | null
  archived_at?: string | null
  agent?: {
    id?: string | null
    name?: string | null
  } | null
}

interface Lane {
  label: string
  group: string
  threadId?: string
  items: TimelineItem[]
}
```

### 3.2 Timeline item

minimap 不直接渲染原始 event。先把 event 或归一化 entry 转成 `TimelineItem`。

```ts
type TimelineItemType =
  | 'user'
  | 'agent'
  | 'thinking'
  | 'tool_use'
  | 'result'
  | 'subagent'
  | 'error'
  | 'outcome'
  | 'model_request'
  | 'status_idle'
  | 'status_running'
  | 'status_rescheduled'
  | 'status_terminated'
  | 'interrupt'
  | 'root'

interface TimelineItem {
  id: string
  type: TimelineItemType
  eventType?: string
  label?: string
  preview?: string
  relativeTime: string
  processedAtMs: number
  durationMs?: number
}
```

debug/归一化模式的转换规则：

| entry kind | left 时间 | width 时长 | 备注 |
|---|---|---|---|
| `tool_call` | `bracketStartMs ?? processedAtMs` | `processedAtMs - bracketStartMs + executionMs` 或 `executionMs` | bracket 覆盖模型请求到工具完成的完整区间 |
| `tool_batch` | 同 `tool_call` | 同 `tool_call` | label 显示并行调用数量 |
| `message` | `bracketStartMs ?? processedAtMs` | `inferenceMs` | agent/model 输出区间 |
| `outcome` | `processedAtMs` | `durationMs` | outcome/grading |
| `idle_gap` | `processedAtMs` | `durationMs` | 渲染为 `status_idle` 斜纹 |
| `status` / `passthrough` | `processedAtMs` | 无 | 作为点状最小块 |

跳过以下 entry：

- `queued_boundary`
- queued message
- streaming passthrough
- 无 id 或非法时间的原始 event

raw firehose 模式可使用原始 event 的 `processedAt` 生成点状 item，通常没有 `durationMs`。

---

## 4. 百分比坐标算法

### 4.1 全局排序与截断

所有 lane 的 items 先扁平化再按 `processedAtMs` 全局排序。时长截断也按全局顺序处理，不按 lane 单独处理。

```ts
interface TimelineTick extends TimelineItem {
  leftPct: number
  widthPct: number
  ms: number
  lane: number
}

function buildTimelineTicks(lanes: Lane[]): TimelineTick[] | null {
  const flat = lanes.flatMap((lane, laneIndex) =>
    lane.items.map((item) => ({ item, lane: laneIndex })),
  )

  if (flat.length === 0) return null

  flat.sort((a, b) => a.item.processedAtMs - b.item.processedAtMs)

  const starts = flat.map((entry) => entry.item.processedAtMs)
  const renderDurations = flat
    .map((entry) => Math.max(0, entry.item.durationMs ?? 0))
    .map((duration, index) =>
      index + 1 < starts.length
        ? Math.min(duration, starts[index + 1] - starts[index])
        : duration,
    )

  // Continue with compression below.
}
```

`renderDurations` 只用于 tick 宽度，`durationMs` 原值仍保留给 tooltip 和详情。

### 4.2 对数压缩

把所有正的 `renderDuration` 和 gap 收集起来，取中位数的 4 倍作为压缩阈值。阈值内保持线性，阈值外对数压缩。

```ts
function compress(ms: number, threshold: number): number {
  if (ms <= 0) return 0
  if (ms < threshold) return ms
  return threshold * (1 + Math.log(ms / threshold))
}
```

完整映射：

```ts
const spans: number[] = []

for (let index = 0; index < flat.length; index++) {
  if (renderDurations[index] > 0) spans.push(renderDurations[index])

  if (index + 1 < flat.length) {
    const gap = starts[index + 1] - (starts[index] + renderDurations[index])
    if (gap > 0) spans.push(gap)
  }
}

if (spans.length === 0) return null

spans.sort((a, b) => a - b)
const threshold = 4 * spans[Math.floor(spans.length / 2)]

const offsets: number[] = []
const widths: number[] = []
let cursor = 0

for (let index = 0; index < flat.length; index++) {
  offsets.push(cursor)

  const width = compress(renderDurations[index], threshold)
  widths.push(width)
  cursor += width

  if (index + 1 < flat.length) {
    cursor += compress(
      Math.max(0, starts[index + 1] - (starts[index] + renderDurations[index])),
      threshold,
    )
  }
}

const total = cursor || 1

return flat.map((entry, index) => {
  let leftPct = 1 + (offsets[index] / total) * 98
  let widthPct = Math.max(0.4, (widths[index] / total) * 98)
  const overflow = leftPct + widthPct - 99

  if (overflow > 0) {
    const shrink = Math.min(widthPct - 0.4, overflow)
    widthPct -= shrink
    leftPct -= overflow - shrink
  }

  return {
    ...entry.item,
    leftPct,
    widthPct,
    ms: starts[index],
    lane: entry.lane,
  }
})
```

### 4.3 设计约束

- 有效范围为 1% 到 99%，左右各留 1% 边距。
- tick 最小宽度为 `0.4%`，保证无 duration 或很短的事件可见。
- 溢出时先收窄 width 到最小值，再必要时左移。
- seek、hover、tooltip 都必须使用压缩后的 `leftPct/widthPct`，不要回到原始时间线做命中。

---

## 5. 渲染设计

### 5.1 Minimap tick

tick 是绝对定位块：

```tsx
<div
  className={cn(
    'absolute top-0.5 bottom-0.5 rounded-sm transition-[left,width,opacity] duration-150 pointer-events-none',
    colorClass,
    isActive ? 'opacity-100 outline outline-[1.5px] outline-offset-1 outline-accent-100 z-30' : null,
    isHovered ? 'opacity-100 outline outline-[1.5px] outline-offset-1 outline-accent-200/50 z-30' : null,
    !isActive && !isHovered ? 'opacity-90' : null,
    isHidden ? '!opacity-0' : null,
  )}
  style={{
    left: `${tick.leftPct}%`,
    width: `${tick.widthPct}%`,
    ...(tick.type === 'status_idle'
      ? {
          backgroundColor: 'hsl(var(--bg-200))',
          backgroundImage:
            'repeating-linear-gradient(-45deg, transparent 0, transparent 6px, hsl(var(--bg-000)) 6px, hsl(var(--bg-000)) 12px)',
        }
      : undefined),
  }}
/>
```

颜色建议：

| type | 颜色 |
|---|---|
| `user` | `#c46686` |
| `agent` / `thinking` | accent blue |
| `tool_use` / `result` | neutral gray |
| `subagent` | `#629987` |
| `error` | danger red, no stripe |
| status/root/model request | muted gray |
| `status_idle` | muted stripe |

### 5.2 Lane row

lane row 高度随状态变化：

| 状态 | 高度 | 背景 |
|---|---|---|
| active | 28px | solid muted background |
| hovered | 20px | translucent muted background |
| default | 16px | more translucent muted background |

row 使用 `data-lane-index`，供 compact minimap 或 tab strip 滚动到对应 lane。

### 5.3 Tooltip

tooltip 锚点为 tick 中心：

```ts
const centerPct = tick.leftPct + tick.widthPct / 2
```

内容包含：

- event type badge
- `preview ?? label`
- 原始 `durationMs`（如果存在）
- `relativeTime`

注意：tooltip 的 duration 不使用截断后的渲染宽度。

### 5.4 单 lane 与多 lane

- 单 lane：minimap 的 `windowRef` 表示纵向列表当前视窗覆盖的时间范围，`durationLabelRef` 显示该范围跨度。
- 多 lane：`windowRef` 表示 playhead，圆点和 `dragLabelRef` 显示当前时间标签。

---

## 6. 交互设计

### 6.1 命中检测

```ts
function pickTickAtClientX(
  clientX: number,
  track: HTMLElement,
  ticks: TimelineTick[],
  options: {
    lane?: number
    maxDistancePct?: number
    visibleIds?: Set<string>
    includeIdle?: boolean
  } = {},
): TimelineTick | null {
  const rect = track.getBoundingClientRect()
  const pct = clamp(((clientX - rect.left) / rect.width) * 100, 0, 100)
  let hit: TimelineTick | null = null
  let nearest: TimelineTick | null = null
  let nearestDistance = Infinity

  for (const tick of ticks) {
    if (options.lane !== undefined && tick.lane !== options.lane) continue
    if (options.visibleIds && !options.visibleIds.has(tick.id)) continue
    if (!options.includeIdle && tick.type === 'status_idle') continue

    if (pct >= tick.leftPct && pct < tick.leftPct + tick.widthPct) {
      if (!hit || tick.leftPct > hit.leftPct) hit = tick
    }

    const distance = Math.abs(tick.leftPct - pct)
    if (distance < nearestDistance) {
      nearestDistance = distance
      nearest = tick
    }
  }

  if (hit) return hit
  if (options.maxDistancePct !== undefined && nearestDistance > options.maxDistancePct) return null
  return nearest
}
```

默认不 seek 到 `status_idle`。hover 可传 `includeIdle: true`，让 idle 也能显示 tooltip。

### 6.2 Seek

minimap seek 流程：

1. pointer X 转压缩百分比。
2. 在 active lane 或目标 lane 中命中 tick。
3. `onSeek(tick.id)` 更新 selected event。
4. 外层 effect 查找纵向列表 `[data-event-id="${id}"]` 并 `scrollIntoView({ block: 'nearest' })`。
5. playhead 按 tick `leftPct` 更新。

不要设计成按原始 `processedAtMs` 直接跳转。视觉坐标经过压缩，原始时间命中会和用户看到的位置错位。

### 6.3 Drag

拖拽采用 Pointer Events：

- `pointerdown` 记录 `startX` 并 `setPointerCapture`。
- 横向移动小于 4px 仍视为点击。
- 达到 4px 后进入 dragging，实时命中 tick 并调用 `onSeek`。
- 多 lane 下，拖到当前 lane 首/尾 tick 外 1.5% 可进入 `"start"` / `"end"` 边界态。
- dragging 期间暂停 scroll-driven playhead 同步，避免两个方向互相覆盖。

### 6.4 Scroll sync

纵向事件列表滚动时：

- 单 lane：扫描可见 `[data-event-id]`，取首尾 tick 更新 minimap window；
- 多 lane：扫描第一个可见 `[data-event-id]`，更新 playhead 对应事件；
- 滚到顶部或底部时，playhead 使用 `"start"` / `"end"` 锁定到 0% / 100%；
- 程序化 `scrollIntoView` 后设置短暂 suppress 窗口，避免 scroll handler 反向改选中事件。

### 6.5 Lane 切换

- lane tab 点击：设置 active lane，清空 selected event，纵向列表滚到顶部。
- inactive minimap lane 点击：先同步 active lane ref，再用同一次 pointer 的 `clientX` 在目标 lane 里 seek。
- compact minimap 或 lane tab 的横向/纵向滚动只用于“让 lane 控件可见”，不承担时间定位。

### 6.6 跨线程跳转

`agent.thread_message_sent` / `agent.thread_message_received` 行内标签点击时：

1. 从 `to_session_thread_id` 或 `from_session_thread_id` 找目标 thread。
2. 如果目标 thread 是已归档且当前折叠，先展开归档 lanes。
3. 切到目标 lane。
4. 在目标 lane 的归一化 entries 中，找时间最接近的 counterpart event type：
   - sent 点击跳 received；
   - received 点击跳 sent。
5. 选中该 counterpart event。

---

## 7. Filtering

`visibleIds` 是筛选匹配集合，不是滚动虚拟化窗口。

- 无筛选：`visibleIds = undefined`，所有 tick 渲染。
- raw firehose：`visibleIds = matching raw event ids`。
- debug/归一化模式：当前 active lane 使用筛选后的 entry ids；其它 lane 的 tick 加入 `visibleIds` 保留上下文。
- 不在 `visibleIds` 内的 tick 使用 `opacity: 0` 隐藏，但 DOM 仍存在。

性能含义：tick DOM 数量随 session 总事件数增长。实现时先保持简单；当真实 sessions 出现数千到上万 tick 时，再评估：

- 取消 `left/width` transition；
- 对 minimap 使用 canvas；
- 或按 lane + viewport 做 tick 采样/聚合。

---

## 8. 流式跟随

纵向事件列表需要钉底行为：

- 初次加载后默认滚到底部，除非用户已经选择了某个事件。
- 当 active lane 是主线程且新事件到达：
  - 如果用户在底部 100px 内，自动跟随到底部；
  - 如果用户手动上滚，显示 “New events” 按钮，不抢滚动位置。
- 子线程 lane 加载时显示 lane-level loading skeleton。

`isPinnedToBottomRef` 只表达用户是否愿意跟随纵向列表底部，不参与 tick 过滤。

---

## 9. 建议实现拆分

建议把时间轴逻辑拆成纯函数和 React hook，便于测试。

```txt
web/src/features/managed-agents/session-detail/
  timeline/
    laneModel.ts          // threads + events -> lanes
    timelineItems.ts      // entries/events -> TimelineItem
    timelineCoordinates.ts// TimelineItem[] -> TimelineTick[]
    hitTesting.ts         // clientX -> tick
    useLaneTimeline.ts    // playhead, dragging, scroll sync
    EventsMinimap.tsx
    LaneTabStrip.tsx
```

纯函数必须不依赖 DOM。DOM 逻辑集中在 hook 和组件里。

---

## 10. 测试计划

### 10.1 单元测试

- `buildTimelineTicks`：
  - 空 input 返回 null；
  - 单个无 duration 事件返回 null 或按产品决策显示最小块；
  - 长 idle gap 被压缩；
  - 渲染 duration 被全局下一个事件截断；
  - `leftPct + widthPct <= 99`；
  - `widthPct >= 0.4`。
- `pickTickAtClientX`：
  - 命中重叠最靠后的 tick；
  - 默认跳过 `status_idle`；
  - `includeIdle` 时可命中 idle；
  - `visibleIds` 外 tick 不参与命中；
  - `maxDistancePct` 生效。
- lane model：
  - main lane 固定 0；
  - 子线程按 `created_at` 排序；
  - archived lanes 默认隐藏；
  - 同名 agent 自动加序号。

### 10.2 组件测试

- 点击 tick 后选中事件并滚动纵向列表。
- active tick 使用 outline，高亮不改变填充色。
- filter 后非匹配 tick 透明但仍存在 DOM。
- lane tab 超出宽度时显示左右滚动按钮。
- 多 lane 拖拽时 start/end 边界态正确。

### 10.3 视觉 QA

至少验证：

- 单 lane 短 session；
- multiagent 3 lanes；
- multiagent 10+ lanes；
- 包含长 idle gap 的 session；
- 包含 error、subagent、tool batch 的 session；
- active/hover/filter/streaming 状态。

---

## 11. 常见误区

- 不要把 `visibleIds` 当虚拟化窗口。
- 不要把 minimap seek 写成原始时间 seek；必须用压缩后的百分比命中。
- 不要把 `error` 和 `status_idle` 都画成斜纹；只有 idle 斜纹。
- 不要在每条 lane 内单独计算坐标；所有 lane 共用全局时间坐标。
- 不要让程序化滚动触发 scroll handler 反向选择另一个事件。
- 不要只靠事件流推断子线程 lane；线程元数据是 lane 骨架来源。

---

## 12. 后续风险

- 超长 session 的 tick DOM 规模可能成为性能瓶颈。
- 如果后端事件缺少 `bracketStartMs`、`executionMs` 或 `inferenceMs`，tick 宽度会退化成最小块。
- thread API 与 event stream 到达顺序不同步时，active lane 可能需要基于 thread id 追踪而不是仅存 lane index。
- archived lane 展开后要重放 pending cross-thread seek，否则用户点击归档线程消息会丢失目标定位。
