-- +goose Up

-- 用量账本是配额判定的事务型投影，不是独立业务资源，因此只保留工作区内部主键。
-- 项目不使用外键；资源写入与账本增量由同一数据库事务维护一致性。
create table if not exists workspace_storage_usage (
	workspace_id bigint not null,
	files_bytes bigint not null default 0,
	filestore_bytes bigint not null default 0,
	updated_at timestamptz not null default now(),
	constraint workspace_storage_usage_pk primary key (workspace_id),
	constraint workspace_storage_usage_files_bytes_check check (files_bytes >= 0),
	constraint workspace_storage_usage_filestore_bytes_check check (filestore_bytes >= 0)
);

-- 迁移时建立一次准确基线。此后正常请求只读写单个账本行，不再实时聚合全部文件。
-- Filestore 到期文件在 TTL 清理事务完成前仍占用额度，因此这里统计全部未软删除文件。
insert into workspace_storage_usage (workspace_id, files_bytes, filestore_bytes, updated_at)
select
	w.id,
	coalesce(files.total_bytes, 0),
	coalesce(filestore.total_bytes, 0),
	now()
from workspaces w
left join (
	select workspace_id, sum(size_bytes) as total_bytes
	from files
	where deleted_at is null
	group by workspace_id
) files on files.workspace_id = w.id
left join (
	select workspace_id, sum(size_bytes) as total_bytes
	from filestore_entries
	where kind = 'file' and deleted_at is null
	group by workspace_id
) filestore on filestore.workspace_id = w.id
on conflict (workspace_id) do update set
	files_bytes = excluded.files_bytes,
	filestore_bytes = excluded.filestore_bytes,
	updated_at = excluded.updated_at;

-- +goose Down

drop table if exists workspace_storage_usage;
