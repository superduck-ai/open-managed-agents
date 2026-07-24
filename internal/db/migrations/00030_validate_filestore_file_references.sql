-- +goose Up

-- Keep both historical-row scans out of 00029's column/constraint migration.
alter table filestore_entries
	validate constraint filestore_entries_blob_shape_check;

alter table filestore_entries
	validate constraint filestore_entries_file_reference_shape_check;

-- +goose Down

alter table filestore_entries
	drop constraint filestore_entries_file_reference_shape_check,
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
