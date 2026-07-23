-- +goose Up

-- Filestore cleanup job 可能跨越备份恢复、租户迁移与数据库合并。
-- jobs.workspace_id 仍是通用任务表在当前库中的路由缓存，但 Filestore 任务的权威归属
-- 必须来自 payload 中的稳定 UUID，执行时再解析为本库 identity。
-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from jobs j
		left join workspaces w on w.id = j.workspace_id
		left join filestore_filesystems fs
			on fs.id::text = j.payload->>'filesystem_id'
			and fs.workspace_uuid = w.uuid
		where j.type in ('filestore_object_cleanup', 'filestore_filesystem_cleanup')
			and (
				w.id is null
				or fs.id is null
				or not (j.payload ? 'filesystem_id')
			)
	) then
		raise exception 'cannot migrate Filestore cleanup jobs to UUID: an internal reference cannot be resolved';
	end if;
end $$;
-- +goose StatementEnd

update jobs j
set payload = (j.payload - 'filesystem_id' - 'filesystem_external_id')
	|| jsonb_build_object(
		'workspace_uuid', w.uuid::text,
		'filesystem_uuid', fs.uuid::text
	)
from workspaces w
join filestore_filesystems fs on fs.workspace_uuid = w.uuid
where j.type in ('filestore_object_cleanup', 'filestore_filesystem_cleanup')
	and w.id = j.workspace_id
	and fs.id::text = j.payload->>'filesystem_id';

-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from jobs
		where type in ('filestore_object_cleanup', 'filestore_filesystem_cleanup')
			and (
				not (payload ? 'workspace_uuid')
				or not (payload ? 'filesystem_uuid')
				or payload ? 'filesystem_id'
			)
	) then
		raise exception 'Filestore cleanup job UUID migration left an incomplete payload';
	end if;
end $$;
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from jobs j
		left join workspaces w
			on w.uuid::text = j.payload->>'workspace_uuid'
		left join filestore_filesystems fs
			on fs.uuid::text = j.payload->>'filesystem_uuid'
			and fs.workspace_uuid = w.uuid
		where j.type in ('filestore_object_cleanup', 'filestore_filesystem_cleanup')
			and (
				w.id is null
				or fs.id is null
				or not (j.payload ? 'workspace_uuid')
				or not (j.payload ? 'filesystem_uuid')
			)
	) then
		raise exception 'cannot restore Filestore cleanup job internal references';
	end if;
end $$;
-- +goose StatementEnd

update jobs j
set workspace_id = w.id,
	payload = (j.payload - 'workspace_uuid' - 'filesystem_uuid')
		|| jsonb_build_object(
			'filesystem_id', fs.id,
			'filesystem_external_id', fs.external_id
		)
from workspaces w
join filestore_filesystems fs on fs.workspace_uuid = w.uuid
where j.type in ('filestore_object_cleanup', 'filestore_filesystem_cleanup')
	and w.uuid::text = j.payload->>'workspace_uuid'
	and fs.uuid::text = j.payload->>'filesystem_uuid';
