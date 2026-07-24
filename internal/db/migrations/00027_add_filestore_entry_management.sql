-- +goose Up

-- Session File resource 对应的命名空间 entry 需要一个不能由 Filestore
-- HTTP metadata 伪造、也不会被普通 Copy 操作继承的数据库 ownership 边界。
alter table filestore_entries
	add column managed_by text,
	add column managed_resource_uuid uuid;

-- NOT VALID 避免在持有 AccessExclusive 锁的本次短事务中扫描历史行；
-- 后续 migration 使用较弱锁单独校验。
alter table filestore_entries
	add constraint filestore_entries_management_shape_check check (
		(
			managed_by is null
			and managed_resource_uuid is null
		)
		or (
			managed_by is not null
			and managed_by <> ''
			and managed_resource_uuid is not null
		)
	) not valid;

-- +goose Down

alter table filestore_entries
	drop constraint filestore_entries_management_shape_check,
	drop column managed_resource_uuid,
	drop column managed_by;
