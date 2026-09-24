# Outcome grader：旧方案复评的更正记录

> 日期：2026-09-16。状态：**旧推荐已撤回，不是已接受的设计决策**。
> 当前讨论边界、固定源码版本、完整证据链与自审清单统一维护在 [运行时复用复评](/Users/yueqi/Coding/Agent/open-managed-agents/docs/design/be/outcomes-grader-em-review.md)。本文只保留旧判断的更正与各方案共有的验收约束，避免两份文档互相矛盾。
>
> 当前已确定的总体方案见：[Managed Agent Outcome Grader 架构设计](./managed-agent-outcome-grader-architecture.md)。本文保留历史选项比较和证据，不再作为最终选型入口。

## 1. 撤回或收窄的判断

| 旧说法 | 修正 | 依据 |
| --- | --- | --- |
| 优先对齐官方，所以主推 J：OMA 自写 grader loop | 撤回。A/G 各自复用现有 Claude agent loop；OMA 仍管 outcome 状态、触发和反馈，不以 hidden workflow 为前提 | [Define outcomes](https://platform.claude.com/docs/en/managed-agents/define-outcomes)规定独立 grader context，不规定重写模型循环；[当前证据链 E2](/Users/yueqi/Coding/Agent/open-managed-agents/docs/design/be/outcomes-grader-em-review.md) |
| 不选 J，就应选 B：第二个托管 code-session | 撤回这个二分。运行上下文、OS 进程、托管 worker/code-session 是不同身份维度 | [官方 multiagent](https://platform.claude.com/docs/en/managed-agents/multiagent-orchestration#how-it-works)及 [OMA task/thread 映射](/Users/yueqi/Coding/Agent/open-managed-agents/internal/codesessions/mapper.go#L569) |
| grader 不是公开 thread，因此不能复用 thread 执行机制 | 撤回。公开事件不同不能证明内部执行器不同；grader 的完整内部 thread/进程拓扑未知 | 官方分别描述 [outcome spans](https://platform.claude.com/docs/en/managed-agents/define-outcomes#outcome-events)和 [multiagent threads](https://platform.claude.com/docs/en/managed-agents/multiagent-orchestration#how-it-works)，没有公开排他性内部实现合同 |
| 一个 sandbox → 一个 EM → 一个 executor，所以只能一个 context | 撤回最后一步推导。R 当前未实现一 session 管理多个独立 Claude 进程，但不是 OS/sandbox 限制，原生子上下文可存在 | [R Manager](/Users/yueqi/Coding/Agent/environment-manager-rs/src/internal/manager/manager.rs#L455)一次装配一个 executor；共享取消/session/token 边界见[主复评 3.2](/Users/yueqi/Coding/Agent/open-managed-agents/docs/design/be/outcomes-grader-em-review.md) |
| EM 私有 Claude 是已可用的替代，剩下只选进程数 | 历史表述已被当前设计修正为：先实现同 sandbox/session 内 1:N Claude Code execution supervisor，再接 OMA outcome 事件驱动；历史、权限和生命周期仍需分别实现，不能注册两个完整 worker/lease 就称两个独立 agent | [当前设计](/Users/yueqi/Coding/Agent/open-managed-agents/docs/design/be/managed-agent-outcome-grader-architecture.md) |
| grader 优先用 hidden workflow，用户须先接受版本耦合，双进程仅作退路 | 撤回。workflow 不是产品合同前提；研究降为实现机制记录，双进程候选不以 workflow 不可用为前提，也不证明 Anthropic 内部拓扑 | 用户最新宏观讨论澄清；官方 outcomes/multiagent 未要求 workflow、OS 进程数或公开 grader thread |
| 扩展 EM 的 outcome type 即可让完整功能到达 agent | 撤回其充分性。静态 payload、运行中投递、独立评分与反馈是不同环节 | [链路文档](/Users/yueqi/Coding/Agent/open-managed-agents/docs/design/be/outcomes-link-and-grader.md)与当前证据链 E4 |

上一版用过 Rust EM `92ac1bf...` 的行号；那是历史快照，不应继续写成“当前版本”。最新核查基线在当前复评文档列明。未用旧研究报告的“审计通过”或“已证伪”标签代替一手源码/官方文档。

用户已确认使用最新 Claude、完整交付而非 MVP。本轮已确定一个 managed session 对应一个 sandbox，由 EM 管理独立上下文的 A/G；先实现 EM 的 1:N execution，再接 OMA outcome 事件驱动。最新执行入口的逐项证据见 [运行时证据](/Users/yueqi/Coding/Agent/open-managed-agents/docs/design/be/outcomes-grader-runtime-evidence.md)。

## 2. 保留的官方约束

- [Define outcomes](https://platform.claude.com/docs/en/managed-agents/define-outcomes)：harness 自动 provision 独立上下文的 grader；explanation 回馈迭代；同一时刻一个活动 outcome；公开 start/ongoing/end。
- [Multiagent orchestration](https://platform.claude.com/docs/en/managed-agents/multiagent-orchestration)：agents 共享 sandbox，各有 session thread、历史和配置；与 outcomes 文档均不限定 OS 进程数，也未要求公开 grader thread。
- 同页结果合同：`needs_revision` 继续；`max_iterations_reached` 后有最终 acknowledgment 再 idle；interrupt 即使在首次评分前也可能产生 end，此时 start ID 为空。
- 同页 Check outcome status：资源 `result` 与每轮 span 的 `result` 不能直接视为同一状态机；资源在完成前报告 pending/running/evaluating。
- [Tools](https://platform.claude.com/docs/en/managed-agents/tools#multiagent-sessions-outcomes-and-mid-session-updates)：grader 无 web_search/web_fetch；不能推导完整工具清单、权限继承或只读保证。
- [Webhooks](https://platform.claude.com/docs/en/managed-agents/webhooks)：evaluation-ended 对应单次评估完成，不是接收定义；投递不保证顺序。
- [Self-hosted sandboxes](https://platform.claude.com/docs/en/managed-agents/self-hosted-sandboxes)：工具执行可位于用户基础设施；不能据此推出 OMA 必须另写 loop，也不能自动承诺 outcomes 在所有 self-hosted 路径已支持。

这些都是公开合同，不作为 Anthropic 内部部署拓扑的证明。

## 3. 各实现路径共有的工作与待验收项

以下是依据公开合同提出的工程验收要求，不代表当前实现或本轮测试已经通过。

1. 接收并持久化 outcome 定义后，可靠启动工作；静态启动 payload 不代替运行中 define_outcome 的消费路径。
2. 定义可评价的完成边界；权限等待、工具等待、预算暂停不能误触发；活动子 agent/后台进程可能继续修改产物。
3. 独立构造 grader 上下文，按 rubric 检查真实产物，而不是只评价 writer 最后一段回答；具体输入和配置继承策略待定。
4. 结构化结果、explanation、usage、轮次、资源状态、span 与 webhook 保持一致；重试/重复结果不能重复推进。
5. 验证不满足→反馈→修订→满足，以及迭代上限的最终 acknowledgment。
6. 处理评分前/中 interrupt、追加消息、迟到结果、worker 重启与替换；明确状态唯一所有者。
7. rubric 不适用与模型/工具/基础设施故障分开，不能把所有异常都映射成官方 `failed`。
8. 不暴露 grader 私有 reasoning；复用底层机制不意味着应自动公开一套新的 session/thread 资源。
9. 工具权限必须实际生效。禁用写文件工具不等于 bash 只读；串行 writer/grader 不等于快照级一致性。
10. 本轮以同 sandbox 共享产物为讨论目标；历史/权限隔离与产物一致性须另行明确，不能把双进程当成隔离证明，也不因此要求另建 sandbox。provider 的 exec/checkpoint 能力不自动等于通用 clone API。

具体候选比较和未收敛项只维护在 [当前复评](/Users/yueqi/Coding/Agent/open-managed-agents/docs/design/be/outcomes-grader-em-review.md)，不再保留 J-first、hidden-workflow-first 推荐或 J/B 二选一问题。
