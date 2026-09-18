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
