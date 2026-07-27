# 数据库标识符引用

- 表自身继续使用 `id bigint generated always as identity` 作为当前数据库内的内部主键，并使用唯一 `uuid` 作为稳定业务标识。
- 需要在整库恢复、租户搬迁、跨库合并或部分数据导入后保持含义的持久化跨表引用，使用被引用资源的 `*_uuid`，不要保存另一张表可重新生成的 identity 主键；例如 `filestore_filesystems` 与 `filestore_entries` 的租户、filesystem、会话和创建者归属都使用对应 UUID。
- `external_id` 服务于对外 API 兼容，不替代内部稳定 UUID；只有确实需要回显或兼容外部协议时才冗余保存。
- 尽量避免在表中引用外部表的 id 时使用 bigint 形式的 id，优先使用 uuid 类型的 id
- 把既有 bigint 引用迁移为 UUID 时，必须先通过源表回填并验证每一条引用均可解析；发现孤立引用应让 migration 失败，不能静默写成 `NULL`。
- PostgreSQL 迁移若需替换引用列，应通过按最终结构重建表来保持字段的语义顺序，不要把新 `*_uuid` 列简单追加到表尾。
