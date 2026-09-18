# 私有 transcript 归档

本功能只处理 `code_session_internal_events`。不改变 ListPage、公开事件、SSE、NATS 或 resume 协议。

## 终态归档

独立 River sweep 发现 `archived_at` 或 `deleted_at` 已满 24 小时、全部 code session lease 已过期且 worker idle 的会话。NULL lease 表示没有活动租约；running/requires_action 均不允许归档。归档任务再次通过条件查询校验，软删除的每个批次也重新检查条件。

`DeleteSession` 只级联软删除公开事件等关联资源，没有处理私有 transcript；独立 sweep 覆盖已经删除的存量会话，修复这一无限保留问题。archive/delete API 写路径不挂归档逻辑。

段先注册 pending，再以唯一对象 key 上传，回读验证 size、SHA-256、行数和序号，并与原始行核对，然后 attached，最后分批软删除。每个删除事务最多 500 行，锁定 attached 注册表行并检查每个序号覆盖。不锁 code_sessions。注册段时以 code session UUID 派生的独立 advisory transaction lock 串行化区间冲突检查，不阻塞 worker append。

上传只尝试一次，重跑只读取已有对象，不覆盖同名 key。上传之前进程退出或上传失败留下的 pending 段由 24 小时清理回收后，下一轮使用新 UUID 重建；上传完成但确认丢失时，重跑可直接校验并 attached。对象读取失败返回错误，不删除数据。

默认关闭且 dry-run。段按解压字节切分；超目标大小的单条事件独占段。序号空洞处额外分段，使区间不会覆盖其他 scope 的未归档事件。单任务有总行数上限，重试优先完成已有段的软删除。

## 验证

`TestTranscriptArchiveTerminalSafety` 覆盖非不可逆终态、未过期 lease、运行 worker 和不足静置期；`TestTranscriptArchiveTerminalRetry` 覆盖 dry-run、大 payload 合并和上传后中断恢复。生产统计 T0 经用户明确指示跳过，模式 B 收益和压缩比尚未测量。

## 物理删除

独立 `transcript_archive_delete` job 受 `hard_delete_enabled` 控制，默认关闭。观察期默认 14 天，设为零也仍先软删除，再由物理删除任务处理。每次先检查是否有已过观察期却没有 attached 覆盖的行；发现 deleting/missing 注册表即返回错误。随后逐段重新读取并校验对象，逐行匹配 UUID 与序号，最多每批 500 行、独立事务，事务内再次锁定 attached 段并执行 HasAttachedCovering。对象丢失或损坏均拒绝删除并记录错误。单任务遵守总删除行数预算。

## Compaction 边界增量归档

模式 A 由独立开关启用，默认关闭。每个 foreground / agent_id scope 独立计算最新 compaction，只归档严格小于边界且 created_at 早于 archive_min_age 的行；无边界的 scope 和边界本身保留。边界前移不会使已选历史重新可达。即使不同 scope 序号交错，也在空洞处分段并按对象内逐条序号删除，不能把起止区间当作实际成员列表。

删除行也会删除幂等键。安全论证依赖 worker lease 60 秒、epoch 围栏拒绝旧 worker（409）、当前 worker 仅持有本次新产生 entry，以及默认 7 天 archive_min_age 远大于 worker 生命周期。若未来改到小时级，必须重做该论证；本实现配置校验不允许小于 7 天。

## 导出与还原

先在所有实例禁用归档/删除并等待运行中任务结束，再在受信环境运行以下命令。CLI 复用配置中的现有 bucket，不创建 bucket、不启动 HTTP 服务、不改 resume 路径。

```sh
CONFIG_FILE=/path/to/config.yaml go run ./cmd/transcript-archive -mode export \
  -organization ORGANIZATION_UUID -workspace WORKSPACE_UUID -code-session CODE_SESSION_UUID > history.jsonl
CONFIG_FILE=/path/to/config.yaml go run ./cmd/transcript-archive -mode restore \
  -organization ORGANIZATION_UUID -workspace WORKSPACE_UUID -code-session CODE_SESSION_UUID
```

导出按序合并 attached 段和 PG 中尚未归档的历史。payload 原始 JSON 可以含格式换行，因此文件是按记录分隔的 JSON 文档流；应使用流式 JSON decoder，不能按物理换行 split。还原逐段校验对象，保留原始 UUID、external_id、sequence_num、payload_uuid、幂等键、worker payload_hash 和时间戳。大 payload 经现有 eventpayload 写入新 blob 后再原子关联；即使旧 blob 已被 GC 删除，也能恢复。已存在的活跃行不覆盖，冲突会返回错误；每批最多 500 条，不推进 append 序号水位。

软删除观察期内且旧 blob 尚存时可用 deleted_at=NULL 取消软删除。旧 blob 已被 GC 回收后，必须使用还原工具重建引用，不能只清空 deleted_at。本测试覆盖段→物理删除→旧 blob GC→还原回 PG→ListPage 一致，以及还原幂等和跨 workspace 隔离。

## 配置与组装

`transcript_archive.enabled=false`、`dry_run=true` 是默认值；模式 B 开关默认开启但仍受总开关控制，模式 A 和物理删除默认关闭。`terminal_dwell` 必须大于零，`archive_min_age` 至少 168h，`soft_delete_window` 允许零。段目标为 1..32 MiB，删除批量为 1..500，单 job 行数为 1..50000。读回解压上限为 64 MiB，阻止不受控分配。

主程序复用现有 ObjectStore 和 River client，注入 component=transcript-archive logger，队列并发为 2。每 5 分钟的 durable sweep 使用 UUID 游标按 100 个 code session 扫描；归档和物理删除是不同 job，已排队任务也读取启动时配置中的开关。配置变更需要重启实例。既有 cleanup worker 每 30 秒认领超过 24 小时的 pending 段，在同一事务中置 deleting 并写 object_cleanup job；attached 不参与回收，对象删除清理全部版本。
