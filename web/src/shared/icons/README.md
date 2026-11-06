# 产品图标

`product-icons.tsx` 统一映射仓库已有的 `lucide-react` 组件，不保存或修改图标路径，不新增依赖。

统一使用 Lucide 的 24×24 viewBox、圆形端点与连接、`currentColor`，产品图标描边为 1.5px。侧栏主项使用 18px，资源子项使用 16px，折叠图标居中；模块空状态沿用既有尺寸与相同描边。同一模块的侧栏和页面共用映射。文件夹与文件内容、模块图标与具体操作保持语义区别。

| 入口                             | Lucide 图标                                           |
| -------------------------------- | ----------------------------------------------------- |
| 快速开始 / 智能体 / 会话         | Rocket / Bot / MessagesSquare                         |
| 概览 / 部署 / 环境               | LayoutDashboard / Package / Server                    |
| 资源 / 工作区                    | Boxes / Building2                                     |
| 技能 / 文件 / 记忆 / 凭据保险库  | BookOpenCheck / Folder / Brain / Vault                |
| MCP 服务器 / MCP 隧道 / Webhooks | Plug / Network / Webhook                              |
| 模型 / 可观测 / 用量 / 成本      | Cpu / Activity / ChartNoAxesColumnIncreasing / Wallet |
| 成员 / API 密钥 / 管理 / 文档    | Users / KeyRound / Settings / BookOpen                |

Lucide 使用 ISC 许可，其中源自 Feather 的图标保留 MIT 许可。安装版本的完整版权、许可及免责声明保存在 [public/licenses/lucide.txt](../../../public/licenses/lucide.txt)，Vite 构建后原样分发为 `/licenses/lucide.txt`。更新依赖时同步该文件。

图标由控件或文字提供可访问名称，装饰 SVG 使用 `aria-hidden`。
