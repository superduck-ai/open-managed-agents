# 前端格式化门禁

## 目标

前端使用固定版本的 Prettier 统一 TypeScript、TSX、JavaScript、JSON、CSS 和 Markdown 格式，使本地修改与 Pull Request CI 使用同一套确定性规则，减少无关样式差异和人工格式争议。

## 配置与命令

- `web/.prettierrc.json` 是前端格式规则的唯一来源，显式定义分号、引号、尾逗号、行宽、缩进和换行符。
- `web/.prettierignore` 排除依赖、构建/覆盖率产物、Bun lockfile，以及由上游流程生成的 quickstart 请求文件。
- `bun run format` 写入统一格式；`bun run format:check` 只检查，不修改文件。
- 仓库根目录提供 `just web-format` 和 `just web-format-check` 作为等价入口。
- `.github/workflows/web-format.yml` 对涉及 `web/` 的 Pull Request 和 `main` 分支推送执行冻结依赖安装与格式检查。

首次引入 Prettier 时对所有受管前端文件建立格式化基线；之后的变更必须保持 `bun run format:check` 通过。生成文件的格式由生成器负责，不应通过手工格式化产生漂移。

## 验收

```bash
cd web
bun run format:check
bun test
bun run build
```
