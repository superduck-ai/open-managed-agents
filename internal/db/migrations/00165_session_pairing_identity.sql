-- +goose Up
alter table session_identity_bindings add column pairing_external_id text not null default '';

-- +goose Down
alter table session_identity_bindings drop column pairing_external_id;
