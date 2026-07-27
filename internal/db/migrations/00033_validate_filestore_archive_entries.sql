-- +goose Up

-- 与 00032 的约束替换事务分开，存量扫描只取得 ShareUpdateExclusive 锁。
alter table filestore_entries
	validate constraint filestore_entries_kind_check;

alter table filestore_entries
	validate constraint filestore_entries_blob_shape_check;

alter table filestore_entries
	validate constraint filestore_entries_archive_shape_check;

-- +goose Down

-- PostgreSQL 不能把已验证约束直接改回 NOT VALID；降级时重建三个约束，
-- 使只回退本 migration 也准确恢复到 00032 的状态。
alter table filestore_entries
	drop constraint filestore_entries_archive_shape_check,
	drop constraint filestore_entries_blob_shape_check,
	drop constraint filestore_entries_kind_check;

alter table filestore_entries
	add constraint filestore_entries_kind_check check (
		kind in ('file', 'directory', 'archive')
	) not valid;

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
