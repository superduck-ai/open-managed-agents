# Open Managed Agents 前端约定

## 当前范围

- 前端应用位于 `web/` 目录下，是一个由仓库根目录中的 Go 服务支撑的静态 Vite 应用。
- 后端事实来源仍然是 Go 服务；不要把前端改造成 BFF 或独立运行时服务。
- 前端应保留 Open Managed Agents 的产品语义、路由意图、API 兼容性、鉴权行为和业务工作流。
- 视觉实现现在应优先采用原生 shadcn/ui 约定

## 运行时与工具链

- 前端的包管理、脚本、测试和构建统一使用 Bun。
- 使用 Vite + React + TypeScript。
- 预期命令：
  - `bun install`
  - `bun run dev`
  - `bun run build`
  - `bun test`
  - `bun run lint:complexity`
  - `bun run lint:naming`
  - `bun run format`
  - `bun run format:check`
- 修改 `web/` 下的前端代码后，在使用浏览器或 SuperDuck 验证前，需要先在仓库根目录运行 `./restart-web.sh` 重启前端开发服务器。
- 生产环境产物应为 `web/dist` 下的静态文件，由 Nginx 或其他静态服务器提供服务。
- 除非用户明确调整架构，否则不要引入 Next.js、Remix、Node 服务或 Bun HTTP 服务。

## 核心技术栈

- 使用 React + TypeScript。
- 路由使用 TanStack Router。
- 服务端状态使用 TanStack Query。
- 稠密数据表格使用 TanStack Table。
- 仅当表单状态复杂到值得引入时，才使用 TanStack Form。
- 通用 UI 优先选择 shadcn/ui 的 `new-york` 组件。
- Base UI 作为与 shadcn 兼容的交互控件首选无样式基础层。
- 样式使用 Tailwind CSS 和语义化 CSS 变量主题令牌。
- 图标默认使用 `lucide-react`，除非现有 shadcn 组件已经提供了更合适的默认图标。

## shadcn/ui 规则

- 在构建或迁移任何通用控件之前，先查看官方 shadcn 组件目录：`https://ui.shadcn.com/docs/components`。
- 下列常见 UI 优先使用官方 shadcn 组件：
  - `Accordion`, `Alert`, `Alert Dialog`, `Avatar`, `Badge`, `Breadcrumb`, `Button`, `Button Group`
  - `Card`, `Checkbox`, `Collapsible`, `Combobox`, `Command`, `Context Menu`
  - `Data Table`, `Dialog`, `Drawer`, `Dropdown Menu`, `Empty`, `Field`
  - `Input`, `Input Group`, `Label`, `Native Select`, `Pagination`, `Popover`
  - `Progress`, `Radio Group`, `Scroll Area`, `Select`, `Separator`, `Sheet`
  - `Skeleton`, `Slider`, `Spinner`, `Switch`, `Table`, `Tabs`, `Textarea`
  - `Toggle`, `Toggle Group`, `Tooltip`, `Typography`，以及适合场景时使用的较新 message/chat 组件。
- 不要手写 shadcn/Base UI 已经提供的行为：portal 挂载、弹层定位、点击外部关闭、Escape 处理、焦点恢复、键盘导航、ARIA menu/dialog/select 角色、switch 语义或 tab roving focus。
- 如果 shadcn 没有完全匹配的组件，先组合最接近的官方组件，再补上最小适配层。
- 生成的 shadcn 组件应保留在仓库内部。可以为项目令牌做适配，但不要分叉成 feature 专属的一次性版本。
- feature 代码不应直接散落原始 `@base-ui/react` 导入。通用 primitive 应封装到 `src/shared/ui/` 中，或使用该目录导出的生成版 shadcn 组件。

## UI 与设计

- 默认视觉风格采用 shadcn `new-york`：紧凑控件、克制边框、语义化令牌、常规圆角、清晰焦点环，以及完整的明暗主题对齐。
- 不要再严格遵循 Vercel/Geist 的 `design.md`；现在以 `web/design.md` 和 `web/design.dark.md` 记录 shadcn 令牌方向。
- 从一开始就构建正常可用的深色与浅色主题。
- 使用 shadcn 风格的语义令牌，例如 `background`、`foreground`、`card`、`popover`、`primary`、`secondary`、`muted`、`accent`、`destructive`、`border`、`input` 和 `ring`。
- 不要新增 `--oma-*` 兼容别名。新的共享 UI 应优先使用 shadcn 令牌类，如 `bg-background`、`text-foreground` 和 `border-border`；迁移时应连同使用这些旧别名的代码一起移除。
- 不要做营销落地页。登录后的首屏应该是真实控制台。
- 常见工具按钮优先使用图标。
- 避免嵌套卡片和装饰性渐变。
- 按钮、单元格和菜单中的文本在窄屏下不得溢出。
- Popover 和菜单必须渲染在周围导航/内容之上，避免被滚动容器裁剪，并保留正确的键盘与指针交互行为。

## 架构边界

- 将前端视为静态控制台应用。
- API 边界必须严格区分：
  - `/api/*` 是 Console API。
  - `/v1/*` 是兼容 Anthropic 的 API 表面。
- 不要为了前端便利而重塑 `/v1` 的行为。
- `/api` 不应依赖 SDK 兼容的错误结构。
- 前端可以同时调用 `/api` 与 `/v1`，但 API client 必须显式区分这两条边界。
- 保持路由模块精简；feature 逻辑应放在 `features/*` 中。
- 优先采用 `quickstart/`、`agents/`、`sessions/`、`resources/` 这类垂直 feature 切片，而不是兜底式 utility 文件。
- 重构时如可行，应保留现有导入的公共外观；待调用方迁移完成后，再清理未使用的适配层。

## 鉴权、会话与 CSRF

- 前端鉴权从 `/api/bootstrap` 开始。
- 将 bootstrap 返回的 account、organization、workspace、permissions 和 CSRF token 存入应用鉴权上下文。
- 会话身份基于 cookie；不要把 session key 存入 `localStorage` 或 `sessionStorage`。
- 所有基于 cookie 鉴权的变更请求都必须发送 `X-CSRF-Token`。
- 登录在可用时使用 `POST /api/auth/login`。
- 登出使用 `POST /api/auth/logout`。
- 收到 `401` 时，清空本地鉴权状态并跳转到登录页。
- 收到 `403` 时，保留会话，但展示无权限状态。
- 不要实现仅存在于前端的鉴权绕过。

## 角色与权限

使用以下角色标签和值：

| 标签        | 值                 | 说明                                           |
| ----------- | ------------------ | ---------------------------------------------- |
| User        | `user`             | Use Workbench                                  |
| Claude Code | `claude_code_user` | Use Workbench and Claude Code                  |
| Developer   | `developer`        | Use Workbench, Claude Code and manage API keys |
| Billing     | `billing`          | Use Workbench and manage billing details       |
| Admin       | `admin`            | Do all of the above, plus manage users         |

- 前端中的权限检查只用于 UX。
- 后端 RBAC 才是权威来源。
- 使用 bootstrap 权限来隐藏或禁用路由、按钮、菜单项和表格操作。
- 如果已经存在权限名，不要在组件中硬编码 `admin` 判断。
- 权限辅助函数应放在 `src/shared/permissions/`。

## API Client

- API client 放在 `src/shared/api/` 下。
- `/api` console 路由和 `/v1` 兼容路由分别使用各自的 client。
- `/api` 请求：
  - 需要携带 credentials。
  - 变更请求需要携带 `X-CSRF-Token`。
  - 默认使用 JSON。
- `/v1/files` 请求：
  - 需要包含 `anthropic-beta: files-api-2025-04-14`。
  - 需要包含 `?beta=true`。
  - 保持后端响应结构不变。
- 在 API client 边界统一归一化错误，收敛成一个小型前端错误类型。

## 路由

- 使用 TanStack Router 的 route guard。
- 受保护路由必须在 bootstrap 成功后才能进入。
- 带权限门控的路由中，若用户已认证但未授权，应渲染 access-denied 状态。
- 避免让路由文件直接耦合原始 fetch 调用。

## Workbench 与流式处理

- Workbench 的流式能力是必需的。
- 不要强行把 SSE 流塞进 TanStack Query。
- 流式辅助函数放在 `src/shared/api/streaming.ts`。
- 流式辅助函数必须支持：
  - POST body
  - abort/cancel
  - 增量事件解析
  - `401`/`403` 处理
  - 服务端错误事件
- Workbench UI 应处理 idle、generating、canceling、partial output、completed 和 failed 状态。
- 迁移 Workbench UI 时，应使用 shadcn 组件替换通用遮罩层和控件，但除非任务明确要求，否则不要重写 CodeMirror、Tiptap 或流式状态机。

## 代码高亮

- Managed Agents quickstart 代码块应渲染为 Highlight.js 风格标记：`code.language-yaml`、`code.language-json`、`code.language-bash` 等，并带有嵌套的 `hljs-*` token span。
- 不要为 quickstart、transcript、debug 或 integration 片段手写新的语法 tokenizer。
- Highlight.js 导入应限制在 `highlight.js/lib/core` 及显式语言模块之内。
- 通过 `web/src/styles.css` 中的语义化 CSS 变量和 `.hljs-*` 选择器来定义 token 样式。
- 展示长 API 调用的代码块必须自动换行，避免水平滚动。
- 测试应断言 `textContent` 以及 `hljs-*`/`language-*` DOM class，而不是跨完整代码 token 使用 `getByText`。

## Files

- Files 页面应使用 `/v1/files?beta=true`。
- 展示 ID、名称、大小、创建时间和操作。
- 上传使用 multipart 字段 `file`。
- 删除必须要求确认。
- 当 `downloadable=false` 时，下载按钮必须禁用。
- 行操作中应提供复制 file ID。
- 保持与 SDK 兼容的响应处理不变。

## Members

- Members 页面必须使用上面列出的五个角色值来实现角色下拉框。
- 角色展示文案必须与现有产品预期一致：
  - User: Use Workbench
  - Claude Code: Use Workbench and Claude Code
  - Developer: Use Workbench, Claude Code and manage API keys
  - Billing: Use Workbench and manage billing details
  - Admin: Do all of the above, plus manage users
- 只有拥有 `members:manage` 的用户才能看到角色变更控件。
- 在 UI 中避免明显的自我锁死，但也要依赖后端进行强制校验。

## 测试

- 优先把测试写在 feature 附近。
- 在可行情况下，工具函数、API client、权限和组件逻辑使用 Bun test。
- 应用外壳可用后，浏览器流程使用 Playwright。
- 完成前端修改前，运行：
  - `bun run format:check`
  - `bun test`
  - `bun run build`
- 如果 UI 变更涉及层级、响应式行为或交互保真度，需要打开浏览器验证。

## 标识符命名

- React 组件、类型、接口和类使用 PascalCase；普通函数、变量和参数使用 camelCase；模块级常量允许 UPPER_CASE；泛型类型参数使用 PascalCase。
- 组件或构造器引用作为参数传递时允许 PascalCase（例如 `Icon`）；普通值参数仍使用 camelCase。
- API/数据库 payload 的 `snake_case` 仅保留在边界属性和解构位置，内部标识符应转换为 camelCase。
- 修改 TypeScript/TSX 标识符或命名配置后运行 `bun run lint:naming`。

## 圈复杂度

- 修改生产 TypeScript/TSX 后运行 `bun run lint:complexity`。新文件和未列入历史预算的文件采用 modified cyclomatic complexity 上限 20。
- `eslint.complexity.config.js` 中的历史文件预算是只能下降的 ratchet，不是目标值；不得为了通过检查提高预算或扩大匹配范围。
- 复杂函数优先拆成领域判断、数据归一化、事件处理和展示组件。保持 API 请求、状态流、路由语义、文案与样式不变，并用对应功能测试和 `bun run build` 验证机械拆分。

## 本地开发

- 后端需在仓库根目录单独启动。
- 前端从 `web/` 目录启动。
- Vite 开发服务器应将 `/api` 和 `/v1` 代理到 Go 服务。
