-- +goose NO TRANSACTION

-- +goose Up
drop index concurrently if exists skills_workspace_display_title_active_key;

create unique index concurrently skills_workspace_display_title_active_key
	on skills (workspace_id, display_title)
	where deleted_at is null and display_title is not null;

-- +goose Down
drop index concurrently if exists skills_workspace_display_title_active_key;
