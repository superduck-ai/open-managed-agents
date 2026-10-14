# Console 导航

首页仪表盘保持原有布局、卡片、文案与操作入口。主界面仅调整原 Manage 分组为工作区设置，其他模块与页面保持原样；组织设置仍按已确认方案精简。

## 控制台导航

顶层首页、LLM models、API keys 保持原样。Managed Agents 默认展开；Build、Analytics 和工作区设置默认折叠，进入其可见子项时自动展开。用户手动展开的其他分组保持原状态。

- Build：Files、Skills、MCP Servers。真实 Workbench、Batches 仍保留路由，暂时隐藏导航入口。
- Managed Agents：Quickstart、Agents、Sessions、Observability、Deployments、Environments、Credential vaults、Memory stores。
- Analytics 与 Claude Code 的导航定义、可见性和页面保持原有行为。
- 工作区设置：Members、API keys、Webhooks、MCP tunnels（Preview）。密钥、Webhook、隧道使用当前工作区的规范设置路径。Members 使用当前工作区上下文；默认工作区引导到组织成员管理。

组织设置独立侧栏只包含组织、成员、工作区。个人资料与外观入口位于账号菜单，其页面使用个人设置侧栏；账号菜单内的语言与主题切换继续生效。

```mermaid
flowchart TD
  Console[控制台] --> Home[首页：保留原有仪表盘]
  Console --> Build[Build]
  Console --> Agents[Managed Agents]
  Console --> Analytics[Analytics：保持原样]
  Console --> Workspace[工作区设置：成员 / 密钥 / Webhook / 隧道]
  Console --> Account[账号菜单]
  Account --> Org[组织设置：组织 / 成员 / 工作区]
  Account --> Personal[个人设置：资料 / 外观]
```

## 下线页面和兼容入口

组织设置移除账单、限制、管理员密钥、服务账号、工作负载身份、隐私控制、身份和访问页面。工作区设置移除原 Manage 分组中的限制、服务账号、隐私控制、安全入口，对应历史路径显示下线提示。主界面的 Analytics、Playground、Claude Code 和账单等其他页面保留原有实现与路由。

`/settings/api-keys` 重定向到当前工作区的 `/settings/workspaces/{workspaceId}/keys`；`/webhooks` 重定向到对应工作区规范入口。`/settings/billing` 替换历史记录并重定向到保留的 `/billing` 页面，让 Workbench 的 Buy credits 操作和已有链接继续使用原有主界面入口；组织设置不恢复账单导航或模拟配置。主界面原有 `/api-keys` 入口保留。真实后端接口、鉴权、资源页面和 Observability 保持原有行为。

## 验证

组件回归覆盖三项组织导航、个人设置上下文、原有首页内容、中英文展示、已下线页面无模拟操作、工作区导航分组及既有真实资源操作。浏览器验收覆盖首页、账号菜单、组织设置、密钥规范入口与历史 URL。侧栏 rail 和 Session 详情的行为见 [Session 会话 Viewer](sessions/session-conversation-workspace.md)。
