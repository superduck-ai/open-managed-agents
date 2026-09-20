# 私有 transcript 归档

本功能只处理 `code_session_internal_events`。不改变 ListPage、公开事件、SSE、NATS 或 resume 协议。

## 终态归档

独立 River sweep 发现 `archived_at` 或 `deleted_at` 已满 24 小时、全部 code session lease 已过期且 worker idle 的会话。NULL lease 表示没有活动租约；running/requires_action 均不允许归档。归档任务再次通过条件查询校验，软删除的每个批次也重新检查条件。

`DeleteSession` 只级联软删除公开事件等关联资源，没有处理私有 transcript；独立 sweep 覆盖已经删除的存量会话，修复这一无限保留问题。archive/delete API 写路径不挂归档逻辑。

段先注册 pending，再以唯一对象 key 上传，回读验证 size、SHA-256、行数和序号，并与原始行核对，然后 attached，最后分批软删除。每个删除事务最多 500 行，锁定 attached 注册表行并检查每个序号覆盖。不锁 code_sessions。注册段时以 code session UUID 派生的独立 advisory transaction lock 串行化区间冲突检查，不阻塞 worker append。

上传只尝试一次，重跑只读取已有对象，不覆盖同名 key。上传之前进程退出或上传失败留下的 pending 段，由现有 object cleanup worker 周期调度：超过 24 小时后，在同一事务中标为 deleting 并入队对象清理，释放区间后下一轮使用新 UUID 重建。清理删除对象的所有版本，attached 段不参与回收；上传完成但确认丢失时，重跑可直接校验并 attached。对象读取失败返回错误，不删除数据。

生产配置默认关闭且 dry-run，由 T8 组装；T4 的 Policy 由调用方显式传入。T5 使用硬删除相关字段，T6 开放 boundary sweep，T4 只执行终态归档。段按解压字节切分；超目标大小的单条事件独占段。序号空洞处额外分段，使区间不会覆盖其他 scope 的未归档事件。单任务有总行数上限，重试优先完成已有段的软删除。

## 验证

`TestTranscriptArchiveTerminalSafety` 覆盖非不可逆终态、未过期 lease、运行 worker 和不足静置期；`TestTranscriptArchiveTerminalRetry` 覆盖 dry-run、大 payload 合并和上传后中断恢复。生产统计 T0 经用户明确指示跳过，模式 B 收益和压缩比尚未测量。

同次执行中，新段复用已恢复的 payload；对象上传后只回读校验一次，校验结果用于 attached 和分批软删。重试仍重新读取并校验对象；每批删除仍由数据库重新检查资格。按字节切段所需的记录编码与最终段编码保持独立，未据此放宽 River 的任务超时。
