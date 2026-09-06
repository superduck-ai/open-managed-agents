# Deployments 配置界面

部署列表和创建／编辑弹窗参考 Claude Console 的信息结构，使用仓库现有 shadcn/ui Base UI 组件与语义主题令牌。API、鉴权、workspace 路由和部署执行合同保持不变。

## 界面与状态

- 列表使用边框表格；触发器展示本地化频率、时间和 IANA 时区，自定义表达式直接显示 Cron。空列表提供创建入口，筛选无结果时提示调整条件。
- 创建与编辑共用配置、资源、触发器三个分区。桌面为左侧说明、右侧表单；窄屏改为单列。内容独立滚动，标题和提交按钮固定可达。
- 配置区保留名称、Agent、环境和初始消息；资源区保留可选凭据保险库与记忆存储。Agent 详情入口继续锁定当前 Agent 及版本。
- 新建默认手动运行，切到定时时默认工作日 09:00 和浏览器 IANA 时区。手动／定时切换保留未提交排程草稿。
- 图形化频率支持每分钟、每小时（选择分钟）、每天、工作日、每周（选择星期）。点击时间字段会打开 shadcn/Base UI Popover，使用小时（00–23）和分钟（00–59）Select 选择精确时间；选择即时回填并更新 Cron 与预览，完成、Escape 或点击外部可关闭。字段按当前语言显示时间，内部保持 `HH:mm`。时区使用可搜索的 Base UI Combobox。
- 图形化字段生成五段 Cron。“编辑 Cron”保留当前表达式并进入自定义模式。编辑既有部署时仅将完全匹配的表达式映射成图形控件，其他表达式原样保留。
- 所有用户文案进入 `en` 和 `zh-CN` 字典，星期、时钟与预览日期使用当前 locale。IANA 标识和 Cron 保持协议形式。字段 ID 使用 React `useId`，不依赖翻译文本。

## 排程预览与提交

`deployment-schedule.ts` 使用 cron-parser 解析 POSIX 表达式，使用 Luxon 解析指定时区的墙上时间。先在 UTC 中枚举日历 occurrence，再解析目标时区，以对齐后端 robfig 的夏令时语义：不存在的时间跳过，重复的时间返回两个 UTC 时刻。预览显示未来五次运行，每分钟刷新；闰日计划不受一年窗口限制。

前端拒绝非五段表达式、API 不支持的 Cron 扩展、无未来 occurrence 和无效时区。错误就地展示，定时配置无效时禁用提交。后端仍是参数校验和执行的最终权威；浏览器与服务器使用各自的 IANA 时区数据。

提交沿用现有 `ManagedEntityFormValues` → API 映射：手动为 `schedule: null`，定时为 `{type: "cron", expression, timezone}`。初始事件、资源引用和 Agent 版本映射不变。本次没有预算字段对应的后端支持，因此不提供预算输入。

## 验证

- 排程单测覆盖非法表达式／时区、图形化往返、工作日、Sunday 别名、闰日和夏令时跳过／重复／排序。
- 组件测试覆盖中文日期、中文字段标签唯一关联，以及自定义 Cron 入口。
- ManagedAgentsPage 测试覆盖实际表单操作、无效排程阻止提交、时间选择器小时／分钟回填、图形化／自定义切换、提交 payload、Agent 详情锁定引用与既有手动运行／暂停操作。
- 浏览器检查中英文明暗主题、窄屏独立滚动、菜单层级与搜索时区；时间弹层检查完成／Escape／点击外部关闭、Enter 重新打开、焦点恢复与运行预览更新。
- 仓库验收：`bun test`、`bun run build`、`just web-format-check`、`bun run lint:naming`、`just duplicates`、`just complexity`。
