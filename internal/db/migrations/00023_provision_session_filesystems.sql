-- +goose Up

-- public session 是 Filestore filesystem 的稳定归属；同一会话只能保留一个活动命名空间。
-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from filestore_filesystems
		where deleted_at is null
		group by workspace_uuid, session_uuid
		having count(*) > 1
	) then
		raise exception 'cannot enforce one active Filestore filesystem per session: duplicate rows exist';
	end if;
end $$;
-- +goose StatementEnd

drop index if exists filestore_filesystems_workspace_session_active_v3_idx;
create unique index filestore_filesystems_workspace_session_active_v4_key
	on filestore_filesystems (workspace_uuid, session_uuid)
	where deleted_at is null;

-- 历史会话也需要满足相同归属不变量。迁移使用与 Go 生成器一致的 Base62 拒绝采样，
-- 并由既有 (workspace_uuid, external_id) 唯一约束处理极低概率的碰撞。
-- +goose StatementBegin
do $$
declare
	alphabet constant text := '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz';
	candidate text;
	random_byte integer;
	suffix text;
	session_record record;
	attempt integer;
begin
	if exists (
		select 1
		from sessions s
		left join organizations o on o.id = s.organization_id
		left join workspaces w
			on w.id = s.workspace_id
			and w.organization_id = s.organization_id
		left join api_keys ak
			on ak.id = s.created_by_api_key_id
			and ak.workspace_id = s.workspace_id
		where s.deleted_at is null
			and (o.id is null or w.id is null)
	) then
		raise exception 'cannot backfill Filestore filesystems: an active session has invalid tenant references';
	end if;

	for session_record in
		select
			s.uuid as session_uuid,
			o.uuid as organization_uuid,
			w.uuid as workspace_uuid,
			ak.uuid as api_key_uuid
		from sessions s
		join organizations o on o.id = s.organization_id
		join workspaces w
			on w.id = s.workspace_id
			and w.organization_id = s.organization_id
		left join api_keys ak
			on ak.id = s.created_by_api_key_id
			and ak.workspace_id = s.workspace_id
		where s.deleted_at is null
			and not exists (
				select 1
				from filestore_filesystems fs
				where fs.workspace_uuid = w.uuid
					and fs.session_uuid = s.uuid
					and fs.deleted_at is null
			)
	loop
		<<candidate_loop>>
		for attempt in 1..3 loop
			suffix := '';
			while char_length(suffix) < 24 loop
				random_byte := get_byte(gen_random_bytes(1), 0);
				if random_byte < 248 then
					suffix := suffix || substr(alphabet, (random_byte % 62) + 1, 1);
				end if;
			end loop;
			candidate := 'claude_chat_' || suffix;

			begin
				insert into filestore_filesystems (
					external_id, organization_uuid, workspace_uuid, session_uuid,
					code_session_uuid, created_by_api_key_uuid
				)
				values (
					candidate, session_record.organization_uuid, session_record.workspace_uuid,
					session_record.session_uuid, null, session_record.api_key_uuid
				);
				exit candidate_loop;
			exception when unique_violation then
				-- 并行启动的新版本可能已经完成建档；这时现有记录就是期望结果。
				if exists (
					select 1
					from filestore_filesystems fs
					where fs.workspace_uuid = session_record.workspace_uuid
						and fs.session_uuid = session_record.session_uuid
						and fs.deleted_at is null
				) then
					exit candidate_loop;
				end if;
				if attempt = 3 then
					raise exception 'could not generate a unique Filestore filesystem ID after 3 attempts';
				end if;
			end;
		end loop;
	end loop;
end $$;
-- +goose StatementEnd

-- +goose Down

drop index if exists filestore_filesystems_workspace_session_active_v4_key;
create index filestore_filesystems_workspace_session_active_v3_idx
	on filestore_filesystems (workspace_uuid, session_uuid)
	where deleted_at is null;
