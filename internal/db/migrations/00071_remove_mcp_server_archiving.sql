-- +goose Up

alter table mcp_servers
    drop column if exists archived_at;

-- +goose Down

alter table mcp_servers
    add column if not exists archived_at timestamptz;
