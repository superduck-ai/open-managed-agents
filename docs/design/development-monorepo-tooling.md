# Monorepo 工具链

## 目标

仓库由三个可独立理解和执行的项目组成：根目录的 Go API、`web/` 中的浏览器控制台，以及 `tests/js-test/` 中的 JavaScript SDK 测试工具。moon 为这些异构项目提供统一项目图、任务入口、输入/输出声明和增量缓存，同时保留 Go module 与两个 Bun package 各自已有的依赖边界。

## 项目边界

| 项目             | 路径             | 语言/层级                       | 主要任务                                                                  |
| ---------------- | ---------------- | ------------------------------- | ------------------------------------------------------------------------- |
| `backend`        | `/`              | Go backend application          | `build`、`test`、`lint`                                                   |
| `web`            | `web/`           | TypeScript frontend application | `build`、`test`、`lint`、`lint-naming`、`lint-complexity`、`format-check` |
| `agent-sdk-test` | `tests/js-test/` | TypeScript backend automation   | `typecheck`                                                               |

`agent-sdk-test` 以 development scope 依赖 `backend`，表达它验证后端 API 的关系；应用项目之间没有伪造编译依赖。workspace 启用 layer relationship 校验，使后续新增依赖必须符合 automation、application、tool、library 等层级方向。

## 工具链与执行

- `.moon/toolchains.yml` 固定 Go 1.26.2 与 Bun 1.3.11，并为 JavaScript 项目选择 Bun package manager。
- `@moonrepo/cli` 固定在 `web/bun.lock`；`scripts/moon.sh` 始终调用仓库内版本，避免开发机 PATH 中同名命令造成结果漂移。
- 每个 task 只声明与自身项目相关的 inputs。Go 根项目不会把嵌套的前端文件当作后端输入；`scripts/go-lint.sh` 从 `go list` 结果中排除 `web/node_modules` 的第三方 Go 示例。前端 build 的 `dist/` 与 Go build 的 `bin/open-managed-agents` 声明为可恢复输出。
- `just workspace-build`、`just workspace-test` 与 `just workspace-check` 是开发者的跨项目入口。`workspace-check` 聚合仓库当前已强制执行的 Go lint、前端命名/复杂度/格式检查与 SDK typecheck；完整前端 ESLint 仍可通过 `web:lint` 单独运行，在既有错误基线清零前不伪装成全绿门禁。单项目调试仍可使用现有 Go、Bun 与 Just 命令。

## 验证与 CI

`scripts/check-monorepo.sh` 解析项目图，并确认三个项目和十个关键 task 均能由 moon 加载。pre-commit 在 workspace 配置或固定 CLI 发生变化时运行该验证。

`.github/workflows/monorepo.yml` 使用仓库固定的 Go、Bun 和 moon 版本，在干净 runner 中：

1. 安装两个 Bun package 的冻结依赖；
2. 验证项目图与 task 定义；
3. 通过同一个 moon action graph 构建 Go API 与 Web 控制台；
4. typecheck JavaScript SDK 测试工具。

CI checkout 保留完整 Git 历史，使 moon 能解析 `main` 与当前提交的 merge base；官方 setup actions 提供已经固定版本的 Go 与 Bun，并通过 `MOON_TOOLCHAIN_FORCE_GLOBALS=true` 复用这些二进制。开发机未设置该变量时，moon 可按 `.moon/toolchains.yml` 准备匹配版本。
