-- +goose Up
-- 明确该凭证是 Claude Code 使用的 OAuth-compatible token，而非通用 model API key。
alter table code_sessions
	rename column model_access_token_hash to oauth_access_token_hash;

alter table code_sessions
	rename column model_access_token_expires_at to oauth_access_token_expires_at;

alter table code_sessions
	rename constraint code_sessions_model_access_token_pair_check to code_sessions_oauth_access_token_pair_check;

alter index code_sessions_model_access_token_hash_key
	rename to code_sessions_oauth_access_token_hash_key;

-- +goose Down
alter index code_sessions_oauth_access_token_hash_key
	rename to code_sessions_model_access_token_hash_key;

alter table code_sessions
	rename constraint code_sessions_oauth_access_token_pair_check to code_sessions_model_access_token_pair_check;

alter table code_sessions
	rename column oauth_access_token_expires_at to model_access_token_expires_at;

alter table code_sessions
	rename column oauth_access_token_hash to model_access_token_hash;
