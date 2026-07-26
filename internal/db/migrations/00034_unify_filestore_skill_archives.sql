-- +goose Up

-- Skill archives are Filestore namespace entries backed by immutable catalog
-- zip objects. Historical rows in the old projection table are intentionally
-- discarded instead of migrated.
drop table filestore_skill_archives;

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

create unique index filestore_entries_skill_archive_active_v1_key
	on filestore_entries (
		workspace_uuid,
		filesystem_uuid,
		managed_resource_uuid
	)
	where deleted_at is null
		and kind = 'archive'
		and managed_by = 'skill_archive';

-- +goose Down

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
	constraint filestore_skill_archives_source_check check (
		source in ('anthropic', 'custom')
	),
	constraint filestore_skill_archives_virtual_path_check check (
		virtual_path ~ '^/skills/[^/]+$'
		and octet_length(virtual_path) <= 4096
	),
	constraint filestore_skill_archives_object_check check (
		char_length(s3_bucket) > 0
		and char_length(s3_key) > 0
		and size_bytes > 0
		and sha256 ~ '^[0-9a-f]{64}$'
	)
);

create unique index filestore_skill_archives_filesystem_path_key
	on filestore_skill_archives (
		workspace_uuid,
		filesystem_uuid,
		virtual_path
	);

create unique index filestore_skill_archives_filesystem_version_key
	on filestore_skill_archives (
		workspace_uuid,
		filesystem_uuid,
		source,
		skill_version_uuid
	);
