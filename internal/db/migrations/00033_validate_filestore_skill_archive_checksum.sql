-- +goose Up

alter table filestore_skill_archives
	drop constraint filestore_skill_archives_object_check,
	add constraint filestore_skill_archives_object_check check (
		char_length(s3_bucket) > 0
		and char_length(s3_key) > 0
		and size_bytes > 0
		and sha256 ~ '^[0-9a-f]{64}$'
	);

-- +goose Down

alter table filestore_skill_archives
	drop constraint filestore_skill_archives_object_check,
	add constraint filestore_skill_archives_object_check check (
		char_length(s3_bucket) > 0
		and char_length(s3_key) > 0
		and size_bytes > 0
		and char_length(sha256) = 64
	);
