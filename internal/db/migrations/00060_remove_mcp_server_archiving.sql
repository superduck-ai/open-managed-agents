-- +goose Up

alter table mcp_servers
    drop column archived_at;

-- +goose Down

alter table mcp_servers
    add column archived_at timestamptz;
