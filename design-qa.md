# Session 文件资源表单 Design QA

## 对照基线

- Source visual truth: `/var/folders/v6/p_dtg3v11rx5t2b_f3vqgdbr0000gn/T/codex-clipboard-cd902e5d-4dc7-4fc3-b40d-56beed064faf.png`
- Browser-rendered implementation: `/tmp/oma-session-mount-path-reference.png`
- Focused implementation region: `/tmp/oma-session-mount-path-card.png`
- Combined comparison: `/tmp/oma-session-mount-path-comparison.png`
- Browser viewport: `1864 × 943` CSS px；浏览器 `devicePixelRatio=2`，Browser 截图按 `1864 × 943` 像素输出
- Source pixels: `1330 × 562`
- Focused implementation pixels: `510 × 236`
- Comparison pixels: `1069 × 236`
- State: 中文、深色主题、Create Session 对话框、已选择 `test.png`、可选挂载路径留空

源图和实现的主题、承载画布不同，因此对照时裁切到 File Resource 卡片，并把源图与实现统一缩放到 `236px` 高度后横向拼接。颜色差异按现有产品深色主题约束处理，布局、字段语义和交互状态按参考图检查。

## 全视图与聚焦区域证据

- 全视图：资源卡片位于 Create Session 的现有滚动表单内，没有遮挡页脚操作或溢出对话框。
- 聚焦区域：File ID 选择器、`挂载路径（可选）` 标签、固定 `/mnt/session/uploads/` 前缀、文件名占位符和目录说明与参考结构一致。
- 目标区域没有位图、插画或品牌资产；聚焦区域足以检查本次全部视觉变化，无需额外资产局部截图。

## 必检表面

- Fonts and typography: 沿用项目 Geist 与现有 shadcn 字号、字重和行高；标签、前缀、占位符、帮助文案层级清晰。
- Spacing and layout rhythm: 标签、输入组和帮助文案保持现有紧凑 `new-york` 节奏；固定前缀与可编辑文本在同一边框内，未重叠或裁切。
- Colors and visual tokens: 实现使用现有 `input`、`muted-foreground`、`ring` 等深色语义令牌；源图浅色主题只作为结构参考，不覆盖产品主题。
- Image quality and asset fidelity: 目标区域无图片资产；文件与删除图标继续使用项目既有 Lucide 图标，没有 CSS 或文本伪造资产。
- Copy and content: 中文使用“挂载路径（可选）”和“文件将挂载到容器的 /mnt/session/uploads/ 目录下。”；选择文件后占位符使用真实文件名。

## 交互与控制台

- 已验证：选择 `test.png` 后，挂载路径占位符显示 `test.png`，固定前缀保持 `/mnt/session/uploads/`。
- 已验证：挂载路径没有 `required`，留空时 Create Session 可用；输入 `../secret.txt` 时标记无效并禁用创建；清空后恢复可用。
- 已验证：留空提交的自动化测试不会发送 `mount_path`，由服务端采用文件名默认值。
- 页面自身没有 `localhost:5174` 来源的 error/warn；日志中的 error 均来自第三方 Chrome 扩展脚本，不属于应用。

## Findings

没有可执行的 P0/P1/P2 差异。实现保留深色主题和更紧凑的产品密度，这是现有设计系统约束，不影响参考图要求的字段结构与交互语义。

## Comparison History

- 初始实现把 Mount path 作为必填的普通输入框，并要求用户理解相对路径，属于 P1 语义与布局差异。
- 修复后改为完整 InputGroup：固定展示 Sandbox 前缀，路径可选，留空采用服务端文件名默认值。
- 最终聚焦对照确认标签、前缀、文件名占位符和帮助文案已一致；无剩余 P0/P1/P2。

## Implementation Checklist

- [x] 可选挂载路径标签
- [x] 固定 `/mnt/session/uploads/` 前缀
- [x] 选择文件后展示文件名占位符
- [x] 留空时省略 `mount_path`
- [x] 自定义相对路径校验
- [x] 深色主题与对话框滚动布局检查
- [x] 应用控制台错误检查

## Follow-up Polish

无必须跟进的 P3 项。

final result: passed
