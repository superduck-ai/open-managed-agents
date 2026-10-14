# 开发质量门禁

## 目标

仓库使用统一、可复现的本地提交门禁和 CI 门禁，使本地开发与 Pull Request CI 对格式、文件卫生和 Go 静态分析保持相同判断。

## Pre-commit 门禁

- `.pre-commit-config.yaml` 固定通用 hook 版本，并排除由上游流程生成的 quickstart 请求文件。
- 通用 hook 检查尾随空白、文件结尾、合并冲突标记、YAML/JSON 语法、私钥、大文件和混合换行符。
- 暂存 Go 文件先由 `gofmt` 格式化，再对其所属 package 执行仓库 `.golangci.yml` 规则；独立的 `unused` 门禁随后分析全部 Go package 和测试，阻止不可达的包级声明进入提交。jscpd 再对 Go 和 TypeScript/TSX 生产代码执行独立的复制代码预算。
- 暂存前端代码、配置、样式和 Markdown 由 `web` 中固定版本的 Prettier 格式化，并遵循 `web/.prettierignore`。
- 官方 `check-added-large-files` hook 使用 `--enforce-all`，因此新增和修改的文件都执行统一的 1 MiB 上限。由服务嵌入的 `internal/platformapi/directory_servers.json` 保存为紧凑 JSON，避免为生成数据引入长期阈值例外。
- `just hooks-install` 为当前 Git clone 安装 hook，同一 clone 下的 worktree 共用该 hook。若机器尚未安装 `pre-commit`，`scripts/pre-commit.sh` 会通过 `uv` 安装固定版本 `4.6.0`；`just hooks-run` 可对全部跟踪文件复跑门禁。

## 配置与执行

- yourbatis 根据 `internal/db/*.xml` 生成的 `*.sqlmap.gen.go` 不提交；`scripts/generate-go.sh` 是统一生成入口。无参数调用会先删除 `internal/db` 下已有的 `*.sqlmap.gen.go`，再执行 `go generate ./internal/db`，避免已删除 Mapper 留下的 ignored 生成文件继续参与编译。Go lint、死代码、复杂度、测试、CI 和 Docker 构建继续使用该全量入口。
- 开发重启使用 `scripts/generate-go.sh --cached`，由重启脚本调用一次；`just server` 和 `just restart-server` 不再另设生成前置步骤。检查通过后才停止旧服务，因此生成失败时旧服务继续运行。其他 Go 业务代码的编译仍由 `go run .` 完成。
- `cmd/generate-go` 只依赖 Go 标准库。缓存输入包含 DB 包非测试、非 Mapper 生成的 Go 源码和 XML、DB 与生成器的本地传递依赖源码、模块文件、workspace 配置、生成入口源码及稳定的 Go 构建环境。通过 `go list -deps -e -json` 发现依赖；无法解析时全量生成且不保存缓存。摘要包含路径和内容，能识别新增、删除和保留原修改时间的修改。
- 缓存同时保存 `internal/db` 下全部 `*.sqlmap.gen.go` 的路径与内容摘要。产物缺失、修改或出现多余文件时缓存失效，先清空全部旧产物再全量生成。缓存保存在当前 worktree 的 ignored 路径 `tmp/go-generation/cache.json`，不跨 worktree 共享。成功的全量生成也可以建立缓存。
- 两种模式均通过操作系统文件锁串行执行；生成子进程继承锁，即使父进程被强制终止，也不会在旧生成进程退出前允许另一个任务生成。进程退出后锁自动释放。开始重建前删除旧缓存；仅生成成功且前后输入摘要一致时原子保存新缓存。收到中断或终止信号时停止生成子进程组。生成失败、中断、依赖解析失败或生成期间输入变化都不会留下有效缓存。使用 `GOFLAGS` 指定 `-overlay` 或 `-modfile` 时保守地禁用缓存，继续全量生成。
- `.golangci.yml` 是常规 Go lint 规则来源；复杂度和死代码等需要不同扫描范围的专项门禁使用独立的固定配置。
- `just lint` 在本地对所有 Go package（包括测试）运行相同配置。
- `.golangci-dead-code.yml` 单独启用 golangci-lint 的 `unused` 分析器并覆盖测试代码；`just dead-code` 通过 `scripts/go-dead-code.sh` 枚举当前 Go module 的仓库 package，避免本地前端依赖中的第三方 Go 示例污染结果。
- `.github/workflows/lint.yml` 在 Pull Request 和 `main` 分支推送时运行固定版本的 `golangci-lint`。
- `.github/workflows/dead-code.yml` 在 Go 代码或死代码配置变化时运行固定版本的 `unused` 分析，pre-commit 使用相同脚本阻止不可达函数、方法、变量、常量和类型进入提交。
- `.jscpd.json` 与 `web/.jscpd.json` 固定 strict token 检测规则：复制块至少 12 行且至少 70 token；Go 生产代码上限为 3.75%，TypeScript/TSX 生产代码上限为 1.1%。两个应用分别执行，避免合并百分比掩盖单侧增长；测试、suite 和生成文件不计入生产预算。
- `.github/workflows/duplicate-code.yml` 和 pre-commit 都调用 `scripts/check-duplicates.sh`。本地通过 `just duplicates` 运行同一门禁，超限时输出 clone 文件与行号供重构定位。
- `.github/workflows/large-files.yml` 在每个 Pull Request 和 `main` 分支推送时通过固定版本的 `uv` 和 `pre-commit` 对全部受跟踪文件运行同一个官方 hook，确保本地与 CI 共用一套实现和阈值。
- 门禁启用 `govet`、`ineffassign`，以及覆盖时间计算、日志参数、切片初始化、序列化标签、nil 错误、变量重赋值、数据库 rows/资源关闭和 tracing span 的专项分析器，同时用 `gofmt` 检查格式。

规则变更应先在本地通过 `just lint`、`just dead-code` 和 `just duplicates`，再提交配置与必要的代码修复；不要通过全局排除、提高重复率预算、`nolint`、伪造引用或跳过标记隐藏既有问题。

## 验收

生成缓存的验收使用 `go test ./cmd/generate-go -count=1`，覆盖失败、中断、并发、输入和产物变化、删除 Mapper、强制重建、无关代码修改及重启脚本。两个 Just 入口另用 `just --dry-run server` 和 `just --dry-run restart-server` 确认只调用一次重启脚本；Go 单测不依赖 Just。DB 实际生成后执行 `go test ./internal/db -count=1`，确认生成代码可编译且 Mapper 测试通过。

```bash
just hooks-install
just hooks-run
just large-files
just lint
just dead-code
just duplicates
just test
```
