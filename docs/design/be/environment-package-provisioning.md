# Environment Packages Provisioning

> 状态：PR 实施设计。本文只定义 Packages 最小可交付合同；通用 Session lifecycle、
> durable cleanup、基于 Packages 构建派生 Template 的系统和重试系统不在本 PR 范围内。

## 1. 目标与非目标

Cloud Environment 保存并回显 Claude-compatible `config.packages`，支持六个 Package
Manager：`apt`、`cargo`、`gem`、`go`、`npm`、`pip`。每个新 Session Sandbox 在 Agent
启动前应用 Packages；空 Packages 继续走原有启动流程。

```json
{
  "type": "packages",
  "apt": ["ffmpeg"],
  "cargo": ["ripgrep@14.1.1"],
  "gem": ["rake:13.2.1"],
  "go": ["golang.org/x/tools/cmd/goimports@v0.35.0"],
  "npm": ["typescript@5.9.3"],
  "pip": ["numpy==2.3.5"]
}
```

本 PR 不新增统一版本 DSL、registry credential、init script、Build/Artifact API、Packages
Template Worker、通用 launch generation、durable cleanup/reconciler 或 provider Kill 重试队列。

职责保持简单：

- OMA Environment API 负责 schema、规范化、校验、持久化与回显。
- OMA Runner 负责 Sandbox 生命周期、manifest 传输、成功门禁和失败清理。
- Environment Manager 的 `provision-packages` 子命令负责实际安装顺序和 argv。
- `managed-agent-sandbox` 镜像负责提供固定 Manager binary、runtime 和 Package Manager。

## 2. Manifest 合同

OMA 通过 stdin 发送一个不超过 1 MiB 的 v1 JSON object：

```json
{
  "version": 1,
  "packages": {
    "type": "packages",
    "apt": [],
    "cargo": [],
    "gem": [],
    "go": [],
    "npm": [],
    "pip": []
  }
}
```

调用命令固定为：

```text
/usr/local/bin/environment-manager provision-packages --protocol v1 --stdin
```

package spec 只存在于 JSON stdin，不拼进 shell command。OMA 在 HTTP 边界规范化缺失数组为
`[]`，并在从持久化 Environment 构造 manifest 时再次执行语义和 1 MiB 大小校验，防止直接
写库或历史脏数据绕过 API。

输入校验拒绝：

- 非 object、错误 `type`、非字符串数组或空白 spec；
- trim 后以 `-` 开头的 Package Manager option；
- URL authority 中包含 userinfo/credential 的 spec；
- 超过 1 MiB 的编码后 manifest。

单条 spec 不增加 OMA 私有长度限制；PEP 508 direct URL 等原生格式可以超过 255 字符，
总量统一由 1 MiB manifest 上限约束。

错误只报告 manager 和违反的规则，不回显原始 spec、URL credential、stdin 或 Package
Manager 输出。

## 3. 安装与结果合同

Environment Manager 按以下固定顺序执行：

```text
apt → cargo → gem → go → npm → pip
```

空 manager 跳过；apt 非空时先执行 `apt-get update`；Go spec 逐个安装，其余 manager 批量
安装。执行首错停止，不并行、不重试、不逐包回滚。任一步失败后 OMA 丢弃整个 Sandbox。

stdout 只允许一个结构化 v1 结果。成功至少包含：

```json
{"version":1,"status":"succeeded","package_count":6,"duration_ms":12450}
```

失败可包含有界的 `category`、`manager`、`stage`、`package_count`、`duration_ms` 和
`exit_code`，不得包含 spec 或子进程输出。OMA 在解码前将完整 stdout 限制为 16 KiB，并仅回显
协议已知的 category（`invalid_manifest` / `package_manager` / `execution_environment` /
`timeout` / `cancelled` / `internal`）、manager
（`apt` / `cargo` / `gem` / `go` / `npm` / `pip`）和 stage
（`decode` / `validate` / `preflight` / `start` / `prepare` / `update` / `install` /
`finalize`）；未知文本统一降级为 `unknown`，避免把错误的 Manager 输出持久化到 Sandbox
`last_error`。OMA 还要求结果可严格解码、版本正确、必需字段合法，并要求 `succeeded` 对应进程
退出码 0、`failed` 对应非零退出码。任何不一致都按协议失败处理。

## 4. Runner 顺序与启动快照

```text
Claim/Ack Work
→ 读取 Environment 和 Managed Agent 启动快照
→ Resolve 并创建 Sandbox
→ 持久化 provider_sandbox_id
→ Provision Packages（非空时）
→ Heartbeat 并检查 Work lease
→ 启动 rclone 并等待 ready
→ 标记 Sandbox running
→ 原子提交 Code Session runtime
→ 启动 Environment Manager task-run
```

Sandbox 创建前固化的启动快照包含 model、prompt、skills、resources、MCP 创建时网络策略和
其他 startup config。Packages 安装期间对这些配置的更新不影响当前启动尝试，只影响下一次
Sandbox 创建。本 PR 不在长安装后重新构造整套 preparation。

Packages 安装后 heartbeat 用于确认 Work 仍有效。lease 丢失时不创建 Code Session、不启动
Manager，并 best-effort Kill 已创建 Sandbox。完整的 archive/update launch fence 留给后续独立
lifecycle 设计。

## 5. Session Event 原子交接

Packages 安装可能持续数分钟，但期间接受的新 Session 消息不能丢失。Runner 因此不在安装前
固化 event 列表，而是在 `CreateManagedAgentRuntime` 的单一 sqlx 事务中完成最终交接：

1. 锁定 idle、未归档的 Session；
2. 在锁内读取最终 Session event 快照；
3. 按固定 `Session → Environment Work` 顺序锁定 active Work；
4. 交叉验证 Session、Work、Environment 和 Code Session 输入的 organization、workspace、
   session 与 environment 标识；
5. 创建 Code Session，写 initialize 和 initial inbound events，推进连续 sequence；
6. 更新 Session/Work runtime metadata；
7. 读取 credential context，并在提交前签发 JWT；
8. 同事务提交。

锁前已提交的 event 进入 initial inbound；锁后写入的 event 等事务提交后走实时转发。inbound
使用现有 idempotency key 去重，冲突项不消耗 sequence。任何 DB 写入、身份校验或凭证签发
失败都会回滚，不暴露部分 runtime。

## 6. 网络与 Template 前提

本 PR 不修改 Limited Environment 的 registry/mirror catalog，也不新增 Packages 网络策略。
Provisioning 发生在 Code Session proxy 启动前，因此只能沿用 Sandbox 创建时已经存在的网络
快照；本 PR 的真实安装验收使用 unrestricted networking。Limited-network Packages 的 host、
redirect、VCS 和 private registry 策略仍明确留在范围外。

全局 provider-neutral 默认值是：

```text
managed-agent-sandbox
```

Compose/e2b-local 显式使用：

```text
managed-agent-sandbox:latest
```

显式短 `name:tag` 用于避免本地多个 RepoTag 时裸名称解析歧义。已有
`deploy/docker-compose/oma-server.local.yaml` 不会被 `just init-compose-config` 覆盖，升级用户
必须手动设置该值。自定义 Template 继续原样透传，但必须包含兼容的
`provision-packages --protocol v1 --stdin`。

迁移 `00035_migrate_packaged_environment_template.sql` 只把仍使用旧默认
`claude-code-interpreter`、类型为 cloud、且至少一个 manager 数组非空的 Environment 改为
provider-neutral 的逻辑默认 `managed-agent-sandbox`。Resolve 遇到该逻辑默认时会通过当前
部署的 `e2b.template` 物化：Compose/e2b-local 得到显式 `managed-agent-sandbox:latest`，
hosted 部署可得到自己的 team/tag。空 Packages、自定义 Template 和已使用新 Template 的记录
不变。

## 7. 失败行为

以下情况均不得启动 Environment Manager：manifest 非法、stdin transport 失败、timeout/
cancel、Manager 非零退出、结构化结果非法、Work lease 丢失、rclone 失败或 runtime 事务失败。

失败路径：

- 启动失败时，provider Kill 成功后 Sandbox 记录标记 `failed`；heartbeat 发现 Work 已由用户
  主动置为 `stopping`/`stopped` 时，Kill 成功后标记 `stopped`；
- provider Kill 失败时记录保持可发现的 `stopping` 并保存错误，允许后续手动 force-stop 重试；
- Environment Work 停止；
- provider Sandbox 使用独立有界 context best-effort Kill；
- 已创建 runtime 但 Manager 启动失败时，执行现有一次性补偿：终止 Code Session、撤销凭证并
  清除仍指向该 Code Session 的 Session/Work metadata。

本 PR 不承诺 provider Kill 或 Code Session termination 的最终一致重试；durable cleanup 是后续
独立工作。

## 8. 测试矩阵

| 层次 | 必须覆盖 |
|---|---|
| API/schema | create/update/get/list round-trip；空数组规范化；非法 type/数组/spec |
| manifest | 空 Packages 跳过；特殊字符原样 stdin；stored config 重校验；1 MiB 上限 |
| result | 缺失/未知字段；status 与进程退出码不一致；16 KiB 上限；未知诊断降级；错误不泄漏 stdout/stderr |
| Runner | 失败、stop/cancel 不启动 Manager；取消后清理仍使用独立 context；Kill 失败仍可 force-stop 重试；成功顺序；安装期间 event 不丢 |
| Runtime DB | Session→Work 锁序；身份 mismatch 回滚；事件顺序/幂等；凭证失败回滚 |
| Migration | 仅迁移旧默认且非空 Packages 的 cloud Environment |
| E2E | 六类 manager 真实安装可用；Claude Agent 实际调用已安装的 `rg`，并在本次 proof prompt 后回到 idle；Environment 更新只影响新 Sandbox；双 Session 文件系统隔离 |

本 PR 的窄范围验收命令：

```text
go test ./internal/environments ./internal/codesessions -count=1
go test ./internal/db -run 'ManagedAgentRuntime|CodeSession' -count=1
go test ./internal/networkpolicy ./internal/runtime/e2bruntime -count=1
go test ./tests -tags='e2b_integration e2e' -run '^$'
```

真实 E2B 测试仍需显式凭证和本地 Template，不作为普通单测默认执行。

## 9. 明确后续项

- Session `launching` 状态或 launch generation，解决 archive/update 与 Manager handoff 的完整竞态；
- durable Sandbox cleanup、provider Kill/Code Session termination 重试；
- e2b Go SDK 的 Wait/Disconnect 完成通知修复；
- 基于 Packages 构建派生 Template、Build lease/retry/readiness 与发布控制面。
