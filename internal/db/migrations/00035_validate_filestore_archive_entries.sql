-- +goose Up

alter table filestore_entries
	validate constraint filestore_entries_kind_check;

alter table filestore_entries
	validate constraint filestore_entries_blob_shape_check;

alter table filestore_entries
	validate constraint filestore_entries_archive_shape_check;

-- +goose Down

select 1;
