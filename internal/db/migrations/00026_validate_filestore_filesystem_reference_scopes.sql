-- +goose Up

-- 早期 UUID 回填只核对了旧主键与 external_id，没有证明会话、Code Session
-- 和 API Key 同属文件系统的组织与工作区。本迁移在最终 UUID schema 上补齐校验；
-- 一旦发现跨租户或跨会话引用便中止部署，避免把错误归属继续带入备份与迁移。
-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from filestore_filesystems fs
		left join organizations o on o.uuid = fs.organization_uuid
		left join workspaces w on w.uuid = fs.workspace_uuid
		left join sessions s on s.uuid = fs.session_uuid
		left join code_sessions cs on cs.uuid = fs.code_session_uuid
		left join api_keys ak on ak.uuid = fs.created_by_api_key_uuid
		where o.id is null
			or w.id is null
			or w.organization_id <> o.id
			or s.id is null
			or s.organization_id <> o.id
			or s.workspace_id <> w.id
			or (
				fs.code_session_uuid is not null
				and (
					cs.id is null
					or cs.organization_id <> o.id
					or cs.workspace_id <> w.id
					or cs.session_id <> s.id
				)
			)
			or (
				fs.created_by_api_key_uuid is not null
				and (ak.id is null or ak.workspace_id <> w.id)
			)
	) then
		raise exception 'Filestore filesystem UUID references cross a tenant or session boundary';
	end if;
end $$;
-- +goose StatementEnd

-- +goose Down

-- 本迁移只验证已有数据，没有可回滚的 schema 变化。
