-- +goose Up
alter table if exists workspaces drop column if exists data_residency;

-- +goose Down
alter table if exists workspaces add column if not exists data_residency jsonb not null default '{"workspace_geo":"us","allowed_inference_geos":"unrestricted","default_inference_geo":"global"}'::jsonb;
