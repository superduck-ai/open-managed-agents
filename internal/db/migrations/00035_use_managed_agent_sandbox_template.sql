-- +goose Up
alter table environments
	alter column resolved_template set default 'managed-agent-sandbox';

-- +goose Down
alter table environments
	alter column resolved_template set default 'claude-code-interpreter';
