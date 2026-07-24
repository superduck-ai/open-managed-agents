-- +goose Up

-- 与添加列和 NOT VALID 约束的短事务分开，历史行扫描只取得
-- ShareUpdateExclusive 锁；失败时 00027 已有完整 migration 版本记录。
alter table filestore_entries
	validate constraint filestore_entries_management_shape_check;

-- +goose Down

-- PostgreSQL 没有把已验证约束直接改回 NOT VALID 的语法；降级时重建
-- 同一约束，使只回退本 migration 也恢复到 00027 的状态。
alter table filestore_entries
	drop constraint filestore_entries_management_shape_check;

alter table filestore_entries
	add constraint filestore_entries_management_shape_check check (
		(
			managed_by is null
			and managed_resource_external_id is null
		)
		or (
			managed_by is not null
			and managed_by <> ''
			and managed_resource_external_id is not null
			and managed_resource_external_id <> ''
		)
	) not valid;
