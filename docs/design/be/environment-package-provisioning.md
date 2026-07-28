# Environment Packages Provisioning

> 状态：MVP 实施合同。本文只定义 Packages 的 API、provisioning、Runner handoff 与验收边界。

## 1. 目标与非目标

Cloud Environment 保存并回显 Claude-compatible `config.packages`，支持 `apt`、`cargo`、
`gem`、`go`、`npm`、`pip`。每个新 Session Sandbox 在 Agent 启动前安装 Packages；空
Packages 沿用原启动流程。

本 MVP 不定义统一版本 DSL、registry credential、init script、Build/Artifact API、派生
Template、通用 launch generation、durable cleanup/reconciler 或 provider Kill 重试队列。

## 2. v1 Manifest 与校验

OMA 通过 stdin 发送以下 JSON object；字段与数组形状固定：

```json
{
  "version": 1,
  "packages": {
    "type": "packages",
    "apt": ["ffmpeg"],
    "cargo": ["ripgrep@14.1.1"],
    "gem": ["rake:13.2.1"],
    "go": ["golang.org/x/tools/cmd/goimports@v0.35.0"],
    "npm": ["typescript@5.9.3"],
    "pip": ["numpy==2.3.5"]
  }
}
```

命令固定为：

```text
/usr/local/bin/environment-manager provision-packages --protocol v1 --stdin
```

spec 只进入 JSON stdin，不拼入 shell command。HTTP 边界把缺失数组规范化为 `[]`，拒绝
非 object、错误 `type`、非字符串数组、空白 spec、trim 后以 `-` 开头的 option，以及 URL
authority 含 userinfo/credential 的 spec。错误只说明 manager 和规则，不回显输入。

编码后的完整 manifest 上限为 1 MiB；不另设单条 spec 限制。该上限在 HTTP 规范化时检查，
也在从持久化 Environment 构造 manifest 时连同语义重新检查，防止历史或直接写库数据绕过。

## 3. 安装与结果门禁

Environment Manager 固定按 `apt → cargo → gem → go → npm → pip` 执行。空 manager 跳过；
apt 非空时先 `apt-get update`；Go 逐项安装，其余 manager 批量安装。首错停止，不并行、不重试、
不逐包回滚；失败后 OMA 丢弃整个 Sandbox。

stdout 只允许一个严格解码的 v1 JSON 结果，例如：

```json
{"version":1,"status":"succeeded","package_count":6,"duration_ms":12450}
```

OMA 在解码前对完整 stdout 应用 16 KiB 上限，并拒绝未知字段、多 JSON 值、错误版本、缺失或
非法必需字段，以及 status/进程退出码不一致（`succeeded` 必须为 0，`failed` 必须非 0）。
成功结果不得携带失败字段。失败诊断只允许协议已知的 category、manager 和 stage；未知值降级
为 `unknown`，原始 stdout/stderr、spec 与子进程输出不得进入 `last_error`。

## 4. Runner 顺序与启动快照

Runner 是线性序列：

```text
Claim/Ack Work
→ 读取 Environment 与 Managed Agent 启动快照
→ Resolve 并创建 Sandbox
→ 持久化 provider_sandbox_id
→ Provision Packages（非空时）
→ Heartbeat 并检查 Work lease
→ 启动 rclone 并等待 ready
→ 标记 Sandbox running
→ 原子提交 Code Session runtime
→ 启动 Environment Manager task-run
```

Sandbox 创建前的快照固定 model、prompt、skills、resources、MCP 创建时网络策略及其他 startup
config。安装期间发生的配置更新不改变本次启动，只影响下一次 Sandbox 创建。安装后的 heartbeat
若发现 lease 已丢失，则不创建 Code Session、不启动 Manager，并清理已创建 Sandbox。

## 5. Session Event 原子交接

安装前不固化 event 列表。`CreateManagedAgentRuntime` 在单一 `sqlx.Tx` 中：先锁定 idle、未归档
Session 并读取最终 event 快照，再按固定 `Session → Environment Work` 锁序锁定 active Work；
交叉校验 organization、workspace、session、environment 与 Work 身份；创建 Code Session，写入
initialize 和 initial inbound events 并推进连续 sequence；更新 Session/Work runtime metadata；
读取 credential context，在提交前签发 JWT，最后整体提交。

锁前已提交的 event 进入 initial inbound；锁后写入的 event 在事务提交后走实时转发。现有
idempotency key 去重，冲突不消耗 sequence。任一写入、身份校验或签发失败均回滚，不暴露部分
runtime。

## 6. 网络、Template 与迁移

Provisioning 发生在 Code Session proxy 启动前，只能使用 Sandbox 创建时固化的网络策略；MVP
真实安装验收使用 unrestricted networking。Limited-network registry/mirror、redirect、VCS 与
private registry 策略不在范围内。

provider-neutral 逻辑默认值为 `managed-agent-sandbox`；Compose/e2b-local 显式使用
`managed-agent-sandbox:latest`，自定义 Template 原样透传且必须实现 v1 命令合同。已有本地
Compose override 不会被初始化命令覆盖，升级者需手动配置。

迁移 `00035_migrate_packaged_environment_template.sql` 仅更新同时满足以下条件的记录：cloud
Environment、`resolved_template = 'claude-code-interpreter'`、六个 manager 中至少一个数组非空。
目标是逻辑默认 `managed-agent-sandbox`，Resolve 再以当前部署的 `e2b.template` 物化。空
Packages、自定义 Template 和已使用新 Template 的记录不变。

## 7. 失败与清理

manifest/transport/result/provisioning、lease、rclone 或 runtime 事务失败均不得启动 Manager。
当前 Runner 的清理语义是：

- 无 provider ID：Sandbox 标记 `failed`，停止 Work；
- 有 provider ID 的启动失败：用独立有界 context 标记 `stopping`、记录有界原因、停止 Work 并
  Kill；Kill 成功后标记 `failed`，失败则保持 `stopping` 并保存合并后的有界错误；
- heartbeat 遇到用户 stop：同一独立有界 context 内标记 `stopping`、Kill、停止 Work；Kill
  成功后标记 `stopped`，失败则保持 `stopping` 并返回合并错误；
- runtime 已提交但 Manager 启动失败：先执行现有一次性 Code Session termination、凭证撤销及
  Session/Work runtime metadata 清除，再清理 Sandbox。

MVP 不保证 Kill 或 Code Session termination 的最终一致重试。

## 8. 最小测试矩阵与命令

| 层次 | 最小覆盖 |
|---|---|
| API/manifest | round-trip 与空数组；非法结构/spec；stdin 原样传输；存量重校验；1 MiB 双边界 |
| result | 严格字段/单值；status/exit 一致；16 KiB；诊断降级；不泄漏输出 |
| Runner | 成功顺序；失败/stop/cancel 不启动 Manager；单个独立清理 context；Kill 失败保持 `stopping`；安装期间 event 不丢 |
| Runtime DB | Session→Work 锁序；身份 mismatch、凭证失败回滚；事件顺序与幂等 |
| Migration/E2E | 00035 精确 eligibility；六类 manager 在 unrestricted Sandbox 中真实可用 |

```text
go test ./internal/environments ./internal/codesessions -count=1
go test ./internal/db -run 'ManagedAgentRuntime|CodeSession' -count=1
go test ./internal/networkpolicy ./internal/runtime/e2bruntime -count=1
go test ./tests -tags='e2b_integration e2e' -run '^$'
```

真实 E2B 测试需要显式凭证和本地 Template，不随普通单测运行。
