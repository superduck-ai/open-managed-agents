-- +goose Up
-- Direct cutover: vault credential secrets move from plaintext secret_payload
-- jsonb to envelope encryption in one step. A one-time DEK wraps each secret
-- (AES-256-GCM), and the DEK is wrapped again by the configured KEK.
-- Pre-existing plaintext is discarded with the dropped column; there is no
-- Expand/Backfill dual-read window. Envelope completeness and active/archived
-- lifecycle rules are enforced in application write paths, not CHECK
-- constraints. See docs/design/be/vault-runtime.md and CONTEXT.md.
--
-- IF NOT EXISTS / IF EXISTS keep this safe when an earlier local migration
-- number already added the envelope columns (renumbered from 00047 -> 00049).

alter table vault_credentials
    add column if not exists ciphertext bytea,
    add column if not exists nonce bytea,
    add column if not exists wrapped_dek bytea,
    add column if not exists format_version int,
    add column if not exists key_provider text,
    add column if not exists key_version bigint,
    add column if not exists version bigint not null default 0;

alter table vault_credentials
    drop column if exists secret_payload;

-- +goose Down

alter table vault_credentials
    add column if not exists secret_payload jsonb;

alter table vault_credentials
    drop column if exists version,
    drop column if exists key_version,
    drop column if exists key_provider,
    drop column if exists format_version,
    drop column if exists wrapped_dek,
    drop column if exists nonce,
    drop column if exists ciphertext;
