# Harness 原生 UI 来源

运行时代码直接引用 npm 发布包 `@deepseek-ai/dsh-client-ui-conversation` 和 `@deepseek-ai/dsh-client-ui-trajectory`，固定为 `0.2.2-alpha.2`，与 Brain 事件版本一致。`web/harness-plugin.ts` 仅将上游客户端 loader factory 转为静态 ESM 导入，不修改其事件投影或组件代码。

`native-theme.css` 来自 DeepSeek Harness 的 `packages/client/ui-theme/src/styles/design-platform.css` 和 `base.css`，删除代码注释，并将 dark/platform selector 对齐 OMA。原 MIT 授权保留在 LICENSE.deepseek。

`projection.ts` 提供上游注册 API 所需的 host；`NativeTrajectory.tsx` 绑定 OMA 分页、语言、状态订阅和原组件 props。图片 slot 及本机附件读取未接入。更新包时必须同时核对 native-types.ts 的 host 合同、Session format version 与主题文件，并运行构建和原生浏览器 fixture。

当前上游文档与源码基准为 [`dsh-v0.2.2-alpha.2` 对应提交](https://github.com/deepseek-ai/deepseek-harness/tree/f3803c96974ad4a94195fb97e5c637e9c2a3afe3)。本次核对的 Session format 仍为 V4，现有 assembler、轨迹 view props 与 host 注册方式保持兼容；`base.css` 与 `0.2.1-rc.4` 相同，`design-platform.css` 同步本次发布的完成状态色与插图令牌，并继续映射 OMA 的 dark/platform selector。浏览器预览入口为 `/tests/trajectory.html`，fixture 通过 `target.sessionId` 选择轨迹并拦截请求，无需真实后端数据。
