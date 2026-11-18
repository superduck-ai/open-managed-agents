-- +goose Up
alter table channels
	add column identity_resolution_mode text,
	add column identity_uuid uuid,
	add column execution_revision bigint not null default 1,
	add column connection_status text not null default 'unknown',
	add column entry_closed boolean not null default false,
	add constraint channels_identity_resolution_mode_check check (identity_resolution_mode in ('fixed', 'pairing')),
	add constraint channels_connection_status_check check (connection_status in ('unknown', 'healthy', 'disconnected'));

-- +goose StatementBegin
do $$
declare
	version_differences bigint;
begin
	if exists (
		select 1 from channel_execution_contexts x
		left join channels c on c.uuid = x.channel_uuid
			and c.external_id = x.channel_external_id
			and c.organization_uuid = x.organization_uuid
			and c.workspace_uuid = x.workspace_uuid
		where c.uuid is null
	) then
		raise exception '渠道执行上下文存在无法归属的记录';
	end if;

	select count(*) into version_differences
	from channel_execution_contexts x join channels c on c.uuid = x.channel_uuid
	where x.mode = 'fixed' and (c.mounts ->> 'agent_version') is distinct from x.agent_version::text;
	raise notice '固定渠道 Agent 版本分歧数：%；保留迁移前实际执行版本', version_differences;
end $$;
-- +goose StatementEnd

update channels c set
	identity_resolution_mode = x.mode,
	identity_uuid = x.identity_uuid,
	execution_revision = x.revision,
	connection_status = x.connection_status,
	entry_closed = x.entry_closed,
	mounts = case
		when x.mode <> 'fixed' then c.mounts
		when x.agent_version is null then c.mounts - 'agent_version'
		else jsonb_set(c.mounts, '{agent_version}', to_jsonb(x.agent_version))
	end
from channel_execution_contexts x
where c.uuid = x.channel_uuid;

-- +goose StatementBegin
do $$
begin
	if exists (
		select 1 from channel_execution_contexts x join channels c on c.uuid = x.channel_uuid
		where (c.identity_resolution_mode, c.identity_uuid, c.execution_revision, c.connection_status, c.entry_closed)
			is distinct from (x.mode, x.identity_uuid, x.revision, x.connection_status, x.entry_closed)
		or (x.mode = 'fixed' and (c.mounts ->> 'agent_version') is distinct from x.agent_version::text)
	) then
		raise exception '渠道执行上下文回填校验失败';
	end if;
end $$;
-- +goose StatementEnd

create index channels_fixed_identity_v1_idx on channels (workspace_uuid, identity_uuid)
	where deleted_at is null and identity_resolution_mode = 'fixed' and entry_closed = false;
drop table channel_execution_contexts;

alter table channel_pairings rename to channel_access_pairings;
alter table channel_scope_pairings rename to channel_pairings;

-- +goose Down
alter table channel_pairings rename to channel_scope_pairings;
alter table channel_access_pairings rename to channel_pairings;

create table channel_execution_contexts (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	channel_uuid uuid not null,
	channel_external_id text not null,
	mode text not null,
	identity_uuid uuid,
	agent_version integer,
	revision bigint not null default 1,
	connection_status text not null default 'unknown',
	entry_closed boolean not null default false,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint channel_execution_contexts_id_pk primary key (id),
	constraint channel_execution_contexts_uuid_key unique (uuid),
	constraint channel_execution_contexts_channel_key unique (channel_uuid),
	constraint channel_execution_contexts_mode_check check (mode in ('fixed', 'pairing')),
	constraint channel_execution_contexts_connection_check check (connection_status in ('unknown', 'healthy', 'disconnected'))
);

insert into channel_execution_contexts (
	organization_uuid, workspace_uuid, channel_uuid, channel_external_id, mode,
	identity_uuid, agent_version, revision, connection_status, entry_closed, created_at, updated_at
)
select organization_uuid, workspace_uuid, uuid, external_id, identity_resolution_mode,
	identity_uuid, (mounts ->> 'agent_version')::integer, execution_revision, connection_status, entry_closed, created_at, updated_at
from channels where identity_resolution_mode is not null;

drop index channels_fixed_identity_v1_idx;
alter table channels
	drop column identity_resolution_mode,
	drop column identity_uuid,
	drop column execution_revision,
	drop column connection_status,
	drop column entry_closed;
