# Transcript 归档 DB 回归验证

归档的 DB 测试必须使用真实事件证明查询和删除发生，不能仅验证空表操作没有报错。测试通过 `TEST_MIGRATION_DATABASE_URL` 连接测试 PostgreSQL，在独立 schema 中应用 migrations，结束后删除该 schema。

| 测试 | 必须验证的行为 |
| --- | --- |
| `TestTranscriptArchivePostgres` | pending、跨 workspace 和混合未覆盖序号拒绝删除；软删返回 1 并置位 deleted_at；等于 cutoff 时保留，晚于 deleted_at 时硬删返回 1；重复删除返回 0；相邻事件不受影响；重复区间返回 ErrDuplicate |
| `TestTranscriptArchiveTerminalCandidatesPostgres` | 区分 archived/deleted 与仅 terminated/running；排除静置期不足、有效 lease、运行 worker 及运行中的同父会话 worker；断言非空候选 scope 和 UUID 游标分页 |
| `TestTranscriptArchiveBoundaryCandidatesPostgres` | 按 foreground/subagent 独立边界筛选旧事件，保留最新边界、近期事件和无边界 scope；断言精确事件序号、分页、范围读取、nullable agent 与组织/workspace 隔离 |
| `TestTranscriptArchiveDeleteBindings` | 两个删除 statement 的 ID、类型、参数顺序与值、IN 序号列表、租户过滤、deleted_at 和 cutoff 谓词；T4 起覆盖软删 terminal/boundary 的 Eligibility 参数和资格谓词 |

运行前先生成 Mapper：

```sh
./scripts/generate-go.sh
TEST_MIGRATION_DATABASE_URL='postgresql://USER:PASSWORD@localhost:5432/TEST_DATABASE?sslmode=disable' \
  go test ./internal/db -run '^TestTranscriptArchive' -count=1
```

测试有效性可用临时故障验证：分别将软删、硬删实现替换为零行成功，将候选会话、候选事件查询替换为空集，新测试必须因行为断言失败。验证结束立即恢复实现，不提交故障代码。这四种故障均已在本次补测时被测试捕获。

DB 区间覆盖检查并不代替对象内容校验。上层必须传入经过对象回读校验的序号；T4 接入归档任务后，每个删除批次均复查 terminal/boundary 资格，并验证“非终态”及“年龄不足”即使有 attached 段也不能软删。测试补强不改变这两个边界，也不改变终态整份归档的语义。

## Codec 编码边界

`EncodeRecord` 在拼入原始 payload 与 metadata 前，检查序列化头部是否具有预期后缀。若字段顺序或 JSON tag 的调整破坏该结构，编码立即返回 `ErrInvalidSegment`，不返回损坏的记录。正常记录的格式及解码行为保持不变；先生成 Mapper，再运行 `go test ./internal/transcriptarchive -count=1` 验证现有原始字节往返与完整性失败用例。

## Pending 恢复与对象读取

`TestTranscriptArchiveMissingObjectRecovery` 覆盖上传前失败、对象缺失时重试不删行、近期 pending 不回收、超龄 pending 经真实 cleanup worker 入队并清理、使用新 UUID/key 重建，以及 attached 段不被清理。`TestTranscriptArchiveReadsObjectsOncePerAttempt` 验证新段只读取一次外置 payload 和一次上传后的归档对象。已有上传后失败重试测试继续覆盖重新读取与校验路径。使用配置中的 PostgreSQL，执行 `go test ./tests -run TestTranscriptArchive -count=1`；每个 fixture 使用独立 schema。

## 删除索引、冷却与预算

`TestTranscriptDeletionIndexPlans` 在隔离 schema 中创建 20,001 行（100 行已软删除），对两个删除查询执行 EXPLAIN (ANALYZE, BUFFERS)，检查选择性数据下使用新增部分索引；该测试不代表生产数据的耗时承诺。`TestDeleteWorkerRepairCooldown` 检查包装错误分类、24 小时 snooze 和首次结构化告警；瞬时错误与成功路径保持原结果。`TestTranscriptArchiveResumeWithSmallerBudget` 覆盖上传后中断、降低预算后 pending/attached 段的多次分批恢复。

## Resume 与复活拒绝的非空覆盖

`TestTranscriptArchivePreservesIdleReclaimResume` 使用独立 schema：先创建边界前旧事件、compaction 边界和边界后事件，再 idle reclaim，通过边界模式归档。测试必须断言旧事件形成 attached 段且被软删除、两个可见事件仍存在，并比较归档前、归档后和新 sandbox 恢复后的 HTTP 字节；不能使用不具备 terminal 资格的会话证明归档安全。

`TestTranscriptArchiveTerminalSafety/terminated_to_running` 先设置过期 archived_at、idle worker 和过期租约，确认查询返回三个可归档事件，再切换 session 与 worker 到 running（租约仍过期，以隔离 worker 状态守卫），断言无归档、无删除。

运行 `go test ./tests -run '^TestTranscript' -count=1`；构造期非法策略与有效边界、worker 超时覆盖、周期调度不匹配/暂停修复分别由 `internal/transcriptretention` 和 `internal/riverjobs` 单测验证。真实 S3 下的 5 万行吞吐与重试次数仍需 staging 验证，本地 fake store 测试不构成性能承诺。
