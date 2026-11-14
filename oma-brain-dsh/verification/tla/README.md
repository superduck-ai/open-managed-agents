# CCRv2 DSH Brain 的 TLA+ 模型

这里的三个 TLA+ 规约分别描述 Brain 领取、租约与 epoch fence 的后端状态机，DSH 心跳与准入时序，以及单个 DSH 进程内的并发准入。规约逐条对应当前实现中的 SQL 和 TypeScript，不描述理想化设计。模型是有界的，结论只覆盖下文列出的常量范围。

## 运行

需要 Java 11 及以上，以及官方 [`tla2tools.jar`](https://github.com/tlaplus/tlaplus/releases/tag/v1.8.0)。

```bash
java -XX:+UseParallelGC -cp /path/to/tla2tools.jar tlc2.TLC -workers auto -config BrainClaim.cfg BrainClaim.tla
```

其他配置把 `-config` 和模块名替换为下表中的对应项。

| 配置 | 模块 | 预期 | 2026-11-14 结果 |
| --- | --- | --- | --- |
| `BrainClaim.cfg` | `BrainClaim` | 通过 | 尚未完成 |
| `BrainClaimStaleTool.cfg` | `BrainClaim` | 违反 `NoStaleSandboxForward` | 违反，11 步反例 |
| `BrainClaimLateInput.cfg` | `BrainClaim` | 违反 `LateInputNotStranded`（设计文档已知边界） | 违反，7 步反例 |
| `BrainLeaseTimingSteady.cfg` | `BrainLeaseTiming` | 通过 | 通过，9,017,236 个不同状态 |
| `BrainLeaseTimingSteadyThreeFailures.cfg` | `BrainLeaseTiming` | 违反 `LeaseNeverClaimable` | 违反 |
| `BrainLeaseTimingAdmission.cfg` | `BrainLeaseTiming` | 违反 `LeaseNeverClaimable` | 尚未完成 |
| `BrainAdmission.cfg` | `BrainAdmission` | 违反 `NoOrphanSession` | 违反，14 步反例 |
| `BrainAdmissionCapacity.cfg` | `BrainAdmission` | 通过 | 通过，92,097 个不同状态 |

## 模型与代码的对应

### `BrainClaim`：后端行状态机

一条 `code_sessions` 行、两个 DSH 执行槽位、离散时钟。常量：租约 2、宽限 1、领取期限 3、耗尽保持 2、最多 2 次领取、epoch ≤ 5、时间 ≤ 8、最多 2 条输入。

| 动作 | 实现 |
| --- | --- |
| `Claimable`、`Claim`、`HoldExhausted` | `code_session_claim_mapper.xml` 的 `LockNextClaimable`、`Claim`、`HoldExhausted`；`DB.ClaimNextCodeSession` |
| `Register` | `DB.registerCodeSessionWorker` 与 `RegisterWorker`（仅校验 epoch 与 active，清空 requested 与领取期限） |
| `Connect` | `UpdateConnection` 在 connected 时把 attempts 归零 |
| `Heartbeat` | `DB.RecordCodeSessionWorkerHeartbeat`（`now > lease + grace` 拒绝） |
| `Release` | `Release`：`retry` 退避、`handoff` 立即可领且 attempts 减一、`idle` 只清租约 |
| `FailPermanently`、`PublishExhausted`、`ResetExhausted` | 同名语句；发布与重置分为两步，可以失败 |
| `ToolValidate`、`ToolForward` | `handleSandboxMCP` / `proxySandboxMCP`：epoch 校验不持行锁，之后再转发到沙箱 |
| `Shutdown`、`RetireIdle`、`AdmissionFails` | `worker.ts` 的停机 handoff、空闲退役和 `claim.ts` 的准入失败原因 |

`Claim` 的响应可以丢失（领取者拿不到凭据）。worker 可以在任意时刻崩溃、重启，也可以停止心跳来表示 GC 停顿。

### `BrainLeaseTiming`：心跳与准入时序

单个 worker，时间单位为 0.2 秒：租约 20 秒、宽限 10 秒、心跳间隔 6 秒 ±10%、心跳超时 5 秒；register 与 `GET /v2/sessions` 都使用 `OmaApi.request` 的 20 秒超时。心跳按固定节奏排期，上一拍仍在途时跳过（`CcrHeartbeat.tick`），首拍在会话创建后一个间隔发出。`LeaseNeverClaimable` 表示持有者存活期间，其他进程的领取条件 `lease + grace < now` 始终不成立。

### `BrainAdmission`：DSH 进程内并发准入

对应 `worker.ts` 的 `admit`、`retire`、`retireIdle` 与 `capacity.ts` 的 `SessionCapacity.reserve`。同步代码段建模为原子动作，`await` 点之间可以交错。`sessions`、`retiring` 和 `api.assignments` 都按 session id 建键，与实现一致。服务端只建模 epoch 和租约：领取期限过期，或当前 epoch 尚未开始心跳（准入期间）时，租约可以失效。上一个模型证明这个窗口在默认超时下可以出现。

## 已验证的性质

- `EpochCredentialUnique`、`AtMostOneCurrentHolder`：每个 epoch 的凭据至多发给一个领取者，同一时刻至多一个执行端持有当前 epoch。
- `LeaseExclusion`：“心跳仍会被接受”与“可被其他进程领取”互斥。领取 SQL 和心跳都使用 `lease + grace` 判定，这一点成立。
- `ExhaustionFencesHolders`：`worker_claimed_by` 为空（已进入耗尽或永久失败）时，没有执行端持有当前 epoch。`HoldExhausted` 中 `CASE WHEN worker_claimed_by IS NULL` 的不重复推进不会让旧领取者重新获得所有权。
- `EpochMonotonic`、`WritesMonotonic`：epoch 单调递增，被接受的 fenced 写入不会来自比之前更旧的 epoch。
- 稳定运行时，最多连续 2 次心跳失败，租约不会进入可领取状态；第 3 次连续失败可能造成接管。这与 `brain-worker-claim.md`“租约时长”一节的推导一致，并给出了边界。
- `CapacityBound`：两个会话、上限 1、两个领取循环时，存活会话数加预留数不超过 `OMA_BRAIN_MAX_SESSIONS`，预留计数与阶段一致。

## 发现

### 1. 同一进程并发准入同一会话会留下孤儿会话（`BrainAdmission`）

反例：循环 l1 领取 epoch 2 并完成 register，在加载 `/v2/sessions` 期间尚未创建会话，也没有心跳，于是租约失效。循环 l2 领取同一会话的 epoch 3。由于 `sessions` 中还没有条目，l2 不会退役旧会话。两次 `createOmaSession` 都写入 `api.assignments.set(id)`，`sessions.set(id)` 后写覆盖先写。被覆盖的会话对象不在 `sessions` 中，因此 `retire` 的 `sessions.get(id) !== session` 判断直接返回，永远不会 `close()`。被覆盖的若是当前 epoch 的会话，它的请求会使用另一个对象写入的凭据，被 409 拒绝。`retire` 结束时还会按 id 删除 `api.assignments`。

`BrainLeaseTiming` 的准入配置说明前提可以达成：register 提交后立即开始计算 20 秒租约，而 register 响应与 `GET /v2/sessions` 各自最多 20 秒，首次心跳还要再等约 6 秒，最坏情况超过 30 秒的租约加宽限。`/v2/sessions` 不在 epoch fence 内，旧 epoch 的读取仍会成功。

epoch fence 保证不会出现双写，这是资源泄漏和可用性问题。可考虑的修复：`admit` 按 session id 串行化；或在 `createOmaSession` 前比较 `sessions` 与 `api.assignments` 中已有的 epoch，只允许更高 epoch 覆盖并退役被覆盖者；同时让会话在 register 返回后立即开始心跳。

### 2. 沙箱 MCP 转发的 epoch 校验是先检查后执行（`BrainClaim`）

`proxySandboxMCP` 在 `ensureSessionSandbox` 之后再次调用 `ValidateCodeSessionIngressWorkerEpoch`，但这次校验不持行锁，随后还会准备 memory commit 并发送上游请求。反例中，校验通过后会话被 handoff 释放并由新 epoch 领取，旧请求仍然到达沙箱。窗口很短，模型中的 `Shutdown` 也比实现中的 `drainSessions` 更粗略，所以这是低风险问题。但它说明工具副作用不受 epoch fence 保护，新 epoch 恢复时必须继续把未知结果当作可能已执行（`tool-result-recovery.md` 已采用这种保守策略）。

### 3. 耗尽或永久失败窗口内到达的新输入会被搁置（已知边界）

反例：`FailPermanently` → 新输入（公共会话变为 running）→ 发布 `status_idle(retries_exhausted)` 把公共会话改回 idle → `ResetExhausted` 成功。此时 requested 为空、公共会话 idle，这条输入只能等下一条输入到来才会被领取。`brain-worker-claim.md` 已经记录了这个窗口；模型确认它同样适用于 `permanent` 路径。

## 未覆盖范围

- 没有检查活性（最终被领取、最终发布错误），因为模型有时间上界。
- JetStream 投递、NAK 重投、Redis ACK key、SSE 每秒校验和 delivery API 只抽象为“在途输入在当前持有者消失后重投”。
- 子代理、审批、模型代理、dsh 日志恢复与 `agents.resume()` 不在模型内。
- 多个 Code Session、多行 `SKIP LOCKED` 竞争、Runner 的重复派发与 `rescheduling` 状态未建模。
