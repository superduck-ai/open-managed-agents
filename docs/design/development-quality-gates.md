# 开发质量门禁

## 目标

仓库使用统一、可复现的本地提交门禁和 CI 门禁，使本地开发与 Pull Request CI 对格式、文件卫生和 Go 静态分析保持相同判断。

## Pre-commit 门禁

- `.pre-commit-config.yaml` 固定通用 hook 版本，并排除由上游流程生成的 quickstart 请求文件。
- 通用 hook 检查尾随空白、文件结尾、合并冲突标记、YAML/JSON 语法、私钥、大文件和混合换行符。
- 暂存 Go 文件先由 `gofmt` 格式化，再对其所属 package 执行仓库 `.golangci.yml` 规则；只检查受影响 package，避免每次提交都扫描整个后端。
- 暂存前端代码、配置、样式和 Markdown 由 `web` 中固定版本的 Prettier 格式化，并遵循 `web/.prettierignore`。
- 本地 hook 调用 `scripts/check-large-files.sh` 扫描 Git index 中的全部受跟踪 blob，而不是只看工作区文件。普通文件上限为 1 MiB；已存在且由服务嵌入的 `internal/platformapi/directory_servers.json` 采用 1.25 MiB 的独立增长预算，避免把历史例外扩散为全仓宽松阈值。
- `just hooks-install` 为当前 Git clone 安装 hook，同一 clone 下的 worktree 共用该 hook。若机器尚未安装 `pre-commit`，`scripts/pre-commit.sh` 会通过 `uv` 安装固定版本 `4.6.0`；`just hooks-run` 可对全部跟踪文件复跑门禁。

## 配置与执行

- `.golangci.yml` 是唯一的 Go lint 规则来源。
- `just lint` 在本地对所有 Go package（包括测试）运行相同配置。
- `.github/workflows/lint.yml` 在 Pull Request 和 `main` 分支推送时运行固定版本的 `golangci-lint`。
- `.github/workflows/large-files.yml` 在每个 Pull Request 和 `main` 分支推送时先验证检查器的失败与边界场景，再检查仓库完整 index，因此历史文件增长也无法绕过只检查新增文件的 hook。
- 门禁启用 `govet`、`ineffassign`，以及覆盖时间计算、日志参数、切片初始化、序列化标签、nil 错误、变量重赋值、数据库 rows/资源关闭和 tracing span 的专项分析器，同时用 `gofmt` 检查格式。

规则变更应先在本地通过 `just lint`，再提交配置与必要的代码修复；不要通过全局排除或跳过标记隐藏既有问题。

## 验收

```bash
just hooks-install
just hooks-run
just test-large-files
just large-files
just lint
go test ./... -count=1
```
