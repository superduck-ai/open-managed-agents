# 开发质量门禁

## 目标

Go 后端使用统一、可复现的静态分析门禁，使本地开发与 Pull Request CI 对可疑代码、无效赋值、资源生命周期、日志参数、序列化约束和格式保持相同判断。

## 配置与执行

- `.golangci.yml` 是唯一的 Go lint 规则来源。
- `just lint` 在本地对所有 Go package（包括测试）运行相同配置。
- `.github/workflows/lint.yml` 在 Pull Request 和 `main` 分支推送时运行固定版本的 `golangci-lint`。
- 门禁启用 `govet`、`ineffassign`，以及覆盖时间计算、日志参数、切片初始化、序列化标签、nil 错误、变量重赋值、数据库 rows/资源关闭和 tracing span 的专项分析器，同时用 `gofmt` 检查格式。

规则变更应先在本地通过 `just lint`，再提交配置与必要的代码修复；不要通过全局排除或跳过标记隐藏既有问题。

## 验收

```bash
just lint
go test ./... -count=1
```
