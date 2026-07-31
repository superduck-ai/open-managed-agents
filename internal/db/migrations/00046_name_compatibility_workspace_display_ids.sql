-- +goose Up

alter table console_api_keys
	rename column workspace_id to workspace_display_id;

alter table workbench_prompts
	rename column workspace_id to workspace_display_id;

-- +goose Down

alter table workbench_prompts
	rename column workspace_display_id to workspace_id;

alter table console_api_keys
	rename column workspace_display_id to workspace_id;
