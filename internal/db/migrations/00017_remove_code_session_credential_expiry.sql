-- +goose Up

alter table code_sessions
    drop constraint if exists code_sessions_oauth_access_token_pair_check;

alter table code_sessions
    drop column if exists oauth_access_token_expires_at;

-- +goose Down

alter table code_sessions
    add column oauth_access_token_expires_at timestamptz;

update code_sessions
set oauth_access_token_expires_at = now() + interval '8 hours'
where oauth_access_token_hash is not null;

alter table code_sessions
    add constraint code_sessions_oauth_access_token_pair_check check (
        (oauth_access_token_hash is null and oauth_access_token_expires_at is null)
        or (oauth_access_token_hash is not null and oauth_access_token_expires_at is not null)
    );
