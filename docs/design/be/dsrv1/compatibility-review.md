# DSRv1 与当前 Brain、deepseek-harness 的兼容性验证

本文保留实现前的依赖复核。后续已通过仓库内 Bun patch 和 DSR 执行器补齐同 turn continuation、挂起与执行标记；接口真实后端验收及范围见 [实验实现状态](implementation.md)。

核对日期：2026-11-03。原始结论：DSRv1 可以复用当前 Brain 的本地原生日志、异步复制、执行屏障和远程工具架构，但当前依赖不支持方案要求的回合中途恢复与挂起。应先完成这两项运行时能力的隔离验证，再冻结协议。仅替换 HTTP 路径不能实现第 7.7 节。

后续全部接口的调用位置、请求响应和扩展测试见 [逐接口评审](interface-review.md)。该报告覆盖后续第 12 版修订；本文保留第一轮恢复与审批顺序的证据。

## 1. 验证基准与范围

| 对象 | 实际核对版本 | 说明 |
| --- | --- | --- |
| OMA | `4b1ea7f43cc9b04fa44eaebfa9212fc1bca0aeb2` | 当前工作目录修改前的 HEAD |
| deepseek-harness | `f3803c96974ad4a94195fb97e5c637e9c2a3afe3` | `/Users/arthur/WebstormProjects/deepseek-harness` 的 HEAD，与协议引用版本一致 |
| Brain DSH 依赖 | `0.2.2-alpha.2` | `oma-brain-dsh/package.json` 固定的 npm 版本，探针直接运行已安装依赖 |
| DSRv1 | 第 10 版草案 | `protocol.md` 与 `api-reference.md`，尚未实现 |

本次阅读两个仓库的实际实现，运行隔离的 Brain 单测与审批顺序探针。没有修改 deepseek-harness，也没有连接配置中的数据库、对象存储或模型服务。未运行 `verify-be`、真实集成、SIGKILL 接管及性能测试。测试通过不代表 DSRv1 通过验收。

## 2. 已验证可沿用的部分

| 能力 | 实现证据 | DSRv1 迁移要求 |
| --- | --- | --- |
| 本地持久化与远端复制分离 | `oma-brain-dsh/src/ccrv2/persistence.ts`、Harness `packages/session/session-persistence/src/handle.ts` | 保留本地单写、flush、前缀核对与分歧报错；替换远端读写协议 |
| 工具执行与公开输出屏障 | `runtime/session.ts:99`、`ccrv2/session.ts:432`、`:650` | 保留 flush、远端确认、预览确认的顺序 |
| 模型与工具隔离 | `runtime/execution.ts`、`oma-sandbox/client.ts`、`llm-ai-sdk/` | 模型代理、沙箱网关、挂载及共享 cwd 不需要并入 transcript 接口 |
| 原生格式 v4 | Harness `packages/core/session/src/types.ts:90` | 保留原生 Header/Event；继续使用 Harness 完整日志校验 |
| 修改远程工具参数 | `runtime/execution.ts:40` | 已对 effective 参数重新校验并传给远端，不能误判为必须修改原生只读参数 |

表内 Brain 路径相对于 `oma-brain-dsh/src/`；Harness 路径相对于独立的 deepseek-harness 仓库。行号对应本次基准。

## 3. 阻断一：当前 resume 会结束旧回合

Harness 的 `packages/core/agent-loop/src/index.ts:795–810` 在恢复写句柄后，无条件调用 `interruptedTurnClosers`，将补偿工具结果、步骤结束和回合结束写入日志，再初始化 Agent。`agent-loop/src/agent.ts` 从 idle 相位启动；调用 `followup` 会开始新回合。

OMA 的 `runtime/session.ts:167–168` 使用这一 resume；`ccrv2/session.ts:305–308` 还会为未完成输入追加 recovery completion。现有 `input-recovery.test.ts` 明确验证恢复不重新执行已接纳输入或工具。这个保护正确，但与 DSRv1 新增的“审批恢复后继续”语义不同。

因此，增加一个“不补 closers”开关还不够。上游需要明确的续执行模式，重建 open turn、step、已记录 assistant 的工具批次及结果，保持 call ID，跳过已有结果，并在 scoped 工具与审批 hooks 安装后由显式入口启动。

主线程和子线程必须使用同一恢复规则。`ccrv2/subagents.ts` 与 `runtime/subagents.ts` 当前会修复或结算中断子线程，不能原样迁移成续执行。父线程必须在子线程恢复关联后再继续等待与结算。

## 4. 阻断二：tool/call 不能证明工具体执行过

Harness `packages/core/tools/src/batch.ts:157–159` 的顺序为先 `settlement.start`，再 `pipeline.prepare`。`start` 在 `:247` 写 `tool/call`，审批 hooks 随后才运行。OMA 审批位于 `runtime/session.ts:99–114` 的 `tools/execute` wrapper 中。把审批移到 `tools/pre-execute` 仍晚于 `tool/call`。

隔离探针在审批回调中读取原生日志、已复制日志和 fake 工具执行计数，结果如下。

| 审批尚未回答时 | 实测值 |
| --- | --- |
| 本地 `tool/call` | 1 条 |
| 远端已确认 `tool/call` | 1 条 |
| 本地 `tool/result` | 0 条 |
| 工具体执行 | 0 次 |
| 会话 busy | `true` |

这直接否定第 7.7 节按 `tool/call` 判断“可能执行过”的分类。若不调整，新 owner 取回 allow 后仍会把从未执行的工具标为未知结果，用户批准不会生效。

建议保留原生 `tool/call` 语义，在授权完成、实际派发前增加持久执行开始记录。可以使用 v4 的命名插件记录，例如待定的 `plugin:oma/tool-dispatch`，不要求立即增加 HTTP 端点或改变原生格式版本。记录需要关联 `input_id`、原生 Session/call ID、结构化工具身份、审批 ID，以及有效参数或摘要。所有可执行路径都必须经过它，包括本地 allow、远程工具与委派工具。

执行顺序应为：取得审批或本地许可 → 校验有效参数 → 写执行开始记录 → 本地 flush → 远端确认 → 再次检查取消与归属 → 执行工具 → 写结果。这个记录表示调用可能产生效果，不承诺工具一定执行成功。

| 日志与审批事实 | 建议恢复动作 |
| --- | --- |
| 有工具块，没有执行开始记录 | 用原始参数重新取得审批；拒绝则结算，允许则按正常屏障执行 |
| 有执行开始记录，没有结果 | 不重放，生成未知结果并让模型先核实 |
| 有结果 | 复用结果，继续剩余批次或下一模型步骤 |
| 本地 completion 已确认 | 只补公开输出、finish 与 ACK |

这仍是待验证的接口建议。需要测试主线程、子线程、并行批次、审批预取以及派发前后各崩溃点，不能宣称副作用 exactly-once。

## 5. 阻断三：挂起不能复用 stop/close

`worker.ts:125` 只回收 `!session.busy` 的会话，而等待审批仍然 busy。`runtime/session.ts:251–256` 的 stop/close 会 cancel Agent；Harness `agent-loop/src/agent.ts:374–422` 会补工具错误并写 `turn/end`。当前关闭路径还会拒绝审批等待并进入输入收尾。

因此需要独立的 suspend/detach 能力。挂起必须停止内存驱动、保留服务端 pending、保留 open turn，不生成补偿工具结果、completion 或 finish，不 ACK task。确认复制前缀、释放本地写句柄并关闭输入流后，才 release idle。存在仍在运行的工具、结果未确认或其他可运行线程时不能挂起整个会话。

恢复接口与挂起接口需要一起验证。只有挂起成功、跨进程恢复后工具获批执行且不重复结算，才满足方案目标。

## 6. 需要补齐的接口与映射

| 位置 | 当前差异 | 最小调整 |
| --- | --- | --- |
| `ToolExecutionBridge.approve` | `runtime/contracts.ts:121` 只有 callId/toolName/input/available；当前审批 ID 随机 | 注册工具时保存 `{kind,name,server?}`；适配层补 input/turn/session 身份，使用稳定审批 ID |
| 审批 ID 算法 | 草案仅写 `sha256` 三元组与 base32 | 固定字段字节编码、域分隔、base32 大小写及填充；增加 TS/Go 共用样本 |
| `updated_input` | 远程工具已有 effective 参数适配，原生委派工具仍使用原参数 | 恢复查询使用原始参数，取得首次 decision 后再应用修改；明确委派工具是否支持修改，未支持时不能静默忽略 |
| `AskUserQuestion` | `user-questions/index.ts:48` 必须取得 externalResult；不是普通 allow 后本地执行 | 明确 builtin 特例及公开 custom 结果映射；审批允许与问答结果到达不可混淆 |
| 本地 input completion | `ccrv2/session-output.ts:7–49` 保存 success/failed/recovery 与文本，中断也映射 success | 新记录保存精确的 finish result/failure_code，支持 success/cancelled/failed；恢复属于执行过程；重启后重建相同请求，不从文案反推 |
| 重投与结束顺序 | `ccrv2/session.ts:573` 直接 publish/checkpoint/ACK | 统一 processing → 日志/输出确认 → finish → processed，terminal 重试不重新执行 |
| 注册响应 | 当前从 register 读取遥测配置，DSR 原草案遗漏 | 保留 `observability_enabled`、`trace_export`；本次已补入两份草案 |
| 输入信封 | 保留 `oma_system_context` 与 `trace_context`；配置随 register 冻结 | 保留服务端可信注入与缺省语义；本次已补入两份草案 |
| epoch | 当前 Assignment/claim 校验 number，DSR 使用 string | DSR 适配层采用十进制字符串，避免无界 `Number` 转换丢失精度 |
| 公开事件 | `ccrv2/subagents.ts:134` 与部分 MCP 错误使用随机 ID | 用可恢复的原生身份派生稳定 ID；恢复与首次发送必须得到同一 ID |
| 错误处理 | 当前部分路径遇任意 409/410 就 fence | 按 `DSR-Error` 区分补 Header、补缺口、续租及真正的 epoch/conflict 终止 |

输入字段由 `internal/codesessions/system_context.go:75–76`、`mapper.go:54` 构造。不得直接信任用户同名字段。遥测配置来源为 `internal/codesessions/ingress.go:304–306`，Brain 在 `worker-registration.ts:42–45` 读取。

## 7. 本次执行证据

运行命令如下，均为本地 fake 服务与已安装 DSH 依赖。

```sh
bun test ./tmp/dsrv1-review/approval-order.test.ts ./oma-brain-dsh/tests/input-recovery.test.ts ./oma-brain-dsh/tests/ccrv2-recovery-checkpoints.test.ts ./oma-brain-dsh/tests/model-replication.test.ts --timeout 15000
```

结果为 21 pass、0 fail，共 1199 次断言。临时探针与原始输出位于当前 worktree 的 `tmp/dsrv1-review/approval-order.test.ts`、`tmp/dsrv1-review/test-output.txt`，未纳入版本控制。探针验证现有语义，不应作为未来 DSR 续执行行为的固定回归。

测试覆盖现行恢复不重放、原生输入身份、稳定输出补发、子线程发现和状态恢复、工具执行复制屏障、公开输出与预览顺序、输入 ACK 顺序。未覆盖新的 DSR HTTP 端点、审批表、reducer、归档、SIGKILL 接管、真正挂起或实际模型/沙箱效果。

## 8. 建议实施顺序与冻结条件

1. 先在隔离 PoC 中验证原生回合续执行、执行开始记录与挂起，主线程和子线程一起覆盖。
2. 固定审批 ID 编码、工具身份、问答结果、参数修改范围与 completion schema，再冻结协议字段。
3. 实现 DSR 客户端与后端状态机，迁移现有屏障和定向测试。不要因为删除 `src/ccrv2/` 而一起删除协议无关的恢复保护。
4. 最后执行方案列出的切换验收，再允许 P4。设计冻结与上线验收应区分；隔离 PoC 不代表可以提前切换生产。

本次仅修改设计文档并生成临时验证代码，没有改变生产代码或运行时行为。已修正“接管关闭 pending”的两处陈旧条款；接管应继承审批，register 本身不发布状态变化。恢复与挂起接口仍是冻结阻断，不能把本次 21 项通过解释为已补齐这些能力。
