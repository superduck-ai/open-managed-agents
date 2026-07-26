-- +goose Up

-- Managed Agent skills borrow immutable catalog archives. Filestore exposes
-- each archive as a virtual directory without copying its members into S3.
create table filestore_skill_archives (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	filesystem_uuid uuid not null,
	source text not null,
	skill_version_uuid uuid not null,
	virtual_path text not null,
	s3_bucket text not null,
	s3_key text not null,
	size_bytes bigint not null,
	sha256 text not null,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint filestore_skill_archives_id_pk primary key (id),
	constraint filestore_skill_archives_uuid_key unique (uuid),
	constraint filestore_skill_archives_external_id_key unique (external_id),
	constraint filestore_skill_archives_source_check check (source in ('anthropic', 'custom')),
	constraint filestore_skill_archives_virtual_path_check check (
		virtual_path ~ '^/skills/[^/]+$'
		and octet_length(virtual_path) <= 4096
	),
	constraint filestore_skill_archives_object_check check (
		char_length(s3_bucket) > 0
		and char_length(s3_key) > 0
		and size_bytes > 0
		and char_length(sha256) = 64
	)
);

create unique index filestore_skill_archives_filesystem_path_key
	on filestore_skill_archives (workspace_uuid, filesystem_uuid, virtual_path);

create unique index filestore_skill_archives_filesystem_version_key
	on filestore_skill_archives (workspace_uuid, filesystem_uuid, source, skill_version_uuid);

-- Refuse to reinterpret an existing user-controlled subtree as the reserved,
-- read-only skill namespace.
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

-- Old prewarm work is no longer executable after the virtual view is enabled.
delete from jobs where type = 'skill_prewarm';

-- +goose Down

drop table filestore_skill_archives;

delete from filestore_entries
where path = '/skills'
	and kind = 'directory'
	and deleted_at is null
	and not exists (
		select 1
		from filestore_entries child
		where child.workspace_uuid = filestore_entries.workspace_uuid
			and child.filesystem_uuid = filestore_entries.filesystem_uuid
			and child.deleted_at is null
			and child.path like '/skills/%'
	);
