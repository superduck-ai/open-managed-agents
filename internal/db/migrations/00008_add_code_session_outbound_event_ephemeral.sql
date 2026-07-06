-- +goose Up
alter table if exists code_session_outbound_events
	add column if not exists ephemeral boolean not null default false;

-- +goose Down
alter table if exists code_session_outbound_events
	drop column if exists ephemeral;
