-- +goose Up

create index if not exists evaluation_experiment_items_session_idx
	on evaluation_experiment_items (organization_uuid, workspace_uuid, session_external_id);

-- +goose Down

drop index if exists evaluation_experiment_items_session_idx;
