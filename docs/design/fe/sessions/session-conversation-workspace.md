# Session 会话 Viewer 设计

## 目标

Session 详情页同时承担继续对话和检查运行上下文两个职责。页面以 Claude Platform 新版 Session Viewer 作为主从布局、信息密度和事件交互基准，同时保留 OMA 的业务语义、API 合同、shadcn/new-york 组件与明暗主题令牌。

本页面不再使用旧版 `Events / Resources / Agent / Environment / Credentials` 页面级页签。主界面固定由左侧会话转录和右侧 Session Inspector 组成，用户无需离开对话即可查看上下文。

## 页面结构

宽屏标题区将名称、状态、Agent、Environment、Vault、耗时、费用和更新时间放在同一摘要行；除状态外，摘要使用点分隔的轻量文本元信息，不使用连续描边 Badge。操作菜单位于右侧。较窄宽度按下述降级规则换成标题行与元信息行。摘要下方依次是转录搜索、线程时间轴和 Viewer；主栏固定展示转录，不提供事件类型筛选或 Debug 视图切换。

Session 画布占满 `SidebarInset` 的全部剩余宽度，不设置页面级 `max-width`，Shell 也不再额外占用横向 padding，避免超宽屏出现与 Viewer 无关的对称空白。Breadcrumb、标题摘要和筛选工具条共用由 Session 画布容器宽度驱动的响应式内部内容轨：窄屏 `16px`、中等宽度 `24px`、桌面 `32px`；Claude 的 Minimap 例外地固定使用 `32px` 外层 gutter。Viewer 与它们共享同一画布边界但不额外添加左右边框。Transcript 正文、Action Card 和 Composer 继续由各自的 `720px` 内容轨限制阅读宽度；Inspector 继续使用独立宽度规则。顶部 Header 按 Claude 的层级分成两行：Breadcrumb 与 Actions 共用第一条 utility row，标题、状态和摘要位于第二行；容器小于 `768px` 时 Actions 才移至 Breadcrumb 下方。`1280px` 后显示 Created/Tokens，`1536px` 后标题与摘要横排并显示 Environment/Resources/Vaults。标题与状态优先保持单行；空间不足时摘要元信息移至标题下方，并按语义优先保留 Agent、Cost 和 Duration。操作按钮保持独立，不得覆盖标题或元信息。

转录工具条保持单行，桌面高度约 `44px`。左侧只保留 Search，搜索框由 Viewer 容器宽度响应：容器小于 `640px` 时为 `176px`，达到 `640px` 后固定为 `224px`；右侧 Copy/Inspector 操作固定可见，单一搜索控件不创建横向滚动轨道。`Copy all` 始终复制当前 Lane 的完整转录，不受搜索结果过滤影响。原始事件浏览统一放在 Inspector 的 Events 页签；主 Transcript 不展示 `All events`、事件类型或 Debug 控件。Inspector Events 无条件挂载 Claude 的 FilterCombobox 触发器：初始显示 `All events` 且 popup 关闭，点击后提供搜索、`Transcript events` 和 wire event types 多选；没有 wire event type 时也不禁用入口。Minimap 内部使用 Claude 的 `8px` 顶部、约 `28px` 主/活动 Lane、`20px` 非活动 Lane、`6px` Lane 间距和 `4px` 底部节奏；多 Lane 布局始终预留一个展开子 Lane 的高度，切换活动 Lane 只能改变内部行高分配，不得改变 viewport 高度或向下挤压 Viewer。viewport 用 `-3px` inline margin 配合 `3px` padding 保护 tick outline，并在多 Lane 纵向滚动时使用 `16px` 底部渐隐。初次加载事件时先显示同尺寸的 shadcn Skeleton，加载完成后原位替换 Minimap；没有可展示事件时不保留空白轨道。`1×` 时只允许纵向滚动并隐藏横向 overflow，轨道使用默认光标；放大后才启用双向滚动，并以 grab/grabbing 光标提示可按住鼠标水平平移。拖动只改变 Minimap viewport 的 `scrollLeft`，不触发事件 seek；位移小于 `4px` 的 pointer 手势仍按点击事件处理。触控板/滚轮横向滚动与鼠标拖动必须共存，两种状态都隐藏原生 scrollbar。Minimap 支持 `1×–4×`、每步 `0.25×` 的缩放；内容足够高时可在 `100px` 最小高度与 `min(内容高度, 60vh)` 最大高度之间纵向调整，默认不超过 `280px`，键盘 ArrowUp/ArrowDown 每次调整 `16px`。所有 Lane 共用同一条时间映射，同一时间发生的跨 Lane 事件必须横向对齐；该几何映射不得改变后端事件数组或转录展示顺序。短事件绘制宽度至少为 `3px`。多 Lane 左侧使用可淡出的 sticky Lane 名称；sent/received 可以可靠配对时使用两端事件位置绘制贝塞尔连接线，尚未收到对应事件时只在已知目标 Lane 上绘制临时连接，目标 Lane 缺失时不猜测。连续至少 `30s` 的 idle 区间使用约 `11px` 固定宽度参与共享时间映射，并以 Lane 背景 window 表达，不再渲染斜纹 idle tick；长时间空闲不得挤压下一轮 turn 的可见宽度。时间线支持 Left/`k`、Right/`j`、Home、End 定位。Minimap hover 详情通过 `document.body` portal 使用 fixed 定位，避免被 viewport 的纵向 overflow 裁剪。进行中的 model bracket 从 `span.model_request_start` 起标记为 open tick，并按当前时间每 `250ms` 增长；时间轴至少保留 `10s` 的可视时间域。open tick 使用约 `2s` 周期的轻量透明度脉冲，并在 `prefers-reduced-motion: reduce` 下禁用。收到 `span.model_request_end` 后使用精确 inference duration，若结束 span 尚未进入 SSE 缓存，则先用紧邻的 idle/status 边界冻结临时时长，历史事件补齐后不得产生宽度跳变。

Viewer 包含两个区域：

1. **转录区**：按 speaker turn 和 model iteration 展示 User/Agent 消息、Thinking 摘要、紧凑工具调用行、线程 lane、minimap、消息输入框和待处理 Action Card。
2. **Session Inspector**：默认打开，提供 `Session / Events / Tools / Resources / Threads / Traces` 六个页签以及关闭按钮。Inspector 关闭后，转录区占满可用宽度，并在工具栏显示重新打开入口。

全局控制台侧边栏不属于 Viewer。进入 Session 时必须保持用户原有展开状态，不得因路由切换自动收起、展开或改成临时抽屉。

## 转录与对齐

- Transcript 内容列、待处理 Action Card 和消息输入框共享最大 `720px` 的居中内容轨道。
- 三者在窄容器中使用相同的 `16px` 水平留白；滚动条采用覆盖式自动隐藏样式，不允许通过 Composer 或 Action Card 的伪滚动容器预留 gutter。左右边界必须逐像素一致。
- 转录先按未过滤的事件流建立 speaker turn，再按 model request bracket 建立 iteration，最后应用搜索；搜索不得把原本由 User、idle、queued、outcome、status 或 speaker 变化分开的 turn 重新合并。
- 前端缓存、Transcript、Inspector Events 和 minimap 必须保持后端 `data[]` 或 SSE 的到达顺序，不得按时间、speaker 或事件类型再次排序。同 ID 更新在原位置替换，新 ID 按到达顺序追加；流式预览被正式消息替换时也必须保留预览原位置，即使 `status_idle` 先到也不能先删除预览再把正式消息追加到 turn 之后。Idle 去重和 Tool Batch 折叠只删除或压缩事件，并将聚合项放在第一条被折叠事件的位置，不能移动其他事件。`status_idle` 到达后保留短暂 grace period，再强制同步历史并清理仍未完成的流式预览；Idle 可以结束 UI 的生成状态，但不能提前销毁等待后续 Agent/message end 事件补齐的 model bracket。
- 对话使用仓库共享的 shadcn `Message` / `Bubble` 结构：User turn 是 OMA 明确保留的右对齐风格，桌面最大占内容轨 `80%`，窄屏放宽到 `92%`；Agent turn 按 Claude 逻辑占满 720px 内容轨，不再额外收窄到 `90%/94%`。
- User 使用 `session-speaker-user/10` 角色色背景和 `0.5px session-border` 的轻量 panel bubble，圆角 `10px`、水平内边距 `11px`、垂直内边距 `6px`；Bubble 高度由正文自然决定，不设置会在单行正文下方制造额外空白的固定最小高度。Agent 名称和时间在连续 turn 中只展示一次；每个 Agent iteration 使用 `10px` 圆角、`0.5px` 语义边框、`10px 4px` 内边距和 `5px` 间距，Agent text、Thinking 和 Tool Call 在 panel 内保持 `6px 2px` 行内节奏。idle、queued、outcome 和 status 等系统边界保持全宽，不伪装成对话气泡。
- Agent 标签使用 `session-speaker-agent` 主题变量，User 标签使用 `session-speaker-user`；两个变量必须同时定义浅色和深色值，不使用 chart token 或硬编码颜色冒充领域语义。
- 完成态消息使用 `react-markdown` 与 `remark-gfm` 渲染 CommonMark/GFM，支持标题、有序/无序/嵌套列表、引用、任务列表、删除线、表格、代码、链接和 Markdown 图片。原始 HTML 不解析；URL 只允许 HTTP(S)、邮件、页内锚点与站内根路径；代码块复用现有 Highlight.js 渲染。单条正文按生产 JS 的 UTF-16 `length/slice` 语义最多渲染前 `50,000` 个字符，超限时显示原始总字符数；普通 Markdown 与 fenced code 使用同一上限。流式消息仍沿用平滑文本更新。
- Agent 正文与 Thinking 摘要都复用 shadcn `Bubble/BubbleContent`；iteration 内使用无额外卡片层级的 `ghost` variant，正文保持 Markdown 语义，Thinking 使用紧凑的 ghost Button。Thinking 在转录中只显示单行斜体摘要 `Thought for {duration}` 或 `Thinking…`；完整原文通过 Inspector Events 查看。
- Agent turn 挂载时只要仍是 open 状态，就播放一次 `180ms ease-out` 入场；首次加载和切换 Lane 后重新挂载的 open turn 也会播放，已经闭合的 turn 直接显示最终状态。`prefers-reduced-motion: reduce` 下不播放位移或缩放。
- 工具调用保持 `24–28px` 的单行结构，展示工具名、截断输入摘要、执行状态和耗时；点击后在 Inspector 的 Events 页签查看原始事件。
- Markdown 的交互链接不能嵌套在事件选择按钮中；正文链接保持自身语义，事件选择使用独立可访问控件。
- 原始事件审计统一位于 Inspector Events，固定使用 `Event / Preview / Time` 三列，并对事件 namespace 做轻量着色。

## 滚动与高度

- Session Detail 是 Console Shell 内的 viewport workspace。`SidebarInset`、内容 wrapper、Session Detail、Viewer 和分栏均使用连续的 `flex/min-h-0/overflow-hidden` 高度链，不依赖 `xl:` 才生效的 `calc(100vh - ...)`。
- 常规双栏状态只有两个主要纵向 scrollport：Transcript 的 shadcn MessageScroller viewport，以及当前 Inspector panel 的 Base UI ScrollArea viewport。
- Composer、Action Card、Viewer、ResizablePanel 和 Inspector 外壳不得声明纵向滚动；textarea 只有超过 `160px` 后才局部滚动。
- Transcript 内容本身不追加底部 spacer；底部呼吸区只由 MessageScroller 内容列统一提供，避免短回复与 Composer 之间重复叠加空白。
- Events 页签可将列表和详情划分为两个独立 scrollport，但详情内部的 `pre/code` 不得再创建第三层纵向滚动。
- Inspector tabs、lane tabs 和 minimap 的横向滚动条默认隐藏；活动 Inspector 标签必须自动滚入可见范围，不使用 scroll-timeline 或自定义属性维护渐隐动画。

## Inspector

Inspector 的默认宽度为 `480px`，最小宽度为 `360px`，最大不超过容器宽度的 `70%`。容器达到 `1056px` 时使用可拖拽双栏，拖动宽度保留在当前 Viewer 实例中；小于阈值时 Inspector 覆盖 Viewer 内容，关闭后恢复原转录滚动位置。所有 Inspector 页签必须约束自身最小宽度和横向溢出，长事件预览不能反向撑宽控制台主区。

Inspector header 与 tab 控件共用 `32px` 总高度。外壳始终使用 panel surface 和 `overflow-hidden`；普通页签各自只使用一个 Base UI ScrollArea。Tab list 可以在极窄宽度下横向移动，隐藏原生 scrollbar，并将活动 tab 自动滚入视图。每个 tab 使用带 `inspector` query 的真实链接：普通点击只切换 Base UI tab，modifier click 保留浏览器新窗口/新标签行为，刷新后恢复合法页签。关闭 Inspector 时，若焦点原本位于 Inspector 内，则转移到重新打开入口；从该入口打开后焦点转移到关闭按钮。Inspector header 只保留页签和关闭按钮，不提供额外的 Full height/More 状态。

六个页签的职责如下：

1. **Session**：ID、状态、创建/更新时间、Agent、Environment、Vault、Deployment 和 Cost。关联实体名称可导航到 OMA 对应详情页；metadata 请求失败时继续用原始 ID 提供同一链接，不能降级成不可点击文本或数量。
2. **Events**：按后端返回顺序排列原始事件，直接使用 `Event / Preview / Time` 粘性表头、`192px` 事件列和 `24px` 紧凑行。FilterCombobox 的 `Transcript events` 与 wire type 使用交集语义；筛选只隐藏行，不重新排序，也不清除仍存在但暂时不可见的已选 detail。Time 列只展示时间，不影响顺序；`processed_at` 为空时显示 queued，不回退到 `created_at`。选择事件后使用 Claude 的纵向 list/detail split：列表至少保留 `120px`，详情默认 `360px` 并可拖动；为满足 OMA 已确认的交互要求，详情在首次出现或切换事件时播放 `180ms ease-out` 的 `translateY(8px) + opacity` 上浮动画，并以顶部 hairline 和轻量向上阴影表达层级；它仍是 Inspector 内的普通分栏，不改成 drawer 或脱离滚动模型的 overlay。`prefers-reduced-motion` 下禁用动画。Agent message/thinking 详情默认展示 Raw JSON；当前浏览器标签页实时捕获到增量帧时可切换 Deltas 紧凑表格，历史事件不伪造增量，也不维护字符缺失/重复比对算法。详情由唯一的 viewport 统一滚动。
3. **Tools**：展示 Name、Permission、Calls、Failed、p50，并提供工具搜索和 `All threads / Current agent / Current thread` Scope；`Current agent` 只按 Lane 的既有 `group` 归属筛选，不按名称猜测。配置工具按 Built-in、Custom 和 MCP Server 分组，未出现在当前 Agent 配置中的实际调用单列为 `Called, not configured`；只有主 Agent 配置可用时才展示其 `Configured on`。默认 detail 是带 `64×64` CSS conic-gradient 结果环的 Overview；Failed 非零时同时展示失败率，Failed/Denied 为零时使用国际化 `none`，Completed 始终保留数字。选择工具后展示调用表，调用行与转录的选择和悬浮状态联动，清除选择后回到 Overview。调用表仅在存在审批时展示 Waited，仅在当前 Scope 跨多个线程时展示 Thread。Time in tools、Executing 和 Waiting 对同一线程内重叠的调用区间做并集合并，不重复累计同一 Tool Batch 中的并行墙钟时间；不同线程分别累计。
4. **Resources**：提供常驻资源筛选，按 Path/Size 展示 Session 挂载资源，并通过 `+ Resource → File` 挂载已有文件；当前产品不展示 GitHub Repository 或 Memory Store 入口。
5. **Threads**：展示有真实数据来源的 Thread、Status、Context；在后端提供线程级费用前不展示 Cost 占位列。detail 始终绑定 active thread，不提供本地关闭态。detail 显示 Agent、Model、Effort 和 `140px`、step-after area 的 Context usage 图；图上 model-request point hover 与 Transcript/Events 使用同一个 event ID 联动。单时间点只显示一个 X 轴标签，短会话显示秒，避免重复时间标签叠加。
6. **Traces**：仅在 `observability.enabled=true` 时查询当前 Session 的 OpenObserve traces。选中 trace 后使用 `trace_id` 查询参数保存详情状态；返回列表或切换到其他 Inspector 页签时删除该参数。observability 路由返回 404 时展示 “Observability is not enabled”，不使用通用加载错误。

Events、Tools、Threads 共用 list 最小 `120px`、detail 默认 `360px` 的纵向 split。拖拽条使用 `7px` 命中带和居中的 `1px` hairline，并在 hover、键盘焦点和拖动时显示语义强调色。用户调整后的 detail 高度按页签在当前 Session Viewer 生命周期内分别记忆；不写入浏览器存储，也不跨 Session 恢复。三者关闭语义不同：Events 清除 event 选择；Tools 清除 tool 选择并回到 Overview；Threads 始终绑定 active thread。只有由消息/事件选择触发的 Events detail 播放上述轻量上浮动画；分栏尺寸、滚动所有权和关闭语义保持不变。

## 数据来源

| 区域          | 数据来源                                            | 说明                                              |
| ------------- | --------------------------------------------------- | ------------------------------------------------- |
| 标题摘要      | Session retrieve + events                           | 状态、引用、用量、时间与实时状态                  |
| 转录与 Events | Session/Thread events + SSE                         | 保留现有缓存、补帧、lane 和实时状态机             |
| Session       | Session retrieve + Agent/Environment/Vault retrieve | 只读取关联实体名称和固定版本信息                  |
| Tools         | 原始事件 + Agent retrieve                           | 聚合配置工具、权限、调用、失败和耗时              |
| Resources     | Session retrieve + File metadata                    | 挂载关系来自 Session；名称和大小按 `file_id` 获取 |
| Threads       | Session threads + thread events + Agent retrieve    | 聚合线程状态、模型用量和 context 阶梯点           |

进入 Resources 页签时重新请求 Session retrieve，以获取后端最新挂载关系；再次点击已激活的 Resources 页签也会刷新。打开 File 表单时按需读取 Files list，提交后调用 Session resources add，并再次刷新 Session。

## 对话与审批行为

- `Enter` 发送，`Shift+Enter` 换行；输入法合成中和键盘长按不得触发发送。
- Composer 使用 shadcn InputGroup 和语义化 form：空状态高 `56px`、圆角 `22px`、单行起步并按内容增长至 `160px`；Send 是 submit，Stop 是普通 button。
- 空消息、发送中、已归档、已终止或已删除的 Session 不能发送；idle Session 仍允许发送新消息。
- running、queued 或 rescheduled Session 显示停止按钮，并通过既有 `user.interrupt` 合同停止。
- 最新 `session.status_idle.stop_reason` 为 `requires_action` 时，在转录与输入框之间展示 Action Card。普通工具审批发送 `user.tool_confirmation`，AskUserQuestion 答案发送 `user.custom_tool_result`；等待期间禁用普通消息输入框。
- 用户离开列表底部后停止自动跟随并显示“回到最新事件”；位于底部时继续跟随流式正文增长。
- Session 最终状态以 SSE/后端响应为准；前端临时状态只用于交互反馈。

## 非目标

- 不重写事件归一化、SSE 重连、thread lane 或 minimap 算法。
- 复用现有 Session resources add 挂载文件；不新增后端 API，也不开放关联实体编辑。
- 不伪造 Thread cost、等待时长或后端未返回的统计数据。
- 不展示 credential secret，也不恢复旧版关联实体完整配置卡片。
