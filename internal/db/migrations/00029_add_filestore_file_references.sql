-- +goose Up

-- Session File resources borrow an existing Files API object. The source UUID
-- makes that ownership distinction explicit so quota and cleanup code never
-- treats the borrowed object as Filestore-owned data.
alter table filestore_entries
	add column source_file_uuid uuid;

alter table filestore_entries
	drop constraint filestore_entries_blob_shape_check;

alter table filestore_entries
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
	) not valid;

alter table filestore_entries
	add constraint filestore_entries_file_reference_shape_check check (
		(
			source_file_uuid is null
			and managed_by is distinct from 'session_file_resource'
		)
		or (
			source_file_uuid is not null
			and kind = 'file'
			and managed_by = 'session_file_resource'
			and managed_resource_uuid is not null
			and expires_at is null
		)
	) not valid;

create index filestore_entries_source_file_active_v1_idx
	on filestore_entries (workspace_uuid, source_file_uuid)
	where deleted_at is null and source_file_uuid is not null;

create unique index filestore_entries_session_resource_active_v1_key
	on filestore_entries (
		workspace_uuid,
		filesystem_uuid,
		managed_resource_uuid
	)
	where deleted_at is null
		and source_file_uuid is not null
		and managed_by = 'session_file_resource';

-- Existing active Session filesystems also receive the fixed namespace roots.
-- New filesystems create the same rows in their Session transaction.
-- Refuse to reinterpret a non-root or managed entry as a fixed root.
-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from filestore_filesystems fs
		join filestore_entries entry
			on entry.workspace_uuid = fs.workspace_uuid
			and entry.filesystem_uuid = fs.uuid
			and entry.deleted_at is null
		where fs.deleted_at is null
			and entry.path in (
				'/outputs',
				'/uploads',
				'/transcripts',
				'/tool_results'
			)
			and (
				entry.kind <> 'directory'
				or entry.parent_path <> '/'
				or entry.managed_by is not null
				or entry.managed_resource_uuid is not null
				or entry.source_file_uuid is not null
			)
	) then
		raise exception 'cannot initialize fixed filestore roots over existing non-root entries';
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
	root.path,
	'/',
	fs.created_by_api_key_uuid,
	fs.session_uuid,
	fs.code_session_uuid,
	fs.created_at,
	now()
from filestore_filesystems fs
cross join (
	values
		('/outputs'),
		('/uploads'),
		('/transcripts'),
		('/tool_results')
) as root(path)
where fs.deleted_at is null
on conflict (workspace_uuid, filesystem_uuid, path)
	where deleted_at is null
	do nothing;

-- +goose Down

-- Downgrading while borrowed references remain would either violate the former
-- MD5 contract or erase their ownership semantics, so fail explicitly.
-- Fixed directory rows remain because they satisfy the old schema and migration
-- cannot distinguish a pre-existing root from one it backfilled without adding
-- a persistent ownership marker; deleting either set would lose user data.
-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from filestore_entries
		where source_file_uuid is not null
	) then
		raise exception 'cannot remove filestore file-reference support while references exist';
	end if;
end
$$;
-- +goose StatementEnd

drop index filestore_entries_session_resource_active_v1_key;
drop index filestore_entries_source_file_active_v1_idx;

alter table filestore_entries
	drop constraint filestore_entries_file_reference_shape_check,
	drop constraint filestore_entries_blob_shape_check,
	drop column source_file_uuid;

alter table filestore_entries
	add constraint filestore_entries_blob_shape_check check (
		(
			kind = 'directory'
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
			and md5 is not null
			and char_length(md5) > 0
			and sha256 is not null
			and char_length(sha256) = 64
			and s3_bucket is not null
			and char_length(s3_bucket) > 0
			and s3_key is not null
			and char_length(s3_key) > 0
		)
	);
