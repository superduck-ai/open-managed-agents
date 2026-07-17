-- +goose Up
-- 只保存 bearer token 的 SHA-256 hash 与截止时间，明文不会进入数据库。
alter table code_sessions
	add column model_access_token_hash text,
	add column model_access_token_expires_at timestamptz;

alter table code_sessions
	add constraint code_sessions_model_access_token_pair_check
	check ((model_access_token_hash is null) = (model_access_token_expires_at is null));

create unique index code_sessions_model_access_token_hash_key
	on code_sessions (model_access_token_hash)
	-- 软删除记录不占用 token hash；active 查询仍会额外检查状态和 expiry。
	where model_access_token_hash is not null and deleted_at is null;

-- +goose Down
drop index if exists code_sessions_model_access_token_hash_key;

alter table code_sessions
	drop constraint if exists code_sessions_model_access_token_pair_check,
	drop column if exists model_access_token_expires_at,
	drop column if exists model_access_token_hash;
