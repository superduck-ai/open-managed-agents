-- +goose Up
alter table environments
	alter column resolved_template set default 'managed-agent-sandbox';

update environments
set resolved_template = 'managed-agent-sandbox',
	updated_at = now()
where resolved_template = 'managed-agent-sandbox:latest';

-- +goose Down
update environments
set resolved_template = 'managed-agent-sandbox:latest',
	updated_at = now()
where resolved_template = 'managed-agent-sandbox';

alter table environments
	alter column resolved_template set default 'managed-agent-sandbox:latest';
