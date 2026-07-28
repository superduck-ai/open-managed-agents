-- +goose Up
-- 旧默认镜像没有 provision-packages。只迁移仍使用旧默认值、且确实配置了
-- 至少一个 Package 的 Cloud Environment；空 Packages 和自定义 Template 保持不变。
-- 从本迁移起 managed-agent-sandbox 是 provider-neutral 的逻辑默认值；
-- runtime Resolve 会用当前部署的 e2b.template 物化它。00034 中“原样透传”的注释
-- 仅描述当时的历史行为，不再代表迁移后默认值的解析语义。
update environments
set resolved_template = 'managed-agent-sandbox',
	updated_at = now()
where resolved_template = 'claude-code-interpreter'
	and config ->> 'type' = 'cloud'
	and jsonb_typeof(config -> 'packages') = 'object'
	and exists (
		select 1
		from jsonb_each(config -> 'packages') as package_manager(name, specs)
		where package_manager.name in ('apt', 'cargo', 'gem', 'go', 'npm', 'pip')
			and case
				when jsonb_typeof(package_manager.specs) = 'array'
					then jsonb_array_length(package_manager.specs)
				else 0
			end > 0
	);

-- +goose Down
-- 数据迁移不可逆：无法区分本迁移更新的记录与之后显式选择新 Template 的记录。
select 1;
