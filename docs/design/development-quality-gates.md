# 开发质量门禁

## 目标

仓库使用统一、可复现的本地提交门禁和 CI 门禁，使本地开发与 Pull Request CI 对格式、文件卫生和 Go 静态分析保持相同判断。

## Pre-commit 门禁

- `.pre-commit-config.yaml` 固定通用 hook 版本，并排除由上游流程生成的 quickstart 请求文件。
- 通用 hook 检查尾随空白、文件结尾、合并冲突标记、YAML/JSON 语法、私钥、大文件和混合换行符。
- 暂存 Go 文件先由 `gofmt` 格式化，再对其所属 package 执行仓库 `.golangci.yml` 规则；独立的 `unused` 门禁随后分析全部 Go package 和测试，阻止不可达的包级声明进入提交。
- 暂存前端代码、配置、样式和 Markdown 由 `web` 中固定版本的 Prettier 格式化，并遵循 `web/.prettierignore`。
- 官方 `check-added-large-files` hook 使用 `--enforce-all`，因此新增和修改的文件都执行统一的 1 MiB 上限。由服务嵌入的 `internal/platformapi/directory_servers.json` 保存为紧凑 JSON，避免为生成数据引入长期阈值例外。
- `just hooks-install` 为当前 Git clone 安装 hook，同一 clone 下的 worktree 共用该 hook。若机器尚未安装 `pre-commit`，`scripts/pre-commit.sh` 会通过 `uv` 安装固定版本 `4.6.0`；`just hooks-run` 可对全部跟踪文件复跑门禁。

## 配置与执行

- `.golangci.yml` 是常规 Go lint 规则来源；复杂度和死代码等需要不同扫描范围的专项门禁使用独立的固定配置。
- `just lint` 在本地对所有 Go package（包括测试）运行相同配置。
- `.golangci-dead-code.yml` 单独启用 golangci-lint 的 `unused` 分析器并覆盖测试代码；`just dead-code` 通过 `scripts/go-dead-code.sh` 枚举当前 Go module 的仓库 package，避免本地前端依赖中的第三方 Go 示例污染结果。
- `.github/workflows/lint.yml` 在 Pull Request 和 `main` 分支推送时运行固定版本的 `golangci-lint`。
- `.github/workflows/dead-code.yml` 在 Go 代码或死代码配置变化时运行固定版本的 `unused` 分析，pre-commit 使用相同脚本阻止不可达函数、方法、变量、常量和类型进入提交。
- `.github/workflows/large-files.yml` 在每个 Pull Request 和 `main` 分支推送时通过固定版本的 `uv` 和 `pre-commit` 对全部受跟踪文件运行同一个官方 hook，确保本地与 CI 共用一套实现和阈值。
- 门禁启用 `govet`、`ineffassign`，以及覆盖时间计算、日志参数、切片初始化、序列化标签、nil 错误、变量重赋值、数据库 rows/资源关闭和 tracing span 的专项分析器，同时用 `gofmt` 检查格式。

规则变更应先在本地通过 `just lint` 和 `just dead-code`，再提交配置与必要的代码修复；不要通过全局排除、`nolint`、伪造引用或跳过标记隐藏既有问题。

## 验收

```bash
just hooks-install
just hooks-run
just large-files
just lint
just dead-code
go test ./... -count=1
```
