-- +goose Up

-- /skills 是只读的 archive namespace。迁移必须先确认历史数据没有占用这个
-- subtree，避免把用户可写的普通 entry 静默解释成受服务管理的 skill archive。
-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from filestore_entries
		where deleted_at is null
			and (
				path like '/skills/%'
				or (
					path = '/skills'
					and (
						kind <> 'directory'
						or parent_path <> '/'
						or managed_by is not null
						or managed_resource_uuid is not null
						or source_file_uuid is not null
					)
				)
			)
	) then
		raise exception 'cannot initialize reserved /skills namespace over existing entries';
	end if;
end
$$;
-- +goose StatementEnd

-- archive 与 file、directory 共用 filestore_entries 生命周期和租户边界。
-- NOT VALID 避免本次短事务在持有 AccessExclusive 锁时扫描历史 rows；
-- PostgreSQL 仍会立即校验迁移后的新写入，00033 再以较弱锁验证存量数据。
alter table filestore_entries
	drop constraint filestore_entries_kind_check,
	add constraint filestore_entries_kind_check check (
		kind in ('file', 'directory', 'archive')
	) not valid;

alter table filestore_entries
	drop constraint filestore_entries_blob_shape_check,
	add constraint filestore_entries_blob_shape_check check (
		(
			kind = 'directory'
			and source_file_uuid is null
			and size_bytes is null
			and media_type is null
			and detected_mime_type is null
			and md5 is null
			and sha256 is null
			and s3_bucket is null
			and s3_key is null
			and s3_etag is null
			and s3_version_id is null
			and expires_at is null
		)
		or (
			kind = 'file'
			and size_bytes is not null
			and size_bytes >= 0
			and media_type is not null
			and (
				source_file_uuid is not null
				or (
					md5 is not null
					and char_length(md5) > 0
				)
			)
			and sha256 is not null
			and char_length(sha256) = 64
			and s3_bucket is not null
			and char_length(s3_bucket) > 0
			and s3_key is not null
			and char_length(s3_key) > 0
		)
		or (
			kind = 'archive'
			and source_file_uuid is null
			and size_bytes is not null
			and size_bytes > 0
			and media_type = 'application/zip'
			and detected_mime_type = 'application/zip'
			and md5 is null
			and sha256 ~ '^[0-9a-f]{64}$'
			and s3_bucket is not null
			and char_length(s3_bucket) > 0
			and s3_key is not null
			and char_length(s3_key) > 0
			and s3_etag is null
			and s3_version_id is null
			and expires_at is null
		)
	) not valid;

-- archive entry 必须位于 /skills 的直接子级，并绑定具体 catalog version。
-- 其他 kind 不能伪装成 skill_archive，防止普通 Filestore mutation 越过只读边界。
alter table filestore_entries
	add constraint filestore_entries_archive_shape_check check (
		(
			kind = 'archive'
			and path ~ '^/skills/[^/]+$'
			and parent_path = '/skills'
			and managed_by = 'skill_archive'
			and managed_resource_uuid is not null
			and downloadable = false
		)
		or (
			kind <> 'archive'
			and managed_by is distinct from 'skill_archive'
		)
	) not valid;

-- 同一 filesystem 内，一个具体 skill version 最多只能有一条活动投影；
-- deleted_at 不为空的历史投影保留用于审计，不阻止后续重新挂载。
create unique index filestore_entries_skill_archive_active_v1_key
	on filestore_entries (
		workspace_uuid,
		filesystem_uuid,
		managed_resource_uuid
	)
	where deleted_at is null
		and kind = 'archive'
		and managed_by = 'skill_archive';

-- 历史 filesystem 没有 /skills 根目录。archive 成员只在读取时从 zip central
-- directory 合成，因此数据库只需要根目录和每个 archive 的一条 entry。
insert into filestore_entries (
	uuid,
	external_id,
	organization_uuid,
	workspace_uuid,
	filesystem_uuid,
	kind,
	path,
	parent_path,
	created_by_api_key_uuid,
	created_by_session_uuid,
	created_by_code_session_uuid,
	created_at,
	updated_at
)
select
	gen_random_uuid(),
	concat('fse_', replace(cast(gen_random_uuid() as text), '-', '')),
	fs.organization_uuid,
	fs.workspace_uuid,
	fs.uuid,
	'directory',
	'/skills',
	'/',
	fs.created_by_api_key_uuid,
	fs.session_uuid,
	fs.code_session_uuid,
	fs.created_at,
	now()
from filestore_filesystems fs
where fs.deleted_at is null
on conflict (workspace_uuid, filesystem_uuid, path)
	where deleted_at is null
	do nothing;

-- 虚拟只读视图启用后，旧 prewarm worker 已经移除，遗留 job 不再可执行。
delete from jobs where type = 'skill_prewarm';

-- +goose Down

-- kind 约束恢复前必须移除所有 archive 投影，包括已经软删除的历史 rows。
-- 这些 rows 只借用 catalog 对象；删除投影不会删除或回收 zip。
delete from filestore_entries
where kind = 'archive';

drop index filestore_entries_skill_archive_active_v1_key;

alter table filestore_entries
	drop constraint filestore_entries_archive_shape_check,
	drop constraint filestore_entries_blob_shape_check,
	drop constraint filestore_entries_kind_check;

alter table filestore_entries
	add constraint filestore_entries_kind_check check (
		kind in ('file', 'directory')
	),
	add constraint filestore_entries_blob_shape_check check (
		(
			kind = 'directory'
			and source_file_uuid is null
			and size_bytes is null
			and media_type is null
			and detected_mime_type is null
			and md5 is null
			and sha256 is null
			and s3_bucket is null
			and s3_key is null
			and s3_etag is null
			and s3_version_id is null
			and expires_at is null
		)
		or (
			kind = 'file'
			and size_bytes is not null
			and size_bytes >= 0
			and media_type is not null
			and (
				source_file_uuid is not null
				or (
					md5 is not null
					and char_length(md5) > 0
				)
			)
			and sha256 is not null
			and char_length(sha256) = 64
			and s3_bucket is not null
			and char_length(s3_bucket) > 0
			and s3_key is not null
			and char_length(s3_key) > 0
		)
	);

-- 无法区分迁移创建的 /skills 与迁移前已经存在的同名空目录，因此回滚时保留
-- 目录 row，避免误删用户数据。旧版本服务会把它当作普通空目录。
