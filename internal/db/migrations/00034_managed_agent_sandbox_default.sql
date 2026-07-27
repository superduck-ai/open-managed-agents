-- +goose Up
-- 裸 Template 名称原样传给 provider：本地 e2b-local 解析为 managed-agent-sandbox:latest，
-- Hosted E2B 解析为 managed-agent-sandbox:default。OMA 不增加别名解析层。
-- 已有 Environment 保留各自的 resolved_template，只有之后创建的记录使用新默认值。
alter table environments
	alter column resolved_template set default 'managed-agent-sandbox';

-- +goose Down
alter table environments
	alter column resolved_template set default 'claude-code-interpreter';
