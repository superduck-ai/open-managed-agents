# Environment Package 安装

Cloud Environment 可以通过与 Claude 兼容的 `config.packages` 配置 `apt`、`cargo`、`gem`、`go`、`npm` 和 `pip` package。HTTP 边界负责校验 package 对象与 spec，将省略的列表规范化为 `[]`，并将编码后的 v1 manifest 限制在 1 MiB 以内。package 配置为空时，保持既有 Sandbox 启动与 runtime 发布流程不变。

对于非空 package 配置，Runner 创建 Sandbox、持久化 provider ID，然后执行：

```text
/usr/local/bin/environment-manager provision-packages --protocol v1 --stdin
```

JSON manifest 只通过 stdin 发送。命令结果必须是单个严格的 v1 JSON 值，且不得超过 16 KiB。仅当 status 与进程退出码一致时才继续启动；错误只暴露白名单内的 category、manager 和 stage，不包含 package spec 或原始 stdout/stderr。

Package 安装发生在 rclone 和 Environment Manager 启动之前。安装失败时，Runner 停止 Work 并终止已创建的 Sandbox。安装成功后，Runner 检查 Work heartbeat；如果并发 stop 已经生效，则执行清理，不再启动 rclone 或 Manager。没有 package 的 Environment 保持原有顺序：创建 Code Session、启动 Manager，然后发布 runtime metadata。

安装命令使用独立的 `environment_runner.package_provision_timeout`，默认 2 分钟。E2B 命令超时与本地 context deadline 使用同一预算：前者约束 Sandbox 进程，后者保证网络或 Wait 调用不会无限阻塞。

Sandbox 镜像或自定义 E2B template 必须提供 `/usr/local/bin/environment-manager`，并实现 `provision-packages` v1 合同。Registry credential、重试、派生 template、持久化清理协调以及事务化 Session event 交接均不在本功能范围内。

聚焦验证命令：

```text
go test ./internal/environments -run 'Test(BuildPackage|NormalizePackages|ValidatePackage|ProvisionPackages)' -count=1
go test ./internal/runtime/e2bruntime -count=1
```
