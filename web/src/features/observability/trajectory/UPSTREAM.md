# Harness 原生 UI 来源

运行时代码直接引用 npm 发布包 `@deepseek-ai/dsh-client-ui-conversation` 和 `@deepseek-ai/dsh-client-ui-trajectory`，固定为 `0.2.1-alpha.2`，与 Brain 事件版本一致。`web/harness-plugin.ts` 仅将上游客户端 loader factory 转为静态 ESM 导入，不修改其事件投影或组件代码。

`native-theme.css` 来自 DeepSeek Harness 的 `packages/client/ui-theme/src/client/design-platform.css` 和 `base.css`，删除代码注释，并将 dark/platform selector 对齐 OMA。原 MIT 授权保留在 LICENSE.deepseek。

`projection.ts` 提供上游注册 API 所需的 host；`NativeTrajectory.tsx` 绑定 OMA 分页、语言、状态订阅和原组件 props。图片 slot 及本机附件读取未接入。更新包时必须同时核对 native-types.ts 的 host 合同、Session format version 与主题文件，并运行构建和原生浏览器 fixture。
