-- +goose Up

-- 活动 Skill Archive 必须仍能唯一解析到原 catalog version，才能无损固化成 File。
-- 已退休且无法解析的历史不参与读取，可以只保留 Resource 身份与路径。
-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from session_resources resource
		where resource.resource_type = 'skill_archive'
			and resource.deleted_at is null
			and (
				(select count(*)
				 from skill_versions version
				 where version.uuid = resource.skill_version_uuid
					and version.workspace_id = resource.workspace_id)
				+
				(select count(*)
				 from builtin_skill_versions version
				 where version.uuid = resource.skill_version_uuid)
			) <> 1
	) then
		raise exception 'cannot snapshot Session skills: an active Skill Archive Resource does not resolve to exactly one catalog version';
	end if;
end $$;
-- +goose StatementEnd

alter table session_resources
	drop constraint session_resources_namespace_shape_check;

drop index session_resources_skill_version_active_v1_key;

-- 为可解析的 custom 与 built-in Skill Archive 分配独立 File identity。
update session_resources resource
set file_uuid = gen_random_uuid()
where resource.resource_type = 'skill_archive'
	and resource.file_uuid is null
	and exists (
		select 1
		from skill_versions version
		where version.uuid = resource.skill_version_uuid
			and version.workspace_id = resource.workspace_id
	);

update session_resources resource
set file_uuid = gen_random_uuid()
where resource.resource_type = 'skill_archive'
	and resource.file_uuid is null
	and exists (
		select 1
		from builtin_skill_versions version
		where version.uuid = resource.skill_version_uuid
	);

-- Resource 与 File 各自保留独立 UUID；File 固化 ZIP 的对象位置与校验事实。
insert into files (
	uuid, external_id, workspace_id, filename, mime_type, detected_mime_type,
	size_bytes, metadata, authorization_metadata, tags, downloadable, md5, sha256,
	s3_bucket, s3_key, s3_etag, s3_version_id, scope_type, scope_id,
	created_by_api_key_id, created_at, deleted_at
)
select
	resource.file_uuid,
	concat('file_', replace(cast(gen_random_uuid() as text), '-', '')),
	resource.workspace_id,
	concat(regexp_replace(resource.path, '^.*/', ''), '.zip'),
	'application/zip',
	'application/zip',
	version.size_bytes,
	jsonb_build_object('skill_source', 'custom'),
	cast('{}' as jsonb),
	cast(array[] as text[]),
	false,
	null,
	lower(version.sha256),
	version.s3_bucket,
	version.s3_key,
	null,
	null,
	null,
	null,
	session.created_by_api_key_id,
	resource.created_at,
	resource.deleted_at
from session_resources resource
join sessions session
	on session.id = resource.session_id
	and session.workspace_id = resource.workspace_id
join skill_versions version
	on version.uuid = resource.skill_version_uuid
	and version.workspace_id = resource.workspace_id
where resource.resource_type = 'skill_archive'
	and resource.file_uuid is not null
union all
select
	resource.file_uuid,
	concat('file_', replace(cast(gen_random_uuid() as text), '-', '')),
	resource.workspace_id,
	concat(regexp_replace(resource.path, '^.*/', ''), '.zip'),
	'application/zip',
	'application/zip',
	version.size_bytes,
	jsonb_build_object('skill_source', 'anthropic'),
	cast('{}' as jsonb),
	cast(array[] as text[]),
	false,
	null,
	lower(version.sha256),
	version.s3_bucket,
	version.s3_key,
	null,
	null,
	null,
	null,
	session.created_by_api_key_id,
	resource.created_at,
	resource.deleted_at
from session_resources resource
join sessions session
	on session.id = resource.session_id
	and session.workspace_id = resource.workspace_id
join builtin_skill_versions version
	on version.uuid = resource.skill_version_uuid
where resource.resource_type = 'skill_archive'
	and resource.file_uuid is not null
	and not exists (
		select 1
		from skill_versions custom_version
		where custom_version.uuid = resource.skill_version_uuid
			and custom_version.workspace_id = resource.workspace_id
	);

alter table session_resources
	drop column skill_version_uuid;

-- Skill Archive 的 File 是 catalog 对象的 Session 快照，不属于 Filestore Owned File。
drop index session_resources_owned_file_uuid_v1_idx;

create index session_resources_owned_file_uuid_v1_idx
	on session_resources (workspace_id, file_uuid)
	where file_uuid is not null and payload is null and resource_type = 'file';

alter table session_resources
	add constraint session_resources_namespace_shape_check check (
		(
			path is null
			and parent_path is null
			and file_uuid is null
			and expires_at is null
		)
		or (
			path is not null
			and parent_path is not null
			and (
				(resource_type = 'file' and file_uuid is not null)
				or (resource_type = 'directory' and file_uuid is null and expires_at is null)
				or (
					resource_type = 'skill_archive'
					and expires_at is null
					and (file_uuid is not null or deleted_at is not null)
				)
			)
		)
	) not valid;

alter table session_resources
	validate constraint session_resources_namespace_shape_check;

-- +goose Down

-- File 快照无法无损还原为唯一的 catalog version UUID。
select 1;
