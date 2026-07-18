-- +goose Up
alter table environments
	alter column resolved_template set default 'managed-agent-sandbox:latest';

-- +goose Down
alter table environments
	alter column resolved_template set default 'claude-code-interpreter';
