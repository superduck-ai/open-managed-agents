-- +goose Up
-- MCP OAuth pending flows stop storing plaintext client_secret / code_verifier.
-- Secrets live in a Secret envelope; platform client secrets are not persisted
-- (client_credential_source=platform) and are re-read from deploy config at
-- token exchange. See docs/design/be/vault-runtime.md.

alter table mcp_oauth_flows
    add column if not exists client_credential_source text not null default 'sealed',
    add column if not exists ciphertext bytea,
    add column if not exists nonce bytea,
    add column if not exists wrapped_dek bytea,
    add column if not exists format_version int,
    add column if not exists key_provider text,
    add column if not exists key_version bigint;

-- Pending plaintext rows cannot be opened after cutover; drop them with the
-- plaintext columns. Completed/failed rows already cleared secrets on update.
delete from mcp_oauth_flows where status = 'pending';

alter table mcp_oauth_flows
    drop column if exists client_secret,
    drop column if exists code_verifier;

alter table mcp_oauth_flows
    alter column client_credential_source drop default;

alter table mcp_oauth_flows
    drop constraint if exists mcp_oauth_flows_client_credential_source_check;

alter table mcp_oauth_flows
    add constraint mcp_oauth_flows_client_credential_source_check
        check (client_credential_source in ('platform', 'sealed'));

-- +goose Down

alter table mcp_oauth_flows
    drop constraint if exists mcp_oauth_flows_client_credential_source_check;

alter table mcp_oauth_flows
    add column if not exists client_secret text,
    add column if not exists code_verifier text not null default '';

alter table mcp_oauth_flows
    drop column if exists key_version,
    drop column if exists key_provider,
    drop column if exists format_version,
    drop column if exists wrapped_dek,
    drop column if exists nonce,
    drop column if exists ciphertext,
    drop column if exists client_credential_source;
