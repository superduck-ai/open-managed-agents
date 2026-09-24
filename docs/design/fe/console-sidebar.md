# Console 侧边栏导航

控制台侧边栏只决定入口是否展示，不删除页面、路由或 API。直接打开既有路径仍然进入对应页面。

## 默认展开

进入控制台时，只有 `Managed Agents` 分组展开。`Build`、`Analytics`、`Manage` 以及其他分组默认折叠。Dashboard、LLM models、API keys 这类顶层链接保持可见。

分组的展开状态保存在当前侧边栏实例中。用户手动展开或折叠后，切换路由不会重置该状态。侧边栏 rail 的整体收起另见 [Session 会话 Viewer](sessions/session-conversation-workspace.md)：进入 Session 详情时保持用户原来的 rail 展开状态。

```mermaid
flowchart TD
  Sidebar[Console sidebar] --> Top[Dashboard / LLM models / API keys]
  Sidebar --> Build[Build collapsed]
  Sidebar --> Agents[Managed Agents expanded]
  Sidebar --> Analytics[Analytics collapsed]
  Sidebar --> Manage[Manage collapsed]
  Build --> Files[Files]
  Build --> Skills[Skills]
```

## 暂时隐藏

以下入口暂时不渲染。导航定义仍留在 `consoleNavigation`，并标为 `hidden`，便于以后恢复：

- `Build` 中的 Workbench（`/workbench`）和 Batches（`/batches`）
- 整个 Claude Code 分组（`/claude-code/usage`、`/claude-code/settings`）

展开 `Build` 后仍显示 Files 和 Skills，不显示 Workbench 和 Batches。Claude Code 分组标题也不出现。产品文案表中的对应词条保持不变。
