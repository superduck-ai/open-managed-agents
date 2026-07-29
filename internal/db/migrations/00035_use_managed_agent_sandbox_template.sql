-- +goose Up
alter table environments
	alter column resolved_template set default 'managed-agent-sandbox';

update environments
set resolved_template = 'managed-agent-sandbox',
	updated_at = now()
where resolved_template = 'claude-code-interpreter';

-- +goose Down
alter table environments
	alter column resolved_template set default 'claude-code-interpreter';

-- Up 迁移后无法区分由旧默认值迁移的记录与用户显式选择裸 template 名称的记录，
-- 因此回滚只恢复列默认值，不改写已有 Environment。
