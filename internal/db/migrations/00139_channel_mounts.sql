-- +goose Up
alter table channels
	add column if not exists mounts jsonb not null default '{}'::jsonb,
	add column if not exists git_token_envelope jsonb;

-- +goose Down
alter table channels
	drop column if exists mounts,
	drop column if exists git_token_envelope;
