-- +goose NO TRANSACTION

-- +goose Up
alter table skills
	alter column display_title set not null;

drop index concurrently if exists skills_workspace_display_title_active_key;

create unique index concurrently skills_workspace_display_title_active_key
	on skills (workspace_id, display_title)
	where deleted_at is null;

-- +goose Down
drop index concurrently if exists skills_workspace_display_title_active_key;

alter table skills
	alter column display_title drop not null;

create unique index concurrently skills_workspace_display_title_active_key
	on skills (workspace_id, display_title)
	where deleted_at is null and display_title is not null;
